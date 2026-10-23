#!/usr/bin/env bash
set -euo pipefail
# CI only: refuse to touch any real VPS deployment data.
[ "${GITHUB_ACTIONS:-}" = true ] || { echo "Docker smoke tests may only run on GitHub Actions" >&2; exit 2; }
base=$(mktemp -d)
cp "$PWD/deploy/docker/compose.yaml" "$PWD/deploy/docker/compose.tunnel.yaml" "$base/"
image="miniprobe-server:ci"
mkdir -p "$base/data" "$base/secrets"
chmod 0700 "$base/data" "$base/secrets"
sudo chown 10001:10001 "$base/data"
printf 'ci-test-fake-token\n' > "$base/secrets/cloudflared.token"
chmod 0600 "$base/secrets/cloudflared.token"
export MINIPROBE_IMAGE="$image"
cleanup() {
  docker compose -f "$base/compose.yaml" down --remove-orphans || true
  sudo rm -rf "$base"
}
trap cleanup EXIT
docker compose -f "$base/compose.yaml" -f "$base/compose.tunnel.yaml" --profile tunnel config --quiet
# Use init-only: this fixture never touches the production database/credentials.
docker run --rm --user "10001:10001" \
  -v "$base/data:/var/lib/miniprobe" "$image" \
  --init-only --data-dir /var/lib/miniprobe \
  --downloads /opt/miniprobe/downloads \
  --public-url "https://probe.example.invalid" --dashboard-mode public
test -s "$base/data/miniprobe.json"
docker compose -f "$base/compose.yaml" up -d --wait server
curl -fsS --max-time 5 http://127.0.0.1:28888/healthz | grep -q '^ok$'
docker compose -f "$base/compose.yaml" exec -T server \
  sh -ec 'test "$(id -u)" = 10001 && test -S /run/miniprobe/admin.sock && test "$(stat -c %a /run/miniprobe/admin.sock)" = 600'
printf '0\n' | docker compose -f "$base/compose.yaml" exec -T server \
  /usr/local/bin/miniprobe-server --manage --admin-socket /run/miniprobe/admin.sock \
  > /tmp/miniprobe-docker-smoke-manager.txt
grep -q 'MiniProbe' /tmp/miniprobe-docker-smoke-manager.txt
docker compose -f "$base/compose.yaml" stop server
test -s "$base/data/miniprobe.json"
echo 'MiniProbe Docker container smoke test passed'
