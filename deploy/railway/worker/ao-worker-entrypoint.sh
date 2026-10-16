#!/usr/bin/env bash
# Placeholder until RW-1.2.2 replaces it with the real worker entrypoint
# (volume setup, remote-host pre-seed, credentials from env). It runs the
# given command, or the daemon in the foreground when there is none.
set -euo pipefail

if [[ $# -gt 0 ]]; then
	exec "$@"
fi
exec ao daemon
