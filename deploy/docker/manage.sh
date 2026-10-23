#!/bin/sh
# MiniProbe Docker host-side helper. Never touches HomeSphere.
set -eu
BASE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$BASE"
COMPOSE="$BASE/compose.yaml"
compose() { docker compose -f "$COMPOSE" "$@"; }
tunnel_compose() { docker compose -f "$COMPOSE" -f "$BASE/compose.tunnel.yaml" --profile tunnel "$@"; }
tunnel_is_running() { [ -n "$(tunnel_compose ps -q cloudflared 2>/dev/null)" ]; }
refresh_tunnel() { tunnel_compose up -d --no-deps --force-recreate cloudflared; }
die() { printf '%s\n' "[错误] $*" >&2; exit 1; }
require_docker() {
  command -v docker >/dev/null 2>&1 || die "没有安装 Docker"
  docker compose version >/dev/null 2>&1 || die "需要 Docker Compose v2"
}
check_data() {
  [ -d "$BASE/data" ] || die "缺少 data/；迁移前不要新建空账本"
  [ ! -L "$BASE/data" ] || die "数据目录不允许是符号链接"
  [ -s "$BASE/data/miniprobe.json" ] || die "缺少原 Server 的 miniprobe.json；拒绝启动空身份 Server"
  [ ! -L "$BASE/data/miniprobe.json" ] || die "数据库不允许是符号链接"
  owner=$(stat -c %u "$BASE/data" 2>/dev/null || echo unknown)
  [ "$owner" = 10001 ] || die "data/ 所有者 UID 必须为 10001（当前 $owner）；迁移后在宿主机以 root 执行 chown -R 10001:10001 data"
}
run_upgrade() {
  [ "$#" -eq 1 ] || die "用法：sh manage.sh upgrade ghcr.io/jackyhuang83/miniprobe-server:sha-<commit>"
  target=$1
  case "$target" in
    ghcr.io/jackyhuang83/miniprobe-server:sha-*) ;;
    ghcr.io/jackyhuang83/miniprobe-server@sha256:*) ;;
    *) die "仅允许使用 MiniProbe GHCR 的 SHA 版本或 digest 镜像，拒绝 latest / 未知仓库" ;;
  esac
  case "$target" in *[!a-zA-Z0-9:./@_-]* ) die "镜像引用含非法字符";; esac
  [ -f "$BASE/.env" ] || die "未找到 .env；先从 .env.example 复制"
  old=$(sed -n 's/^MINIPROBE_IMAGE=//p' "$BASE/.env" | tail -n 1)
  [ -n "$old" ] || die ".env 缺少 MINIPROBE_IMAGE"
  [ "$target" != "$old" ] || die "目标与当前镜像相同"
  check_data
  had_tunnel=0
  if tunnel_is_running; then had_tunnel=1; fi
  printf '%s\n' "正在预拉取指定镜像（现有 Server 不停止）..."
  docker pull "$target" || die "镜像拉取失败，原 Server 未改变"
  previous=$(mktemp "$BASE/.env.prev.XXXXXXXX")
  staged=$(mktemp "$BASE/.env.next.XXXXXXXX")
  chmod 0600 "$previous" "$staged"
  cp "$BASE/.env" "$previous"
  sed "/^MINIPROBE_IMAGE=/d" "$BASE/.env" > "$staged"
  printf 'MINIPROBE_IMAGE=%s\n' "$target" >> "$staged"
  chmod 0600 "$staged"
  mv -f "$staged" "$BASE/.env"
  printf '%s\n' "启动新镜像并检查容器健康..."
  # Shared network namespace requires cloudflared recreation after Server replacement.
  if compose up -d --no-deps --wait server && { [ "$had_tunnel" -eq 0 ] || refresh_tunnel; }; then
    rm -f "$previous"
    printf '%s\n' "[成功] Server 健康检查通过；原数据未改动。旧镜像留存在本机供手动回滚。"
    return 0
  fi
  printf '%s\n' "[警告] 新镜像启动失败，恢复原镜像配置并尝试重启。" >&2
  mv -f "$previous" "$BASE/.env"
  if compose up -d --no-deps --wait server && { [ "$had_tunnel" -eq 0 ] || refresh_tunnel; }; then
    die "已回滚旧镜像，升级未完成"
  fi
  die "升级失败且自动回滚未通过健康检查；请检查 docker compose logs server，不要删除数据"
}
require_docker
case "${1:-help}" in
  start)
    check_data
    had_tunnel=0
    if tunnel_is_running; then had_tunnel=1; fi
    compose up -d --no-deps --wait server
    if [ "$had_tunnel" -eq 1 ]; then refresh_tunnel; fi
    ;;
  status)
    compose ps
    ;;
  menu)
    check_data
    compose exec server /usr/local/bin/miniprobe-server --manage --admin-socket /run/miniprobe/admin.sock
    ;;
  logs)
    compose logs --tail=100 server
    ;;
  stop)
    compose stop server
    ;;
  upgrade)
    shift
    run_upgrade "$@"
    ;;
  tunnel-start)
    [ -s "$BASE/secrets/cloudflared.token" ] || die "缺少独立 MiniProbe Tunnel Token 文件；不要使用 HomeSphere Token"
    [ ! -L "$BASE/secrets/cloudflared.token" ] || die "Tunnel Token 不允许是符号链接"
    token_owner=$(stat -c %u "$BASE/secrets/cloudflared.token")
    [ "$token_owner" = 10001 ] || die "Token 文件应由 UID 10001 所有，请在宿主机执行 chown 10001:10001 secrets/cloudflared.token"
    token_mode=$(stat -c %a "$BASE/secrets/cloudflared.token")
    [ "$token_mode" = 600 ] || die "Token 文件权限应为 0600，请在宿主机执行 chmod 0600 secrets/cloudflared.token"
    refresh_tunnel
    ;;
  tunnel-stop)
    tunnel_compose stop cloudflared
    ;;
  *)
    printf '%s\n' "MiniProbe Docker · 独立于 HomeSphere"
    printf '%s\n' "用法：./manage.sh {start|status|menu|logs|stop|upgrade <image>|tunnel-start|tunnel-stop}"
    ;;
esac
