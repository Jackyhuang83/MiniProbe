#!/bin/sh
set -eu

# MiniProbe Server one-click installer.
# Management is intentionally local-only: SSH to this VPS and run `miniprobe`.

if [ "$(id -u)" -ne 0 ]; then
  echo "MiniProbe Server 安装需要 root 权限。请先 sudo -i。" >&2
  exit 1
fi
case "$(uname -s)" in Linux) ;; *) echo "MiniProbe Server 当前仅支持 Linux" >&2; exit 1;; esac
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "Server 暂不支持此架构: $(uname -m)" >&2; exit 1 ;;
esac

PORT=${MINIPROBE_PORT:-28888}
LISTEN=${MINIPROBE_LISTEN:-":$PORT"}
INSTALL_DIR=${MINIPROBE_INSTALL_DIR:-/opt/miniprobe}
DATA_DIR=${MINIPROBE_DATA_DIR:-/var/lib/miniprobe}
DOWNLOADS_DIR="$INSTALL_DIR/downloads"
ADMIN_SOCKET=${MINIPROBE_ADMIN_SOCKET:-/run/miniprobe/admin.sock}
BUNDLE_DIR=${MINIPROBE_BUNDLE_DIR:-}
VERSION=${MINIPROBE_VERSION:-v0.4.25-alpha}
RELEASE_BASE=${MINIPROBE_RELEASE_BASE:-https://github.com/Jackyhuang83/MiniProbe/releases/download/$VERSION}
PUBLIC_URL=${MINIPROBE_PUBLIC_URL:-}
DASHBOARD_MODE=${MINIPROBE_DASHBOARD_MODE:-protected}
DASHBOARD_PASS=${MINIPROBE_DASHBOARD_PASSWORD:-}

mkdir -p "$INSTALL_DIR" "$DOWNLOADS_DIR" "$DATA_DIR" /etc/miniprobe
chmod 0750 "$DATA_DIR" /etc/miniprobe

fetch() {
  src=$1 dst=$2
  if [ -n "$BUNDLE_DIR" ] && [ -f "$BUNDLE_DIR/$src" ]; then
    cp "$BUNDLE_DIR/$src" "$dst"
    return
  fi
  if [ -z "$RELEASE_BASE" ]; then
    echo "缺少安装文件。请设置 MINIPROBE_BUNDLE_DIR（本地 dist 目录）或 MINIPROBE_RELEASE_BASE（Release 下载地址）。" >&2
    exit 1
  fi
  url="${RELEASE_BASE%/}/$src"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 3 --connect-timeout 10 "$url" -o "$dst"
  elif command -v wget >/dev/null 2>&1; then
    wget -q --timeout=20 -O "$dst" "$url"
  else
    echo "需要 curl 或 wget。" >&2; exit 1
  fi
}

random_password() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 24 | tr -d '/+=' | cut -c1-20
  else
    od -An -N20 -tx1 /dev/urandom | tr -d ' \n' | cut -c1-20
  fi
}

echo "[1/4] 下载并校验 MiniProbe Server / Agent..."
if ! command -v sha256sum >/dev/null 2>&1; then
  echo "缺少 sha256sum，不能验证更新文件；已中止，旧程序未修改。" >&2
  exit 1
fi
STAGE_DIR=$(mktemp -d "$INSTALL_DIR/.release-stage-XXXXXXXX")
trap 'rm -rf "$STAGE_DIR"' EXIT HUP INT TERM
fetch "SHA256SUMS" "$STAGE_DIR/SHA256SUMS"

verify_asset() {
  name=$1
  expected=$(awk -v wanted="$name" '$2==wanted {print $1}' "$STAGE_DIR/SHA256SUMS")
  if [ -z "$expected" ]; then
    echo "Release 校验清单缺少 $name；旧程序未修改。" >&2
    exit 1
  fi
  actual=$(sha256sum "$STAGE_DIR/$name" | awk '{print $1}')
  if [ "$expected" != "$actual" ]; then
    echo "SHA256 校验失败：$name；旧程序未修改。" >&2
    exit 1
  fi
}
fetch "miniprobe-server-linux-$ARCH" "$STAGE_DIR/miniprobe-server-linux-$ARCH"
verify_asset "miniprobe-server-linux-$ARCH"
for a in amd64 arm64 armv7; do
  echo "      校验 Agent linux/$a"
  fetch "miniprobe-agent-linux-$a" "$STAGE_DIR/miniprobe-agent-linux-$a"
  verify_asset "miniprobe-agent-linux-$a"
