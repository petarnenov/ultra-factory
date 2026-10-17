package codexappserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// A user's shell_environment_policy (inherit = "core") strips AO's variables
// from the commands Codex runs; every thread open sets them explicitly, without
// credentials.
func TestThreadOpenForwardsAOShellEnv(t *testing.T) {
	env := map[string]string{"AO_SESSION_ID": "ao-7", "AO_DATA_DIR": "/data", "AO_BROWSER_CAPABILITY": "secret", "HOME": "/home/u"}
	for _, operation := range []string{"start", "resume", "fork"} {
		t.Run(operation, func(t *testing.T) {
			d, srv := newTestDriver(t)
			cfg := ports.ChatStartConfig{WorkspacePath: t.TempDir(), Permissions: "auto", Env: env}
			var conv ports.ChatConversation
			var err error
			method := "thread/" + operation
			switch operation {
			case "start":
				conv, err = d.Start(context.Background(), cfg)
			case "resume":
				conv, err = d.Resume(context.Background(), ports.ChatResumeConfig{WorkspacePath: cfg.WorkspacePath, ProviderConversationID: "t", Permissions: cfg.Permissions, Env: env})
			case "fork":
				srv.reply(method, `{"thread":{"id":"forked"}}`)
				conv, err = d.ForkIntoHost(context.Background(), "main", "turn-A", cfg)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer conv.Close()
			var params map[string]any
			if err := json.Unmarshal(srv.awaitFrame(func(f frame) bool { return f.Method == method }).Params, &params); err != nil {
				t.Fatal(err)
			}
			config, _ := params["config"].(map[string]any)
			if config["shell_environment_policy.set.AO_SESSION_ID"] != "ao-7" || config["shell_environment_policy.set.AO_DATA_DIR"] != "/data" {
				t.Fatalf("%s config lacks AO's variables: %+v", operation, config)
			}
			for _, leaked := range []string{"shell_environment_policy.set.AO_BROWSER_CAPABILITY", "shell_environment_policy.set.HOME"} {
				if _, ok := config[leaked]; ok {
					t.Fatalf("%s config forwards %s: %+v", operation, leaked, config)
				}
			}
		})
	}
}
