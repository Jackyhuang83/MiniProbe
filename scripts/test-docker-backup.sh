#!/usr/bin/env bash
# Offline mocked-Docker tests; never touches a real MiniProbe installation.
set -Eeuo pipefail
ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
SCRIPT="$ROOT/deploy/docker/backup.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin" "$TMP/base/data/network-history" "$TMP/base/data/traffic-history" \
  "$TMP/base/secrets" "$TMP/backups"
printf '%s\n' '{"nodes":{"demo":{}},"secret":"test-nonproduction-secret"}' \
  > "$TMP/base/data/miniprobe.json"
printf 'sample history\n' > "$TMP/base/data/traffic-history/sample"
printf 'fixture\n' > "$TMP/base/.env"
printf 'fixture\n' > "$TMP/base/compose.yaml"
printf 'fixture\n' > "$TMP/base/compose.tunnel.yaml"
printf 'fixture\n' > "$TMP/base/manage.sh"
printf 'test-token-not-for-production\n' > "$TMP/base/secrets/cloudflared.token"
chmod 600 "$TMP/base/secrets/cloudflared.token"
cat > "$TMP/bin/docker" <<'MOCK'
#!/usr/bin/env bash
set -eu
case "$1" in
  compose) printf 'mock-server-id\n' ;;
  inspect) printf 'true false\n' ;;
  pause) printf 'pause\n' >> "$BACKUP_TEST_LOG"; touch "$BACKUP_TEST_PAUSED" ;;
  unpause) printf 'unpause\n' >> "$BACKUP_TEST_LOG"; rm -f "$BACKUP_TEST_PAUSED" ;;
  *) exit 1 ;;
esac
MOCK
cat > "$TMP/bin/cp" <<'MOCK'
#!/usr/bin/env bash
set -eu
if [ "${BACKUP_TEST_FAIL_COPY:-0}" = 1 ] && [ "${1:-}" = '-a' ]; then
  exit 42
fi
exec /usr/bin/cp "$@"
MOCK
chmod +x "$TMP/bin/docker" "$TMP/bin/cp"
export PATH="$TMP/bin:$PATH"
export BACKUP_TEST_LOG="$TMP/events"
export BACKUP_TEST_PAUSED="$TMP/paused"
export MINIPROBE_BACKUP_BASE="$TMP/base"
export MINIPROBE_BACKUP_DIR="$TMP/backups"
export MINIPROBE_BACKUP_LOCK="$TMP/backup.lock"
export MINIPROBE_BACKUP_MARKER="$TMP/backup.pause"
[ "$(id -u)" -eq 0 ] || {
  echo 'Skipping root-only backup integration mock on non-root runner'
  exit 0
}

bash "$SCRIPT"
test ! -e "$TMP/paused"
test ! -e "$TMP/backup.pause"
test "$(cat "$TMP/events")" = "$(printf 'pause\nunpause')"
ARCHIVE=$(find "$TMP/backups" -maxdepth 1 -name 'miniprobe-*.tar.gz' | head -n 1)
test -s "$ARCHIVE"
tar -tzf "$ARCHIVE" | grep -x 'data/miniprobe.json' >/dev/null
tar -tzf "$ARCHIVE" | grep -x 'secrets/cloudflared.token' >/dev/null
(cd "$TMP/backups" && sha256sum -c "$(basename "$ARCHIVE").sha256" >/dev/null)

# Failure after pausing must unpause and preserve the last valid backup.
export BACKUP_TEST_FAIL_COPY=1
if bash "$SCRIPT" >"$TMP/failure.log" 2>&1; then
  echo 'Expected injected copy failure' >&2
  exit 1
fi
unset BACKUP_TEST_FAIL_COPY
test ! -e "$TMP/paused"
test ! -e "$TMP/backup.pause"
test -s "$ARCHIVE"

# Never keep more than seven fully verified compressed snapshots.
for i in {1..8}; do
  bash "$SCRIPT" >/dev/null
done
test "$(find "$TMP/backups" -maxdepth 1 -name 'miniprobe-*.tar.gz' | wc -l)" -eq 7
test "$(find "$TMP/backups" -maxdepth 1 -name 'miniprobe-*.tar.gz.sha256' | wc -l)" -eq 7
test ! -e "$TMP/paused"
echo '[通过] MiniProbe Docker backup mock: consistent snapshot, recovery, retention, SHA256'
