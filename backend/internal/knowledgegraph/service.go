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

// PRFailures reads normalized blocking evidence for a pull request.
type PRFailures interface {
	ForPR(ctx context.Context, prURL string) (PRFailureEvidence, error)
}

// ChangedPaths reads committed paths from the worker's local workspace.
type ChangedPaths interface {
	ChangedPaths(ctx context.Context, sess domain.Session, target string) ([]string, error)
}

// PRFailureEvidence contains stored PR facts used in task context.
type PRFailureEvidence struct {
	Checks   []FailedCheck
	Comments []UnresolvedComment
}

// FailedCheck describes one failed check and its stored log tail.
type FailedCheck struct {
	Name    string
	LogTail string
}

// UnresolvedComment describes an open human review comment.
type UnresolvedComment struct {
	Author string
	File   string
	Body   string
}

// Deps configures a Service.
type Deps struct {
	Graph *Graph
	// Embedder enables semantic search; nil keeps full-text search.
	Embedder Embedder
	MinScore float64
	// MergeScore is the vector score at which a new lesson merges into a
	// stored one; Load always sets it when embeddings are configured.
	MergeScore float64
	Sessions   Sessions
	Tasks      TaskReports
	Failures   PRFailures
	Paths      ChangedPaths
	HostID     string
	Logger     *slog.Logger
	Now        func() time.Time
	NewID      func() string
}

