// Package agentlaunch resolves one launch config from explicit directives,
// Project Settings, and the machine catalog with one precedence per role.
package agentlaunch

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/modelcatalog"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Catalog reports the model catalog of one harness on this machine. The
// production implementation is the daemon's agent model-catalog service.
type Catalog interface {
	Models(context.Context, string, string, bool) (ports.AgentModelCatalog, error)
}

// Request describes one launch resolution. Base is the project's base agent
// config, Role the selected role's, and Explicit the spawn/trigger overrides.
type Request struct {
	// Machine names the machine whose settings and catalog apply. Remote spawns
	// resolve on the target daemon, so this is the target machine.
	Machine string
	// ProjectID scopes project-aware model catalogs.
	ProjectID string
	// Harness is the already-selected harness.
	Harness domain.AgentHarness
	// RoleHarness is the role's Settings harness; a Settings model written for
	// another harness must be listed in the selected harness's catalog.
	RoleHarness domain.AgentHarness
	// RoleName labels the role in blocking errors (for example "worker").
	RoleName string
	Base     domain.AgentConfig
	Role     domain.AgentConfig
	Explicit domain.AgentConfig
	// EffortExplicit distinguishes an omitted effort from a caller that
	// deliberately supplied an empty value to reset to the provider default.
	EffortExplicit bool
	// SkipPermissionDefault leaves an unset permission mode empty. Reviewers own
	// their sandbox and must not inherit the spawn default.
	SkipPermissionDefault bool
	// AvailableHarnesses is the machine's supported harness set. Empty means
	// the caller already validated the harness and wants no harness list.
	AvailableHarnesses []domain.AgentHarness
	Catalog            Catalog
}

// Result is one fully resolved launch configuration and its provenance.
type Result struct {
	Config       ports.AgentConfig
	ModelSource  domain.AgentConfigSource
	EffortSource domain.AgentConfigSource
	Machine      string
}

// Error reports a launch config that cannot be resolved safely. It carries
// the machine's valid options so callers can say what to change.
type Error struct {
	Machine   string
	Harness   domain.AgentHarness
	Field     string
	Reason    string
	Harnesses []string
	Models    []string
	Efforts   []string
}

const maxListed = 100

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "agent configuration for machine %q cannot launch", e.Machine)
	if e.Harness != "" {
		fmt.Fprintf(&b, " harness %q", e.Harness)
	}
	b.WriteString(": ")
	b.WriteString(e.Reason)
	if len(e.Harnesses) > 0 {
		fmt.Fprintf(&b, "; valid harnesses: %s", list(e.Harnesses))
	}
	if e.Harness != "" && len(e.Models) > 0 {
		fmt.Fprintf(&b, "; valid models for %q: %s", e.Harness, list(e.Models))
	}
	if e.Harness != "" && len(e.Efforts) > 0 {
		fmt.Fprintf(&b, "; valid effort levels for that model: %s", list(e.Efforts))
	}
	return b.String()
}

// Details returns the structured options an API client renders in place of a
// free-form message.
func (e *Error) Details() map[string]any {
	details := map[string]any{
		"machine":   e.Machine,
		"field":     e.Field,
		"harnesses": e.Harnesses,
		"models":    e.Models,
		"efforts":   e.Efforts,
	}
	if e.Harness != "" {
		details["harness"] = string(e.Harness)
	}
	return details
}

