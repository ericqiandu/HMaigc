#!/usr/bin/env bash

set -Eeuo pipefail

readonly TEST_ROOT="$(mktemp -d)"
trap 'rm -rf -- "$TEST_ROOT"' EXIT
mkdir -p "$TEST_ROOT/bin" "$TEST_ROOT/state" "$TEST_ROOT/backups"
touch "$TEST_ROOT/docker.log"
touch "$TEST_ROOT/curl.log"

cat >"$TEST_ROOT/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf '%s\n' "$*" >>"${FAKE_DOCKER_LOG:?}"
arguments=" $* "
case "$arguments" in
    *" pull ghcr.io/example/hmaigc-backend:v1.0.58 "*) exit 0 ;;
    *" pull ghcr.io/example/hmaigc-web:v1.0.58 "*) exit 0 ;;
    *" image inspect ghcr.io/example/hmaigc-backend:v1.0.58 "*)
        printf 'ghcr.io/example/hmaigc-backend@sha256:%064d\n' 1
        ;;
    *" image inspect ghcr.io/example/hmaigc-web:v1.0.58 "*)
        printf 'ghcr.io/example/hmaigc-web@sha256:%064d\n' 2
        ;;
    *" pull ghcr.io/example/hmaigc-ops-controller@sha256:"*) exit 0 ;;
    *" run "*"hmaigc-ops-controller@sha256:"*" validate "*)
        [[ "${FAKE_CANDIDATE_HEALTH_FAILURE:-false}" != "true" ]]
        ;;
    *" info "* | *" compose version "* | *" compose "*" config -q "* | *" compose "*" stop web backend "*) exit 0 ;;
    *" compose "*" run --rm --no-deps "*"hmaigc-agent-runtime-retirement-audit"*)
        printf '{"candidateRuns":0,"retirableRuns":0,"blockers":[]}\n'
        ;;
    *)
        printf 'unexpected docker invocation: %s\n' "$*" >&2
        exit 90
        ;;
esac
EOF
chmod +x "$TEST_ROOT/bin/docker"

cat >"$TEST_ROOT/bin/flock" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$TEST_ROOT/bin/flock"

cat >"$TEST_ROOT/bin/jq" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ " $* " == *" -e "* ]]; then
    exit 0
fi
if [[ " $* " == *" -j "* ]]; then
    printf 'assets/app.js\0assets/app.css\0'
    exit 0
fi
exit 2
EOF
chmod +x "$TEST_ROOT/bin/jq"

cat >"$TEST_ROOT/bin/curl" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
output_file=""
url=""
while (($#)); do
    case "$1" in
        --output)
            output_file="$2"
            shift 2
            ;;
        http://* | https://*)
            url="$1"
            shift
            ;;
        *) shift ;;
    esac
done
printf '%s\n' "$url" >>"${FAKE_CURL_LOG:?}"
if [[ "$url" == */manifest.json ]]; then
    printf '{"release":"v1.0.57","files":[{"path":"assets/app.js"},{"path":"assets/app.css"}]}' >"$output_file"
    exit 0
fi
if [[ "${FAKE_PUBLIC_APP_FAILURE:-false}" == "true" && "$url" == "https://app.example.invalid/canvas/" ]]; then
    exit 22
fi
if [[ "${FAKE_PUBLIC_CDN_HANG:-false}" == "true" ]]; then
    sleep 30
fi
if [[ "${FAKE_PUBLIC_CDN_FAILURE:-false}" == "true" && "$url" == */assets/app.css ]]; then
    exit 28
fi
if [[ -n "$output_file" ]]; then
    if [[ "$url" == "https://app.example.invalid/canvas/" ]]; then
        printf '<main id="root"></main>' >"$output_file"
    else
        printf 'asset' >"$output_file"
    fi
fi
EOF
chmod +x "$TEST_ROOT/bin/curl"

