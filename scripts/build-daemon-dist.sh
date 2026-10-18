#!/usr/bin/env bash
# Cross-compile the standalone `ao` binary (CLI + daemon) for macOS and Linux,
# packaged with a Node.js LTS runtime for Claude Code Chat, so it can be copied
# to another machine without cloning this repository.
#
# The daemon is pure Go (modernc SQLite, no cgo), so every target builds from
# any host with CGO_ENABLED=0. The Claude ACP adapter is platform-independent
# JavaScript (built once by frontend/scripts/build-acp-runtime.mjs, with AO's
# patches); only the `node` executable differs per target. It is downloaded from
# nodejs.org and checked against the release's SHASUMS256.txt.
#
# Output, one archive per target plus checksums:
#
#   dist/daemon/ao-<version>-<os>-<arch>.tar.gz
#     ao-<version>-<os>-<arch>/daemon/ao        the daemon and CLI
#     ao-<version>-<os>-<arch>/acp-runtime/     Node LTS + claude-agent-acp
#     ao-<version>-<os>-<arch>/README.txt
#   dist/daemon/SHA256SUMS
#
# The daemon finds acp-runtime/ next to daemon/ by itself; no env var needed.
#
# Environment:
#   DAEMON_PLATFORMS     space-separated os/arch list
#                        (default: darwin/arm64 darwin/amd64 linux/amd64 linux/arm64)
#   DAEMON_VERSION       version stamped into `ao version` (default: frontend/package.json)
#   DAEMON_NODE          1 bundles Node (default), 0 ships the bare binary
#   DAEMON_NODE_MAJOR    Node LTS line to bundle (default: 24)
#   DAEMON_NODE_VERSION  exact Node version, e.g. 24.11.1 (default: newest LTS of the line)
#   DAEMON_DIST          output directory (default: dist/daemon)
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
platforms="${DAEMON_PLATFORMS:-darwin/arm64 darwin/amd64 linux/amd64 linux/arm64}"
out="${DAEMON_DIST:-$root/dist/daemon}"
case "$out" in /*) ;; *) out="$root/$out" ;; esac
with_node="${DAEMON_NODE:-1}"
node_major="${DAEMON_NODE_MAJOR:-24}"

version="${DAEMON_VERSION:-$(sed -nE 's/^[[:space:]]*"version":[[:space:]]*"([^"]+)".*/\1/p' "$root/frontend/package.json" | head -n1)}"
version="${version:-dev}"
commit="$(git -C "$root" rev-parse --short=9 HEAD 2>/dev/null || echo unknown)"
if [[ -n "$(git -C "$root" status --porcelain --untracked-files=no -- backend frontend/acp-runtime 2>/dev/null)" ]]; then
	commit="$commit-dirty"
fi
date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
pkg="github.com/aoagents/agent-orchestrator/backend/internal/cli"
ldflags="-s -w -X $pkg.Version=$version -X $pkg.Commit=$commit -X $pkg.Date=$date"

for target in $platforms; do
	case "$target" in darwin/arm64 | darwin/amd64 | linux/amd64 | linux/arm64) ;;
	*) echo "unsupported target: $target (darwin|linux)/(arm64|amd64)" >&2; exit 2 ;;
	esac
done

stage="$(mktemp -d "${TMPDIR:-/tmp}/ao-daemon-dist.XXXXXX")"
trap 'rm -rf "$stage"' EXIT

sha256() { if command -v shasum >/dev/null; then shasum -a 256 "$@"; else sha256sum "$@"; fi; }

node_version=""
acp_src="$root/frontend/resources/acp-runtime"
if [[ "$with_node" == 1 ]]; then
	command -v node >/dev/null || { echo "node is required to build the Claude ACP adapter (make setup)" >&2; exit 1; }
	echo "Building the Claude ACP adapter (frontend/scripts/build-acp-runtime.mjs)"
	(cd "$root/frontend" && node scripts/build-acp-runtime.mjs)
	[[ -f "$acp_src/node_modules/@agentclientprotocol/claude-agent-acp/dist/index.js" ]] || {
		echo "ACP adapter missing from $acp_src" >&2; exit 1;
	}
	node_version="${DAEMON_NODE_VERSION:-}"
	if [[ -z "$node_version" ]]; then
		node_version="$(curl -fsSL https://nodejs.org/dist/index.json | node -e '
			let raw = ""; process.stdin.on("data", (c) => (raw += c)).on("end", () => {
				const major = process.argv[1];
				const hit = JSON.parse(raw).find((r) => r.lts && r.version.startsWith(`v${major}.`));
				if (!hit) { console.error(`no LTS release for Node ${major}`); process.exit(1); }
				console.log(hit.version.slice(1));
			});' "$node_major")"
	fi
	curl -fsSL "https://nodejs.org/dist/v$node_version/SHASUMS256.txt" -o "$stage/SHASUMS256.txt"
	echo "Bundling Node.js v$node_version (LTS) from nodejs.org"
