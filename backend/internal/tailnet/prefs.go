package tailnet

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Prefs is the user's local view over discovered hosts: hidden ones and label
// overrides. It lives under the data dir, so sibling installs keep their own.
type Prefs struct {
	Hidden []string          `json:"hidden,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// PrefsPath returns the prefs file under dataDir.
func PrefsPath(dataDir string) string { return filepath.Join(dataDir, "tailnet", "hosts.json") }

// LoadPrefs reads path; a missing file is the zero Prefs.
func LoadPrefs(path string) (Prefs, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- AO-owned prefs file.
	if errors.Is(err, os.ErrNotExist) {
		return Prefs{}, nil
	}
	if err != nil {
		return Prefs{}, err
	}
	var p Prefs
	if err := json.Unmarshal(raw, &p); err != nil {
		return Prefs{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return p, nil
}

// SavePrefs writes p to path atomically.
func SavePrefs(path string, p Prefs) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (p Prefs) hidden(hostID string) bool {
	for _, id := range p.Hidden {
		if id == hostID {
			return true
		}
	}
	return false
}

func (p *Prefs) setHidden(hostID string, hidden bool) {
	var kept []string
	for _, id := range p.Hidden {
		if id != hostID {
			kept = append(kept, id)
		}
	}
	if hidden {
		kept = append(kept, hostID)
	}
	p.Hidden = kept
}

func (p *Prefs) setLabel(hostID, label string) {
	if label == "" {
		delete(p.Labels, hostID)
		if len(p.Labels) == 0 {
			p.Labels = nil
		}
		return
	}
	if p.Labels == nil {
		p.Labels = map[string]string{}
	}
	p.Labels[hostID] = label
}
