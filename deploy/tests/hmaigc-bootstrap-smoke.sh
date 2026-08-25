#!/usr/bin/env bash

set -Eeuo pipefail

readonly REPOSITORY_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly OPS_IMAGE="${HMAIGC_OPS_TEST_IMAGE:-hmaigc-ops-controller:ci}"
readonly PYTHON_BIN="${PYTHON_BIN:-python3}"
readonly TEST_ROOT="$(mktemp -d)"
readonly STATE_VOLUME="hmaigc-bootstrap-smoke-$$"
cleanup() {
    docker volume rm "$STATE_VOLUME" >/dev/null 2>&1 || true
    rm -rf -- "$TEST_ROOT"
}
trap cleanup EXIT

docker_host_path() {
    if command -v cygpath >/dev/null 2>&1; then
        cygpath -w "$1"
    else
        printf '%s\n' "$1"
    fi
}

docker_run() {
    if command -v cygpath >/dev/null 2>&1; then
        MSYS_NO_PATHCONV=1 docker run "$@"
    else
        docker run "$@"
    fi
}

install -m 600 "$REPOSITORY_ROOT/deploy/tests/fixtures/production.env" "$TEST_ROOT/legacy.env"
"$PYTHON_BIN" - "$TEST_ROOT/controller.db" <<'PY'
import sqlite3
import sys

database = sqlite3.connect(sys.argv[1])
database.execute("""CREATE TABLE operation_records (
    id TEXT PRIMARY KEY, action TEXT, target_version TEXT, current_version_at_start TEXT,
    result_version TEXT, status TEXT, phase TEXT, error TEXT, exit_code INTEGER,
    actor_user_id TEXT, actor_display_name TEXT, idempotency_key TEXT, request_hash TEXT,
    created_at DATETIME, started_at DATETIME, completed_at DATETIME, updated_at DATETIME
)""")
database.commit()
database.close()
PY
host_test_root="$(docker_host_path "$TEST_ROOT")"
docker volume create "$STATE_VOLUME" >/dev/null
docker_run --rm --read-only \
    --tmpfs /tmp:rw,noexec,nosuid,size=64m \
    --volume "$host_test_root:/bootstrap" \
    --volume "$STATE_VOLUME:/var/lib/hmaigc-ops" \
    --entrypoint sh "$OPS_IMAGE" -c \
    'install -m 600 /bootstrap/legacy.env /tmp/legacy.env && exec hmaigc-ops-bootstrap import "$@"' _ \
    --source-env /tmp/legacy.env \
    --source-db /bootstrap/controller.db \
    --state-root /var/lib/hmaigc-ops \
    --controller-image example.invalid/hmaigc-ops-controller@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc \
    --controller-version v1.0.58 \
    --protocol-version 1 \
    --state-volume hmaigc-ops-state-fixture
before="$(docker_run --rm --read-only --volume "$STATE_VOLUME:/state:ro" --entrypoint sh "$OPS_IMAGE" -c \
    'sha256sum /state/config/production.env /state/config/control.env')"
docker_run --rm --read-only --volume "$STATE_VOLUME:/state:ro" --entrypoint sh "$OPS_IMAGE" \
    -c 'cat /state/config/control.env' >"$TEST_ROOT/control.env"

docker compose --env-file "$TEST_ROOT/control.env" \
    -f "$REPOSITORY_ROOT/deploy/docker-compose.ops.yml" config -q
docker_run --rm --read-only \
    --volume "$STATE_VOLUME:/var/lib/hmaigc-ops:ro" \
    --entrypoint hmaigc-ops-controller "$OPS_IMAGE" validate
after="$(docker_run --rm --read-only --volume "$STATE_VOLUME:/state:ro" --entrypoint sh "$OPS_IMAGE" -c \
    'sha256sum /state/config/production.env /state/config/control.env')"
[[ "$before" == "$after" ]] || {
    printf 'read-only controller validation changed canonical configuration\n' >&2
    exit 1
}

docker_run --rm --volume "$STATE_VOLUME:/state" --entrypoint sed "$OPS_IMAGE" -i \
    's#HMAIGC_OPS_IMAGE=.*#HMAIGC_OPS_IMAGE=example.invalid/hmaigc-ops-controller:v1.0.58#' \
    /state/config/control.env
if docker_run --rm --read-only \
    --volume "$STATE_VOLUME:/var/lib/hmaigc-ops:ro" \
    --entrypoint hmaigc-ops-controller "$OPS_IMAGE" validate; then
    printf 'controller validation accepted a mutable image tag\n' >&2
    exit 1
fi

(cd "$REPOSITORY_ROOT/backend" && go test ./internal/opsbootstrap ./internal/opsconfig -count=1)
printf 'hmaigc bootstrap smoke test passed\n'
