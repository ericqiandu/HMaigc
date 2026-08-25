#!/usr/bin/env bash

set -Eeuo pipefail

readonly TEST_ROOT="$(mktemp -d)"
trap 'rm -rf -- "$TEST_ROOT"' EXIT
mkdir -p "$TEST_ROOT/bin" "$TEST_ROOT/state" "$TEST_ROOT/backups"

cat >"$TEST_ROOT/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail

arguments=" $* "
case "$arguments" in
    *" info "* | *" compose version "* | *" compose "*" config -q "* | *" compose "*" pull "* | *" compose "*" up "* | *" compose "*" stop "* | *" volume inspect "* | *" pull alpine:"*)
        exit 0
        ;;
    *" compose "*" exec -T backend wget "*)
        if [[ "${HMAIGC_VERSION:-}" == "${FAKE_FAIL_VERSION:-never}" ]]; then
            printf '{"code":0,"data":{"status":"ok","version":"broken","commit":"test"},"msg":"ok"}'
        else
            printf '{"code":0,"data":{"status":"ok","version":"%s","commit":"test"},"msg":"ok"}' "$HMAIGC_VERSION"
        fi
        ;;
    *" compose "*" exec -T web wget "*"api/health"*)
        if [[ "${HMAIGC_VERSION:-}" == "${FAKE_FAIL_VERSION:-never}" ]]; then
            printf '{"code":0,"data":{"status":"ok","version":"broken","commit":"test"},"msg":"ok"}'
        else
            printf '{"code":0,"data":{"status":"ok","version":"%s","commit":"test"},"msg":"ok"}' "$HMAIGC_VERSION"
        fi
        ;;
    *" compose "*" exec -T web wget "*"canvas/"*)
        if [[ "${FAKE_EXTERNAL_WEB_ASSETS:-false}" == "true" ]]; then
            printf '<html><head><link rel="modulepreload" href="https://static.example.invalid/releases/v1.0.10/assets/lazy.js"><link rel="stylesheet" href="https://static.example.invalid/releases/v1.0.10/assets/app.css"></head><body><div id="root"></div><script type="module" src="https://static.example.invalid/releases/v1.0.10/assets/app.js"></script></body></html>'
        else
            printf '<html><head><link rel="stylesheet" href="/assets/app.css"></head><body><div id="root"></div><script type="module" src="/assets/app.js"></script></body></html>'
        fi
        ;;
    *" compose "*" exec -T web wget "*"/assets/app.js"*)
        [[ "${FAKE_MISSING_WEB_ASSET:-false}" != "true" ]] || exit 8
        printf 'console.log("ready")'
        ;;
    *" compose "*" exec -T web wget "*"/assets/app.css"*)
        printf 'body { color: black; }'
        ;;
    *" compose "*" exec -T postgres "*"pg_dump"*)
        printf 'fake-postgres-dump'
        ;;
    *" compose "*" exec -T postgres "*"pg_restore --list"*)
        cat >/dev/null
        ;;
    *" compose "*" exec -T postgres "*)
        cat >/dev/null || true
        ;;
    *" compose "*" ps "*)
        printf 'fake services healthy\n'
        ;;
    *" compose "*" logs "*)
        printf 'fake service logs\n'
        ;;
    *" run "*"tar -czf "*"backend-data.tgz"*)
        for argument in "$@"; do
            case "$argument" in
                BACKUP_RELATIVE=*)
                    backup_relative="${argument#BACKUP_RELATIVE=}"
                    printf 'fake-backend-data' >"$HMAIGC_OPS_STATE_DIR/$backup_relative/backend-data.tgz"
                    ;;
            esac
        done
        ;;
    *" run "*"tar -xzf "*"backend-data.tgz"*)
        exit 0
        ;;
    *)
        printf 'unexpected docker invocation: %s\n' "$*" >&2
        exit 1
        ;;
esac
EOF
chmod +x "$TEST_ROOT/bin/docker"

cat >"$TEST_ROOT/bin/curl" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "$*" == *"static.example.invalid"* ]]; then
    [[ "$*" == *"--connect-timeout 5"* ]] || {
        printf 'remote entry asset request has no bounded connect timeout: %s\n' "$*" >&2
        exit 64
    }
    [[ "$*" == *"--max-time 15"* ]] || {
        printf 'remote entry asset request has no bounded transfer timeout: %s\n' "$*" >&2
        exit 65
    }
    [[ "$*" != *"--retry-all-errors"* ]] || {
        printf 'remote entry asset request retries permanent errors: %s\n' "$*" >&2
        exit 66
    }
    [[ "$*" != *"lazy.js"* ]] || {
        printf 'deployment verification fetched a non-entry preload: %s\n' "$*" >&2
        exit 67
    }
    [[ "${FAKE_CDN_ENTRY_FAILURE:-false}" != "true" ]] || exit 28
    exit 0