cat >"$TEST_ROOT/.env.production" <<EOF
HMAIGC_IMAGE_REGISTRY=ghcr.io/example
HMAIGC_VERSION=v1.0.57
HMAIGC_BACKEND_IMAGE=ghcr.io/example/hmaigc-backend@sha256:$(printf '%064d' 7)
HMAIGC_WEB_IMAGE=ghcr.io/example/hmaigc-web@sha256:$(printf '%064d' 8)
BACKUP_HELPER_IMAGE=alpine@sha256:$(printf '%064d' 6)
HMAIGC_COMPOSE_PROJECT_NAME=hmaigc-stage-test
HMAIGC_BACKEND_DATA_VOLUME=hmaigc-stage-backend
HMAIGC_POSTGRES_DATA_VOLUME=hmaigc-stage-postgres
HMAIGC_REDIS_DATA_VOLUME=hmaigc-stage-redis
HMAIGC_OPS_STATE_VOLUME=hmaigc-stage-ops
HMAIGC_OPS_STATE_DIR=$TEST_ROOT
HMAIGC_STATE_DIR=$TEST_ROOT/state
HMAIGC_BACKUP_DIR=$TEST_ROOT/backups
HMAIGC_STATIC_ASSET_BASE_URL=https://static.example.invalid/hmaigc/web
POSTGRES_DB=hmaigc
POSTGRES_USER=hmaigc
POSTGRES_PASSWORD=test-only
CANVAS_ENVIRONMENT=production
CANVAS_CORS_ORIGINS=https://app.example.invalid
EOF
chmod 600 "$TEST_ROOT/.env.production"
mkdir -p "$TEST_ROOT/config"
cat >"$TEST_ROOT/config/control.env" <<EOF
HMAIGC_OPS_IMAGE=ghcr.io/example/hmaigc-ops-controller@sha256:$(printf '%064d' 9)
HMAIGC_OPS_VERSION=v1.0.57
HMAIGC_OPS_PROTOCOL_VERSION=1
HMAIGC_OPS_COMPOSE_PROJECT_NAME=hmaigc-stage-ops
HMAIGC_OPS_STATE_VOLUME=hmaigc-stage-ops
HMAIGC_BACKEND_GID=101
HMAIGC_IMAGE_REGISTRY=ghcr.io/example
CANVAS_ENVIRONMENT=production
EOF
chmod 600 "$TEST_ROOT/config/control.env"

readonly REPOSITORY_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly STAGE_COMMAND="$REPOSITORY_ROOT/deploy/hmaigc-stage.sh"
readonly CURRENT_BACKEND_IMAGE="ghcr.io/example/hmaigc-backend@sha256:$(printf '%064d' 7)"
readonly CURRENT_WEB_IMAGE="ghcr.io/example/hmaigc-web@sha256:$(printf '%064d' 8)"
readonly TARGET_BACKEND_IMAGE="ghcr.io/example/hmaigc-backend@sha256:$(printf '%064d' 1)"
readonly TARGET_WEB_IMAGE="ghcr.io/example/hmaigc-web@sha256:$(printf '%064d' 2)"
readonly TARGET_CONTROLLER_IMAGE="ghcr.io/example/hmaigc-ops-controller@sha256:$(printf '%064d' 3)"
readonly TEST_ENV=(
    "PATH=$TEST_ROOT/bin:$PATH"
    "FAKE_DOCKER_LOG=$TEST_ROOT/docker.log"
    "FAKE_CURL_LOG=$TEST_ROOT/curl.log"
    "HMAIGC_ENV_FILE=$TEST_ROOT/.env.production"
    "HMAIGC_COMPOSE_FILE=$REPOSITORY_ROOT/docker-compose.production.yml"
    "HMAIGC_OPS_COMPOSE_FILE=$REPOSITORY_ROOT/deploy/docker-compose.ops.yml"
    "HMAIGC_OPS_STATE_DIR=$TEST_ROOT"
)

run_stage() {
    local command="$1"
    local backend_image="${2:-$TARGET_BACKEND_IMAGE}"
    local web_image="${3:-$TARGET_WEB_IMAGE}"
    env "${TEST_ENV[@]}" "$STAGE_COMMAND" "$command" \
        --operation-id op-stage \
        --generation 1 \
        --current-version v1.0.57 \
        --target-version v1.0.58 \
        --backend-image "$backend_image" \
        --web-image "$web_image" \
        --controller-image "$TARGET_CONTROLLER_IMAGE"
}

prepare_output="$(run_stage prepare "" "")"
grep -Fq "\"backendImage\":\"$TARGET_BACKEND_IMAGE\"" <<<"$prepare_output"
grep -Fq "\"webImage\":\"$TARGET_WEB_IMAGE\"" <<<"$prepare_output"
grep -Fq "\"controllerImage\":\"$TARGET_CONTROLLER_IMAGE\"" <<<"$prepare_output"
if grep -Fq 'hmaigc-backend:v1.0.58' <<<"$prepare_output"; then
    printf 'prepare leaked a mutable backend tag into stage facts\n' >&2
    exit 1
fi

quiesce_output="$(run_stage quiesce)"
grep -Fq '"serviceState":"maintenance"' <<<"$quiesce_output"
grep -Fq '"writesQuiesced":true' <<<"$quiesce_output"
grep -Fq ' stop web backend' "$TEST_ROOT/docker.log"

