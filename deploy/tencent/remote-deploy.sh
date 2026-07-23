#!/usr/bin/env bash
#
# Runs ON the Tencent Lighthouse server (invoked over SSH by CI).
# Usage: bash ~/auth/remote-deploy.sh <image-tag>
#
# Pulls the requested auth-backend image tag, restarts the stack, reloads Caddy
# (bind-mounted Caddyfile changes don't recreate the container), and health-checks
# the backend locally (public HTTPS is not used here — cross-border TLS to the bare
# IP can be reset, so we verify on 127.0.0.1).

set -euo pipefail

VERSION="${1:?usage: remote-deploy.sh <image-tag>}"
cd "$(dirname "$0")"

export AUTH_IMAGE_TAG="$VERSION"

docker compose pull auth
docker compose up -d

# Apply any Caddyfile change without a full recreate; fall back to restart.
docker compose exec -T caddy caddy reload --config /etc/caddy/Caddyfile 2>/dev/null \
  || docker compose restart caddy

for i in $(seq 1 30); do
  if curl -sf http://127.0.0.1:3000/health | grep -q '"status":"ok"'; then
    echo "Health check passed (version ${VERSION})"
    exit 0
  fi
  echo "waiting for backend... (${i}/30)"
  sleep 5
done

echo "Health check failed after ~150s"
exit 1