func list(items []string) string {
	if len(items) <= maxListed {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s, ... (%d more)", strings.Join(items[:maxListed], ", "), len(items)-maxListed)
}

// Merge applies the documented precedence without consulting a catalog. It
// also returns the model and effort sources for provenance.
func Merge(base, role, explicit domain.AgentConfig, effortExplicit bool) (ports.AgentConfig, domain.AgentConfigSource, domain.AgentConfigSource) {
	config := ports.AgentConfig{
		Model:       strings.TrimSpace(base.Model),
		Effort:      strings.TrimSpace(base.Effort),
		Mode:        strings.TrimSpace(base.Mode),
		Permissions: base.Permissions,
	}
	modelSource := sourceFor(config.Model, domain.AgentConfigSourceSettings)
	effortSource := sourceFor(config.Effort, domain.AgentConfigSourceSettings)
	if roleModel := strings.TrimSpace(role.Model); roleModel != "" {
		config.Model = roleModel
		modelSource = domain.AgentConfigSourceSettings
	}
	if roleEffort := strings.TrimSpace(role.Effort); roleEffort != "" {
		config.Effort = roleEffort
		effortSource = domain.AgentConfigSourceSettings
	}
	if roleMode := strings.TrimSpace(role.Mode); roleMode != "" {
		config.Mode = roleMode
	}
	if role.Permissions != "" {
		config.Permissions = role.Permissions
	}
	if explicitModel := strings.TrimSpace(explicit.Model); explicitModel != "" {
		config.Model = explicitModel
		modelSource = domain.AgentConfigSourceExplicit
	}
	if explicitEffort := strings.TrimSpace(explicit.Effort); explicitEffort != "" {
		config.Effort = explicitEffort
		effortSource = domain.AgentConfigSourceExplicit
	} else if effortExplicit {
		config.Effort = ""
		effortSource = domain.AgentConfigSourceExplicit
	}
	if explicitMode := strings.TrimSpace(explicit.Mode); explicitMode != "" {
		config.Mode = explicitMode
	}
	if explicit.Permissions != "" {
		config.Permissions = explicit.Permissions
	}
	return config, modelSource, effortSource
}

// Resolve applies the documented precedence: explicit directive, then Project
// Settings, then the machine default, otherwise it blocks with valid options.
func Resolve(ctx context.Context, req Request) (Result, error) {
	harnesses := harnessNames(req.AvailableHarnesses)
	if req.Harness == "" {
		return Result{}, &Error{
			Machine: req.Machine, Field: "harness",
			Reason: "no harness is selected for this role", Harnesses: harnesses,
		}
	}
	if len(harnesses) > 0 && !contains(harnesses, string(req.Harness)) {
		return Result{}, &Error{
			Machine: req.Machine, Harness: req.Harness, Field: "harness",
			Reason:    fmt.Sprintf("harness %q is not available on this machine", req.Harness),
			Harnesses: harnesses,
		}
	}

	config, modelSource, effortSource := Merge(req.Base, req.Role, req.Explicit, req.EffortExplicit)
	if config.Permissions == "" && !req.SkipPermissionDefault {
		config.Permissions = ports.PermissionModeAuto
	}
	// Command Code's catalog advertises no effort levels, so forward the raw
	// level and let `cmd --effort` reject unknown values at launch.
	if req.Harness == domain.HarnessCommandCode {
		return Result{
			Config: config, ModelSource: modelSource, EffortSource: effortSource,
			Machine: req.Machine,
		}, nil
	}

	catalog, err := loadCatalog(ctx, req, false)
	if err != nil {
		return Result{}, err
	}
	result, resolutionErr := resolveWithCatalog(req, config, modelSource, effortSource, catalog)
	if resolutionErr == nil {
		return result, nil
	}
	// A cached catalog may predate a CLI upgrade or new entitlement. Confirm a
	// strict rejection against fresh discovery before blocking the launch.
	if req.Catalog == nil || !strictResolutionField(resolutionErr.Field) {
		return Result{}, resolutionErr
	}
	fresh, err := loadCatalog(ctx, req, true)
	if err != nil {
		return Result{}, resolutionErr
	}
	result, resolutionErr = resolveWithCatalog(req, config, modelSource, effortSource, fresh)
	if resolutionErr != nil {
		return Result{}, resolutionErr
	}
	return result, nil
}

func loadCatalog(ctx context.Context, req Request, refresh bool) (ports.AgentModelCatalog, error) {
	if req.Catalog == nil {
		return modelcatalog.Base(string(req.Harness)), nil
	}
	catalog, err := req.Catalog.Models(ctx, string(req.Harness), req.ProjectID, refresh)
	if err != nil {
		return ports.AgentModelCatalog{}, &Error{
			Machine: req.Machine, Harness: req.Harness, Field: "catalog",
			Reason: fmt.Sprintf("the model catalog is unavailable: %v", err),
			Models: catalogModelIDs(catalog),
		}
	}
	return catalog, nil
}
func strictResolutionField(field string) bool {
	return field == "catalog" || field == "model" || field == "effort"
}

// settingsModelFromOtherHarness reports a Settings model written for a
// different harness, which direct entry must not treat as a custom value.
func settingsModelFromOtherHarness(req Request, modelSource domain.AgentConfigSource) bool {
	return modelSource == domain.AgentConfigSourceSettings &&
		req.RoleHarness != "" && req.RoleHarness != req.Harness
}

func crossHarnessModelError(req Request, model string, models []string) *Error {
	role := strings.TrimSpace(req.RoleName)
	if role == "" {
		role = "selected"
	}
	return &Error{
		Machine: req.Machine, Harness: req.Harness, Field: "model",
		Reason: fmt.Sprintf(
			"Project Settings model %q for the %s role was configured for harness %q and is not valid for this harness",
			model, role, req.RoleHarness),
		Models: models,
	}
}

// resolveWithCatalog validates one merged configuration against one catalog.
func resolveWithCatalog(req Request, config ports.AgentConfig, modelSource, effortSource domain.AgentConfigSource, catalog ports.AgentModelCatalog) (Result, *Error) {
	result := Result{
		Config:       config,
		ModelSource:  modelSource,
		EffortSource: effortSource,
		Machine:      req.Machine,
	}
	modelIDs := catalogModelIDs(catalog)
	// Agents whose picker is a mode list (for example Amp) keep the selection
	// in Mode. Treat it as the model so a project default survives.
	if config.Model == "" && catalog.SelectionMode == ports.ModelSelectionModeList && strings.TrimSpace(config.Mode) != "" {
		config.Model = strings.TrimSpace(config.Mode)
		switch {
		case strings.TrimSpace(req.Explicit.Mode) != "":
			modelSource = domain.AgentConfigSourceExplicit
		case strings.TrimSpace(req.Role.Mode) != "" || strings.TrimSpace(req.Base.Mode) != "":
			modelSource = domain.AgentConfigSourceSettings
		}
		result.Config = config
		result.ModelSource = modelSource
	}

	var selected *ports.AgentModelInfo
	crossHarnessSettings := settingsModelFromOtherHarness(req, modelSource)
	strictModel := req.Harness == domain.HarnessClaudeCode && len(catalog.Models) > 0 ||
		(!catalog.AllowCustom && catalog.CustomModelEntry != ports.CustomModelEntryDirect) ||
		crossHarnessSettings
	if catalog.Stale && config.Model != "" && strictModel {
		return Result{}, staleCatalogError(req, catalog, config.Model)
	}
	if config.Model == "" {
		for i := range catalog.Models {
			if catalog.Models[i].IsDefault && strings.TrimSpace(catalog.Models[i].ID) != "" {
				selected = &catalog.Models[i]
				break
			}
		}
		if selected == nil {
			if len(modelIDs) > 0 {
				return Result{}, &Error{
					Machine: req.Machine, Harness: req.Harness, Field: "model",
					Reason: "no model is configured at any level and this harness reports no default model",
					Models: modelIDs,
				}
			}
			return result, nil
		}
		result.Config.Model = selected.ID
		result.ModelSource = domain.AgentConfigSourceMachineDefault
	} else {
		for i := range catalog.Models {
			if strings.EqualFold(catalog.Models[i].ID, config.Model) {
				selected = &catalog.Models[i]
				break
			}
		}
		if selected == nil {
			strictClaude := req.Harness == domain.HarnessClaudeCode && len(catalog.Models) > 0
			if strictClaude || (!catalog.AllowCustom && catalog.CustomModelEntry != ports.CustomModelEntryDirect) ||
				crossHarnessSettings {
				if catalog.Stale {
					return Result{}, staleCatalogError(req, catalog, config.Model)
				}
				if crossHarnessSettings {
					return Result{}, crossHarnessModelError(req, config.Model, modelIDs)
				}
				return Result{}, &Error{
					Machine: req.Machine, Harness: req.Harness, Field: "model",
					Reason: fmt.Sprintf("model %q is not valid for this harness", config.Model),
					Models: modelIDs,
				}
			}
			// The harness accepts model IDs AO cannot enumerate; forward them and
			// leave effort validation to the harness.
			return result, nil
		}
		if len(selected.Efforts) == 0 && req.Harness == domain.HarnessClaudeCode {
			// A family alias ("opus") carries no capabilities of its own; the CLI
			// resolves it to the family's newest model, which the picker shows.
			if concrete := newestClaudeFamilyModel(catalog.Models, config.Model); concrete != nil {
				selected = concrete
			}
		}
	}

	if result.Config.Effort == "" {
		if selected.DefaultEffort != "" && !isEffortExplicit(req) {
			result.Config.Effort = selected.DefaultEffort
			result.EffortSource = domain.AgentConfigSourceMachineDefault
		}
		return result, nil
	}
	if selected == nil {
		return result, nil
	}
	if catalog.Stale {
		return Result{}, staleCatalogError(req, catalog, result.Config.Model)
	}
	efforts := append([]string(nil), selected.Efforts...)
	if !contains(efforts, result.Config.Effort) {
		reason := fmt.Sprintf("effort %q is not valid for model %q", result.Config.Effort, result.Config.Model)
		if len(efforts) == 0 {
			reason = fmt.Sprintf("model %q accepts no effort setting", result.Config.Model)
		}
		return Result{}, &Error{
			Machine: req.Machine, Harness: req.Harness, Field: "effort",
			Reason: reason, Models: modelIDs, Efforts: efforts,
		}
	}
	return result, nil
}

func isEffortExplicit(req Request) bool {
	return req.EffortExplicit || strings.TrimSpace(req.Explicit.Effort) != ""
}

func staleCatalogError(req Request, catalog ports.AgentModelCatalog, model string) *Error {
	return &Error{
		Machine: req.Machine, Harness: req.Harness, Field: "catalog",
		Reason: fmt.Sprintf("the cached catalog is stale and cannot validate model %q", model),
		Models: catalogModelIDs(catalog),
	}
}

func sourceFor(value string, source domain.AgentConfigSource) domain.AgentConfigSource {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return source
}

func catalogModelIDs(catalog ports.AgentModelCatalog) []string {
	out := make([]string, 0, len(catalog.Models))
	for _, item := range catalog.Models {
		if id := strings.TrimSpace(item.ID); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func harnessNames(harnesses []domain.AgentHarness) []string {
	out := make([]string, 0, len(harnesses))
	for _, harness := range harnesses {
		out = append(out, string(harness))
	}
	return out
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

var (
	claudeFamilyAliases = []string{"fable", "opus", "sonnet", "haiku"}
	claudeVersionNumber = regexp.MustCompile(`\d+`)
	claudeVersionNoise  = regexp.MustCompile(`\(.*?\)|\[.*?\]`)
)

// newestClaudeFamilyModel returns the newest catalog model of the family a
// bare alias such as "opus" names, or nil when there is none.
func newestClaudeFamilyModel(models []ports.AgentModelInfo, alias string) *ports.AgentModelInfo {
	family := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(alias), "[1m]"))
	if !contains(claudeFamilyAliases, family) {
		return nil
	}
	var best *ports.AgentModelInfo
	var bestVersion []int
	for i := range models {
		if len(models[i].Efforts) == 0 {
			continue
		}
		version := claudeFamilyVersion(models[i], family)
		if version == nil {
			continue
		}
		if best == nil || compareVersions(version, bestVersion) > 0 {
			best, bestVersion = &models[i], version
		}
	}
	return best
}

// claudeFamilyVersion reads major.minor from a model's label or ID, or nil
// when the model is not in the family. Snapshot dates are ignored.
func claudeFamilyVersion(model ports.AgentModelInfo, family string) []int {
	for _, text := range []string{model.Label, model.ID} {
		lower := claudeVersionNoise.ReplaceAllString(strings.ToLower(text), "")
		if !strings.Contains(lower, family) {
			continue
		}
		var version []int
		for _, part := range claudeVersionNumber.FindAllString(lower, -1) {
			if len(part) <= 2 && len(version) < 2 {
				n, _ := strconv.Atoi(part)
				version = append(version, n)
			}
		}
		if len(version) > 0 {
			return version
		}
	}
	return nil
}

func compareVersions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x - y
		}
	}
	return 0
}
