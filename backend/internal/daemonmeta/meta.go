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

// InstanceID is a random identifier generated once per daemon process. The
// daemon writes it to running.json and reports it from /healthz and /readyz,
// so a client can confirm that the daemon answering on the recorded port is
// the one that wrote the run-file. Unlike a PID, it is meaningful from inside
// a sandbox or container with its own PID namespace, where the daemon's PID is
// not visible.
func InstanceID() string { return instanceID() }
