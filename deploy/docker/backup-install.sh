#!/usr/bin/env bash
# Install MiniProbe-only root backup timer on the Docker host.
set -Eeuo pipefail
umask 077
[ "$(id -u)" -eq 0 ] || { echo '[错误] 请用 root 执行' >&2; exit 1; }
BASE=/opt/miniprobe-docker
UNIT=/etc/systemd/system
BACKUPS=/var/backups/miniprobe-docker
[ -s "$BASE/backup.sh" ] || { echo '[错误] 找不到 $BASE/backup.sh' >&2; exit 1; }
[ -s "$BASE/data/miniprobe.json" ] || { echo '[错误] MiniProbe 尚未完成迁移' >&2; exit 1; }
command -v systemctl >/dev/null || { echo '[错误] 需要 systemd' >&2; exit 1; }
bash -n "$BASE/backup.sh"
chmod 0750 "$BASE/backup.sh"
install -d -m 0700 "$BACKUPS"

cat > "$UNIT/miniprobe-docker-backup.service" <<'UNITFILE'
[Unit]
Description=MiniProbe Docker consistent local backup (does not touch HomeSphere)
Requires=docker.service
After=docker.service
ConditionPathExists=/opt/miniprobe-docker/data/miniprobe.json

[Service]
Type=oneshot
User=root
Group=root
UMask=0077
ExecStart=/usr/bin/bash /opt/miniprobe-docker/backup.sh
# A watchdog for abnormal job termination: never leave the Server frozen.
ExecStopPost=/usr/bin/bash -c 'm=/run/lock/miniprobe-docker-backup.pause; if [ -s "$m" ]; then id=$(cat "$m"); /usr/bin/docker unpause "$id" >/dev/null 2>&1 || true; rm -f "$m"; fi'
TimeoutStartSec=300
Nice=10
IOSchedulingClass=best-effort
IOSchedulingPriority=7
NoNewPrivileges=true
PrivateTmp=true
UNITFILE

cat > "$UNIT/miniprobe-docker-backup.timer" <<'UNITFILE'
[Unit]
Description=MiniProbe Docker daily local backup timer

[Timer]
OnCalendar=*-*-* 04:30:00 Asia/Tokyo
RandomizedDelaySec=5m
Persistent=true
Unit=miniprobe-docker-backup.service

[Install]
WantedBy=timers.target
UNITFILE

chmod 0644 "$UNIT/miniprobe-docker-backup.service" "$UNIT/miniprobe-docker-backup.timer"
systemctl daemon-reload

echo '=== 首次执行备份并校验 ==='
if ! systemctl start miniprobe-docker-backup.service; then
  echo '[错误] 首次备份失败，未启用自动定时器' >&2
  journalctl --no-pager -u miniprobe-docker-backup.service -n 12 || true
  exit 1
fi
echo '=== 启用每天的自动备份 ==='
systemctl enable --now miniprobe-docker-backup.timer
echo '=== 最近一次备份结果 ==='
journalctl --no-pager -u miniprobe-docker-backup.service -n 6
echo '=== 下次执行时间 ==='
systemctl list-timers --all miniprobe-docker-backup.timer --no-pager
echo '[成功] MiniProbe 已启用独立的每日本地备份（日本时间 04:30 起 5 分钟内）'
