#!/usr/bin/env bash
# MiniProbe Docker: root-only consistent local snapshot without restarting containers.
# Saves only MiniProbe data, Tunnel token and deployment configuration (never HomeSphere).
set -Eeuo pipefail
umask 077

BASE=${MINIPROBE_BACKUP_BASE:-/opt/miniprobe-docker}
BACKUPS=${MINIPROBE_BACKUP_DIR:-/var/backups/miniprobe-docker}
LOCK=${MINIPROBE_BACKUP_LOCK:-/run/lock/miniprobe-docker-backup.lock}
MARKER=${MINIPROBE_BACKUP_MARKER:-/run/lock/miniprobe-docker-backup.pause}
KEEP=7
MAX_BACKUP_BYTES=$((1024 * 1024 * 1024)) # 1 GiB limit across retained archives

fail() { printf '[错误] %s\n' "$*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || fail "请用 root 运行备份"
for command in docker tar cp gzip sha256sum flock python3 du df find stat awk sort date mktemp install grep timeout; do
  command -v "$command" >/dev/null 2>&1 || fail "缺少依赖: $command"
done

# One job at a time, even if the timer overlaps a manual invocation.
exec 9>"$LOCK"
flock -n 9 || fail "已有备份任务运行中"

[ -d "$BASE/data" ] && [ ! -L "$BASE/data" ] || fail "MiniProbe data 目录异常"
[ -s "$BASE/data/miniprobe.json" ] && [ ! -L "$BASE/data/miniprobe.json" ] || fail "数据库缺失或为符号链接"
[ -f "$BASE/secrets/cloudflared.token" ] && [ ! -L "$BASE/secrets/cloudflared.token" ] || fail "缺少独立 Tunnel Token"
[ "$(stat -c %a "$BASE/secrets/cloudflared.token")" = "600" ] || fail "Tunnel Token 权限不是 0600"
for file in .env compose.yaml compose.tunnel.yaml manage.sh; do
  [ -f "$BASE/$file" ] && [ ! -L "$BASE/$file" ] || fail "缺少部署文件: $file"
done
[ ! -e "$MARKER" ] || fail "检测到未清除的暂停标记；请先核对 Server 状态"

[ ! -L "$BACKUPS" ] || fail "备份目录不允许为符号链接"
install -d -m 0700 "$BACKUPS"
chmod 0700 "$BACKUPS"

# Require enough space for an uncompressed snapshot, compressed archive, and margin.
source_bytes=$(du -sb "$BASE/data" | awk '{print $1}')
free_bytes=$(df -PB1 "$BACKUPS" | awk 'END {print $4}')
[ "$free_bytes" -ge "$((source_bytes * 2 + 134217728))" ] ||
  fail "剩余空间不足；不删除任何旧备份"

CID=$(docker compose -f "$BASE/compose.yaml" ps -q server)
[ -n "$CID" ] || fail "MiniProbe Server 容器不存在"
state=$(docker inspect --format '{{.State.Running}} {{.State.Paused}}' "$CID")
[ "$state" = "true false" ] || fail "Server 状态不适合备份: $state"

WORK=$(mktemp -d "$BACKUPS/.work.XXXXXXXX")
ARCHIVE_PART=""
cleanup() {
  rc=$?
  trap - EXIT
  if [ -s "$MARKER" ]; then
    paused_id=$(cat "$MARKER")
    if ! docker unpause "$paused_id" >/dev/null 2>&1; then
      printf '[紧急] 无法解除 Server 暂停！请执行 docker unpause %s\n' "$paused_id" >&2
    else
      rm -f "$MARKER"
    fi
  fi
  if [ -n "$ARCHIVE_PART" ]; then rm -f -- "$ARCHIVE_PART"; fi
  rm -rf -- "$WORK"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

install -d -m 0700 "$WORK/secrets"
cp -- "$BASE/.env" "$BASE/compose.yaml" "$BASE/compose.tunnel.yaml" "$BASE/manage.sh" "$WORK/"
cp -- "$BASE/secrets/cloudflared.token" "$WORK/secrets/cloudflared.token"
chmod 0600 "$WORK/secrets/cloudflared.token"

# Briefly freeze writes only during the local copy, then immediately resume.
# Cloudflared is a separate container and is never stopped or recreated.
printf '%s\n' "$CID" > "$MARKER"
chmod 0600 "$MARKER"
docker pause "$CID" >/dev/null
timeout 20s cp -a -- "$BASE/data" "$WORK/data"
docker unpause "$CID" >/dev/null
rm -f "$MARKER"

[ -s "$WORK/data/miniprobe.json" ] || fail "快照缺少数据库"
[ -d "$WORK/data/traffic-history" ] || fail "快照缺少流量历史"
[ -d "$WORK/data/network-history" ] || fail "快照缺少网络历史"
[ -z "$(find "$WORK" -type l -print -quit)" ] || fail "快照含符号链接，拒绝备份"
python3 - "$WORK/data/miniprobe.json" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as f:
    db = json.load(f)
if not isinstance(db.get("nodes"), dict) or not db.get("secret"):
    raise SystemExit("备份数据库缺少节点或签名身份")
PY

STAMP=$(date -u +%Y%m%dT%H%M%S-%NZ)
NAME="miniprobe-$STAMP.tar.gz"
ARCHIVE_PART="$BACKUPS/.$NAME.partial"
tar -C "$WORK" -czf "$ARCHIVE_PART" \
  data secrets .env compose.yaml compose.tunnel.yaml manage.sh
gzip -t "$ARCHIVE_PART"
tar -tzf "$ARCHIVE_PART" >/dev/null
tar -tzf "$ARCHIVE_PART" | grep -x 'data/miniprobe.json' >/dev/null ||
  fail "压缩包缺少数据库"
chmod 0600 "$ARCHIVE_PART"
mv -n -- "$ARCHIVE_PART" "$BACKUPS/$NAME"
ARCHIVE_PART=""
(
  cd "$BACKUPS"
  sha256sum "$NAME" > "$NAME.sha256"
  chmod 0600 "$NAME.sha256"
  sha256sum -c "$NAME.sha256" >/dev/null
)

# Keep at most seven snapshots and at most 1 GiB of compressed backups.
mapfile -d '' -t files < <(
  find "$BACKUPS" -maxdepth 1 -type f -name 'miniprobe-*.tar.gz' -printf '%f\0' | sort -z
)
total=0
for f in "${files[@]}"; do
  size=$(stat -c %s "$BACKUPS/$f")
  total=$((total + size))
done
while [ "${#files[@]}" -gt "$KEEP" ] || [ "$total" -gt "$MAX_BACKUP_BYTES" ]; do
  [ "${#files[@]}" -gt 1 ] || break
  oldest=${files[0]}
  oldsize=$(stat -c %s "$BACKUPS/$oldest")
  rm -f -- "$BACKUPS/$oldest" "$BACKUPS/$oldest.sha256"
  files=("${files[@]:1}")
  total=$((total - oldsize))
done
printf '[成功] 本地备份完成：%s\n' "$BACKUPS/$NAME"
printf '[通过] gzip / tar / SHA256 / 数据库格式校验，Server 已恢复运行\n'
printf '[保留] %s 份备份，压缩文件合计 %s MiB（上限 1024 MiB）\n' \
  "${#files[@]}" "$(( (total + 1048575) / 1048576 ))"
