package cli

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// hostSlotInput mirrors the daemon's HostSlotInput: one paired host and the
// coordinator's worker slots on it. The host is sent as its stored ID.
type hostSlotInput struct {
	HostID string `json:"hostId"`
	Slots  int    `json:"slots"`
}

// coordinatorLimitsRequest mirrors the PUT coordinator-limits body. A nil
// HostSlots keeps the stored slots; an empty slice clears them.
type coordinatorLimitsRequest struct {
	WorkerLimit          *int             `json:"workerLimit,omitempty"`
	HostSlots            *[]hostSlotInput `json:"hostSlots,omitempty"`
	RequestedBySessionID string           `json:"requestedBySessionId"`
}

// savedHostEntries reads the desktop's saved hosts, the lookup --host uses.
// A missing file means no saved hosts.
func savedHostEntries() ([]remoteEntry, error) {
	path, err := remotesFilePath()
	if err != nil {
		return nil, err
	}
	return readRemoteEntries(path)
}

// savedHostLabels maps each paired host ID to its label for display. A
// host that cannot be read is shown by its ID.
func (c *commandContext) savedHostLabels() map[string]string {
	labels := map[string]string{}
	entries, err := savedHostEntries()
	if err != nil {
		return labels
	}
	for _, entry := range entries {
		if entry.HostID != "" {
			labels[entry.HostID] = entry.Label
		}
	}
	return labels
}

// parseHostSlots reads host:slots pairs such as Linux:2,Z600:1. Hosts resolve
// through the --host lookup; the daemon checks the slot total later.
func parseHostSlots(spec string, entries []remoteEntry) ([]hostSlotInput, error) {
	var slots []hostSlotInput
	seen := map[string]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		cut := strings.LastIndex(part, ":")
		if cut <= 0 || cut == len(part)-1 {
			return nil, usageError{fmt.Errorf("--hosts: want <host>:<slots>[,<host>:<slots>...], got %q", part)}
		}
		selector := strings.TrimSpace(part[:cut])
		count, err := strconv.Atoi(strings.TrimSpace(part[cut+1:]))
		if err != nil || count < 1 {
			return nil, usageError{fmt.Errorf("--hosts: slots for %q must be a whole number of at least 1", selector)}
		}
		id, err := resolveSavedHostID(entries, selector)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			return nil, usageError{fmt.Errorf("--hosts: host %q is listed more than once", selector)}
		}
		seen[id] = true
		slots = append(slots, hostSlotInput{HostID: id, Slots: count})
	}
	return slots, nil
}

// resolveSavedHostID returns the stored ID of a saved host. An unpaired host
// has no ID and cannot take coordinator workers.
func resolveSavedHostID(entries []remoteEntry, selector string) (string, error) {
	entry, err := selectRemoteEntry(entries, selector)
	if err != nil {
		return "", err
	}
	if entry.HostID == "" {
		return "", usageError{fmt.Errorf("host %q must be paired again in the desktop app before it can take coordinator workers", entry.Label)}
	}
	return entry.HostID, nil
}

// formatAgentSelection renders a stored selection; nil means the project
// default applies.
func formatAgentSelection(sel *agentSelection) string {
	if sel == nil {
		return "project default"
	}
	switch sel.Mode {
	case "fixed":
		return "fixed:" + sel.ProfileID
	case "random":
		if len(sel.ProfileIDs) == 0 {
			return "random"
		}
		return "random:" + strings.Join(sel.ProfileIDs, ",")
	}
	return sel.Mode
}

// hostSlotText renders each host slot with its live usage, for example
// "Linux 0/1, Z600 0/1". Labels fall back to the stored host ID.
func hostSlotText(slots []coordinatorHostSlot, labels map[string]string) string {
	parts := make([]string, 0, len(slots))
	for _, slot := range slots {
		name := labels[slot.HostID]
		if name == "" {
			name = slot.HostID
		}
		parts = append(parts, fmt.Sprintf("%s %d/%d", name, slot.Used, slot.Slots))
	}
	return strings.Join(parts, ", ")
}

func newCoordinatorSetLimitsCommand(ctx *commandContext) *cobra.Command {
	var (
		maxWorkers  int
		hosts       string
		requestedBy string
		asJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "set-limits <coordinator-session-id>",
		Short: "Change a live coordinator's worker limit and host slots",
		Long: "Change how many workers a live coordinator may hold and where they run. " +
			"--max-workers sets the limit (at most the project's per-coordinator maximum); " +
			"--hosts sets host slots as <host>:<slots>[,...] and --hosts \"\" clears them. " +
			"Lowering the limit keeps running workers and blocks new spawns until the " +
			"count drops. Only the project's live root may change it: it acts as " +
			"AO_SESSION_ID, or as --requested-by.",
		Args: oneSessionIDArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := normalizeSessionID(args[0])
			if err != nil {
				return err
			}
			caller := strings.TrimSpace(requestedBy)
			if caller == "" && !ctx.targetsRemote() {
				caller = strings.TrimSpace(os.Getenv("AO_SESSION_ID"))
			}
			if caller == "" {
				return usageError{errors.New("requested-by session id is required (pass --requested-by or set AO_SESSION_ID)")}
			}
			maxWorkersSet := cmd.Flags().Changed("max-workers")
			hostsSet := cmd.Flags().Changed("hosts")
			if !maxWorkersSet && !hostsSet {
				return usageError{errors.New("pass --max-workers, --hosts, or both")}
			}
			req := coordinatorLimitsRequest{RequestedBySessionID: caller}
			if maxWorkersSet {
				if maxWorkers < 1 {
					return usageError{errors.New("--max-workers must be at least 1")}
				}
				req.WorkerLimit = &maxWorkers
			}
			if hostsSet {
				slots := []hostSlotInput{}
				if strings.TrimSpace(hosts) != "" {
					entries, err := savedHostEntries()
					if err != nil {
						return err
					}
					if slots, err = parseHostSlots(hosts, entries); err != nil {
						return err
					}
				}
				req.HostSlots = &slots
			}
			var res sessionOwnershipDTO
			if err := ctx.putJSON(cmd.Context(), "sessions/"+url.PathEscape(id)+"/coordinator-limits", req, &res); err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), res)
			}
			return writeCoordinatorLimitLine(cmd.OutOrStdout(), res, ctx.savedHostLabels())
		},
	}
	flags := cmd.Flags()
	flags.IntVar(&maxWorkers, "max-workers", 0, "Live workers this coordinator may hold, at least 1 (up to the project's per-coordinator maximum)")
	flags.StringVar(&hosts, "hosts", "", "Host slots as <host>:<slots>[,<host>:<slots>...]; host is a saved host label or ID; \"\" clears the slots")
	flags.StringVar(&requestedBy, "requested-by", "", "Session id of the live root (defaults to AO_SESSION_ID)")
	flags.BoolVar(&asJSON, "json", false, "Output as JSON")
	addHostFlag(cmd, ctx)
	return cmd
}

// writeCoordinatorLimitLine prints the limit and host slots after a change.
func writeCoordinatorLimitLine(out io.Writer, res sessionOwnershipDTO, labels map[string]string) error {
	limit := 0
	if res.EffectivePlacementLimit != nil {
		limit = *res.EffectivePlacementLimit
	}
	line := fmt.Sprintf("coordinator %s limit: %d", res.SessionID, limit)
	if len(res.HostSlots) > 0 {
		line += "  hosts: " + hostSlotText(res.HostSlots, labels)
	}
	_, err := fmt.Fprintln(out, line)
	return err
}
