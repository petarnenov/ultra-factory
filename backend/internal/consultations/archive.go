package consultations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var consulNames = []string{"deepseek", "astra", "fable"}
var voteLine = regexp.MustCompile(`^(D[0-9]{2,}): (APPROVE|REJECT)$`)

// ParseBallot accepts a version line followed only by unique decision votes.
func ParseBallot(text string, round int) []Ballot {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) < 2 || lines[0] != "Proposal: proposal-"+strconv.Itoa(round)+".md" {
		return []Ballot{}
	}
	out := []Ballot{}
	seen := map[string]bool{}
	for _, line := range lines[1:] {
		match := voteLine.FindStringSubmatch(line)
		if match == nil || seen[match[1]] {
			return []Ballot{}
		}
		seen[match[1]] = true
		out = append(out, Ballot{Decision: match[1], Stance: match[2]})
	}
	return out
}

func artifactLayout() []Artifact {
	out := []Artifact{{Name: "request.md", Phase: "request"}}
	for _, phase := range []string{"independent", "critique"} {
		for _, name := range consulNames {
			out = append(out, Artifact{Name: phase + "/" + name + ".md", Phase: phase, Consul: name})
		}
	}
	for round := 1; round <= 2; round++ {
		out = append(out, Artifact{Name: fmt.Sprintf("proposal-%d.md", round), Phase: "proposal", Round: round})
		for _, name := range consulNames {
			out = append(out, Artifact{Name: fmt.Sprintf("ballots/round-%d/%s.md", round, name), Phase: "ballot", Round: round, Consul: name})
		}
	}
	out = append(out, Artifact{Name: "final.md", Phase: "final"})
	for _, name := range consulNames {
		out = append(out, Artifact{Name: name + ".md", Phase: "collected", Consul: name})
	}
	return out
}

func safeFile(root *os.Root, path string) (*os.File, error) {
	parts := strings.Split(path, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink rejected")
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, fmt.Errorf("nonregular artifact")
		}
	}
	f, err := openNonblocking(root, path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("nonregular artifact")
	}
	return f, nil
}

func readArtifact(ctx context.Context, root *os.Root, path string, capBytes int) (string, string, bool, error) {
	f, err := safeFile(root, path)
	if err != nil {
		return "", "", false, err
	}
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	prefix := make([]byte, 0, capBytes)
	buf := make([]byte, 32<<10)
	total := 0
	for {
		if err := ctx.Err(); err != nil {
			return "", "", false, err
		}
		n, e := f.Read(buf)
		total += n
		if total > 32<<20 {
			return "", "", false, fmt.Errorf("artifact exceeds hash budget")
		}
		_, _ = hash.Write(buf[:n])
		if len(prefix) < capBytes {
			prefix = append(prefix, buf[:min(n, capBytes-len(prefix))]...)
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", "", false, e
		}
	}

	for i := 0; i < len(prefix); {
		r, n := utf8.DecodeRune(prefix[i:])
		if r == utf8.RuneError && n == 1 {
			prefix = prefix[:i]
			break
		}
		i += n
	}
	return string(prefix), hex.EncodeToString(hash.Sum(nil)), total > len(prefix), nil
}

// ReadArchive reads exact known artifacts and never stores the raw manifest.
func ReadArchive(ctx context.Context, root *os.Root, host, date, name string, now time.Time) (Detail, error) {
	if !segment.MatchString(host) || !segment.MatchString(name) {
		return Detail{}, ErrInvalid
	}
	if parsed, err := time.Parse("2006-01-02", date); err != nil || parsed.Format("2006-01-02") != date {
		return Detail{}, ErrInvalid
	}
	base := date + "/" + name + "/"
	d := Detail{Summary: Summary{ID: EncodeID(host + "/" + date + "/" + name), Name: name, Date: date, Host: host, SyncedAt: now.UTC().Format(time.RFC3339Nano)}, Warnings: []string{}, Artifacts: []Artifact{}}
	raw, _, truncated, err := readArtifact(ctx, root, base+"state.json", 64<<10)
	if err != nil || truncated {
		d.Warnings = append(d.Warnings, "Manifest missing, unsafe or oversized")
	} else {
		var state map[string]json.RawMessage
		if json.Unmarshal([]byte(raw), &state) != nil {
			d.Warnings = append(d.Warnings, "Manifest is malformed")
		} else {
			applyState(&d.Summary, state)
		}
	}
	remaining := TotalContentCap
	observed := map[int]bool{}
	phases := map[string]bool{}
	for _, a := range artifactLayout() {
		a.Ballots = []Ballot{}
		text, digest, trunc, err := readArtifact(ctx, root, base+a.Name, min(ContentCap, remaining))
		if err != nil {
			a.Missing = true
			if !os.IsNotExist(err) {
				d.Warnings = append(d.Warnings, "Unsafe or unreadable artifact: "+a.Name)
			}
		} else {

			encoded, _ := json.Marshal(text)
			for len(encoded) > remaining {
				text = text[:len(text)*remaining/len(encoded)]
				for len(text) > 0 && !utf8.ValidString(text) {
					text = text[:len(text)-1]
				}
				trunc = true
				encoded, _ = json.Marshal(text)
				if text == "" {
					break
				}
			}
			a.Content = text
			a.Hash = digest
			a.Truncated = trunc
			remaining = max(0, remaining-len(encoded))
			if a.Round > 0 {
				observed[a.Round] = true
			}
			phases[a.Phase] = true
			if a.Phase == "ballot" && !trunc {
				a.Ballots = ParseBallot(text, a.Round)
			}
		}
		d.Artifacts = append(d.Artifacts, a)
	}
	d.Rounds = len(observed)
	for _, p := range []string{"independent", "critique"} {
		if phases[p] {
			d.PhaseRounds++
		}
	}
	d.PhaseRounds += d.Rounds
	filtered := d.Artifacts[:0]
	for _, a := range d.Artifacts {
		if a.Round > 0 && !observed[a.Round] {
			continue
		}
		if a.Missing || (a.Phase == "ballot" && len(a.Ballots) == 0) || a.Truncated {
			d.Partial = true
		}
		filtered = append(filtered, a)
	}
	d.Artifacts = filtered
	d.Partial = d.Partial || len(d.Warnings) > 0 || d.Requester == ""
	return d, nil
}

func applyState(s *Summary, state map[string]json.RawMessage) {
	for key, target := range map[string]*string{"requester": &s.Requester, "case": &s.Case, "host": &s.RecordedHost, "created_at": &s.CreatedAt} {
		var value string
		if json.Unmarshal(state[key], &value) == nil && len(value) <= 512 && !strings.ContainsAny(value, "/\\\x00\n\r") {
			*target = value
		}
	}
	for key, target := range map[string]*string{"phase": &s.Phase, "decision_status": &s.Decision, "archive_status": &s.Archive, "delivery_status": &s.Delivery, "closure_status": &s.Closure, "escalation_status": &s.Escalation, "mode": &s.Mode} {
		var value string
		if json.Unmarshal(state[key], &value) == nil && regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`).MatchString(value) {
			*target = value
		}
	}
}
