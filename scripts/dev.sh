#!/usr/bin/env bash
# scripts/dev.sh - 本地开发模式生命周期管理
#
# 与根目录 start.sh 的区别:
#   - start.sh 面向 Linux 生产/预览,用 ss/setsid,只支持 preview (dist) 模式
#   - dev.sh   面向日常开发,跨平台 (Linux + Windows Git Bash),
#              后端走 `go run` (源码模式),前端走 vite HMR (`npm run dev`)
#              改动前端代码浏览器自动热更新,无需手动 rebuild
#
# 用法: scripts/dev.sh [start|stop|restart|status]
#
# 环境变量:
#   FRONTEND_HOST  (默认 0.0.0.0)
#   FRONTEND_PORT  (默认 6191)
#   BACKEND_PORT   (默认 6192)
#   LOG_DIR        (默认 Windows=%TEMP%/video-site-61, Linux=/tmp/video-site-61)

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

FRONTEND_HOST="${FRONTEND_HOST:-0.0.0.0}"
FRONTEND_PORT="${FRONTEND_PORT:-6191}"
BACKEND_PORT="${BACKEND_PORT:-6192}"

# 日志目录:Windows 优先 %TEMP%,否则 /tmp
if [[ -z "${LOG_DIR:-}" ]]; then
  if [[ -n "${TEMP:-}" && -d "${TEMP}" ]]; then
    LOG_DIR="$TEMP/video-site-61"
  else
    LOG_DIR="/tmp/video-site-61"
  fi
fi
mkdir -p "$LOG_DIR"
BACKEND_LOG="$LOG_DIR/backend-dev.log"
FRONTEND_LOG="$LOG_DIR/frontend-dev.log"

# Windows 上 Git Bash 的 kill 对编译出的 .exe 不生效,要用 taskkill /F
is_windows() {
  [[ "${OS:-}" == "Windows_NT" ]] || [[ "${OSTYPE:-}" == msys* ]] || [[ "${OSTYPE:-}" == cygwin ]]
}

force_kill_pid() {
  local pid="$1"
  if is_windows; then
    taskkill //F //PID "$pid" >/dev/null 2>&1 || true
  else
    kill -9 "$pid" 2>/dev/null || true
  fi
}

graceful_kill_pid() {
  local pid="$1"
  if is_windows; then
    # taskkill 不带 /F 给 console app 发 WM_CLOSE/Ctrl+Break
    taskkill //PID "$pid" >/dev/null 2>&1 || true
  else
    kill "$pid" 2>/dev/null || true
  fi
}

# 跨平台查端口占用 PID (lsof > fuser > ss > netstat fallback)
pids_on_port() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    lsof -ti ":$port" 2>/dev/null | sort -u
  elif command -v fuser >/dev/null 2>&1; then
    fuser -n tcp "$port" 2>/dev/null | tr -s ' \t' '\n' | grep -E '^[0-9]+$' | sort -u
  elif command -v ss >/dev/null 2>&1; then
    ss -ltnp 2>/dev/null \
      | awk -v needle=":$port" '$4 ~ needle {print $0}' \
      | sed -nE 's/.*pid=([0-9]+).*/\1/p' \
      | sort -u
  else
    # netstat fallback:Windows 格式 $4=LISTENING,Linux 格式 $6=LISTEN
    netstat -ano 2>/dev/null \
      | awk -v port=":$port" '
          ($2 ~ port && toupper($4) == "LISTENING") { print $NF; next }
          ($4 ~ port && $6 == "LISTEN")              { print $NF; next }
        ' \
      | grep -E '^[0-9]+$' | sort -u
  fi
}

print_port_status() {
  local name="$1" port="$2"
  local pids
  pids="$(pids_on_port "$port" | tr '\n' ' ' | sed 's/[[:space:]]*$//')"
  if [[ -n "$pids" ]]; then
    echo "$name listening on port $port (pid: $pids)"
  else
    echo "$name not running on port $port"
  fi
}

