#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
. "$SCRIPT_DIR/../../scripts/dependency-freshness.sh"
CHECK_DIR="${1:-$SCRIPT_DIR/..}"

case "$(dependency_freshness "$CHECK_DIR")" in
	fresh) ;;
	missing)
		printf '%s\n' 'frontend dependencies are missing. Run: make setup' >&2
		exit 1
		;;
	stale)
		printf '%s\n' 'frontend dependencies are out of date. Run: make setup' >&2
		exit 1
		;;
esac
