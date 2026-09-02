#!/usr/bin/env bash

set -Eeuo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=deploy/lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"
# shellcheck source=deploy/lib/backup.sh
source "$SCRIPT_DIR/lib/backup.sh"

COMMAND="${1:-}"
[[ -n "$COMMAND" ]] || fail "必须指定运维阶段命令"
shift

OPERATION_ID=""
GENERATION=""
CURRENT_VERSION=""
TARGET_VERSION=""
TARGET_BACKEND_IMAGE=""
TARGET_WEB_IMAGE=""
TARGET_BACKUP_HELPER_IMAGE=""
BACKUP_PATH_ARGUMENT=""
PROTECTED_BACKUP_PATH_ARGUMENT=""
TARGET_CONTROLLER_IMAGE=""

while (($#)); do
    case "$1" in
        --operation-id) OPERATION_ID="${2:-}" ;;
        --generation) GENERATION="${2:-}" ;;
        --current-version) CURRENT_VERSION="${2:-}" ;;
        --target-version) TARGET_VERSION="${2:-}" ;;
        --backend-image) TARGET_BACKEND_IMAGE="${2:-}" ;;
        --web-image) TARGET_WEB_IMAGE="${2:-}" ;;
        --backup-helper-image) TARGET_BACKUP_HELPER_IMAGE="${2:-}" ;;
        --backup-path) BACKUP_PATH_ARGUMENT="${2:-}" ;;
		--protected-backup-path) PROTECTED_BACKUP_PATH_ARGUMENT="${2:-}" ;;
        --controller-image) TARGET_CONTROLLER_IMAGE="${2:-}" ;;
        *) fail "未知阶段参数：$1" ;;
    esac
    (($# >= 2)) || fail "阶段参数缺少值：$1"
    shift 2
done

[[ "$OPERATION_ID" =~ ^[A-Za-z0-9_-]{1,128}$ ]] || fail "operation ID 非法"
[[ "$GENERATION" =~ ^[1-9][0-9]*$ ]] || fail "Runner generation 非法"

OUTPUT_SERVICE_STATE=""
OUTPUT_CURRENT_VERSION=""
OUTPUT_RESULT_VERSION=""
OUTPUT_BACKUP_PATH=""
OUTPUT_BACKUP_CHECKSUM_STATUS=""
OUTPUT_BACKEND_IMAGE=""
OUTPUT_WEB_IMAGE=""
OUTPUT_BACKUP_HELPER_IMAGE=""
OUTPUT_CONTROLLER_IMAGE=""
OUTPUT_CONTROLLER_VERSION=""
OUTPUT_CONTROLLER_HANDOFF=""
OUTPUT_WARNING_CODE=""
OUTPUT_WARNING_MESSAGE=""
OUTPUT_WRITES_QUIESCED=false
OUTPUT_VERIFIED_RECOVERY_POINT=false
OUTPUT_DATA_MIGRATION_STARTED=false
OUTPUT_TARGET_BACKEND_HEALTHY=false
OUTPUT_TARGET_WEB_HEALTHY=false
OUTPUT_RELEASE_COMMITTED=false

json_escape() {
    local value="$1"
    value="${value//\\/\\\\}"
    value="${value//\"/\\\"}"
    value="${value//$'\n'/\\n}"
    value="${value//$'\r'/\\r}"
    value="${value//$'\t'/\\t}"
    printf '%s' "$value"
}

emit_output() {
    printf '{'
    printf '"serviceState":"%s"' "$(json_escape "$OUTPUT_SERVICE_STATE")"
    printf ',"currentVersion":"%s"' "$(json_escape "$OUTPUT_CURRENT_VERSION")"
    printf ',"resultVersion":"%s"' "$(json_escape "$OUTPUT_RESULT_VERSION")"
    printf ',"backupPath":"%s"' "$(json_escape "$OUTPUT_BACKUP_PATH")"
    printf ',"backupChecksumStatus":"%s"' "$(json_escape "$OUTPUT_BACKUP_CHECKSUM_STATUS")"
    printf ',"backendImage":"%s"' "$(json_escape "$OUTPUT_BACKEND_IMAGE")"
    printf ',"webImage":"%s"' "$(json_escape "$OUTPUT_WEB_IMAGE")"
    printf ',"backupHelperImage":"%s"' "$(json_escape "$OUTPUT_BACKUP_HELPER_IMAGE")"
    printf ',"controllerImage":"%s"' "$(json_escape "$OUTPUT_CONTROLLER_IMAGE")"
    printf ',"controllerVersion":"%s"' "$(json_escape "$OUTPUT_CONTROLLER_VERSION")"
    printf ',"controllerHandoff":"%s"' "$(json_escape "$OUTPUT_CONTROLLER_HANDOFF")"
    printf ',"writesQuiesced":%s' "$OUTPUT_WRITES_QUIESCED"
    printf ',"verifiedRecoveryPoint":%s' "$OUTPUT_VERIFIED_RECOVERY_POINT"
    printf ',"dataMigrationStarted":%s' "$OUTPUT_DATA_MIGRATION_STARTED"
    printf ',"targetBackendHealthy":%s' "$OUTPUT_TARGET_BACKEND_HEALTHY"
    printf ',"targetWebHealthy":%s' "$OUTPUT_TARGET_WEB_HEALTHY"
    printf ',"releaseCommitted":%s' "$OUTPUT_RELEASE_COMMITTED"
    if [[ -n "$OUTPUT_WARNING_CODE" ]]; then
        printf ',"warnings":[{"code":"%s","message":"%s"}]' \
            "$(json_escape "$OUTPUT_WARNING_CODE")" "$(json_escape "$OUTPUT_WARNING_MESSAGE")"
    else
        printf ',"warnings":[]'
    fi
    printf '}'
}

configure_current_runtime() {
    validate_release_version "$CURRENT_VERSION"
    unset HMAIGC_BACKEND_IMAGE HMAIGC_WEB_IMAGE
    configure_deploy_runtime "$CURRENT_VERSION"
    if [[ -n "$TARGET_BACKUP_HELPER_IMAGE" ]]; then
        require_immutable_image "$TARGET_BACKUP_HELPER_IMAGE"
        BACKUP_HELPER_IMAGE="$TARGET_BACKUP_HELPER_IMAGE"
        export BACKUP_HELPER_IMAGE
    fi
}

configure_target_runtime() {
    validate_release_version "$TARGET_VERSION"
    configure_current_runtime
    use_release_images "$TARGET_BACKEND_IMAGE" "$TARGET_WEB_IMAGE"
    HMAIGC_VERSION="$TARGET_VERSION"
    export HMAIGC_VERSION
}

control_env_value() {
    local file="$1"
    local key="$2"
    awk -v key="$key" '
        BEGIN { FS = "=" }
        /^[[:space:]]*#/ { next }
        $1 == key { sub(/^[^=]*=/, ""); sub(/\r$/, ""); print; exit }
    ' "$file"
}

write_candidate_control_env() {
    local source="$1"
    local destination="$2"
    local image="$3"
    local version="$4"
    require_immutable_image "$image"
    validate_release_version "$version"
    awk -v image="$image" -v version="$version" '
        BEGIN { seen_image = 0; seen_version = 0 }
        /^HMAIGC_OPS_IMAGE=/ { print "HMAIGC_OPS_IMAGE=" image; seen_image = 1; next }
        /^HMAIGC_OPS_VERSION=/ { print "HMAIGC_OPS_VERSION=" version; seen_version = 1; next }
        { print }
        END {
            if (!seen_image) print "HMAIGC_OPS_IMAGE=" image
            if (!seen_version) print "HMAIGC_OPS_VERSION=" version
        }
    ' "$source" >"$destination"
    chmod 600 "$destination"
}

controller_compose() {
    local environment_file="$1"
    shift
    local project_name
    project_name="$(control_env_value "$environment_file" HMAIGC_OPS_COMPOSE_PROJECT_NAME)"
    project_name="${project_name:-hmaigc-ops}"
    docker compose --project-name "$project_name" --env-file "$environment_file" \
        -f "${HMAIGC_OPS_COMPOSE_FILE:-/opt/hmaigc/deploy/docker-compose.ops.yml}" "$@"
}

validate_candidate_controller() {
    local candidate_env="$1"
    local candidate_image="$2"
    local state_volume
    state_volume="$(control_env_value "$candidate_env" HMAIGC_OPS_STATE_VOLUME)"
    [[ -n "$state_volume" ]] || return 1
    docker run --rm --read-only \
        --security-opt no-new-privileges \
        --volume "$state_volume:/var/lib/hmaigc-ops:ro" \
        --env-file "$candidate_env" \
        --env HMAIGC_CONTROL_ENV_FILE=/var/lib/hmaigc-ops/config/control.candidate.env \
        --entrypoint hmaigc-ops-controller \
        "$candidate_image" validate
}

handoff_controller() {
    local ops_state_dir config_dir control_env candidate_env previous_env old_image
    ops_state_dir="${HMAIGC_OPS_STATE_DIR:-}"
    [[ -n "$ops_state_dir" ]] || fail "缺少 HMAIGC_OPS_STATE_DIR，无法切换控制器"
    config_dir="$ops_state_dir/config"
    control_env="$config_dir/control.env"
    candidate_env="$config_dir/control.candidate.env"
    previous_env="$config_dir/control.previous.env"
    [[ -f "$control_env" ]] || fail "控制器规范配置不存在：$control_env"
    require_immutable_image "$TARGET_CONTROLLER_IMAGE"
    old_image="$(control_env_value "$control_env" HMAIGC_OPS_IMAGE)"
    require_immutable_image "$old_image"
    if [[ "$old_image" == "$TARGET_CONTROLLER_IMAGE" ]]; then
        OUTPUT_CONTROLLER_IMAGE="$old_image"
        OUTPUT_CONTROLLER_VERSION="$(control_env_value "$control_env" HMAIGC_OPS_VERSION)"
        OUTPUT_CONTROLLER_HANDOFF=unchanged
        return 0
    fi

    rm -f -- "$candidate_env" "$previous_env"
    write_candidate_control_env "$control_env" "$candidate_env" "$TARGET_CONTROLLER_IMAGE" "$TARGET_VERSION"
    if ! docker pull "$TARGET_CONTROLLER_IMAGE" >/dev/null ||
        ! validate_candidate_controller "$candidate_env" "$TARGET_CONTROLLER_IMAGE"; then
        rm -f -- "$candidate_env"
        OUTPUT_CONTROLLER_IMAGE="$old_image"
        OUTPUT_CONTROLLER_VERSION="$(control_env_value "$control_env" HMAIGC_OPS_VERSION)"
        OUTPUT_CONTROLLER_HANDOFF=restored_previous
        OUTPUT_WARNING_CODE=controller_handoff_failed
        OUTPUT_WARNING_MESSAGE="候选控制器校验失败，业务版本已成功，继续使用原控制器"
        return 0
    fi

    install -m 600 "$control_env" "$previous_env"
    mv -f "$candidate_env" "$control_env"
    if controller_compose "$control_env" pull ops-controller &&
        controller_compose "$control_env" up -d ops-controller --wait; then
        rm -f -- "$previous_env"
        OUTPUT_CONTROLLER_IMAGE="$TARGET_CONTROLLER_IMAGE"
        OUTPUT_CONTROLLER_VERSION="$TARGET_VERSION"
        OUTPUT_CONTROLLER_HANDOFF=updated
        return 0
    fi

    mv -f "$previous_env" "$control_env"
    if ! controller_compose "$control_env" up -d ops-controller --wait; then
        fail "候选控制器启动失败，原控制器也无法恢复"
    fi
    OUTPUT_CONTROLLER_IMAGE="$old_image"
    OUTPUT_CONTROLLER_VERSION="$(control_env_value "$control_env" HMAIGC_OPS_VERSION)"
    OUTPUT_CONTROLLER_HANDOFF=restored_previous
    OUTPUT_WARNING_CODE=controller_handoff_failed
    OUTPUT_WARNING_MESSAGE="候选控制器启动失败，业务版本已成功并恢复原控制器"
}

verify_public_asset() {
    local base_url="$1"
    local asset_path="$2"
    case "/$asset_path/" in
        *"/../"* | *"/./"*)
            printf '静态资源清单包含非法路径：%s\n' "$asset_path" >&2
            return 1
            ;;
    esac
    [[ -n "$asset_path" && "$asset_path" != /* && "$asset_path" != http://* && "$asset_path" != https://* ]] || {
        printf '静态资源清单包含非法路径：%s\n' "$asset_path" >&2
        return 1
    }
    local url="${base_url%/}/$asset_path"
    if ! curl --fail --silent --show-error --location \
        --connect-timeout 5 --max-time 15 \
        --retry 1 --retry-all-errors --retry-delay 1 \
        "$url" --output /dev/null; then
        printf '公网静态资源校验失败：%s\n' "$url" >&2
        return 1
    fi
}
export -f verify_public_asset

verify_public_application() {
    local origins="$1"
    local origin url response_file
    local -a configured_origins
    IFS=',' read -r -a configured_origins <<<"$origins"
    ((${#configured_origins[@]} > 0)) || {
        printf '公网应用入口校验缺少 CANVAS_CORS_ORIGINS\n' >&2
        return 1
    }
    for origin in "${configured_origins[@]}"; do
        origin="${origin#"${origin%%[![:space:]]*}"}"
        origin="${origin%"${origin##*[![:space:]]}"}"
        [[ "$origin" =~ ^https://[^/?#[:space:]]+$ ]] || {
            printf '公网应用 Origin 不是无路径 HTTPS Origin：%s\n' "${origin:-未配置}" >&2
            return 1
        }
        url="${origin}/canvas/"
        response_file="$(mktemp)"
        if ! curl --fail --silent --show-error --location \
            --connect-timeout 5 --max-time 15 \
            --retry 1 --retry-all-errors --retry-delay 1 \
            "$url" --output "$response_file"; then
            rm -f -- "$response_file"
            printf '公网应用入口校验失败：%s\n' "$url" >&2
            return 1
        fi
        if ! grep -Fq 'id="root"' "$response_file"; then
            rm -f -- "$response_file"
            printf '公网应用入口未返回 HMaigc SPA：%s\n' "$url" >&2
            return 1
        fi
        rm -f -- "$response_file"
    done
}
export -f verify_public_application

verify_public_release_inner() {
    local manifest_url="$1"
    local manifest_file="$2"
    local expected_version="$3"
    local base_url="$4"
    curl --fail --silent --show-error --location \
        --connect-timeout 5 --max-time 15 \
        --retry 1 --retry-all-errors --retry-delay 1 \
        "$manifest_url" --output "$manifest_file"
    jq -e --arg version "$expected_version" \
        '.release == $version and (.files | type == "array") and (.files | length > 0)' \
        "$manifest_file" >/dev/null
    jq -j '.files[] | .path, "\u0000"' "$manifest_file" |
        xargs -0 -r -P 8 -n 1 bash -Eeuo pipefail -c \
            'verify_public_asset "$1" "$2"' _ "$base_url"
}
export -f verify_public_release_inner

verify_public_environment_inner() {
    local origins="$1"
    shift
    verify_public_application "$origins"
    verify_public_release_inner "$@"
}
export -f verify_public_environment_inner

verify_public_release() {
    local version="$1"
    local base_url application_origins release_base manifest_url manifest_file total_timeout
    base_url="$(env_value HMAIGC_STATIC_ASSET_BASE_URL)"
    application_origins="$(env_value CANVAS_CORS_ORIGINS)"
    [[ "$base_url" == https://* && "$base_url" != */ ]] ||
        fail "HMAIGC_STATIC_ASSET_BASE_URL 必须是无尾斜杠的 HTTPS 地址"
    total_timeout="${PUBLIC_VERIFY_TOTAL_TIMEOUT_SECONDS:-120}"
    [[ "$total_timeout" =~ ^[1-9][0-9]*$ ]] && ((total_timeout <= 120)) ||
        fail "公网校验总时限必须是 1 到 120 秒"
    release_base="$base_url/releases/$version"
    manifest_url="$release_base/manifest.json"
    manifest_file="$(mktemp)"

    if ! timeout "${total_timeout}s" bash -Eeuo pipefail -c \
        'verify_public_environment_inner "$@"' _ \
        "$application_origins" "$manifest_url" "$manifest_file" "$version" "$release_base"; then
        rm -f -- "$manifest_file"
        fail "公网应用入口或静态资源校验失败，或超过 ${total_timeout} 秒总时限"
    fi
    rm -f -- "$manifest_file"
}

execute_stage() {
    case "$COMMAND" in
        prepare | online-preflight)
            configure_current_runtime
            preflight
            if [[ "$COMMAND" == "online-preflight" ]]; then
                verify_running_release "$CURRENT_VERSION" || fail "当前版本未通过本机验活"
            fi
            if [[ -n "$TARGET_VERSION" ]]; then
                resolve_release_images "$TARGET_VERSION"
                use_release_images "$RESOLVED_BACKEND_IMAGE" "$RESOLVED_WEB_IMAGE"
                if [[ "$COMMAND" == "online-preflight" ]]; then
                    HMAIGC_VERSION="$TARGET_VERSION" audit_target_agent_runtime_upgrade "$TARGET_VERSION" online
                fi
                OUTPUT_RESULT_VERSION="$TARGET_VERSION"
                OUTPUT_BACKEND_IMAGE="$RESOLVED_BACKEND_IMAGE"
                OUTPUT_WEB_IMAGE="$RESOLVED_WEB_IMAGE"
                OUTPUT_CONTROLLER_IMAGE="$TARGET_CONTROLLER_IMAGE"
            else
                OUTPUT_RESULT_VERSION="$CURRENT_VERSION"
                OUTPUT_BACKEND_IMAGE="$HMAIGC_BACKEND_IMAGE"
                OUTPUT_WEB_IMAGE="$HMAIGC_WEB_IMAGE"
            fi
            OUTPUT_SERVICE_STATE=current_online
            OUTPUT_CURRENT_VERSION="$CURRENT_VERSION"
            OUTPUT_BACKUP_HELPER_IMAGE="$BACKUP_HELPER_IMAGE"
            ;;
        public-verify)
            configure_current_runtime
            preflight
            verify_public_release "$CURRENT_VERSION"
            OUTPUT_SERVICE_STATE=current_online
            OUTPUT_CURRENT_VERSION="$CURRENT_VERSION"
            OUTPUT_RESULT_VERSION="$CURRENT_VERSION"
            ;;
        quiesce)
            configure_current_runtime
            stop_application
            OUTPUT_SERVICE_STATE=maintenance
            OUTPUT_CURRENT_VERSION="$CURRENT_VERSION"
            OUTPUT_WRITES_QUIESCED=true
            ;;
        quiesced-audit)
            configure_target_runtime
            audit_target_agent_runtime_upgrade "$TARGET_VERSION" quiesced
            OUTPUT_SERVICE_STATE=maintenance
            OUTPUT_CURRENT_VERSION="$CURRENT_VERSION"
            OUTPUT_RESULT_VERSION="$TARGET_VERSION"
            OUTPUT_WRITES_QUIESCED=true
            ;;
        backup)
            configure_current_runtime
            docker pull "$BACKUP_HELPER_IMAGE" >/dev/null
			OUTPUT_BACKUP_PATH="$(create_backup "$CURRENT_VERSION" "$PROTECTED_BACKUP_PATH_ARGUMENT")"
            verify_backup "$OUTPUT_BACKUP_PATH"
            OUTPUT_SERVICE_STATE=maintenance
            OUTPUT_CURRENT_VERSION="$CURRENT_VERSION"
            OUTPUT_WRITES_QUIESCED=true
            OUTPUT_VERIFIED_RECOVERY_POINT=true
            OUTPUT_BACKUP_CHECKSUM_STATUS=verified
            OUTPUT_BACKUP_HELPER_IMAGE="$BACKUP_HELPER_IMAGE"
            ;;
        start-target)
            if [[ -n "$TARGET_VERSION" ]]; then
                configure_target_runtime
                start_release "$TARGET_VERSION"
                OUTPUT_SERVICE_STATE=target_online
                OUTPUT_RESULT_VERSION="$TARGET_VERSION"
                OUTPUT_BACKEND_IMAGE="$TARGET_BACKEND_IMAGE"
                OUTPUT_WEB_IMAGE="$TARGET_WEB_IMAGE"
                OUTPUT_DATA_MIGRATION_STARTED=true
            else
                configure_current_runtime
                start_release "$CURRENT_VERSION"
                OUTPUT_SERVICE_STATE=current_online
                OUTPUT_RESULT_VERSION="$CURRENT_VERSION"
                OUTPUT_BACKEND_IMAGE="$HMAIGC_BACKEND_IMAGE"
                OUTPUT_WEB_IMAGE="$HMAIGC_WEB_IMAGE"
            fi
            OUTPUT_CURRENT_VERSION="$CURRENT_VERSION"
            OUTPUT_TARGET_BACKEND_HEALTHY=true
            OUTPUT_TARGET_WEB_HEALTHY=true
            ;;
        verify-target)
            if [[ -n "$TARGET_VERSION" ]]; then
                configure_target_runtime
                verify_running_release "$TARGET_VERSION"
                OUTPUT_SERVICE_STATE=target_online
                OUTPUT_RESULT_VERSION="$TARGET_VERSION"
                OUTPUT_BACKEND_IMAGE="$TARGET_BACKEND_IMAGE"
                OUTPUT_WEB_IMAGE="$TARGET_WEB_IMAGE"
            else
                configure_current_runtime
                verify_running_release "$CURRENT_VERSION"
                OUTPUT_SERVICE_STATE=current_online
                OUTPUT_RESULT_VERSION="$CURRENT_VERSION"
                OUTPUT_BACKEND_IMAGE="$HMAIGC_BACKEND_IMAGE"
                OUTPUT_WEB_IMAGE="$HMAIGC_WEB_IMAGE"
            fi
            OUTPUT_CURRENT_VERSION="$CURRENT_VERSION"
            OUTPUT_TARGET_BACKEND_HEALTHY=true
            OUTPUT_TARGET_WEB_HEALTHY=true
            ;;
        commit-release)
            configure_target_runtime
            [[ -n "$BACKUP_PATH_ARGUMENT" ]] || fail "提交发布缺少已验证恢复点"
            verify_backup "$BACKUP_PATH_ARGUMENT"
            write_production_release_config "$TARGET_VERSION" "$TARGET_BACKEND_IMAGE" "$TARGET_WEB_IMAGE"
            write_release_state "$TARGET_VERSION" "$CURRENT_VERSION" "$BACKUP_PATH_ARGUMENT"
            OUTPUT_SERVICE_STATE=target_online
            OUTPUT_CURRENT_VERSION="$TARGET_VERSION"
            OUTPUT_RESULT_VERSION="$TARGET_VERSION"
            OUTPUT_BACKEND_IMAGE="$TARGET_BACKEND_IMAGE"
            OUTPUT_WEB_IMAGE="$TARGET_WEB_IMAGE"
            OUTPUT_TARGET_BACKEND_HEALTHY=true
            OUTPUT_TARGET_WEB_HEALTHY=true
            OUTPUT_RELEASE_COMMITTED=true
            ;;
        restore-current)
            configure_current_runtime
            start_release "$CURRENT_VERSION"
            OUTPUT_SERVICE_STATE=current_restored
            OUTPUT_CURRENT_VERSION="$CURRENT_VERSION"
            OUTPUT_RESULT_VERSION="$CURRENT_VERSION"
            ;;
		restore-backup)
            configure_current_runtime
            [[ -n "$BACKUP_PATH_ARGUMENT" ]] || fail "恢复数据缺少已验证恢复点"
            restore_backup "$BACKUP_PATH_ARGUMENT" "$CURRENT_VERSION"
            start_release "$CURRENT_VERSION"
            OUTPUT_SERVICE_STATE=current_restored
            OUTPUT_CURRENT_VERSION="$CURRENT_VERSION"
            OUTPUT_RESULT_VERSION="$CURRENT_VERSION"
            OUTPUT_VERIFIED_RECOVERY_POINT=true
            OUTPUT_BACKUP_PATH="$BACKUP_PATH_ARGUMENT"
			OUTPUT_BACKUP_CHECKSUM_STATUS=verified
			;;
		restore-rollback-backup)
			configure_current_runtime
			[[ -n "$BACKUP_PATH_ARGUMENT" ]] || fail "回滚缺少已验证的上一版本恢复点"
			restore_backup "$BACKUP_PATH_ARGUMENT" "$TARGET_VERSION"
			OUTPUT_SERVICE_STATE=maintenance
			OUTPUT_CURRENT_VERSION="$CURRENT_VERSION"
			OUTPUT_RESULT_VERSION="$TARGET_VERSION"
			OUTPUT_WRITES_QUIESCED=true
			OUTPUT_DATA_MIGRATION_STARTED=true
			;;
        handoff-controller)
            configure_target_runtime
            [[ -n "$TARGET_CONTROLLER_IMAGE" ]] || fail "控制器交接缺少候选镜像摘要"
            handoff_controller
            OUTPUT_SERVICE_STATE=target_online
            OUTPUT_CURRENT_VERSION="$TARGET_VERSION"
            OUTPUT_RESULT_VERSION="$TARGET_VERSION"
            ;;
        *) fail "未知运维阶段命令：$COMMAND" ;;
    esac
}

execute_stage 1>&2
emit_output
