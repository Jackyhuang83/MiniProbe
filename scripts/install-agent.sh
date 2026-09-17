#!/bin/sh
set -eu

SERVER=""
TOKEN=""
NODE_ID=""
SERVER_KEY=""
INTERFACE="auto"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --server) SERVER=${2:-}; shift 2 ;;
    --token) TOKEN=${2:-}; shift 2 ;;
    --node-id) NODE_ID=${2:-}; shift 2 ;;
    --server-key) SERVER_KEY=${2:-}; shift 2 ;;
    --interface) INTERFACE=${2:-auto}; shift 2 ;;
    *) echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ "$(id -u)" -ne 0 ]; then
  echo "MiniProbe Agent 安装需要 root 权限。请先 sudo -i 后重新执行安装命令。" >&2
  exit 1
fi
[ -n "$SERVER" ] || { echo "--server is required" >&2; exit 2; }
[ -n "$TOKEN" ] || { echo "--token is required" >&2; exit 2; }
[ -n "$NODE_ID" ] || { echo "--node-id is required" >&2; exit 2; }
[ -n "$SERVER_KEY" ] || { echo "--server-key is required (v0.4.0+)" >&2; exit 2; }
SERVER=${SERVER%/}

case "$(uname -s)" in Linux) ;; *) echo "MiniProbe Agent 当前仅支持 Linux" >&2; exit 1;; esac
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  armv7l|armv7) ARCH=armv7 ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

INSTALL_DIR=/opt/miniprobe
STATE_DIR=/var/lib/miniprobe-agent
STATE_FILE="$STATE_DIR/state.json"
BOOTSTRAP_ID=$(od -An -N8 -tx1 /dev/urandom 2>/dev/null | tr -d ' \n' || date +%s)
BIN="$INSTALL_DIR/miniprobe-agent"
TMP="$INSTALL_DIR/.miniprobe-agent.new"
mkdir -p "$INSTALL_DIR" "$STATE_DIR"
chmod 0750 "$STATE_DIR"
URL="$SERVER/downloads/miniprobe-agent-linux-$ARCH"

echo "[1/3] 下载 MiniProbe Agent ($ARCH)..."
if command -v curl >/dev/null 2>&1; then
  curl -fsSL --retry 3 --connect-timeout 10 "$URL" -o "$TMP"
elif command -v wget >/dev/null 2>&1; then
  wget -q --timeout=15 -O "$TMP" "$URL"
else
  echo "需要 curl 或 wget。" >&2
  exit 1
fi
[ -s "$TMP" ] || { echo "Agent 下载失败：$URL" >&2; exit 1; }
chmod 0755 "$TMP"
mv -f "$TMP" "$BIN"

cat > "$INSTALL_DIR/agent.env" <<ENV
MINIPROBE_ENDPOINT=$SERVER
MINIPROBE_TOKEN=$TOKEN
MINIPROBE_NODE_ID=$NODE_ID
MINIPROBE_SERVER_KEY=$SERVER_KEY
MINIPROBE_BOOTSTRAP_ID=$BOOTSTRAP_ID
MINIPROBE_INTERFACE=$INTERFACE
MINIPROBE_STATE_FILE=$STATE_FILE
ENV
chmod 0600 "$INSTALL_DIR/agent.env"

# Use the same small wrapper for systemd and OpenRC. This makes environment
# loading deterministic across distributions instead of relying on OpenRC
# function-scope export behavior.
cat > "$INSTALL_DIR/run-agent.sh" <<'RUNNER'
#!/bin/sh
set -eu
set -a
. /opt/miniprobe/agent.env
set +a
exec /opt/miniprobe/miniprobe-agent
RUNNER
chmod 0755 "$INSTALL_DIR/run-agent.sh"

echo "[2/3] 安装系统服务..."
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  cat > /etc/systemd/system/miniprobe-agent.service <<'UNIT'
[Unit]
Description=MiniProbe Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/opt/miniprobe/agent.env
ExecStart=/opt/miniprobe/run-agent.sh
Restart=always
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/miniprobe-agent
ProtectHome=true
PrivateTmp=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=true
StandardOutput=null
StandardError=journal
LogRateLimitIntervalSec=60s
LogRateLimitBurst=10

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  systemctl enable miniprobe-agent >/dev/null
  # `enable --now` does not restart an already-running service. Explicit restart
  # ensures an in-place Agent upgrade begins executing the new binary now.
  systemctl restart miniprobe-agent
elif command -v rc-service >/dev/null 2>&1; then
  cat > /etc/init.d/miniprobe-agent <<'RC'
#!/sbin/openrc-run
name="MiniProbe Agent"
command="/opt/miniprobe/run-agent.sh"
command_background="yes"
pidfile="/run/miniprobe-agent.pid"
output_log="/dev/null"
error_log="/dev/null"
RC
  chmod 0755 /etc/init.d/miniprobe-agent
  rc-update add miniprobe-agent default >/dev/null 2>&1 || true
  rc-service miniprobe-agent restart >/dev/null
else
  echo "未检测到 systemd/OpenRC。Agent 已下载到 $BIN，但未自动创建服务。" >&2
  exit 1
fi

echo "[3/3] 完成。"
echo "MiniProbe Agent 已安装并启动，节点 ID: $NODE_ID"
echo "流量计数状态保存在 $STATE_FILE；Agent/系统重启不会把本周期流量归零。"