stop_port() {
  local name="$1" port="$2"
  local pids
  pids="$(pids_on_port "$port" | tr '\n' ' ' | sed 's/[[:space:]]*$//')"
  if [[ -z "$pids" ]]; then
    echo "$name is not running on port $port"
    return 0
  fi

  echo "stopping $name on port $port (pid: $pids)"
  for pid in $pids; do
    graceful_kill_pid "$pid"
  done

  for _ in $(seq 1 20); do
    if [[ -z "$(pids_on_port "$port" || true)" ]]; then
      return 0
    fi
    sleep 0.2
  done

  echo "$name did not stop gracefully; sending SIGKILL"
  for pid in $pids; do
    force_kill_pid "$pid"
  done

  # SIGKILL 后再等一下端口释放
  for _ in $(seq 1 20); do
    if [[ -z "$(pids_on_port "$port" || true)" ]]; then
      return 0
    fi
    sleep 0.2
  done
}

wait_for_port() {
  local name="$1" port="$2" log="$3"
  for _ in $(seq 1 90); do
    if [[ -n "$(pids_on_port "$port" || true)" ]]; then
      print_port_status "$name" "$port"
      return 0
    fi
    sleep 1
  done
  echo "$name did not start on port $port within 90s. Check log: $log" >&2
  return 1
}

# 启动前自检:必填工具、配置、数据目录;可选工具给警告而非失败
check_prereqs() {
  local ok=1
  echo "[dev] pre-flight checks..."

  # 必填:go/node/npm (后端 go run,前端 npm run dev)
  local required_cmds=(go node npm)
  for cmd in "${required_cmds[@]}"; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      echo "  ✗ $cmd: not found in PATH (required)"
      ok=0
    else
      echo "  ✓ $cmd: $(command -v "$cmd")"
    fi
  done

  # 必填:backend/config.yaml
  local cfg="$ROOT_DIR/backend/config.yaml"
  if [[ ! -f "$cfg" ]]; then
    echo "  ✗ config: $cfg not found (required)"
    ok=0
  else
    echo "  ✓ config: $cfg"
  fi

  # 必填:backend/data 目录 (db_path / local_preview_dir 的父目录)
  local data_dir="$ROOT_DIR/backend/data"
  if [[ ! -d "$data_dir" ]]; then
    if mkdir -p "$data_dir" 2>/dev/null; then
      echo "  ✓ data dir: $data_dir (created)"
    else
      echo "  ✗ data dir: cannot create $data_dir (permission denied?)"
      ok=0
    fi
  else
    echo "  ✓ data dir: $data_dir"
  fi

  # 可选:ffmpeg / ffprobe (后端 preview.enabled=true 时用)
  for cmd in ffmpeg ffprobe; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      echo "  ⚠ $cmd: not found in PATH (video preview generation will fail)"
    else
      echo "  ✓ $cmd: $(command -v "$cmd")"
    fi
  done

  echo ""
  if [[ $ok -eq 0 ]]; then
    echo "[dev] pre-flight FAILED. Fix the items marked ✗ above."
    return 1
  fi
  echo "[dev] pre-flight OK."
  return 0
}

# 取 Tailscale 状态,逐行打印访问 URL;没装/未连给提示
tailscale_info_lines() {
  local port="$1"
  if ! command -v tailscale >/dev/null 2>&1; then
    echo "  - Tailscale: not installed (skip; install from https://tailscale.com to enable remote access)"
    return 0
  fi

  local json
  if ! json=$(tailscale status --json 2>/dev/null); then
    echo "  - Tailscale: installed, but 'tailscale status' failed (run 'tailscale up' first?)"
    return 0
  fi

  if ! command -v python >/dev/null 2>&1 && ! command -v python3 >/dev/null 2>&1; then
    echo "  - Tailscale: installed, but no python on PATH to parse status JSON"
    return 0
  fi

  local py
  py="$(command -v python || command -v python3)"
  local tmp
  tmp=$(mktemp 2>/dev/null) || { echo "  - Tailscale: cannot create temp file"; return 0; }
  printf '%s' "$json" > "$tmp"

  "$py" - "$port" "$tmp" <<'PY' 2>/dev/null || echo "  - Tailscale: installed, but status JSON could not be parsed"
import json, sys
port, path = sys.argv[1], sys.argv[2]
with open(path, 'r', encoding='utf-8') as f:
    d = json.load(f)
state = (d.get('BackendState') or '').strip()
if state.lower() != 'running':
    print(f"  - Tailscale: service installed, but tailnet not connected (BackendState={state!r}). Run 'tailscale up' first.")
    sys.exit(0)
self_ = d.get('Self') or {}
dns = (self_.get('DNSName') or '').rstrip('.')
ips = self_.get('TailscaleIPs') or []
print("  - Tailscale: connected")
if dns:
    print(f"  - Tailnet home:  http://{dns}:{port}/")
    print(f"  - Tailnet admin: http://{dns}:{port}/admin")
for ip in ips[:1]:
    ip = (ip or '').strip()
    if ip:
        print(f"  - Tail IP home:  http://{ip}:{port}/")
        print(f"  - Tail IP admin: http://{ip}:{port}/admin")
        break
PY
  rm -f "$tmp"
}

