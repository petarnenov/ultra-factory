package knowledgegraph

import (
	"context"
	"strings"
	"time"
	"unicode"
)

// Runner executes Cypher; *Client implements it.
type Runner interface {
	Run(ctx context.Context, statement string, params map[string]any) ([]Row, error)
}

// Graph holds the domain queries over a Runner.
type Graph struct {
	run Runner
}

// NewGraph wraps r.
func NewGraph(r Runner) *Graph { return &Graph{run: r} }

// TaskFact is what one host knows about one worker task.
type TaskFact struct {
	// Key is "<hostId>/<sessionId>", unique across hosts.
	Key            string
	SessionID      string
	Host           string
	Project        string
	Title          string
	Issue          string
	Outcome        string
	Harness        string
	Model          string
	Effort         string
	PRs            []string
	ElapsedSeconds int64
	InputTokens    int64
	OutputTokens   int64
	Stages         string
	// Report is "<state>: <text>" of a new worker report, or "".
	Report string
	At     time.Time
}

// Outcomes that feed lesson usefulness: a lesson served to a task that
// later merged helped; one served to a closed or failed task did not.
const recordTaskCypher = `
MERGE (t:Task {id: $key})
ON CREATE SET t.createdAt = datetime($at)
SET t.sessionId = $sessionId, t.host = $host, t.project = $project,
    t.updatedAt = datetime($at)
SET t += $props
FOREACH (r IN CASE WHEN $report = '' THEN [] ELSE [$report] END |
  SET t.summary = r, t.reports = coalesce(t.reports, []) + [r])
MERGE (p:Project {id: $project})
MERGE (t)-[:IN_PROJECT]->(p)
FOREACH (_ IN CASE WHEN $issueKey = '' THEN [] ELSE [1] END |
  MERGE (i:Issue {key: $issueKey})
  ON CREATE SET i.project = $project, i.issue = $issue
  MERGE (t)-[:FOR_ISSUE]->(i))
WITH t
OPTIONAL MATCH (t)-[r:RECEIVED]->(l:Lesson)
WHERE r.scored IS NULL AND t.outcome IN ['merged', 'closed', 'failed']
FOREACH (_ IN CASE WHEN r IS NULL THEN [] ELSE [1] END |
  SET r.scored = t.outcome,
      l.helped = coalesce(l.helped, 0) + CASE t.outcome WHEN 'merged' THEN 1 ELSE 0 END,
      l.hurt = coalesce(l.hurt, 0) + CASE t.outcome WHEN 'merged' THEN 0 ELSE 1 END)
RETURN count(t) AS n`

// RecordTask upserts a task and scores the lessons it was served.
func (g *Graph) RecordTask(ctx context.Context, f TaskFact) error {
	props := map[string]any{}
	setIf := func(k, v string) {
		if v != "" {
			props[k] = v
		}
	}
	setIf("title", f.Title)
	setIf("issue", f.Issue)
	setIf("outcome", f.Outcome)
	setIf("harness", f.Harness)
	setIf("model", f.Model)
	setIf("effort", f.Effort)
	setIf("stages", f.Stages)
	if len(f.PRs) > 0 {
		props["prs"] = f.PRs
	}
	if f.ElapsedSeconds > 0 {
		props["elapsedSeconds"] = f.ElapsedSeconds
	}
	if f.InputTokens > 0 {
		props["inputTokens"] = f.InputTokens
	}
	if f.OutputTokens > 0 {
		props["outputTokens"] = f.OutputTokens
	}
	_, err := g.run.Run(ctx, recordTaskCypher, map[string]any{
		"key": f.Key, "sessionId": f.SessionID, "host": f.Host, "project": f.Project,
		"issue": f.Issue, "issueKey": issueKey(f.Project, f.Issue), "props": props,
		"report": f.Report, "at": f.At.UTC().Format(time.RFC3339Nano),
	})
	return err
}

// Lesson is a reusable finding a worker recorded.
type Lesson struct {
	ID      string
	TaskKey string
	Session string
	Host    string
	Project string
	Kind    string
	Text    string
	Topics  []string
	At      time.Time
}

const addLessonCypher = `
MERGE (t:Task {id: $taskKey})
ON CREATE SET t.createdAt = datetime($at), t.sessionId = $sessionId,
              t.host = $host, t.project = $project
CREATE (l:Lesson {id: $id, kind: $kind, text: $text, project: $project,
                  createdAt: datetime($at), helped: 0, hurt: 0})
MERGE (t)-[:LEARNED]->(l)
FOREACH (name IN $topics |
  MERGE (tp:Topic {name: name})
  MERGE (l)-[:ABOUT]->(tp))
RETURN l.id AS id`

// AddLesson stores l and links it to its task and topics.
func (g *Graph) AddLesson(ctx context.Context, l Lesson) error {
	topics := l.Topics
	if topics == nil {
		topics = []string{}
	}
	_, err := g.run.Run(ctx, addLessonCypher, map[string]any{
		"taskKey": l.TaskKey, "sessionId": l.Session, "host": l.Host,
		"project": l.Project, "id": l.ID, "kind": l.Kind, "text": l.Text,
		"topics": topics, "at": l.At.UTC().Format(time.RFC3339Nano),
	})
	return err
}

