package agentlaunch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type fakeCatalog struct {
	catalog ports.AgentModelCatalog
	err     error
}

func (c fakeCatalog) Models(context.Context, string, string, bool) (ports.AgentModelCatalog, error) {
	return c.catalog, c.err
}

func request() Request {
	return Request{
		Machine:   "build-box",
		ProjectID: "p",
		Harness:   domain.HarnessOpenCode,
		AvailableHarnesses: []domain.AgentHarness{
			domain.HarnessOpenCode, domain.HarnessOpenCodeV2, domain.HarnessClaudeCode,
		},
	}
}

func TestResolvePrecedence(t *testing.T) {
	req := request()
	req.Role = domain.AgentConfig{Model: "opencode/settings-model", Effort: "high"}
	req.Base = domain.AgentConfig{Model: "opencode/base-model", Effort: "low"}
	req.Catalog = fakeCatalog{catalog: ports.AgentModelCatalog{Models: []ports.AgentModelInfo{
		{ID: "opencode/machine-default", IsDefault: true, Efforts: []string{"low", "high"}},
		{ID: "opencode/settings-model", Efforts: []string{"low", "high"}},
		{ID: "opencode/base-model", Efforts: []string{"low", "high"}},
		{ID: "opencode/explicit-model", Efforts: []string{"low", "high"}},
	}}}

	// Project Settings beat the machine default.
	got, err := Resolve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Model != "opencode/settings-model" || got.ModelSource != domain.AgentConfigSourceSettings {
		t.Fatalf("settings result = %#v, want settings model", got)
	}
	if got.Config.Effort != "high" || got.EffortSource != domain.AgentConfigSourceSettings {
		t.Fatalf("settings effort = %#v, want high from settings", got)
	}

	// An explicit model and effort beat Project Settings.
	req.Explicit = domain.AgentConfig{Model: "opencode/explicit-model", Effort: "low"}
	got, err = Resolve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Model != "opencode/explicit-model" || got.ModelSource != domain.AgentConfigSourceExplicit {
		t.Fatalf("explicit result = %#v, want explicit model", got)
	}
	if got.Config.Effort != "low" || got.EffortSource != domain.AgentConfigSourceExplicit {
		t.Fatalf("explicit effort = %#v, want explicit low", got)
	}
}

func TestResolveUsesMachineDefaultWhenNothingIsConfigured(t *testing.T) {
	req := request()
	req.Catalog = fakeCatalog{catalog: ports.AgentModelCatalog{Models: []ports.AgentModelInfo{
		{ID: "opencode/default", IsDefault: true, DefaultEffort: "medium", Efforts: []string{"low", "medium"}},
	}}}
	got, err := Resolve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Model != "opencode/default" || got.ModelSource != domain.AgentConfigSourceMachineDefault {
		t.Fatalf("model = %#v, want machine default", got)
	}
	if got.Config.Effort != "medium" || got.EffortSource != domain.AgentConfigSourceMachineDefault {
		t.Fatalf("effort = %#v, want catalog default effort", got)
	}
}

func TestResolveHonorsRoleModelForDifferentExplicitHarness(t *testing.T) {
	req := request()
	req.Harness = domain.HarnessOpenCode
	req.Role = domain.AgentConfig{Model: "opencode/deepseek-v4.1-flash", Effort: "high"}
	req.Catalog = fakeCatalog{catalog: ports.AgentModelCatalog{Models: []ports.AgentModelInfo{
		{ID: "opencode/beam-free", IsDefault: true},
		{ID: "opencode/deepseek-v4.1-flash", Efforts: []string{"low", "high"}},
	}}}
	got, err := Resolve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Model != "opencode/deepseek-v4.1-flash" {
		t.Fatalf("model = %q, want the role model instead of the harness default", got.Config.Model)
	}
	if got.Config.Effort != "high" {
		t.Fatalf("effort = %q, want the role effort", got.Config.Effort)
	}
	if got.ModelSource != domain.AgentConfigSourceSettings || got.EffortSource != domain.AgentConfigSourceSettings {
		t.Fatalf("sources = %q/%q, want settings/settings", got.ModelSource, got.EffortSource)
	}
}