print_access_summary() {
  echo ""
  echo "Access URLs:"
  echo "  - Local home:  http://127.0.0.1:$FRONTEND_PORT/"
  echo "  - Local admin: http://127.0.0.1:$FRONTEND_PORT/admin"
  tailscale_info_lines "$FRONTEND_PORT"
  echo ""
  echo "Logs: $LOG_DIR"
  echo "Stop:  bash scripts/dev.sh stop"
  echo "Status:bash scripts/dev.sh status"
}

start_backend() {
  if [[ -n "$(pids_on_port "$BACKEND_PORT" || true)" ]]; then
    print_port_status "backend" "$BACKEND_PORT"
    return 0
  fi

  echo "starting backend (go run, source mode) on 127.0.0.1:$BACKEND_PORT"
  echo "  log: $BACKEND_LOG"
  ( cd "$ROOT_DIR/backend" && nohup go run ./cmd/server >"$BACKEND_LOG" 2>&1 </dev/null & )
  wait_for_port "backend" "$BACKEND_PORT" "$BACKEND_LOG"
}

start_frontend() {
  if [[ -n "$(pids_on_port "$FRONTEND_PORT" || true)" ]]; then
    print_port_status "frontend" "$FRONTEND_PORT"
    return 0
  fi

  echo "starting frontend (npm run dev, vite HMR) on $FRONTEND_HOST:$FRONTEND_PORT"
  echo "  log: $FRONTEND_LOG"
  ( cd "$ROOT_DIR" && nohup npm run dev -- --host "$FRONTEND_HOST" --port "$FRONTEND_PORT" >"$FRONTEND_LOG" 2>&1 </dev/null & )
  wait_for_port "frontend" "$FRONTEND_PORT" "$FRONTEND_LOG"
}

status() {
  print_port_status "backend" "$BACKEND_PORT"
  print_port_status "frontend" "$FRONTEND_PORT"
  echo "logs: $LOG_DIR"
}

usage() {
  cat <<EOF
Usage: scripts/dev.sh [start|stop|restart|status]

Environment:
  FRONTEND_HOST=$FRONTEND_HOST
  FRONTEND_PORT=$FRONTEND_PORT
  BACKEND_PORT=$BACKEND_PORT
  LOG_DIR=$LOG_DIR

Why this exists:
  - start.sh uses ss/setsid (Linux only) and serves built dist (no HMR)
  - This script works on Linux + Windows Git Bash and runs vite HMR,
    so frontend source edits hot-reload in the browser automatically
EOF
}

main() {
  local action="${1:-start}"
  case "$action" in
    start)
      check_prereqs || exit 1
      start_backend
      start_frontend
      status
      print_access_summary
      ;;
    stop)
      stop_port "frontend" "$FRONTEND_PORT"
      stop_port "backend" "$BACKEND_PORT"
      ;;
    restart)
      stop_port "frontend" "$FRONTEND_PORT"
      stop_port "backend" "$BACKEND_PORT"
      check_prereqs || exit 1
      start_backend
      start_frontend
      status
      print_access_summary
      ;;
    status)   status ;;
    -h|--help|help) usage ;;
    *) usage >&2; exit 2 ;;
  esac
}

main "$@"
