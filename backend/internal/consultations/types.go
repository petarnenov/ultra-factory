package consultations

import (
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"time"
)

const ContentCap = 256 << 10
const TotalContentCap = 1 << 20

var ErrInvalid = errors.New("invalid consultation identifier or cursor")
var ErrMissing = errors.New("consultation not found")
var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type ClosureEvidence struct {
	Consul string `json:"consul"`
	Status string `json:"status"`
}

type ArbitrationEvidence struct {
	Status string `json:"status"`
	Model  string `json:"model"`
	Effort string `json:"effort"`
	Host   string `json:"host"`
}

// Summary contains only recorded, allowlisted metadata.
type Summary struct {
	ID                 string              `json:"id"`
	Name               string              `json:"name"`
	Date               string              `json:"date"`
	Host               string              `json:"host"`
	RecordedHost       string              `json:"recordedHost"`
	Requester          string              `json:"requester"`
	Case               string              `json:"case"`
	Project            string              `json:"project"`
	Branch             string              `json:"branch"`
	SHA                string              `json:"sha"`
	PatchDigest        string              `json:"patchDigest"`
	VerifiedAt         string              `json:"verifiedAt"`
	EscalatedAt        string              `json:"escalatedAt"`
	EscalationDeadline string              `json:"escalationDeadline"`
	RespondedAt        string              `json:"respondedAt"`
	ConsulClosure      []ClosureEvidence   `json:"consulClosure"`
	Arbitration        ArbitrationEvidence `json:"arbitration"`
	Phase              string              `json:"phase"`
	Decision           string              `json:"decision"`
	Archive            string              `json:"archive"`
	Delivery           string              `json:"delivery"`
	Closure            string              `json:"closure"`
	Escalation         string              `json:"escalation"`
	Mode               string              `json:"mode"`
	CreatedAt          string              `json:"createdAt"`
	SyncedAt           string              `json:"syncedAt"`
	Rounds             int                 `json:"rounds"`
	ProposalVersions   int                 `json:"proposalVersions"`
	RatificationRounds []int               `json:"ratificationRounds"`
	PhaseRounds        int                 `json:"phaseRounds"`
	Partial            bool                `json:"partial"`
}

type Ballot struct {
	Decision string `json:"decision"`
	Stance   string `json:"stance"`
}

type Artifact struct {
	Name        string   `json:"name"`
	Phase       string   `json:"phase"`
	Consul      string   `json:"consul"`
	Round       int      `json:"round"`
	Content     string   `json:"content"`
	Hash        string   `json:"hash"`
	Truncated   bool     `json:"truncated"`
	Missing     bool     `json:"missing"`
	AbsentSince string   `json:"absentSince"`
	Ballots     []Ballot `json:"ballots"`
	Parsed      bool     `json:"parsed"`
}

type Detail struct {
	Summary
	Artifacts []Artifact `json:"artifacts"`
	Warnings  []string   `json:"warnings"`
}

type ScanStatus struct {
	Enabled    bool   `json:"enabled"`
	LastScan   string `json:"lastScan"`
	DurationMS int64  `json:"durationMs"`
	Examined   int    `json:"examined"`
	Skipped    int    `json:"skipped"`
	Imported   int    `json:"imported"`
	Error      string `json:"error"`
}

type Page struct {
	Items      []Summary  `json:"items"`
	NextCursor string     `json:"nextCursor"`
	Scan       ScanStatus `json:"scan"`
}

func EncodeID(key string) string { return base64.RawURLEncoding.EncodeToString([]byte(key)) }

func DecodeID(id string) (string, error) {
	if len(id) > 700 {
		return "", ErrInvalid
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(id)
	if err != nil || EncodeID(string(raw)) != id {
		return "", ErrInvalid
	}
	parts := strings.Split(string(raw), "/")
	if err != nil || len(parts) != 3 || !segment.MatchString(parts[0]) || !datePattern.MatchString(parts[1]) || !segment.MatchString(parts[2]) || parts[2] == "." || parts[2] == ".." {
		return "", ErrInvalid
	}
	if _, err := time.Parse("2006-01-02", parts[1]); err != nil {
		return "", ErrInvalid
	}
	return string(raw), nil
}