fi
if [[ "$*" == *"/api/health"* ]]; then
    if [[ "${HMAIGC_VERSION:-}" == "${FAKE_FAIL_VERSION:-never}" ]]; then
        printf '{"code":0,"data":{"status":"ok","version":"broken","commit":"test"},"msg":"ok"}'
    else
        printf '{"code":0,"data":{"status":"ok","version":"%s","commit":"test"},"msg":"ok"}' "$HMAIGC_VERSION"
    fi
else
    printf '<html><body><div id="root"></div></body></html>'
fi
EOF
chmod +x "$TEST_ROOT/bin/curl"

cat >"$TEST_ROOT/bin/flock" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$TEST_ROOT/bin/flock"

cat >"$TEST_ROOT/.env.production" <<EOF
HMAIGC_IMAGE_REGISTRY=ghcr.io/example
HMAIGC_VERSION=v1.0.10
HMAIGC_COMPOSE_PROJECT_NAME=hmaigc-test
HMAIGC_BACKEND_DATA_VOLUME=hmaigc-test-backend
HMAIGC_POSTGRES_DATA_VOLUME=hmaigc-test-postgres
HMAIGC_REDIS_DATA_VOLUME=hmaigc-test-redis
HMAIGC_OPS_STATE_VOLUME=hmaigc-test-ops
HMAIGC_OPS_STATE_DIR=$TEST_ROOT
POSTGRES_DB=hmaigc
POSTGRES_USER=hmaigc
POSTGRES_PASSWORD=test-only
CANVAS_ENVIRONMENT=production
CANVAS_CORS_ORIGINS=https://test.example.invalid
CANVAS_HTTP_HOST=127.0.0.1
CANVAS_HTTP_PORT=3000
HMAIGC_STATE_DIR=$TEST_ROOT/state
HMAIGC_BACKUP_DIR=$TEST_ROOT/backups
HMAIGC_BACKUP_RETENTION=0
EOF
chmod 600 "$TEST_ROOT/.env.production"

readonly REPOSITORY_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly DEPLOY_COMMAND="$REPOSITORY_ROOT/deploy/hmaigc.sh"
readonly TEST_ENV=(
    "PATH=$TEST_ROOT/bin:$PATH"
    "HMAIGC_ENV_FILE=$TEST_ROOT/.env.production"
    "HMAIGC_COMPOSE_FILE=$REPOSITORY_ROOT/docker-compose.production.yml"
)

run_deploy() {
    env "${TEST_ENV[@]}" "$DEPLOY_COMMAND" "$@"
}

assert_state() {
    local key="$1"
    local expected="$2"
    local actual
    actual="$(awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print; exit }' "$TEST_ROOT/state/release.env")"
    [[ "$actual" == "$expected" ]] || {
        printf 'state %s: expected %s, got %s\n' "$key" "$expected" "$actual" >&2
        exit 1
    }
}

run_deploy install v1.0.10
assert_state CURRENT_VERSION v1.0.10

run_deploy upgrade v1.0.11
assert_state CURRENT_VERSION v1.0.11
assert_state PREVIOUS_VERSION v1.0.10

run_deploy rollback
assert_state CURRENT_VERSION v1.0.10
assert_state PREVIOUS_VERSION v1.0.11

if env "${TEST_ENV[@]}" FAKE_FAIL_VERSION=v1.0.12 "$DEPLOY_COMMAND" upgrade v1.0.12; then
    printf 'upgrade to intentionally broken version unexpectedly succeeded\n' >&2
    exit 1
fi
assert_state CURRENT_VERSION v1.0.10

if env "${TEST_ENV[@]}" FAKE_FAIL_VERSION=v1.0.10 "$DEPLOY_COMMAND" upgrade v1.0.13; then
    printf 'upgrade unexpectedly continued while current release verification failed\n' >&2
    exit 1
fi
assert_state CURRENT_VERSION v1.0.10

run_deploy verify

env "${TEST_ENV[@]}" FAKE_EXTERNAL_WEB_ASSETS=true "$DEPLOY_COMMAND" verify

if env "${TEST_ENV[@]}" FAKE_EXTERNAL_WEB_ASSETS=true FAKE_CDN_ENTRY_FAILURE=true "$DEPLOY_COMMAND" verify; then
    printf 'verification unexpectedly accepted an unreachable external entry asset\n' >&2
    exit 1
fi

if env "${TEST_ENV[@]}" FAKE_MISSING_WEB_ASSET=true "$DEPLOY_COMMAND" verify; then
    printf 'verification unexpectedly accepted a release with a missing web entry asset\n' >&2
    exit 1
fi

run_deploy status
printf 'hmaigc deploy smoke test passed\n'
