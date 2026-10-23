#!/usr/bin/env bash
# Deploys the knowledge graph stack to KG_HOST over SSH (ADR 0012).
# Idempotent: re-running updates the compose files and keeps data and .env.
set -euo pipefail

KG_HOST="${KG_HOST:-petar@100.121.193.125}"
KG_REMOTE_DIR="${KG_REMOTE_DIR:-.ao/knowledge-graph}"
KG_SSH="${KG_SSH:-ssh}"
KG_SCP="${KG_SCP:-scp}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

remote() { "$KG_SSH" -o ConnectTimeout=15 "$KG_HOST" "$@"; }

echo "==> Copying stack to $KG_HOST:~/$KG_REMOTE_DIR"
remote "mkdir -p ~/$KG_REMOTE_DIR"
"$KG_SCP" -q "$here/compose.yaml" "$here/schema.cypher" "$KG_HOST:$KG_REMOTE_DIR/"

echo "==> Ensuring .env (generated once, never overwritten)"
remote "cd ~/$KG_REMOTE_DIR && if [ ! -f .env ]; then
  addr=\$(tailscale ip -4 | head -1)
  [ -n \"\$addr\" ] || { echo 'no Tailscale IPv4 address on host' >&2; exit 1; }
  umask 077
  printf 'KG_PASSWORD=%s\nKG_BIND_ADDR=%s\nKG_HEAP=1G\nKG_PAGECACHE=1G\n' \
    \"\$(openssl rand -hex 24)\" \"\$addr\" > .env
  echo 'created .env'
fi"

echo "==> Starting Neo4j and Ollama, applying the schema"
remote "cd ~/$KG_REMOTE_DIR && docker compose -p ao-knowledge-graph up -d --wait neo4j ollama \
  && docker compose -p ao-knowledge-graph run --rm schema"

echo "==> Pulling the embedding model"
remote "cd ~/$KG_REMOTE_DIR && grep -q '^KG_EMBED_MODEL=' .env || echo 'KG_EMBED_MODEL=bge-m3' >> .env
  model=\$(grep '^KG_EMBED_MODEL=' .env | cut -d= -f2)
  docker compose -p ao-knowledge-graph exec -T ollama ollama pull \"\$model\" >/dev/null && echo \"model \$model ready\""

addr="$(remote "grep '^KG_BIND_ADDR=' ~/$KG_REMOTE_DIR/.env | cut -d= -f2")"
echo "==> Knowledge graph is up: http://$addr:7474 (embeddings: http://$addr:${KG_OLLAMA_PORT:-11435})"
echo "    Password: ssh $KG_HOST grep KG_PASSWORD ~/$KG_REMOTE_DIR/.env"
