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

# This function is exclusively host-side: it never runs inside the Server
# container and never grants a container access to the Docker daemon.
run_auto_upgrade() {
  [ -t 0 ] && [ -t 1 ] || die "自动更新需要交互式 SSH 终端，请使用 sh manage.sh menu"
  [ -f "$BASE/.env" ] || die "缺少 .env，未进行修改"
  check_data
  current=$(sed -n 's/^MINIPROBE_IMAGE=//p' "$BASE/.env" | tail -n 1)
  [ -n "$current" ] || die ".env 缺少 MINIPROBE_IMAGE"
  source_tag="ghcr.io/jackyhuang83/miniprobe-server:docker-dev1"
  printf '%s\n' "========== MiniProbe Docker Server 更新 =========="
  printf '%s\n' "当前镜像: $current"
  printf '%s\n' "从 MiniProbe GHCR 拉取最近已发布的 Docker 镜像（当前服务不中断）..."
  docker pull "$source_tag" || die "拉取失败，当前 Server 未修改"

  # Resolve the moving channel to an immutable, repository-scoped SHA256
  # digest before any upgrade. Never store :latest/:docker-dev1 in .env.
  candidates=$(docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$source_tag") ||
    die "镜像缺少可验证的仓库摘要"
  target=""
  for ref in $candidates; do
    case "$ref" in
      ghcr.io/jackyhuang83/miniprobe-server@sha256:*) target="$ref" ;;
    esac
  done
  [ -n "$target" ] || die "无法找到 MiniProbe 官方仓库镜像摘要"
  checksum=${target##*sha256:}
  [ "${#checksum}" -eq 64 ] || die "镜像摘要长度异常"
  case "$checksum" in *[!0123456789abcdef]*) die "镜像摘要格式异常";; esac

  new_id=$(docker image inspect --format '{{.Id}}' "$target") ||
    die "不能读取新镜像身份，当前 Server 未修改"
  old_id=$(docker image inspect --format '{{.Id}}' "$current" 2>/dev/null || true)
  if [ -n "$old_id" ] && [ "$old_id" = "$new_id" ]; then
    printf '%s\n' "已是最新已发布 Docker 镜像，无需重启或重复安装。"
    return 0
  fi

  printf '%s\n' "可用新版镜像: $target"
  printf '%s\n' "升级将保留 data/、节点凭据和 Tunnel Token，并做健康检查；失败会尝试回滚。"
  printf '%s' "输入 YES 确认升级（其他输入取消）: "
  IFS= read -r confirmation </dev/tty || die "未取得终端确认，已取消"
  if [ "$confirmation" != YES ]; then
    printf '%s\n' "已取消，现有 Server 未修改。"
    return 0
  fi
  run_upgrade "$target"
}

# Status is local evidence only, not an end-to-end HTTPS / Agent check.
tunnel_status() {
  printf '%s\n' "========== MiniProbe Docker Tunnel 状态 =========="
  server_id=$(compose ps -q server 2>/dev/null || true)
  if [ -n "$server_id" ]; then
    health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}no-healthcheck{{end}}' "$server_id" 2>/dev/null || true)
    printf 'MiniProbe Server: %s\n' "${health:-unknown}"
  else
    printf '%s\n' "MiniProbe Server: 未运行"
  fi
  if tunnel_is_running; then
    printf '%s\n' "Cloudflare Tunnel: 容器运行中"
    tunnel_compose ps cloudflared
  else
    printf '%s\n' "Cloudflare Tunnel: 未运行"
  fi
  if [ -s "$BASE/secrets/cloudflared.token" ] && [ ! -L "$BASE/secrets/cloudflared.token" ]; then
    printf '%s\n' "独立 Tunnel Token 文件: 已配置（不显示内容）"
  else
    printf '%s\n' "独立 Tunnel Token 文件: 未配置或不可用"
  fi
  printf '%s\n' "说明：容器运行中不等于公网 HTTPS 或远端 Agent 一定可连接。"
}

validate_tunnel_token() {
  [ -s "$BASE/secrets/cloudflared.token" ] || die "缺少独立 MiniProbe Tunnel Token 文件；不要使用 HomeSphere Token"
  [ ! -L "$BASE/secrets/cloudflared.token" ] || die "Tunnel Token 不允许是符号链接"
  owner=$(stat -c %u "$BASE/secrets/cloudflared.token")
  [ "$owner" = 10001 ] || die "Token 文件必须属于 UID 10001，请检查 MiniProbe Token 文件属主"
  mode=$(stat -c %a "$BASE/secrets/cloudflared.token")
  [ "$mode" = 600 ] || die "Token 文件权限必须是 0600"
}

tunnel_start_safe() {
  [ -t 0 ] && [ -t 1 ] || die "需要交互式 SSH 终端"
  check_data
  if tunnel_is_running; then
    printf '%s\n' "Tunnel 已运行；不会重新创建容器。"
    return 0
  fi
  server_id=$(compose ps -q server 2>/dev/null || true)
  [ -n "$server_id" ] || die "MiniProbe Server 未运行，不启动 Tunnel"
  health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$server_id" 2>/dev/null || true)
  [ "$health" = healthy ] || die "MiniProbe Server 未通过健康检查，不启动 Tunnel"
  validate_tunnel_token
  printf '%s\n' "请确认 Cloudflare MiniProbe 专用 Tunnel 的 Public Hostname 指向 http://127.0.0.1:28888"
  printf '%s' "输入 START 确认启动（其它输入取消）: "
  IFS= read -r answer </dev/tty || die "未读取到终端输入；已取消"
  [ "$answer" = START ] || { printf '%s\n' "已取消，未修改服务。"; return 0; }
  refresh_tunnel
  printf '%s\n' "已启动 Tunnel 容器；请通过公网 HTTPS 与 Agent 在线状态验证实际连通性。"
}

tunnel_stop_safe() {
  [ -t 0 ] && [ -t 1 ] || die "需要交互式 SSH 终端"
  if ! tunnel_is_running; then
    printf '%s\n' "Tunnel 尚未运行，无需停止。"
    return 0
  fi
  printf '%s\n' "[警告] 停止 Tunnel 可能使 Dashboard 和所有经此入口上报的 Agent 失联。"
  printf '%s\n' "不会自动开放公网 HTTP，也不会删除 Token 或改动 Agent 地址。"
  printf '%s' "输入 STOP 确认停止（其它输入取消）: "
  IFS= read -r answer </dev/tty || die "未读取到终端输入；已取消"
  [ "$answer" = STOP ] || { printf '%s\n' "已取消，Tunnel 保持运行。"; return 0; }
  tunnel_compose stop cloudflared
  printf '%s\n' "Tunnel 已停止；如需恢复，执行 sh manage.sh tunnel-start。"
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
    if compose exec server /usr/local/bin/miniprobe-server --manage --admin-socket /run/miniprobe/admin.sock; then
      :
    else
      status=$?
      case "$status" in
        73) run_auto_upgrade ;;
        74) tunnel_status ;;
        75) tunnel_start_safe ;;
        76) tunnel_stop_safe ;;
        *) exit "$status" ;;
      esac
    fi
    ;;
  update)
    run_auto_upgrade
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
    validate_tunnel_token
    refresh_tunnel
    ;;
  tunnel-stop)
    tunnel_compose stop cloudflared
    ;;
  tunnel-status)
    tunnel_status
    ;;
  *)
    printf '%s\n' "MiniProbe Docker · 独立于 HomeSphere"
    printf '%s\n' "用法：./manage.sh {start|status|menu|update|logs|stop|upgrade <image>|tunnel-status|tunnel-start|tunnel-stop}"
    ;;
esac
