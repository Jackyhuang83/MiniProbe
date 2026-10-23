#!/bin/sh
set -eu
# Do not silently create a new signing identity if migration data was not copied.
# Fresh installations must explicitly initialize once with --init-only.
if [ "${1:-}" != "--init-only" ]; then
  [ -s /var/lib/miniprobe/miniprobe.json ] || {
    echo "MiniProbe Docker: miniprobe.json 缺失；拒绝以空身份启动。先迁移旧 Server 数据或明确执行 --init-only。" >&2
    exit 1
  }
fi
exec /usr/local/bin/miniprobe-server "$@"
