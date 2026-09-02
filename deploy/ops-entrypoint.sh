#!/usr/bin/env sh

set -eu

PRODUCTION_ENV_FILE="${HMAIGC_ENV_FILE:-/var/lib/hmaigc-ops/config/production.env}"
CONTROL_ENV_FILE="${HMAIGC_CONTROL_ENV_FILE:-/var/lib/hmaigc-ops/config/control.env}"

for config_file in "$PRODUCTION_ENV_FILE" "$CONTROL_ENV_FILE"; do
    if [ ! -r "$config_file" ]; then
        printf '错误：规范配置不可读：%s\n' "$config_file" >&2
        exit 1
    fi
done

exec "$@"
