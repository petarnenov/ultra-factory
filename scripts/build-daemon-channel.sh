#!/usr/bin/env bash
# Stage the daemon update channel: stable-named bundles plus a manifest,
# built from scripts/build-daemon-dist.sh; see docs/self-hosted-remote.md.
set -euo pipefail

usage() {
	cat <<'TEXT'
Usage: build-daemon-channel.sh [options]

Stages dist/daemon-channel/{ao-<os>-<arch>.tar.gz,SHA256SUMS,manifest.json}.

Options:
  --channel NAME        channel name, for example main (DAEMON_CHANNEL)
  --out DIR             output directory (DAEMON_CHANNEL_DIST)
  --repo OWNER/REPO     repository that publishes the channel (DAEMON_REPO)
  --ref REF             git ref the build came from (DAEMON_REF)
  --platforms LIST      "os/arch" pairs (DAEMON_PLATFORMS)
  -h, --help            show this help

Environment passed through to scripts/build-daemon-dist.sh: DAEMON_VERSION,
DAEMON_NODE, DAEMON_NODE_MAJOR, DAEMON_NODE_VERSION.
TEXT
}

die() {
	printf '[build-daemon-channel] %s\n' "$*" >&2
	exit 1
}

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
channel="${DAEMON_CHANNEL:-}"
out="${DAEMON_CHANNEL_DIST:-$root/dist/daemon-channel}"
repo="${DAEMON_REPO:-}"
ref="${DAEMON_REF:-}"
platforms="${DAEMON_PLATFORMS:-darwin/arm64 darwin/amd64 linux/amd64 linux/arm64}"

while (($#)); do
	case "$1" in
		--channel) channel="${2:?--channel needs a name}"; shift 2 ;;
		--out) out="${2:?--out needs a path}"; shift 2 ;;
		--repo) repo="${2:?--repo needs OWNER/REPO}"; shift 2 ;;
		--ref) ref="${2:?--ref needs a ref}"; shift 2 ;;
		--platforms) platforms="${2:?--platforms needs a list}"; shift 2 ;;
		-h | --help) usage; exit 0 ;;
		*) usage >&2; exit 2 ;;
	esac
done

command -v python3 >/dev/null || die "python3 is required"
[[ -n "$channel" ]] || die "--channel or DAEMON_CHANNEL is required"

work="$(mktemp -d "${TMPDIR:-/tmp}/ao-daemon-channel.XXXXXX")"
trap 'rm -rf "$work"' EXIT

echo "[build-daemon-channel] building channel $channel for: $platforms"
DAEMON_DIST="$work/daemon" DAEMON_PLATFORMS="$platforms" \
	bash "$root/scripts/build-daemon-dist.sh" || die "scripts/build-daemon-dist.sh failed"

version="$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["version"])' "$work/daemon/build.json")"
commit="$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["commit"])' "$work/daemon/build.json")"
built_at="$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["date"])' "$work/daemon/build.json")"

rm -rf "$out"
mkdir -p "$out"
for target in $platforms; do
	goos="${target%/*}"
	goarch="${target#*/}"
	[[ -f "$work/daemon/ao-$version-$goos-$goarch.tar.gz" ]] || die "missing bundle for $target"
	cp "$work/daemon/ao-$version-$goos-$goarch.tar.gz" "$out/ao-$goos-$goarch.tar.gz"
done
cp "$root/scripts/ao-self-update.sh" "$out/ao-self-update.sh"
cp "$root/scripts/install-self-update.sh" "$out/install-self-update.sh"

python3 -c '
import hashlib, json, pathlib, sys

out, channel, repo, ref, version, commit, built_at, platforms = sys.argv[1:9]

def digest(path):
    hasher = hashlib.sha256()
    with open(path, "rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            hasher.update(block)
    return hasher.hexdigest()

assets = {}
sums = []
for target in platforms.split():
    os_name, _, arch = target.partition("/")
    name = "ao-%s-%s.tar.gz" % (os_name, arch)
    path = pathlib.Path(out, name)
    if not path.exists():
        raise SystemExit("missing staged bundle: %s" % path)
    checksum = digest(path)
    assets["%s-%s" % (os_name, arch)] = {
        "file": name,
        "sha256": checksum,
        "size": path.stat().st_size,
    }
    sums.append("%s  %s" % (checksum, name))

for extra in ("ao-self-update.sh", "install-self-update.sh"):
    path = pathlib.Path(out, extra)
    if path.exists():
        sums.append("%s  %s" % (digest(path), extra))

pathlib.Path(out, "SHA256SUMS").write_text("\n".join(sorted(sums)) + "\n")
manifest = {
    "channel": channel,
    "version": version,
    "commit": commit,
    "assets": assets,
}
if repo:
    manifest["repository"] = repo
if ref:
    manifest["ref"] = ref
if built_at:
    manifest["builtAt"] = built_at
pathlib.Path(out, "manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
' "$out" "$channel" "$repo" "$ref" "$version" "$commit" "$built_at" "$platforms" || die "staging the channel failed"

echo "[build-daemon-channel] $out/manifest.json"
python3 -c '
import json, sys

doc = json.load(open(sys.argv[1]))
print("  channel %s version %s commit %s" % (doc["channel"], doc["version"], doc["commit"]))
for key in sorted(doc["assets"]):
    asset = doc["assets"][key]
    print("  %-16s %-24s %s bytes" % (key, asset["file"], asset["size"]))
' "$out/manifest.json"
