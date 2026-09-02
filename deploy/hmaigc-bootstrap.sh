#!/usr/bin/env bash

set -Eeuo pipefail

SOURCE_ENV=""
SOURCE_DB=""
STATE_VOLUME=""
CONTROLLER_IMAGE=""
CONTROLLER_VERSION=""
PROTOCOL_VERSION=""

fail() {
    printf '错误：%s\n' "$*" >&2
    exit 1
}

while (($#)); do
    case "$1" in
        --source-env) SOURCE_ENV="${2:-}" ;;
        --source-db) SOURCE_DB="${2:-}" ;;
        --state-volume) STATE_VOLUME="${2:-}" ;;
        --controller-image) CONTROLLER_IMAGE="${2:-}" ;;
        --controller-version) CONTROLLER_VERSION="${2:-}" ;;
        --protocol-version) PROTOCOL_VERSION="${2:-}" ;;
        --help)
            printf 'usage: %s --source-env PATH --source-db PATH --state-volume NAME --controller-image IMAGE@sha256:DIGEST --controller-version VERSION --protocol-version 1\n' "$0"
            exit 0
            ;;
        *) fail "未知 bootstrap 参数：$1" ;;
    esac
    (($# >= 2)) || fail "bootstrap 参数缺少值：$1"
    shift 2
done

[[ -f "$SOURCE_ENV" ]] || fail "旧生产配置不存在：$SOURCE_ENV"
[[ -f "$SOURCE_DB" ]] || fail "旧控制器数据库不存在：$SOURCE_DB"
[[ -n "$STATE_VOLUME" ]] || fail "必须明确指定 ops-state 卷"
[[ "$CONTROLLER_IMAGE" =~ ^.+@sha256:[0-9a-f]{64}$ ]] || fail "目标控制器必须使用不可变摘要"
[[ "$CONTROLLER_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || fail "目标控制器版本无效"
[[ "$PROTOCOL_VERSION" == "1" ]] || fail "当前仅支持控制协议版本 1"

command -v docker >/dev/null 2>&1 || fail "缺少 docker"
docker info >/dev/null 2>&1 || fail "Docker Engine 不可用"
docker compose version >/dev/null 2>&1 || fail "Docker Compose 插件不可用"
if [[ -n "$(docker ps --quiet --filter label=com.docker.compose.service=ops-controller)" ]]; then
    fail "检测到旧控制器仍在运行；请先停止旧控制器，再执行一次性迁移"
fi
docker volume inspect "$STATE_VOLUME" >/dev/null 2>&1 || docker volume create "$STATE_VOLUME" >/dev/null

source_env_directory="$(cd "$(dirname "$SOURCE_ENV")" && pwd)"
source_db_directory="$(cd "$(dirname "$SOURCE_DB")" && pwd)"
source_env_name="$(basename "$SOURCE_ENV")"
source_db_name="$(basename "$SOURCE_DB")"

docker pull "$CONTROLLER_IMAGE" >/dev/null
docker run --rm --read-only \
    --security-opt no-new-privileges \
    --tmpfs /tmp:rw,noexec,nosuid,size=64m \
    --volume "$source_env_directory:/bootstrap/env:ro" \
    --volume "$source_db_directory:/bootstrap/db:ro" \
    --volume "$STATE_VOLUME:/var/lib/hmaigc-ops" \
    --entrypoint hmaigc-ops-bootstrap \
    "$CONTROLLER_IMAGE" import \
    --source-env "/bootstrap/env/$source_env_name" \
    --source-db "/bootstrap/db/$source_db_name" \
    --state-root /var/lib/hmaigc-ops \
    --controller-image "$CONTROLLER_IMAGE" \
    --controller-version "$CONTROLLER_VERSION" \
    --protocol-version "$PROTOCOL_VERSION" \
    --state-volume "$STATE_VOLUME"

HMAIGC_OPS_STATE_VOLUME="$STATE_VOLUME" "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/hmaigc-ops.sh" status
