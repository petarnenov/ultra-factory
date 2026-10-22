package tailnet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/mobilebridge"
)

// SiblingHostIDs returns the host IDs of the other AO installs on this machine
// (~/.ao/data, ~/.ao/dev/data); they share this tailnet address, never remotes.
func SiblingHostIDs(dataDir string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	self, _ := filepath.Abs(dataDir)
	var ids []string
	for _, dir := range []string{filepath.Join(home, ".ao", "data"), filepath.Join(home, ".ao", "dev", "data")} {
		if filepath.Clean(dir) == filepath.Clean(self) {
			continue
		}
		// #nosec G304 -- fixed AO-owned identity files.
		raw, err := os.ReadFile(mobilebridge.IdentityPath(dir))
		if err != nil {
			continue
		}
		var id struct {
			HostID string `json:"hostId"`
		}
		if json.Unmarshal(raw, &id) == nil && strings.TrimSpace(id.HostID) != "" {
			ids = append(ids, id.HostID)
		}
	}
	return ids
}