// An explicit harness keeps the configured model and effort, but a value the
// selected harness cannot run blocks with that harness's valid options instead
// of silently falling back to the harness's own default.
func TestResolveBlocksSettingsModelInvalidForExplicitHarness(t *testing.T) {
	req := request()
	req.Harness = domain.HarnessOpenCode
	req.Role = domain.AgentConfig{Model: "claude/sonnet", Effort: "high"}
	req.Catalog = fakeCatalog{catalog: ports.AgentModelCatalog{Models: []ports.AgentModelInfo{
		{ID: "opencode/one", IsDefault: true},
		{ID: "opencode/two"},
	}}}
	_, err := Resolve(context.Background(), req)
	var resolutionErr *Error
	if !errors.As(err, &resolutionErr) {
		t.Fatalf("error = %v, want *Error", err)
	}
	if resolutionErr.Field != "model" || resolutionErr.Machine != "build-box" {
		t.Fatalf("error = %#v, want a model block on build-box", resolutionErr)
	}
	for _, want := range []string{"build-box", "opencode/one", "opencode/two"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message %q missing %q", err.Error(), want)
		}
	}
}

func TestResolveBlocksInvalidModelAndListsOptions(t *testing.T) {
	req := request()
	req.Explicit = domain.AgentConfig{Model: "opencode/missing"}
	req.Catalog = fakeCatalog{catalog: ports.AgentModelCatalog{Models: []ports.AgentModelInfo{
		{ID: "opencode/one", IsDefault: true},
		{ID: "opencode/two"},
	}}}
	_, err := Resolve(context.Background(), req)
	var resolutionErr *Error
	if !errors.As(err, &resolutionErr) {
		t.Fatalf("error = %v, want *Error", err)
	}
	if resolutionErr.Field != "model" || len(resolutionErr.Models) != 2 {
		t.Fatalf("error = %#v, want model field and full model list", resolutionErr)
	}
	for _, want := range []string{"build-box", "opencode", "opencode/one", "opencode/two"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message %q missing %q", err.Error(), want)
		}
	}
}

func TestResolveBlocksInvalidEffortAndListsLevels(t *testing.T) {
	req := request()
	req.Explicit = domain.AgentConfig{Model: "opencode/one", Effort: "ultra"}
	req.Catalog = fakeCatalog{catalog: ports.AgentModelCatalog{Models: []ports.AgentModelInfo{
		{ID: "opencode/one", IsDefault: true, Efforts: []string{"low", "high"}},
	}}}
	_, err := Resolve(context.Background(), req)
	var resolutionErr *Error
	if !errors.As(err, &resolutionErr) {
		t.Fatalf("error = %v, want *Error", err)
	}
	if resolutionErr.Field != "effort" || strings.Join(resolutionErr.Efforts, ",") != "low,high" {
		t.Fatalf("error = %#v, want low/high effort list", resolutionErr)
	}
}

func TestResolveBlocksUnresolvableModelWhenCatalogHasOptions(t *testing.T) {
	req := request()
	req.Catalog = fakeCatalog{catalog: ports.AgentModelCatalog{Models: []ports.AgentModelInfo{
		{ID: "opencode/one"},
	}}}
	if _, err := Resolve(context.Background(), req); err == nil {
		t.Fatal("want a blocking error when no model is configured and the catalog has no default")
	}
}

func TestResolveBlocksUnknownHarnessWithMachineList(t *testing.T) {
	req := request()
	req.Harness = "not-an-agent"
	_, err := Resolve(context.Background(), req)
	var resolutionErr *Error
	if !errors.As(err, &resolutionErr) {
		t.Fatalf("error = %v, want *Error", err)
	}
	if resolutionErr.Field != "harness" || len(resolutionErr.Harnesses) != 3 {
		t.Fatalf("error = %#v, want harness list", resolutionErr)
	}
}

func TestResolveBlocksUnavailableCatalog(t *testing.T) {
	req := request()
	req.Catalog = fakeCatalog{err: errors.New("not logged in")}
	if _, err := Resolve(context.Background(), req); err == nil {
		t.Fatal("want a blocking error when the catalog cannot validate the launch")
	}
}

func TestResolveAcceptsCustomModelWhenTheHarnessAllowsIt(t *testing.T) {
	req := request()
	req.Explicit = domain.AgentConfig{Model: "opencode/custom-id"}
	req.Catalog = fakeCatalog{catalog: ports.AgentModelCatalog{
		AllowCustom: true,
		Models:      []ports.AgentModelInfo{{ID: "opencode/one", IsDefault: true}},
	}}
	got, err := Resolve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Model != "opencode/custom-id" {
		t.Fatalf("model = %q, want custom id", got.Config.Model)
	}
}

func TestResolveUsesStaticCatalogWithoutADiscoveredOne(t *testing.T) {
	req := Request{Machine: "build-box", Harness: domain.HarnessAmp, Explicit: domain.AgentConfig{Model: "high"}}
	got, err := Resolve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Model != "high" {
		t.Fatalf("model = %q, want high", got.Config.Model)
	}

	req.Explicit.Model = "not-a-mode"
	if _, err := Resolve(context.Background(), req); err == nil {
		t.Fatal("want a blocking error for an unknown Amp mode")
	}
}
