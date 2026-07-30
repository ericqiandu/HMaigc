#!/usr/bin/env bash

set -Eeuo pipefail

readonly DEPLOY_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly DEFAULT_ENV_FILE="$DEPLOY_ROOT/.env.production"
readonly DEFAULT_COMPOSE_FILE="$DEPLOY_ROOT/docker-compose.production.yml"
readonly BACKUP_HELPER_IMAGE="alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce"

ENV_FILE="${HMAIGC_ENV_FILE:-$DEFAULT_ENV_FILE}"
COMPOSE_FILE="${HMAIGC_COMPOSE_FILE:-$DEFAULT_COMPOSE_FILE}"

log() {
    printf '%s %s\n' "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" "$*" >&2
}

fail() {
    log "错误：$*" >&2
    exit 1
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || fail "缺少命令：$1"
}

env_value() {
    local key="$1"
    awk -v key="$key" '
        BEGIN { FS = "=" }
        /^[[:space:]]*#/ { next }
        $1 == key {
            sub(/^[^=]*=/, "")
            sub(/\r$/, "")
            if (($0 ~ /^".*"$/) || ($0 ~ /^\047.*\047$/)) {
                $0 = substr($0, 2, length($0) - 2)
            }
            print
            exit
        }
    ' "$ENV_FILE"
}

resolve_from_root() {
    local value="$1"
    case "$value" in
        /*) printf '%s\n' "$value" ;;
        *) printf '%s/%s\n' "$DEPLOY_ROOT" "$value" ;;
    esac
}

validate_release_version() {
    local version="$1"
    [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] ||
        fail "发布版本必须是不可变标签（例如 v1.0.10），禁止使用 latest 或分支名：$version"
}

configure_deploy_runtime() {
    local version="$1"
    [[ -f "$ENV_FILE" ]] || fail "生产配置不存在：$ENV_FILE，请先复制 .env.production.example"
    [[ -f "$COMPOSE_FILE" ]] || fail "生产 Compose 不存在：$COMPOSE_FILE"

    validate_release_version "$version"
    export HMAIGC_VERSION="$version"
    export HMAIGC_IMAGE_REGISTRY="${HMAIGC_IMAGE_REGISTRY:-$(env_value HMAIGC_IMAGE_REGISTRY)}"
    export HMAIGC_COMPOSE_PROJECT_NAME="${HMAIGC_COMPOSE_PROJECT_NAME:-$(env_value HMAIGC_COMPOSE_PROJECT_NAME)}"
    export HMAIGC_BACKEND_DATA_VOLUME="${HMAIGC_BACKEND_DATA_VOLUME:-$(env_value HMAIGC_BACKEND_DATA_VOLUME)}"
    export HMAIGC_POSTGRES_DATA_VOLUME="${HMAIGC_POSTGRES_DATA_VOLUME:-$(env_value HMAIGC_POSTGRES_DATA_VOLUME)}"
    export HMAIGC_REDIS_DATA_VOLUME="${HMAIGC_REDIS_DATA_VOLUME:-$(env_value HMAIGC_REDIS_DATA_VOLUME)}"

    : "${HMAIGC_IMAGE_REGISTRY:?生产配置必须填写 HMAIGC_IMAGE_REGISTRY}"
    HMAIGC_COMPOSE_PROJECT_NAME="${HMAIGC_COMPOSE_PROJECT_NAME:-hmaigc}"
    HMAIGC_BACKEND_DATA_VOLUME="${HMAIGC_BACKEND_DATA_VOLUME:-hmaigc-backend-data}"
    HMAIGC_POSTGRES_DATA_VOLUME="${HMAIGC_POSTGRES_DATA_VOLUME:-hmaigc-postgres-data}"
    HMAIGC_REDIS_DATA_VOLUME="${HMAIGC_REDIS_DATA_VOLUME:-hmaigc-redis-data}"
    export HMAIGC_COMPOSE_PROJECT_NAME HMAIGC_BACKEND_DATA_VOLUME HMAIGC_POSTGRES_DATA_VOLUME HMAIGC_REDIS_DATA_VOLUME

    local configured_state configured_backups
    configured_state="${HMAIGC_STATE_DIR:-$(env_value HMAIGC_STATE_DIR)}"
    configured_backups="${HMAIGC_BACKUP_DIR:-$(env_value HMAIGC_BACKUP_DIR)}"
    STATE_DIR="$(resolve_from_root "${configured_state:-.deploy}")"
    BACKUP_DIR="$(resolve_from_root "${configured_backups:-.deploy/backups}")"
    STATE_FILE="$STATE_DIR/release.env"
    LOCK_FILE="$STATE_DIR/deploy.lock"
    LOG_DIR="$STATE_DIR/logs"
    mkdir -p "$STATE_DIR" "$BACKUP_DIR" "$LOG_DIR"
    chmod 700 "$STATE_DIR" "$BACKUP_DIR" "$LOG_DIR"

    COMPOSE=(docker compose --project-name "$HMAIGC_COMPOSE_PROJECT_NAME" --env-file "$ENV_FILE" -f "$COMPOSE_FILE")
}

compose() {
    "${COMPOSE[@]}" "$@"
}

acquire_deploy_lock() {
    exec 9>"$LOCK_FILE"
    flock -n 9 || fail "已有安装、升级、备份或回滚任务正在执行"
}

state_value() {
    local key="$1"
    [[ -f "$STATE_FILE" ]] || return 0
    awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print; exit }' "$STATE_FILE"
}

write_release_state() {
    local current="$1"
    local previous="$2"
    local rollback_backup="$3"
    local temporary="$STATE_FILE.tmp"
    {
        printf 'CURRENT_VERSION=%s\n' "$current"
        printf 'PREVIOUS_VERSION=%s\n' "$previous"
        printf 'ROLLBACK_BACKUP=%s\n' "$rollback_backup"
        printf 'UPDATED_AT=%s\n' "$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
    } >"$temporary"
    chmod 600 "$temporary"
    mv -f "$temporary" "$STATE_FILE"
}

preflight() {
    local command
    for command in docker curl awk grep sed sha256sum tar flock tee stat; do
        require_command "$command"
    done
    if [[ "$(uname -s)" == "Linux" ]]; then
        local env_mode
        env_mode="$(stat -c '%a' "$ENV_FILE")"
        (( (8#$env_mode & 077) == 0 )) ||
            fail "生产配置权限必须是 600（当前 $env_mode）：$ENV_FILE"
    fi
    docker info >/dev/null 2>&1 || fail "Docker Engine 不可用"
    docker compose version >/dev/null 2>&1 || fail "Docker Compose 插件不可用"
    compose config -q
}

start_operation_log() {
    local operation="$1"
    local log_file="$LOG_DIR/$(date -u +'%Y%m%d-%H%M%S')--${operation}.log"
    exec > >(tee -a "$log_file") 2>&1
    log "操作日志：$log_file"
}

pull_release() {
    local version="$1"
    validate_release_version "$version"
    export HMAIGC_VERSION="$version"
    log "拉取发布镜像：$version"
    compose pull backend web
}

start_infrastructure() {
    log "启动 PostgreSQL 与 Redis"
    compose up -d postgres redis --wait
}

stop_application() {
    log "停止 Web 与后端写入"
    compose stop web backend >/dev/null 2>&1 || true
}

backend_health_json() {
    compose exec -T backend wget -qO- http://127.0.0.1:8080/api/health
}

health_version() {
    sed -n 's/.*"version":"\([^"]*\)".*/\1/p'
}

