#!/usr/bin/env bash

set -Eeuo pipefail

readonly CONFIG_READER_IMAGE="alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce"
STATE_VOLUME="${HMAIGC_OPS_STATE_VOLUME:-hmaigc-ops-state}"

fail() {
    printf '错误：%s\n' "$*" >&2
    exit 1
}

require_immutable_image() {
    [[ "$1" =~ ^.+@sha256:[0-9a-f]{64}$ ]] || fail "控制器镜像不是不可变摘要：${1:-未配置}"
}

command -v docker >/dev/null 2>&1 || fail "缺少 docker"
docker info >/dev/null 2>&1 || fail "Docker Engine 不可用"
docker compose version >/dev/null 2>&1 || fail "Docker Compose 插件不可用"
docker volume inspect "$STATE_VOLUME" >/dev/null 2>&1 || fail "ops-state 卷不存在：$STATE_VOLUME"

temporary_directory="$(mktemp -d)"
trap 'rm -rf -- "$temporary_directory"' EXIT
control_env="$temporary_directory/control.env"
compose_file="$temporary_directory/docker-compose.ops.yml"
docker run --rm --read-only --volume "$STATE_VOLUME:/state:ro" \
    --entrypoint sh "$CONFIG_READER_IMAGE" -c 'cat /state/config/control.env' >"$control_env"
chmod 600 "$control_env"
ops_image="$(awk -F= '$1 == "HMAIGC_OPS_IMAGE" { sub(/^[^=]*=/, ""); print; exit }' "$control_env")"
require_immutable_image "$ops_image"
docker run --rm --read-only --entrypoint sh "$ops_image" \
    -c 'cat /opt/hmaigc/deploy/docker-compose.ops.yml' >"$compose_file"

project_name="$(awk -F= '$1 == "HMAIGC_OPS_COMPOSE_PROJECT_NAME" { sub(/^[^=]*=/, ""); print; exit }' "$control_env")"
project_name="${project_name:-hmaigc-ops}"
compose=(docker compose --project-name "$project_name" --env-file "$control_env" -f "$compose_file")
"${compose[@]}" config -q
"${compose[@]}" pull ops-controller
"${compose[@]}" up -d ops-controller --wait
"${compose[@]}" exec -T ops-controller hmaigc-opsctl "$@"
