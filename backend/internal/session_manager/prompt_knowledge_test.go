package sessionmanager

import (
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestBuildTaskPromptKnowledgeSection(t *testing.T) {
	const knowledge = "## Knowledge Graph\n\nlesson body"
	tests := []struct {
		name string
		cfg  taskPromptConfig
		want string
	}{
		{
			name: "worker prompt gets section",
			cfg:  taskPromptConfig{Role: sessionPromptRoleWorker, Prompt: "Fix it.\n\n", Knowledge: "  " + knowledge + "\n"},
			want: "Fix it.\n\n" + knowledge,
		},
		{
			name: "worker issue prompt gets section",
			cfg:  taskPromptConfig{Role: sessionPromptRoleWorker, IssueID: "42", Knowledge: knowledge},
			want: buildBaseTaskPrompt(taskPromptConfig{Role: sessionPromptRoleWorker, IssueID: "42"}) + "\n\n" + knowledge,
		},
		{
			name: "empty base prompt stays empty",
			cfg:  taskPromptConfig{Role: sessionPromptRoleWorker, Knowledge: knowledge},
			want: "",
		},
		{
			name: "blank knowledge leaves prompt",
			cfg:  taskPromptConfig{Role: sessionPromptRoleWorker, Prompt: "Fix it.", Knowledge: " \n "},
			want: "Fix it.",
		},
		{
			name: "orchestrator never gets section",
			cfg:  taskPromptConfig{Role: sessionPromptRoleOrchestrator, Prompt: "Coordinate.", Knowledge: knowledge},
			want: "Coordinate.",
		},
		{
			name: "coordinator never gets section",
			cfg:  taskPromptConfig{Role: sessionPromptRoleCoordinator, Prompt: "Coordinate.", Knowledge: knowledge},
			want: "Coordinate.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildTaskPrompt(tt.cfg); got != tt.want {
				t.Fatalf("buildTaskPrompt() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildPromptPassesKnowledgeContext(t *testing.T) {
	got := buildPrompt(ports.SpawnConfig{Kind: domain.KindWorker, Prompt: "Fix it.", KnowledgeContext: "## Knowledge Graph"})

	if !strings.HasSuffix(got, "\n\n## Knowledge Graph") {
		t.Fatalf("buildPrompt() = %q, want knowledge section appended", got)
	}
}