release_state_before="$(sha256sum "$TEST_ROOT/.env.production")"
run_stage public-verify >/dev/null
grep -Fq 'https://app.example.invalid/canvas/' "$TEST_ROOT/curl.log"
if env "${TEST_ENV[@]}" FAKE_PUBLIC_APP_FAILURE=true "$STAGE_COMMAND" public-verify \
    --operation-id op-stage --generation 1 --current-version v1.0.57 --target-version v1.0.58 \
    --backend-image "$TARGET_BACKEND_IMAGE" --web-image "$TARGET_WEB_IMAGE"; then
    printf 'public verification unexpectedly accepted a failed application origin\n' >&2
    exit 1
fi
if env "${TEST_ENV[@]}" FAKE_PUBLIC_CDN_FAILURE=true "$STAGE_COMMAND" public-verify \
    --operation-id op-stage --generation 1 --current-version v1.0.57 --target-version v1.0.58 \
    --backend-image "$TARGET_BACKEND_IMAGE" --web-image "$TARGET_WEB_IMAGE"; then
    printf 'public verification unexpectedly accepted a failed manifest asset\n' >&2
    exit 1
fi

if env "${TEST_ENV[@]}" "$STAGE_COMMAND" quiesce \
    --operation-id op-stage --generation 1 --current-version v1.0.57 --target-version v1.0.58 \
    --backend-image "$TARGET_BACKEND_IMAGE" --web-image "$TARGET_WEB_IMAGE" \
    --backup-helper-image alpine:latest; then
    printf 'stage unexpectedly ignored a mutable checkpoint backup helper image\n' >&2
    exit 1
fi
[[ "$(sha256sum "$TEST_ROOT/.env.production")" == "$release_state_before" ]]

started_at="$(date +%s)"
if env "${TEST_ENV[@]}" FAKE_PUBLIC_CDN_HANG=true PUBLIC_VERIFY_TOTAL_TIMEOUT_SECONDS=2 "$STAGE_COMMAND" public-verify \
    --operation-id op-stage --generation 1 --current-version v1.0.57 --target-version v1.0.58 \
    --backend-image "$TARGET_BACKEND_IMAGE" --web-image "$TARGET_WEB_IMAGE"; then
    printf 'public verification unexpectedly accepted a hung endpoint\n' >&2
    exit 1
fi
elapsed="$(( $(date +%s) - started_at ))"
((elapsed < 5)) || {
    printf 'public verification ignored the whole-operation deadline: %ss\n' "$elapsed" >&2
    exit 1
}

if run_stage start-target ghcr.io/example/hmaigc-backend:v1.0.58 "$TARGET_WEB_IMAGE"; then
    printf 'start-target unexpectedly accepted a mutable image tag\n' >&2
    exit 1
fi

control_before="$(sha256sum "$TEST_ROOT/config/control.env")"
handoff_output="$(env "${TEST_ENV[@]}" FAKE_CANDIDATE_HEALTH_FAILURE=true "$STAGE_COMMAND" handoff-controller \
    --operation-id op-stage --generation 1 --current-version v1.0.57 --target-version v1.0.58 \
    --backend-image "$TARGET_BACKEND_IMAGE" --web-image "$TARGET_WEB_IMAGE" \
    --controller-image "$TARGET_CONTROLLER_IMAGE")"
grep -Fq '"serviceState":"target_online"' <<<"$handoff_output"
grep -Fq '"controllerHandoff":"restored_previous"' <<<"$handoff_output"
grep -Fq '"code":"controller_handoff_failed"' <<<"$handoff_output"
[[ "$(sha256sum "$TEST_ROOT/config/control.env")" == "$control_before" ]]

grep -Fq "pull ghcr.io/example/hmaigc-backend:v1.0.58" "$TEST_ROOT/docker.log"
grep -Fq "image inspect ghcr.io/example/hmaigc-web:v1.0.58" "$TEST_ROOT/docker.log"

retention_root="$TEST_ROOT/retention"
mkdir -p "$retention_root/20260825-010101--v1.0.55" \
    "$retention_root/20260825-020202--v1.0.56" \
    "$retention_root/20260825-030303--v1.0.57"
BACKUP_DIR="$retention_root"
HMAIGC_BACKUP_RETENTION=1
log() { :; }
fail() { printf '%s\n' "$*" >&2; return 1; }
env_value() { printf ''; }
# shellcheck source=deploy/lib/backup.sh
source "$REPOSITORY_ROOT/deploy/lib/backup.sh"
prune_backups_if_configured "$retention_root/20260825-010101--v1.0.55"
[[ -d "$retention_root/20260825-010101--v1.0.55" ]]
[[ -d "$retention_root/20260825-030303--v1.0.57" ]]
[[ ! -d "$retention_root/20260825-020202--v1.0.56" ]]
printf 'hmaigc stage smoke test passed\n'
