// Package sessionhealth records durable progress heartbeats and derives
// session health at read time. It never persists a derived status.
package sessionhealth

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Configuration environment variables.
const (
	EnvProviderAfter = "AO_STALL_PROVIDER_AFTER_SECONDS"
	EnvToolAfter     = "AO_STALL_TOOL_AFTER_SECONDS"
	EnvLongToolAfter = "AO_STALL_LONG_TOOL_AFTER_SECONDS"
	EnvTerminalAfter = "AO_STALL_TERMINAL_AFTER_SECONDS"
	EnvGitAfter      = "AO_STALL_GIT_AFTER_SECONDS"
	EnvBlockedAfter  = "AO_STALL_BLOCKED_AFTER_SECONDS"
	EnvLongTools     = "AO_STALL_LONG_TOOLS"
	EnvTick          = "AO_STALL_TICK_SECONDS"
)

// Default thresholds. They are deliberately conservative: a legitimate long
// tool call must finish inside its budget before health reports a stall.
const (
	DefaultProviderAfter = 15 * time.Minute
	DefaultToolAfter     = 30 * time.Minute
	DefaultLongToolAfter = 60 * time.Minute
	DefaultTerminalAfter = 20 * time.Minute
	DefaultGitAfter      = 20 * time.Minute
	DefaultBlockedAfter  = 20 * time.Minute
	defaultTick          = 30 * time.Second
)

// Config controls the derived-health thresholds.
type Config struct {
	// ProviderSilence is the longest a working Chat/ACP session may go without
	// a provider stream event or tool boundary.
	ProviderSilence time.Duration
	// ToolSilence is the budget for an in-flight tool call.
	ToolSilence time.Duration
	// LongToolSilence replaces ToolSilence for tools known to run long (shell,
	// task/subagent, terminal).
	LongToolSilence time.Duration
	// TerminalSilence is the budget for PTY output silence.
	TerminalSilence time.Duration
	// GitSilence is the budget for the most recent signal when it is a git
	// change in the session workspace.
	GitSilence time.Duration
	// BlockedAfter is how long a needs-input or reported credential failure may
	// sit before it is surfaced as blocked rather than merely waiting.
	BlockedAfter time.Duration
	// LongTools are the normalized tool names given LongToolSilence.
	LongTools map[string]struct{}
}

// DefaultConfig returns the conservative defaults.
func DefaultConfig() Config {
	return Config{
		ProviderSilence: DefaultProviderAfter,
		ToolSilence:     DefaultToolAfter,
		LongToolSilence: DefaultLongToolAfter,
		TerminalSilence: DefaultTerminalAfter,
		GitSilence:      DefaultGitAfter,
		BlockedAfter:    DefaultBlockedAfter,
		LongTools:       defaultLongTools(),
	}
}

func defaultLongTools() map[string]struct{} {
	tools := map[string]struct{}{}
	for _, name := range []string{
		"bash", "shell", "run_shell_command", "run_command", "execute",
		"exec_command", "terminal", "task", "agent", "subagent", "wait",
	} {
		tools[name] = struct{}{}
	}
	return tools
}

// FromEnv overlays AO_STALL_* variables on DefaultConfig. Values are seconds;
// an invalid value fails instead of silently disabling the safeguard.
func FromEnv(getenv func(string) string) (Config, error) {
	cfg := DefaultConfig()
	var errs []error
	seconds := func(name string, dst *time.Duration) {
		raw := strings.TrimSpace(getenv(name))
		if raw == "" {
			return
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			errs = append(errs, fmt.Errorf("%s must be a positive integer number of seconds, got %q", name, raw))
			return
		}
		*dst = time.Duration(value) * time.Second
	}
	seconds(EnvProviderAfter, &cfg.ProviderSilence)
	seconds(EnvToolAfter, &cfg.ToolSilence)
	seconds(EnvLongToolAfter, &cfg.LongToolSilence)
	seconds(EnvTerminalAfter, &cfg.TerminalSilence)
	seconds(EnvGitAfter, &cfg.GitSilence)
	seconds(EnvBlockedAfter, &cfg.BlockedAfter)
	if raw := strings.TrimSpace(getenv(EnvLongTools)); raw != "" {
		cfg.LongTools = map[string]struct{}{}
		for _, name := range strings.Split(raw, ",") {
			if key := normalizeTool(name); key != "" {
				cfg.LongTools[key] = struct{}{}
			}
		}
	}
	return cfg, joinErrors(errs)
}

func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	texts := make([]string, 0, len(errs))
	for _, err := range errs {
		texts = append(texts, err.Error())
	}
	return fmt.Errorf("%s", strings.Join(texts, "; "))
}

func normalizeTool(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// TickInterval returns the observer cadence, or defaultTick when unset.
func TickInterval(getenv func(string) string) time.Duration {
	raw := strings.TrimSpace(getenv(EnvTick))
	if raw == "" {
		return defaultTick
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultTick
	}
	return time.Duration(value) * time.Second
}

// BudgetFor returns the threshold for one signal class. An unknown class uses
// the provider budget, which is the strictest default.
func (c Config) BudgetFor(class string) time.Duration {
	switch class {
	case "tool":
		return c.ToolSilence
	case "terminal":
		return c.TerminalSilence
	case "git":
		return c.GitSilence
	default:
		return c.ProviderSilence
	}
}

// ToolBudget returns the in-flight budget for a tool name.
func (c Config) ToolBudget(tool string) time.Duration {
	if c.isLongTool(tool) {
		return c.LongToolSilence
	}
	return c.ToolSilence
}

func (c Config) isLongTool(tool string) bool {
	if _, ok := c.LongTools[normalizeTool(tool)]; ok {
		return true
	}
	return false
}
