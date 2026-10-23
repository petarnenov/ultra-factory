package consultations

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/knowledgegraph"
)

// Store uses the existing graph writers and read-only query credentials.
type Store struct {
	Write  knowledgegraph.Runner
	Read   knowledgegraph.Runner
	mu     sync.RWMutex
	status ScanStatus
}

func (s *Store) Status() ScanStatus          { s.mu.RLock(); defer s.mu.RUnlock(); return s.status }
func (s *Store) setStatus(status ScanStatus) { s.mu.Lock(); defer s.mu.Unlock(); s.status = status }

// Schema is repeatable and uses the daemon's graph writer credentials.
func (s *Store) Schema(ctx context.Context) error {
	for _, q := range []string{
		"CREATE CONSTRAINT consultation_key IF NOT EXISTS FOR (c:ArchitectureConsultation) REQUIRE c.key IS UNIQUE",
		"CREATE CONSTRAINT consultation_artifact_key IF NOT EXISTS FOR (a:ConsultationArtifact) REQUIRE a.key IS UNIQUE",
		"CREATE INDEX consultation_date IF NOT EXISTS FOR (c:ArchitectureConsultation) ON (c.date)",
		"CREATE INDEX consultation_host IF NOT EXISTS FOR (c:ArchitectureConsultation) ON (c.host)",
	} {
		if _, err := s.Write.Run(ctx, q, nil); err != nil {
			return err
		}
	}
	return nil
}

const upsert = `
MERGE (c:ArchitectureConsultation {key: $key})
SET c.summary = $summary, c.date = $date, c.host = $host, c.syncedAt = $at,
    c.warnings = $warnings
WITH c
UNWIND $artifacts AS item
MERGE (a:ConsultationArtifact {key: $key + '/' + item.name})
MERGE (c)-[:HAS_ARTIFACT]->(a)
SET a.name = item.name
FOREACH (_ IN CASE WHEN item.missing AND a.observed = true THEN [1] ELSE [] END |
 SET a.absentSince = coalesce(a.absentSince, $at))
FOREACH (_ IN CASE WHEN (NOT item.missing AND (a.content <> item.content OR a.absentSince IS NOT NULL)) OR a.content IS NULL THEN [1] ELSE [] END |
 SET a.content = item.content, a.absentSince = null)
FOREACH (_ IN CASE WHEN NOT item.missing THEN [1] ELSE [] END |
 SET a.observed = true)
RETURN count(a) AS n`

func (s *Store) Import(ctx context.Context, d Detail) error {
	key, err := DecodeID(d.ID)
	if err != nil {
		return err
	}
	summary, err := json.Marshal(d.Summary)
	if err != nil {
		return err
	}
	observed := map[string]Artifact{}
	for _, a := range d.Artifacts {
		observed[a.Name] = a
	}
	items := make([]map[string]any, 0, len(artifactLayout()))
	for _, a := range artifactLayout() {
		if stored, ok := observed[a.Name]; ok {
			a = stored
		} else {
			a.Missing = true
			a.Ballots = []Ballot{}
		}
		raw, e := json.Marshal(a)
		if e != nil {
			return e
		}
		items = append(items, map[string]any{"name": a.Name, "missing": a.Missing, "content": string(raw)})
	}
	_, err = s.Write.Run(ctx, upsert, map[string]any{"key": key, "summary": string(summary), "date": d.Date, "host": d.Host, "at": d.SyncedAt, "warnings": d.Warnings, "artifacts": items})
	return err
}

func (s *Store) List(ctx context.Context, limit int, cursor string) (Page, error) {
	page := Page{Items: []Summary{}, Scan: s.Status()}
	if limit < 1 || limit > 50 {
		return page, ErrInvalid
	}
	after := ""
	if cursor != "" {
		var err error
		after, err = DecodeID(cursor)
		if err != nil {
			return page, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.Read.Run(ctx, `MATCH (c:ArchitectureConsultation)
WHERE $after = '' OR c.date < $date OR (c.date = $date AND c.key < $after)
RETURN c.key AS key, c.summary AS summary ORDER BY c.date DESC, c.key DESC LIMIT $limit`, map[string]any{"after": after, "date": strings.Split(after+"//", "/")[1], "limit": limit + 1})
	if err != nil {
		return page, err
	}
	for i, row := range rows {
		if i == limit {
			page.NextCursor = page.Items[len(page.Items)-1].ID
			break
		}
		var summary Summary
		if err := json.Unmarshal([]byte(row.String("summary")), &summary); err != nil {
			return page, err
		}
		page.Items = append(page.Items, summary)
	}
	return page, nil
}

func (s *Store) Detail(ctx context.Context, id string) (Detail, error) {
	key, err := DecodeID(id)
	if err != nil {
		return Detail{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.Read.Run(ctx, `MATCH (c:ArchitectureConsultation {key: $key})
OPTIONAL MATCH (c)-[:HAS_ARTIFACT]->(a:ConsultationArtifact)
RETURN c.summary AS summary, c.warnings AS warnings, a.content AS artifact,
       a.absentSince AS absentSince, a.observed AS observed ORDER BY a.name LIMIT 32`, map[string]any{"key": key})
	if err != nil {
		return Detail{}, err
	}
	if len(rows) == 0 {
		return Detail{}, ErrMissing
	}
	d := Detail{Artifacts: []Artifact{}, Warnings: rows[0].Strings("warnings")}
	if err := json.Unmarshal([]byte(rows[0].String("summary")), &d.Summary); err != nil {
		return d, err
	}
	byName := map[string]Artifact{}
	for _, row := range rows {
		if row.String("artifact") == "" {
			continue
		}
		var a Artifact
		if err := json.Unmarshal([]byte(row.String("artifact")), &a); err != nil {
			return d, err
		}

		a.AbsentSince = row.String("absentSince")
		if a.AbsentSince != "" {
			a.Missing = true
			d.Partial = true
		}
		byName[a.Name] = a
	}

	rounds := map[int]bool{}
	for _, a := range byName {
		if a.Round > 0 && (!a.Missing || a.AbsentSince != "") {
			rounds[a.Round] = true
		}
	}
	for _, a := range artifactLayout() {
		if stored, ok := byName[a.Name]; ok && (a.Round == 0 || rounds[a.Round]) {
			d.Artifacts = append(d.Artifacts, stored)
		}
	}
	d.Rounds = len(rounds)
	d.PhaseRounds = d.Rounds
	for _, phase := range []string{"independent", "critique"} {
		for _, a := range d.Artifacts {
			if a.Phase == phase && (!a.Missing || a.AbsentSince != "") {
				d.PhaseRounds++
				break
			}
		}
	}
	return d, nil
}
