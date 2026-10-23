package consultations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func archive(t *testing.T) (string, *os.Root) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "2026-10-23", "case"), 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return dir, root
}
func put(t *testing.T, dir, name, text string) {
	t.Helper()
	path := filepath.Join(dir, "2026-10-23", "case", name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestArchiveEvidence(t *testing.T) {
	dir, root := archive(t)
	put(t, dir, "state.json", `{"requester":"Engineer","case":"Design","host":"recorded","phase":"arbitration","decision_status":"resolved","archive_status":"verified","delivery_status":"unconfirmed","closure_status":"not_started","escalation_status":"timed_out","mode":"d03_fallback","session_id":"secret","path":"/secret","caller":"secret"}`)
	for _, a := range artifactLayout() {
		if a.Round == 2 {
			continue
		}
		text := "Evidence"
		if a.Phase == "ballot" {
			text = "Proposal: proposal-1.md\nD01: APPROVE\nD03: REJECT"
		}
		put(t, dir, a.Name, text)
	}
	d, err := ReadArchive(context.Background(), root, "host", "2026-10-23", "case", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if d.Partial || d.Rounds != 1 || d.PhaseRounds != 3 || d.Mode != "d03_fallback" || d.Requester != "Engineer" {
		t.Fatalf("bad summary %+v", d.Summary)
	}
	for _, a := range d.Artifacts {
		if a.Phase == "ballot" && len(a.Ballots) != 2 {
			t.Fatal(a)
		}
	}
	other, _ := ReadArchive(context.Background(), root, "other", "2026-10-23", "case", time.Now())
	if d.ID == other.ID {
		t.Fatal("host collision")
	}
}
func TestArchivePartialUnsafeAndTruncation(t *testing.T) {
	dir, root := archive(t)
	put(t, dir, "state.json", `{"requester":"/secret/path","unknown":"secret","decision_status":{}}`)
	text := strings.Repeat("x", ContentCap+100)
	put(t, dir, "final.md", text)
	put(t, dir, "proposal-2.md", "second")
	put(t, dir, "logs/secret.log", "secret")
	if err := os.Symlink(filepath.Join(dir, "2026-10-23/case/final.md"), filepath.Join(dir, "2026-10-23/case/request.md")); err != nil {
		t.Fatal(err)
	}
	d, err := ReadArchive(context.Background(), root, "h", "2026-10-23", "case", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !d.Partial || d.Requester != "" || d.Rounds != 1 || len(d.Warnings) == 0 {
		t.Fatalf("bad partial %+v", d)
	}
	sum := sha256.Sum256([]byte(text))
	for _, a := range d.Artifacts {
		if strings.Contains(a.Name, "logs") {
			t.Fatal("log imported")
		}
		if a.Name == "final.md" && (!a.Truncated || len(a.Content) != ContentCap || a.Hash != hex.EncodeToString(sum[:])) {
			t.Fatal("bad truncation")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := readArtifact(ctx, root, "2026-10-23/case/final.md", 10); err == nil {
		t.Fatal("ignored cancel")
	}
	for _, manifest := range []string{"{", strings.Repeat("x", 65537)} {
		put(t, dir, "state.json", manifest)
		d, _ := ReadArchive(context.Background(), root, "h", "2026-10-23", "case", time.Now())
		if len(d.Warnings) == 0 {
			t.Fatal("missing manifest warning")
		}
	}
	for _, date := range []string{"2026-02-30", "../secret"} {
		if _, err := ReadArchive(context.Background(), root, "h", date, "case", time.Now()); err == nil {
			t.Fatal("accepted invalid date")
		}
	}
}
func TestStrictBallots(t *testing.T) {
	for _, text := range []string{"D01: APPROVE", "Proposal: proposal-2.md\nD01: APPROVE", "Proposal: proposal-1.md\nD01: APPROVE\nD01: REJECT", "Proposal: proposal-1.md\nD01: maybe APPROVE", "Proposal: proposal-1.md\nD01: APPROVE\nReason: yes"} {
		if len(ParseBallot(text, 1)) != 0 {
			t.Fatalf("accepted ambiguous ballot %q", text)
		}
	}
	if len(ParseBallot("Proposal: proposal-1.md\nD01: APPROVE", 1)) != 1 {
		t.Fatal("valid ballot rejected")
	}
}
func TestIDs(t *testing.T) {
	key := "host/2026-10-23/case"
	if got, err := DecodeID(EncodeID(key)); err != nil || got != key {
		t.Fatal(got, err)
	}
	for _, id := range []string{"!", EncodeID("../bad"), strings.Repeat("x", 701), EncodeID("h/2026-10-23/..")} {
		if _, err := DecodeID(id); err == nil {
			t.Fatalf("accepted %s", id)
		}
	}
}

func TestContentBudgetAndInvalidUTF8(t *testing.T) {
	dir, root := archive(t)
	for _, a := range artifactLayout() {
		put(t, dir, a.Name, strings.Repeat("\x01", ContentCap))
	}
	put(t, dir, "request.md", "prefix\xffinvalid")
	d, err := ReadArchive(context.Background(), root, "host", "2026-10-23", "case", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if d.Artifacts[0].Content != "prefix" || !d.Artifacts[0].Truncated {
		t.Fatal("invalid UTF-8 was not bounded")
	}
	total := 0
	for _, a := range d.Artifacts {
		raw, _ := json.Marshal(a.Content)
		total += len(raw)
	}
	if total > TotalContentCap+64 {
		t.Fatalf("encoded content exceeded budget: %d", total)
	}
}