public_base_url() {
    local host port
    host="${CANVAS_HTTP_HOST:-$(env_value CANVAS_HTTP_HOST)}"
    port="${CANVAS_HTTP_PORT:-$(env_value CANVAS_HTTP_PORT)}"
    host="${host:-127.0.0.1}"
    port="${port:-3000}"
    if [[ "$host" == "0.0.0.0" || "$host" == "::" ]]; then
        host="127.0.0.1"
    fi
    printf 'http://%s:%s\n' "$host" "$port"
}

verify_backend_release() {
    local expected="$1"
    local response actual
    response="$(backend_health_json)" || return 1
    actual="$(printf '%s' "$response" | health_version)"
    [[ "$actual" == "$expected" ]] || {
        log "后端运行版本不匹配：期望 $expected，实际 ${actual:-未返回}"
        return 1
    }
}

verify_public_release() {
    local expected="$1"
    local base response actual
    base="$(public_base_url)"
    response="$(curl --fail --silent --show-error --max-time 10 "$base/api/health")" || return 1
    actual="$(printf '%s' "$response" | health_version)"
    [[ "$actual" == "$expected" ]] || {
        log "公网入口版本不匹配：期望 $expected，实际 ${actual:-未返回}"
        return 1
    }
    curl --fail --silent --show-error --max-time 10 "$base/canvas/" |
        grep -Fq '<div id="root"></div>'
}

verify_running_release() {
    local expected="$1"
    export HMAIGC_VERSION="$expected"
    log "核对当前运行版本：$expected"
    verify_backend_release "$expected" || return 1
    verify_public_release "$expected"
}

start_release() {
    local version="$1"
    export HMAIGC_VERSION="$version"
    start_infrastructure
    log "启动并验活后端：$version"
    compose up -d backend --wait
    verify_backend_release "$version" || return 1
    log "启动并验活 Web：$version"
    compose up -d web --wait
    verify_public_release "$version"
}
