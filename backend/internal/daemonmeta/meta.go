package daemonmeta

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// ServiceName identifies the AO daemon in loopback health/readiness probes.
// The CLI uses it with the reported PID to avoid signaling an unrelated process
// when a stale run-file's PID has been reused.
const ServiceName = "agent-orchestrator-daemon"

var instanceID = sync.OnceValue(func() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("daemonmeta: read random instance id: " + err.Error())
	}
	return hex.EncodeToString(b[:])
})

// InstanceID is a random identifier generated once per daemon process, written
// to running.json and reported by /healthz, so a client can confirm the owner.
func InstanceID() string { return instanceID() }
