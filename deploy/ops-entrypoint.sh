#!/usr/bin/env sh

set -eu

SOURCE_ENV_FILE="${HMAIGC_OPS_SOURCE_ENV_FILE:-/run/hmaigc-config/.env.production}"
TARGET_ENV_FILE="${HMAIGC_ENV_FILE:-/var/lib/hmaigc-ops/deployment.env}"

if [ ! -f "$SOURCE_ENV_FILE" ]; then
    printf '错误：控制器生产配置源文件不存在：%s\n' "$SOURCE_ENV_FILE" >&2
    exit 1
fi

target_directory="$(dirname "$TARGET_ENV_FILE")"
mkdir -p "$target_directory"
install -m 600 "$SOURCE_ENV_FILE" "$TARGET_ENV_FILE"

exec "$@"
