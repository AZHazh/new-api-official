#!/usr/bin/env bash
# =============================================================================
# New-API Official Version Deployment Script
#
# 基于天豆版部署脚本改造,适配官方 calciumion/new-api 镜像
# 部署到 8080 端口,避免和旧版 3000 端口冲突
# =============================================================================
set -Eeuo pipefail

usage() {
  cat <<'EOF'
用法:
  deploy-official-new-api.sh <tag>

示例:
  deploy-official-new-api.sh latest
  deploy-official-new-api.sh v1.7.0

环境变量:
  APP_DIR       默认: /opt/new-api-official
  SERVICE       默认: new-api-official
  CONTAINER     默认: new-api-official-app
  IMAGE_REPO    默认: calciumion/new-api
  DB_PATH       默认: /opt/new-api-official/data/new-api.db
  PORT          默认: 8080
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

TAG="${1:-}"
if [[ -z "$TAG" ]]; then
  usage
  exit 1
fi

APP_DIR="${APP_DIR:-/opt/new-api-official}"
SERVICE="${SERVICE:-new-api-official}"
CONTAINER="${CONTAINER:-new-api-official-app}"
IMAGE_REPO="${IMAGE_REPO:-calciumion/new-api}"
IMAGE="${IMAGE_REPO}:${TAG}"
PORT="${PORT:-8080}"
COMPOSE_DEPLOY="${APP_DIR}/docker-compose.deploy.yml"
COMPOSE_OVERRIDE="${APP_DIR}/docker-compose.override.yml"
DB_PATH="${DB_PATH:-${APP_DIR}/data/new-api.db}"
BACKUP_TIME="$(date +%F-%H%M%S)"
BACKUP_DIR="${APP_DIR}/backups/new-api-backup-${BACKUP_TIME}-${TAG}"

log() {
  printf '[deploy-official] %s\n' "$*"
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf '缺少必需命令: %s\n' "$1" >&2
    exit 1
  fi
}

require_cmd docker
require_cmd sqlite3
require_cmd curl

# 首次部署时初始化目录和配置
if [[ ! -d "$APP_DIR" ]]; then
  log "首次部署,创建目录: $APP_DIR"
  mkdir -p "$APP_DIR"/{data,logs,backups}

  log "生成 docker-compose.deploy.yml"
  cat > "$COMPOSE_DEPLOY" <<DEPLOYEOF
services:
  ${SERVICE}:
    image: ${IMAGE}
    container_name: ${CONTAINER}
    restart: always
    command: --log-dir /app/logs
    ports:
      - "${PORT}:3000"
    volumes:
      - ${APP_DIR}/data:/data
      - ${APP_DIR}/logs:/app/logs
    environment:
      - SQLITE_PATH=/data/new-api.db
      - TZ=Asia/Shanghai
      - ERROR_LOG_ENABLED=true
      - BATCH_UPDATE_ENABLED=true
      - NODE_NAME=${SERVICE}
DEPLOYEOF

  log "初始化完成。请检查 $COMPOSE_DEPLOY 并根据需要调整环境变量"
  log "如需 Redis/PostgreSQL,请手动添加到 compose 文件"
fi

if [[ ! -f "$COMPOSE_DEPLOY" ]]; then
  printf '缺少 compose 文件: %s\n' "$COMPOSE_DEPLOY" >&2
  printf '请先运行脚本进行首次初始化\n' >&2
  exit 1
fi

cd "$APP_DIR"

log "应用目录: $APP_DIR"
log "目标镜像: $IMAGE"
log "监听端口: $PORT"
log "备份目录: $BACKUP_DIR"

mkdir -p "$BACKUP_DIR"

log "检查磁盘空间"
du -sh "${APP_DIR}/data" 2>/dev/null || true
df -h "$APP_DIR" || true

if [[ -f "$DB_PATH" ]]; then
  log "备份 SQLite 数据库 (sqlite3 .backup)"
  sqlite3 "$DB_PATH" ".backup '$BACKUP_DIR/new-api.db'"
else
  log "数据库文件不存在,跳过备份 (首次部署或使用外部数据库)"
fi

log "备份 compose 文件和日志"
cp -a "$COMPOSE_DEPLOY" "$BACKUP_DIR/"
cp -a "$COMPOSE_OVERRIDE" "$BACKUP_DIR/" 2>/dev/null || true
cp -a "${APP_DIR}/logs" "$BACKUP_DIR/" 2>/dev/null || true

log "写入 compose override"
cat > "$COMPOSE_OVERRIDE" <<EOF
services:
  ${SERVICE}:
    image: ${IMAGE}
EOF

log "拉取镜像"
docker compose \
  -f "$COMPOSE_DEPLOY" \
  -f "$COMPOSE_OVERRIDE" \
  pull "$SERVICE"

log "重新创建服务"
docker compose \
  -f "$COMPOSE_DEPLOY" \
  -f "$COMPOSE_OVERRIDE" \
  up -d --force-recreate "$SERVICE"

log "等待容器启动"
sleep 5

log "容器状态"
docker ps --filter "name=${CONTAINER}" --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'

RUNNING_IMAGE="$(docker inspect "$CONTAINER" --format '{{.Config.Image}}' 2>/dev/null || true)"
if [[ "$RUNNING_IMAGE" != "$IMAGE" ]]; then
  printf '运行中的镜像不符合预期。期望 %s, 实际 %s\n' "$IMAGE" "${RUNNING_IMAGE:-<none>}" >&2
  exit 1
fi

log "挂载点"
docker inspect "$CONTAINER" --format '{{range .Mounts}}{{println .Source "->" .Destination}}{{end}}'

log "最近日志"
docker logs --tail=80 "$CONTAINER"

log "本地 HTTP 检查"
if ! curl -fsSI "http://127.0.0.1:${PORT}" >/tmp/new-api-official-http-check.txt 2>&1; then
  cat /tmp/new-api-official-http-check.txt 2>/dev/null || true
  printf 'HTTP 检查失败: http://127.0.0.1:%s\n' "$PORT" >&2
  exit 1
fi
cat /tmp/new-api-official-http-check.txt

log "部署完成"
log "备份已保存到: $BACKUP_DIR"
log "访问地址: http://localhost:${PORT}"
log ""
log "如需切换流量,请修改 nginx upstream 配置:"
log "  upstream new_api {"
log "    server 127.0.0.1:${PORT};  # 新版"
log "    # server 127.0.0.1:3000;  # 旧版(注释掉)"
log "  }"