// MarkServed links a task to the lessons its prompt carried, so the task's
// outcome can later score them.
func (g *Graph) MarkServed(ctx context.Context, taskKey string, lessonIDs []string, at time.Time) error {
	if len(lessonIDs) == 0 {
		return nil
	}
	_, err := g.run.Run(ctx, `
MERGE (t:Task {id: $taskKey})
ON CREATE SET t.createdAt = datetime($at)
WITH t
MATCH (l:Lesson) WHERE l.id IN $ids
MERGE (t)-[r:RECEIVED]->(l)
ON CREATE SET r.at = datetime($at)
RETURN count(r) AS n`, map[string]any{"taskKey": taskKey, "ids": lessonIDs, "at": at.UTC().Format(time.RFC3339Nano)})
	return err
}

// Query asks for prior context for a task.
type Query struct {
	Project string
	Issue   string
	Text    string
	// ExcludeTask keeps the asking task out of its own results.
	ExcludeTask string
	Limit       int
}

// Item is one piece of prior context.
type Item struct {
	// Section is "attempt", "lesson" or "task".
	Section string
	ID      string
	Kind    string
	Title   string
	Outcome string
	Text    string
	Lessons []string
	Score   float64
}

const (
	attemptsCypher = `
MATCH (i:Issue {key: $issueKey})<-[:FOR_ISSUE]-(t:Task)
WHERE t.id <> $exclude
OPTIONAL MATCH (t)-[:LEARNED]->(l:Lesson)
WITH t, collect(l.text)[..5] AS lessons
RETURN 'attempt' AS section, t.id AS id, '' AS kind, t.title AS title,
       coalesce(t.outcome, 'in_progress') AS outcome,
       coalesce(t.summary, '') AS text, lessons, 1.0 AS score
ORDER BY t.updatedAt DESC LIMIT $limit`
	lessonsCypher = `
CALL db.index.fulltext.queryNodes('lesson_text', $q) YIELD node AS l, score
WHERE l.project = $project
WITH l, score * (1.0 + coalesce(l.helped, 0)) / (1.0 + coalesce(l.hurt, 0)) AS rank
ORDER BY rank DESC LIMIT $limit
OPTIONAL MATCH (t:Task)-[:LEARNED]->(l)
RETURN 'lesson' AS section, l.id AS id, l.kind AS kind,
       coalesce(t.title, '') AS title, coalesce(t.outcome, '') AS outcome,
       l.text AS text, [] AS lessons, rank AS score`
	tasksCypher = `
CALL db.index.fulltext.queryNodes('task_text', $q) YIELD node AS t, score
WHERE t.project = $project AND t.outcome IS NOT NULL AND t.id <> $exclude
  AND ($issue = '' OR coalesce(t.issue, '') <> $issue)
RETURN 'task' AS section, t.id AS id, '' AS kind, t.title AS title,
       t.outcome AS outcome, coalesce(t.summary, '') AS text, [] AS lessons,
       score
ORDER BY score DESC LIMIT $limit`
)

// Prior returns earlier attempts on the issue, lessons and similar tasks.
func (g *Graph) Prior(ctx context.Context, q Query) ([]Item, error) {
	if q.Limit <= 0 {
		q.Limit = 5
	}
	terms := SearchTerms(q.Text)
	var parts []string
	if q.Issue != "" {
		parts = append(parts, attemptsCypher)
	}
	if terms != "" {
		parts = append(parts, lessonsCypher, tasksCypher)
	}
	if len(parts) == 0 {
		return nil, nil
	}
	// UNION ALL needs each ordered part in its own subquery.
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString("\nUNION ALL\n")
		}
		b.WriteString("CALL () {" + p + "\n}\nRETURN section, id, kind, title, outcome, text, lessons, score")
	}
	rows, err := g.run.Run(ctx, b.String(), map[string]any{
		"issueKey": issueKey(q.Project, q.Issue), "issue": q.Issue, "project": q.Project,
		"q": terms, "exclude": q.ExcludeTask, "limit": q.Limit,
	})
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(rows))
	for _, r := range rows {
		items = append(items, Item{
			Section: r.String("section"), ID: r.String("id"), Kind: r.String("kind"),
			Title: r.String("title"), Outcome: r.String("outcome"), Text: r.String("text"),
			Lessons: r.Strings("lessons"), Score: r.Float("score"),
		})
	}
	return items, nil
}

func issueKey(project, issue string) string {
	issue = strings.TrimPrefix(strings.TrimSpace(issue), "#")
	if project == "" || issue == "" {
		return ""
	}
	return project + "/" + issue
}

const maxSearchTerms = 24

// SearchTerms turns free text into a Lucene OR query of plain words, so no
// user text reaches the query parser as syntax.
func SearchTerms(text string) string {
	seen := map[string]bool{}
	var terms []string
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(word)) < 3 || stopWords[word] || seen[word] {
			continue
		}
		seen[word] = true
		terms = append(terms, word)
		if len(terms) == maxSearchTerms {
			break
		}
	}
	return strings.Join(terms, " OR ")
}

var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true, "this": true,
	"from": true, "into": true, "are": true, "was": true, "not": true, "but": true,
	"you": true, "your": true, "use": true, "when": true, "then": true, "than": true,
	"its": true, "has": true, "have": true, "will": true, "should": true, "must": true,
	"issue": true, "work": true, "task": true, "https": true, "http": true, "com": true,
	"github": true,
}