fi

readme() {
	local name="$1" goos="$2" goarch="$3"
	cat <<EOF
AO daemon $version (commit $commit, built $date) for $goos/$goarch
EOF
	if [[ -n "$node_version" ]]; then
		printf 'Bundled Node.js v%s (LTS) for Claude Code Chat mode.\n' "$node_version"
	fi
	cat <<EOF

daemon/ao is the AO CLI and its daemon. It runs without the desktop app and
without cloning the repository.

Install
  mkdir -p ~/.local/share/ao
  tar -xzf $name.tar.gz -C ~/.local/share/ao --strip-components=1
  export PATH="\$HOME/.local/share/ao/daemon:\$PATH"   # add to ~/.zshrc or ~/.bashrc
  ao version

  Keep daemon/ and acp-runtime/ side by side: the daemon finds the bundled
  Node runtime next to its own executable. Put daemon/ on PATH as above
  instead of symlinking ao elsewhere, or set AO_ACP_RUNTIME_DIR to the
  acp-runtime directory.

Run as a remote worker host
  ao daemon &                 # or keep it running under tmux, launchd, or systemd
  ao status                   # wait for: ready
  ao remote-host enable       # LAN listener; see: ao remote-host enable --help
  ao remote-host status       # Address, Password, Host ID for Settings -> Remote hosts

Requirements on this machine
  git, plus the agent CLIs you will run (claude, codex, ...), each logged in.
  tmux for Terminal UI sessions. gh for GitHub PRs (gh auth login && gh auth setup-git).
EOF
	if [[ -n "$node_version" && "$goos" == linux ]]; then
		cat <<'EOF'
  The bundled Node needs glibc 2.28+ (Debian 10+, Ubuntu 20.04+, RHEL 8+).
  It does not run on musl distributions such as Alpine; daemon/ao itself is
  static and runs anywhere.
EOF
	fi
}

rm -rf "$out"
mkdir -p "$out"
echo "Building ao $version ($commit) into $out"
for target in $platforms; do
	goos="${target%/*}"
	goarch="${target#*/}"
	name="ao-$version-$goos-$goarch"
	dir="$stage/$name"
	mkdir -p "$dir/daemon"
	(cd "$root/backend" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
		go build -trimpath -ldflags "$ldflags" -o "$dir/daemon/ao" ./cmd/ao)

	if [[ -n "$node_version" ]]; then
		node_arch="$goarch"; [[ "$goarch" == amd64 ]] && node_arch=x64
		node_name="node-v$node_version-$goos-$node_arch"
		curl -fsSL "https://nodejs.org/dist/v$node_version/$node_name.tar.gz" -o "$stage/$node_name.tar.gz"
		expected="$(awk -v f="$node_name.tar.gz" '$2 == f {print $1}' "$stage/SHASUMS256.txt")"
		actual="$(sha256 "$stage/$node_name.tar.gz" | awk '{print $1}')"
		[[ -n "$expected" && "$expected" == "$actual" ]] || {
			echo "Node checksum mismatch for $node_name.tar.gz (expected ${expected:-none}, got $actual)" >&2; exit 1;
		}
		tar -xzf "$stage/$node_name.tar.gz" -C "$stage"
		mkdir -p "$dir/acp-runtime"
		# The adapter is platform-independent; only node/ is replaced per target.
		tar -C "$acp_src" --exclude ./node --exclude ./.ao-acp-runtime.json -cf - . | tar -C "$dir/acp-runtime" -xf -
		mkdir -p "$dir/acp-runtime/node/bin"
		cp "$stage/$node_name/bin/node" "$dir/acp-runtime/node/bin/node"
		cp "$stage/$node_name/LICENSE" "$dir/acp-runtime/node/LICENSE"
		chmod 755 "$dir/acp-runtime/node/bin/node"
		rm -rf "$stage/$node_name" "$stage/$node_name.tar.gz"
	fi

	readme "$name" "$goos" "$goarch" >"$dir/README.txt"
	COPYFILE_DISABLE=1 tar -czf "$out/$name.tar.gz" -C "$stage" "$name"
	printf '  %-40s %s\n' "$name.tar.gz" "$(du -h "$out/$name.tar.gz" | awk '{print $1}')"
done

(cd "$out" && sha256 ./*.tar.gz) | sed 's# \./# #' >"$out/SHA256SUMS"
{
	printf '{\n  "version": "%s",\n  "commit": "%s",\n  "date": "%s",\n  "platforms": [' \
		"$version" "$commit" "$date"
	separator=""
	for target in $platforms; do
		printf '%s"%s-%s"' "$separator" "${target%/*}" "${target#*/}"
		separator=", "
	done
	printf ']\n}\n'
} >"$out/build.json"
echo "Build metadata: $out/build.json"
echo "Checksums: $out/SHA256SUMS"