done

# Keep one recovery copy until the running service proves its new version.
if [ -f "$INSTALL_DIR/miniprobe-server" ]; then
  cp -p "$INSTALL_DIR/miniprobe-server" "$STAGE_DIR/old-server"
fi
for a in amd64 arm64 armv7; do
  name="miniprobe-agent-linux-$a"
  if [ -f "$DOWNLOADS_DIR/$name" ]; then
    cp -p "$DOWNLOADS_DIR/$name" "$STAGE_DIR/old-$name"
  fi
done
for a in amd64 arm64 armv7; do
  name="miniprobe-agent-linux-$a"
  chmod 0755 "$STAGE_DIR/$name"
  mv -f "$STAGE_DIR/$name" "$DOWNLOADS_DIR/$name"
done
chmod 0755 "$STAGE_DIR/miniprobe-server-linux-$ARCH"
mv -f "$STAGE_DIR/miniprobe-server-linux-$ARCH" "$INSTALL_DIR/miniprobe-server"

if [ -z "$PUBLIC_URL" ]; then
  IP=""
  if command -v ip >/dev/null 2>&1; then
    IP=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}' || true)
    if [ -z "$IP" ]; then
      IP=$(ip -6 addr show scope global 2>/dev/null | awk '/inet6/{gsub(/\/.*/,"",$2); print $2; exit}' || true)
      [ -n "$IP" ] && IP="[$IP]"
    fi
  fi
  if [ -n "$IP" ]; then PUBLIC_URL="http://$IP:$PORT"; else PUBLIC_URL="http://SERVER_IP:$PORT"; fi
fi

case "$DASHBOARD_MODE" in
  public|protected|disabled) ;;
  *) echo "MINIPROBE_DASHBOARD_MODE 必须是 public / protected / disabled" >&2; exit 1 ;;
esac

FIRST_INSTALL=0
if [ ! -f "$DATA_DIR/miniprobe.json" ]; then
  FIRST_INSTALL=1
  if [ "$DASHBOARD_MODE" = "protected" ] && [ -z "$DASHBOARD_PASS" ]; then
    DASHBOARD_PASS=$(random_password)
  fi
  if [ "$DASHBOARD_MODE" = "protected" ]; then
    "$INSTALL_DIR/miniprobe-server" \
      --data-dir "$DATA_DIR" \
      --downloads "$DOWNLOADS_DIR" \
      --init-only \
      --public-url "$PUBLIC_URL" \
      --dashboard-mode "$DASHBOARD_MODE" \
      --dashboard-password "$DASHBOARD_PASS"
  else
    "$INSTALL_DIR/miniprobe-server" \
      --data-dir "$DATA_DIR" \
      --downloads "$DOWNLOADS_DIR" \
      --init-only \
      --public-url "$PUBLIC_URL" \
      --dashboard-mode "$DASHBOARD_MODE"
  fi
fi

cat > /usr/local/bin/miniprobe <<EOF2
#!/bin/sh
exec "$INSTALL_DIR/miniprobe-server" --manage --admin-socket "$ADMIN_SOCKET" "\$@"
EOF2
chmod 0755 /usr/local/bin/miniprobe

# Server listen mode is stored outside the database so the local manager can
# safely switch between Direct and Cloudflare Tunnel by changing only bind scope.
if [ ! -f /etc/miniprobe/server.env ]; then
  cat > /etc/miniprobe/server.env <<ENV
MINIPROBE_LISTEN='$LISTEN'
ENV
  chmod 0600 /etc/miniprobe/server.env
fi
cat > "$INSTALL_DIR/run-server.sh" <<RUNNER
#!/bin/sh
set -eu
. /etc/miniprobe/server.env
exec "$INSTALL_DIR/miniprobe-server" --listen "\$MINIPROBE_LISTEN" --data-dir "$DATA_DIR" --downloads "$DOWNLOADS_DIR" --admin-socket "$ADMIN_SOCKET"
RUNNER
chmod 0755 "$INSTALL_DIR/run-server.sh"

