#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="${HMAIGC_ENV_FILE:-$ROOT_DIR/.env.production}"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.ops.yml"
export HMAIGC_HOST_ENV_FILE="$ENV_FILE"

fail() {
    printf '错误：%s\n' "$*" >&2
    exit 1
}

[[ -f "$ENV_FILE" ]] || fail "生产配置不存在：$ENV_FILE"
[[ -f "$COMPOSE_FILE" ]] || fail "运维控制器 Compose 不存在：$COMPOSE_FILE"
command -v docker >/dev/null 2>&1 || fail "缺少 docker"
docker info >/dev/null 2>&1 || fail "Docker Engine 不可用"
docker compose version >/dev/null 2>&1 || fail "Docker Compose 插件不可用"

COMPOSE=(docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE")
"${COMPOSE[@]}" config -q
"${COMPOSE[@]}" pull ops-controller
"${COMPOSE[@]}" up -d ops-controller --wait
"${COMPOSE[@]}" exec -T ops-controller hmaigc-opsctl "$@"