// Service is the daemon's view of the knowledge graph: it records task
// outcomes in the background and answers lesson and context queries.
type Service struct {
	graph    *Graph
	sessions Sessions
	tasks    TaskReports
	failures PRFailures
	paths    ChangedPaths
	hostID   string
	log      *slog.Logger
	now      func() time.Time
	newID    func() string

	embedder   Embedder
	minScore   float64
	mergeScore float64
	// embedKick wakes the embed loop after a write.
	embedKick chan struct{}
	// embedDims is the model's vector size, probed by the embed loop.
	embedDims int

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
		graph: d.Graph, sessions: d.Sessions, tasks: d.Tasks, failures: d.Failures, paths: d.Paths, hostID: d.HostID,
		log: d.Logger, now: d.Now, newID: d.NewID,
		embedder: d.Embedder, minScore: d.MinScore, mergeScore: d.MergeScore, embedKick: make(chan struct{}, 1),
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

// AddedLesson identifies the stored lesson; Merged means an existing
// near-identical lesson absorbed the new one.
type AddedLesson struct {
	ID     string
	Merged bool
}

// AddLesson validates in and stores it against the worker's task, merging
// it into a near-identical lesson when embeddings are configured.
func (s *Service) AddLesson(ctx context.Context, in LessonInput) (AddedLesson, error) {
	lesson, err := s.validLesson(ctx, in)
	if err != nil {
		return AddedLesson{}, err
	}
	vector := s.embedLesson(ctx, lesson.Text)
	if id, ok := s.mergeDuplicate(ctx, lesson, vector); ok {
		return AddedLesson{ID: id, Merged: true}, nil
	}
	if err := s.graph.AddLesson(ctx, lesson); err != nil {
		return AddedLesson{}, unavailable(err)
	}
	s.storeEmbedding(ctx, lesson.ID, vector)
	return AddedLesson{ID: lesson.ID}, nil
}

func (s *Service) validLesson(ctx context.Context, in LessonInput) (Lesson, error) {
	text := strings.TrimSpace(domain.SanitizeControlChars(in.Text))
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind == "" {
		kind = "tip"
	}
	switch {
	case in.SessionID == "":
		return Lesson{}, apierr.Invalid("SESSION_ID_REQUIRED", "sessionId is required", nil)
	case text == "":
		return Lesson{}, apierr.Invalid("LESSON_TEXT_REQUIRED", "lesson text is required", nil)
	case utf8.RuneCountInString(text) > maxLessonText:
		return Lesson{}, apierr.Invalid("LESSON_TEXT_TOO_LONG", "lesson text must be at most 1000 characters", nil)
	case !lessonKinds[kind]:
		return Lesson{}, apierr.Invalid("INVALID_LESSON_KIND", "kind must be pitfall, fix, tip, or context", nil)
	}
	topics, err := normalizeTopics(in.Topics)
	if err != nil {
		return Lesson{}, err
	}
	sess, err := s.sessions.Get(ctx, in.SessionID)
	if err != nil {
		return Lesson{}, err
	}
	return Lesson{
		ID: s.newID(), TaskKey: s.taskKey(sess.ID), Session: string(sess.ID), Host: s.hostID,
		Project: string(sess.ProjectID), Kind: kind, Text: text, Topics: topics, At: s.now(),
	}, nil
}

// embedLesson returns nil without an embedder or when embedding fails; the
// lesson is then stored unmerged and the embed loop backfills its vector.
func (s *Service) embedLesson(ctx context.Context, text string) []float64 {
	if s.embedder == nil {
		return nil
	}
	vecs, err := s.embedder.EmbedDocuments(ctx, []string{text})
	if err != nil {
		s.log.Warn("knowledge graph: lesson embedding failed, storing without dedupe", "err", err)
		return nil
	}
	return vecs[0]
}

// mergeDuplicate confirms the closest stored lesson that makes the same
// claim. Lookup failures leave the lesson to be stored as new.
func (s *Service) mergeDuplicate(ctx context.Context, l Lesson, vector []float64) (string, bool) {
	if vector == nil || s.mergeScore <= 0 {
		return "", false
	}
	matches, err := s.graph.SimilarLessons(ctx, l.Project, l.Kind, vector, s.mergeScore)
	if err != nil {
		s.log.Warn("knowledge graph: duplicate lesson lookup failed", "err", err)
		return "", false
	}
	for _, m := range matches {
		if !sameClaim(m.Text, l.Text) {
			continue
		}
		found, err := s.graph.ConfirmLesson(ctx, m.ID, l)
		if err != nil {
			s.log.Warn("knowledge graph: confirm lesson failed", "lesson", m.ID, "err", err)
			return "", false
		}
		if found {
			return m.ID, true
		}
	}
	return "", false
}

// storeEmbedding saves the vector the dedupe lookup already computed, or
// leaves the lesson to the embed loop.
func (s *Service) storeEmbedding(ctx context.Context, id string, vector []float64) {
	if vector != nil {
		err := s.graph.SetEmbeddings(ctx, []EmbedTarget{{Label: "Lesson", ID: id}}, [][]float64{vector})
		if err == nil {
			return
		}
		s.log.Warn("knowledge graph: store lesson embedding failed", "lesson", id, "err", err)
	}
	s.kickEmbed()
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
	items, err := s.prior(ctx, Query{Project: string(project), Text: in.Text, ExcludeTask: exclude, Limit: in.Limit})
	if err != nil {
		return nil, unavailable(err)
	}
	return items, nil
}

// PriorContext renders the spawn-time section for a new worker's task and
// the lesson ids it carries. Errors leave the spawn to proceed without it.
func (s *Service) PriorContext(ctx context.Context, project domain.ProjectID, issue domain.IssueID, text string) (string, []string, error) {
	items, err := s.prior(ctx, Query{Project: string(project), Issue: string(issue), Text: text})
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

// prior searches by meaning when an embedder is configured. An embedding
// failure falls back to full-text search instead of failing the lookup.
func (s *Service) prior(ctx context.Context, q Query) ([]Item, error) {
	if s.embedder != nil && SearchTerms(q.Text) != "" {
		vec, err := s.embedder.EmbedQuery(ctx, q.Text)
		if err == nil {
			q.Vector, q.MinScore = vec, s.minScore
		} else {
			s.log.Warn("knowledge graph: query embedding failed, using full-text search", "err", err)
		}
	}
	return s.graph.Prior(ctx, q)
}

func unavailable(err error) error {
	return apierr.Unavailable("KNOWLEDGE_GRAPH_UNAVAILABLE", "The knowledge graph could not be reached: "+err.Error())
}