echo "[2/4] 创建服务..."
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  cat > /etc/systemd/system/miniprobe-server.service <<UNIT
[Unit]
Description=MiniProbe Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/run-server.sh
Restart=always
RestartSec=3
RuntimeDirectory=miniprobe
RuntimeDirectoryMode=0750
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=$DATA_DIR /run/miniprobe
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
StandardOutput=null
StandardError=journal
LogRateLimitIntervalSec=60s
LogRateLimitBurst=20

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  systemctl enable miniprobe-server >/dev/null
  # An upgrade replaces the binary atomically. Explicitly restart an already
  # running service so it begins executing the new inode immediately.
  if systemctl is-active --quiet miniprobe-server; then
    systemctl restart miniprobe-server
  else
    systemctl start miniprobe-server
  fi
elif command -v rc-service >/dev/null 2>&1; then
  cat > /etc/init.d/miniprobe-server <<RC
#!/sbin/openrc-run
name="MiniProbe Server"
command="$INSTALL_DIR/run-server.sh"
command_background="yes"
pidfile="/run/miniprobe-server.pid"
output_log="/dev/null"
error_log="/dev/null"
start_pre() {
  mkdir -p /run/miniprobe
  chmod 0750 /run/miniprobe
}
RC
  chmod 0755 /etc/init.d/miniprobe-server
  rc-update add miniprobe-server default >/dev/null 2>&1 || true
  rc-service miniprobe-server restart >/dev/null
else
  echo "未检测到 systemd/OpenRC，无法自动安装服务。" >&2; exit 1
fi

echo "[3/4] 校验运行中的版本..."
VERIFIED=0
PLAIN_VERSION=$(printf '%s' "$VERSION" | sed 's/^v//')
if command -v curl >/dev/null 2>&1; then
  # The root-only admin socket works in both Direct and CF Tunnel modes.
  n=0
  while [ "$n" -lt 20 ]; do
    output=$(curl -fsS --max-time 3 --unix-socket "$ADMIN_SOCKET" \
      "http://localhost/v1/server-version" 2>/dev/null || true)
    case "$output" in
      *'"server_version":"'"$PLAIN_VERSION"'"'*)
        VERIFIED=1
        break
        ;;
    esac
    n=$((n+1))
    sleep 1
  done
fi
if [ "$VERIFIED" != "1" ]; then
  echo "[错误] Server 新版本未能通过运行校验，不会报告安装成功。" >&2
  if [ -f "$STAGE_DIR/old-server" ]; then
    mv -f "$STAGE_DIR/old-server" "$INSTALL_DIR/miniprobe-server"
    for a in amd64 arm64 armv7; do
      name="miniprobe-agent-linux-$a"
      if [ -f "$STAGE_DIR/old-$name" ]; then
        mv -f "$STAGE_DIR/old-$name" "$DOWNLOADS_DIR/$name"
      fi
    done
    if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
      systemctl restart miniprobe-server || true
    elif command -v rc-service >/dev/null 2>&1; then
      rc-service miniprobe-server restart || true
    fi
    echo "已尝试恢复升级前程序，请检查服务。" >&2
  fi
  exit 1
fi

echo "[4/4] 完成"
echo " 当前 Server 已确认运行: $VERSION"
echo ""
echo "============================================================"
echo " MiniProbe 已安装"
echo " Agent / Dashboard 地址: $PUBLIC_URL"
echo " 管理方式: SSH 登录本机后执行  miniprobe"
case "$PUBLIC_URL" in
  http://*) echo " 安全提示: 当前 Direct HTTP 未加密，仅建议用于首次部署 / 故障恢复；正式公网使用请在 miniprobe 菜单 8 启用 Cloudflare Tunnel HTTPS。" ;;
esac
if [ "$FIRST_INSTALL" = "1" ]; then
  case "$DASHBOARD_MODE" in
    public) echo " Dashboard: Public" ;;
    disabled) echo " Dashboard: Disabled" ;;
    protected)
      echo " Dashboard: Password Protected"
      echo " Dashboard 密码: $DASHBOARD_PASS"
      ;;
  esac
else
  echo " Dashboard 设置: 保持原配置不变"
fi
echo "============================================================"
echo ""
echo "节点添加、删除、Token 重置、Dashboard 模式等管理操作均不提供公网 Web API。"
echo "请执行：miniprobe"
echo "如有防火墙，请允许 Agent 所需的 TCP/$PORT。确认 Direct 模式正常后，可在 miniprobe 菜单 8 切换 Cloudflare Tunnel；验证成功后公网 $PORT 会自动停止监听。"
