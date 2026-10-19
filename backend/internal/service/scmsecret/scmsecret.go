// Package scmsecret stores per-machine, per-host SCM credentials under the AO
// data directory. Tokens leave only as in-memory session values.
package scmsecret

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ErrNotFound reports that no token is stored for the requested host and login.
var ErrNotFound = errors.New("scm secret: no token stored for this account")

// ErrInsecurePermissions reports a token file readable by another user on
// platforms where Go can represent those mode bits.
var ErrInsecurePermissions = errors.New("scm secret: token file permissions are too broad")

// Account identifies one stored GitHub host/login pair.
type Account struct {
	Host  string `json:"host"`
	Login string `json:"login"`
}

// Store is a machine-local GitHub token store rooted at $AO_DATA_DIR.
type Store struct {
	dataDir string
}

// New returns a store rooted at dataDir. It creates no files until a token is
// written.
func New(dataDir string) *Store {
	return &Store{dataDir: strings.TrimSpace(dataDir)}
}

func (s *Store) githubDir() string {
	return filepath.Join(s.dataDir, "secrets", "github")
}

func (s *Store) path(host, login string) (string, error) {
	if s == nil || s.dataDir == "" {
		return "", errors.New("scm secret: AO data directory is not configured")
	}
	normalizedHost, err := domain.NormalizeGitHubHost(host)
	if err != nil {
		return "", fmt.Errorf("scm secret: invalid GitHub host %q: %w", host, err)
	}
	if strings.TrimSpace(login) == "" {
		return "", errors.New("scm secret: login is required")
	}
	if err := domain.ValidateGitHubAccount(login); err != nil {
		return "", fmt.Errorf("scm secret: invalid GitHub login %q: %w", login, err)
	}
	// Both host and login are case-insensitive.
	return filepath.Join(s.githubDir(), normalizedHost, strings.ToLower(login)), nil
}

// GitHubToken returns the token stored for host/login on this machine.
func (s *Store) GitHubToken(_ context.Context, host, login string) (string, error) {
	path, err := s.path(host, login)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("scm secret: stat token file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("scm secret: token path is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%w: %s has mode %04o, want 0600", ErrInsecurePermissions, path, info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("scm secret: read token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", ErrNotFound
	}
	return token, nil
}

// SetGitHubToken stores token for host/login with owner-only permissions. The
// write is atomic so a crash cannot leave a partial token in place.
func (s *Store) SetGitHubToken(_ context.Context, host, login, token string) error {
	path, err := s.path(host, login)
	if err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("scm secret: token is required")
	}
	if strings.ContainsAny(token, "\r\n\x00") {
		return errors.New("scm secret: token must be a single line")
	}
	hostDir := filepath.Dir(path)
	if err := os.MkdirAll(hostDir, 0o700); err != nil {
		return fmt.Errorf("scm secret: create secret directory: %w", err)
	}
	if runtime.GOOS != "windows" {
		for _, dir := range []string{s.githubDir(), hostDir} {
			if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // G302: directories need 0700, not 0600.
				return fmt.Errorf("scm secret: secure secret directory: %w", err)
			}
		}
	}
	tmp, err := os.CreateTemp(hostDir, ".token-*")
	if err != nil {
		return fmt.Errorf("scm secret: create temp token file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("scm secret: secure temp token file: %w", err)
	}
	if _, err := tmp.WriteString(token + "\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("scm secret: write token file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("scm secret: sync token file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("scm secret: close token file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		// Windows cannot rename over an existing file.
		if runtime.GOOS == "windows" {
			_ = os.Remove(path)
			err = os.Rename(tmpName, path)
		}
		if err != nil {
			return fmt.Errorf("scm secret: install token file: %w", err)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("scm secret: secure token file: %w", err)
		}
	}
	return nil
}

// DeleteGitHubToken removes a stored token. A missing token is not an error.
func (s *Store) DeleteGitHubToken(_ context.Context, host, login string) error {
	path, err := s.path(host, login)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("scm secret: remove token file: %w", err)
	}
	return nil
}

// ListGitHubAccounts returns stored host/login pairs, never token values.
func (s *Store) ListGitHubAccounts(_ context.Context) ([]Account, error) {
	if s == nil || s.dataDir == "" {
		return nil, errors.New("scm secret: AO data directory is not configured")
	}
	hosts, err := os.ReadDir(s.githubDir())
	if errors.Is(err, os.ErrNotExist) {
		return []Account{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scm secret: list secret directory: %w", err)
	}
	accounts := make([]Account, 0, len(hosts))
	for _, hostEntry := range hosts {
		if !hostEntry.IsDir() || strings.HasPrefix(hostEntry.Name(), ".") {
			continue
		}
		host, err := domain.NormalizeGitHubHost(hostEntry.Name())
		if err != nil || host != hostEntry.Name() {
			continue
		}
		logins, err := os.ReadDir(filepath.Join(s.githubDir(), hostEntry.Name()))
		if err != nil {
			return nil, fmt.Errorf("scm secret: list host directory: %w", err)
		}
		for _, loginEntry := range logins {
			if loginEntry.IsDir() || strings.HasPrefix(loginEntry.Name(), ".") {
				continue
			}
			if domain.ValidateGitHubAccount(loginEntry.Name()) != nil {
				continue
			}
			accounts = append(accounts, Account{Host: host, Login: loginEntry.Name()})
		}
	}
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].Host != accounts[j].Host {
			return accounts[i].Host < accounts[j].Host
		}
		return accounts[i].Login < accounts[j].Login
	})
	return accounts, nil
}
