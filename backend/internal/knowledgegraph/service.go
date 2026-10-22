package knowledgegraph

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/service/taskreport"
)

// Sessions reads sessions on this daemon.
type Sessions interface {
	Get(ctx context.Context, id domain.SessionID) (domain.Session, error)
}

// TaskReports classifies a session's task outcome.
type TaskReports interface {
	Get(ctx context.Context, projectID domain.ProjectID, id domain.SessionID) (taskreport.Report, error)
}

// Deps configures a Service.
type Deps struct {
	Graph    *Graph
	Sessions Sessions
	Tasks    TaskReports
	HostID   string
	Logger   *slog.Logger
	Now      func() time.Time
	NewID    func() string
}

// Service is the daemon's view of the knowledge graph: it records task
// outcomes in the background and answers lesson and context queries.
type Service struct {
	graph    *Graph
	sessions Sessions
	tasks    TaskReports
	hostID   string
	log      *slog.Logger
	now      func() time.Time
	newID    func() string

	queue chan job
	mu    sync.Mutex
	// recorded maps a session to the last outcome written, so repeated
	// CDC updates of a terminated session write once.
	recorded map[domain.SessionID]string
}

type job struct {
	session domain.SessionID
	report  string
}

const queueSize = 256

// New builds a Service. Call Start to run the recorder.
func New(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.NewID == nil {
		d.NewID = uuid.NewString
	}
	return &Service{
		graph: d.Graph, sessions: d.Sessions, tasks: d.Tasks, hostID: d.HostID,
		log: d.Logger, now: d.Now, newID: d.NewID,
		queue: make(chan job, queueSize), recorded: map[domain.SessionID]string{},
	}
}

func (s *Service) taskKey(id domain.SessionID) string {
	return s.hostID + "/" + string(id)
}

// Lesson kinds a worker may record.
var lessonKinds = map[string]bool{"pitfall": true, "fix": true, "tip": true, "context": true}

const (
	maxLessonText  = 1000
	maxTopics      = 5
	maxTopicLength = 40
)

// LessonInput is a worker's request to record a lesson.
type LessonInput struct {
	SessionID domain.SessionID
	Kind      string
	Text      string
	Topics    []string
}

// AddLesson validates in and stores it against the worker's task.
func (s *Service) AddLesson(ctx context.Context, in LessonInput) (string, error) {
	text := strings.TrimSpace(domain.SanitizeControlChars(in.Text))
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind == "" {
		kind = "tip"
	}
	switch {
	case in.SessionID == "":
		return "", apierr.Invalid("SESSION_ID_REQUIRED", "sessionId is required", nil)
	case text == "":
		return "", apierr.Invalid("LESSON_TEXT_REQUIRED", "lesson text is required", nil)
	case utf8.RuneCountInString(text) > maxLessonText:
		return "", apierr.Invalid("LESSON_TEXT_TOO_LONG", "lesson text must be at most 1000 characters", nil)
	case !lessonKinds[kind]:
		return "", apierr.Invalid("INVALID_LESSON_KIND", "kind must be pitfall, fix, tip, or context", nil)
	}
	topics, err := normalizeTopics(in.Topics)
	if err != nil {
		return "", err
	}
	sess, err := s.sessions.Get(ctx, in.SessionID)
	if err != nil {
		return "", err
	}
	id := s.newID()
	err = s.graph.AddLesson(ctx, Lesson{
		ID: id, TaskKey: s.taskKey(sess.ID), Session: string(sess.ID), Host: s.hostID,
		Project: string(sess.ProjectID), Kind: kind, Text: text, Topics: topics, At: s.now(),
	})
	if err != nil {
		return "", unavailable(err)
	}
	return id, nil
}

func normalizeTopics(raw []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range raw {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		if utf8.RuneCountInString(t) > maxTopicLength {
			return nil, apierr.Invalid("INVALID_TOPIC", "topics must be at most 40 characters", nil)
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > maxTopics {
		return nil, apierr.Invalid("TOO_MANY_TOPICS", "at most 5 topics", nil)
	}
	return out, nil
}

// SearchInput scopes a search to the asking session's project or an
// explicit one.
type SearchInput struct {
	SessionID domain.SessionID
	ProjectID domain.ProjectID
	Text      string
	Limit     int
}

// Search returns lessons and similar tasks for free text.
func (s *Service) Search(ctx context.Context, in SearchInput) ([]Item, error) {
	if strings.TrimSpace(in.Text) == "" {
		return nil, apierr.Invalid("QUERY_REQUIRED", "q is required", nil)
	}
	if in.Limit < 0 || in.Limit > 50 {
		return nil, apierr.Invalid("INVALID_LIMIT", "limit must be between 1 and 50", nil)
	}
	project, exclude := in.ProjectID, ""
	if in.SessionID != "" {
		sess, err := s.sessions.Get(ctx, in.SessionID)
		if err != nil {
			return nil, err
		}
		project, exclude = sess.ProjectID, s.taskKey(sess.ID)
	}
	if project == "" {
		return nil, apierr.Invalid("PROJECT_REQUIRED", "projectId or sessionId is required", nil)
	}
	if SearchTerms(in.Text) == "" {
		return []Item{}, nil
	}
	items, err := s.graph.Prior(ctx, Query{Project: string(project), Text: in.Text, ExcludeTask: exclude, Limit: in.Limit})
	if err != nil {
		return nil, unavailable(err)
	}
	return items, nil
}

// PriorContext renders the spawn-time section for a new worker's task and
// the lesson ids it carries. Errors leave the spawn to proceed without it.
func (s *Service) PriorContext(ctx context.Context, project domain.ProjectID, issue domain.IssueID, text string) (string, []string, error) {
	items, err := s.graph.Prior(ctx, Query{Project: string(project), Issue: string(issue), Text: text})
	if err != nil {
		return "", nil, err
	}
	var ids []string
	for _, it := range items {
		if it.Section == "lesson" && it.ID != "" {
			ids = append(ids, it.ID)
		}
	}
	return FormatSection(items), ids, nil
}

// MarkServed records which lessons a spawned worker received.
func (s *Service) MarkServed(ctx context.Context, id domain.SessionID, lessonIDs []string) error {
	return s.graph.MarkServed(ctx, s.taskKey(id), lessonIDs, s.now())
}

func unavailable(err error) error {
	return apierr.Unavailable("KNOWLEDGE_GRAPH_UNAVAILABLE", "The knowledge graph could not be reached: "+err.Error())
}
