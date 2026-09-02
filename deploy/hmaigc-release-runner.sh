#!/usr/bin/env bash

set -Eeuo pipefail

readonly RELEASE_VERSION_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$'

fail() {
    printf 'source release runner error: %s\n' "$*" >&2
    exit 1
}

validate_inputs() {
    local version="$1"
    local deploy_root="$2"
    [[ "$version" =~ $RELEASE_VERSION_PATTERN ]] || fail "invalid release version: $version"
    [[ "$deploy_root" == /* && "$deploy_root" != "/" && "$deploy_root" != *".."* && "$deploy_root" != *"//"* ]] ||
        fail "deploy root must be a dedicated absolute path"
}

write_result() {
    local result_file="$1"
    local exit_code="$2"
    local temporary="${result_file}.tmp.$$"
    printf '%s\n' "$exit_code" >"$temporary"
    chmod 600 "$temporary"
    mv -f -- "$temporary" "$result_file"
}

run_worker() {
    local version="$1"
    local deploy_root="$2"
    local operation_dir="$3"
    local release_dir="$deploy_root/releases/$version"
    local result_file="$operation_dir/result"
    local current_release_file="$deploy_root/shared/deploy-state/current-release"
    local current_release_tmp="${current_release_file}.tmp.$$"
    local exit_code=125

    trap 'write_result "$result_file" "$exit_code"' EXIT
    export HMAIGC_ENV_FILE="$deploy_root/shared/production.env"
    export HMAIGC_DEPLOY_STATE_DIR="$deploy_root/shared/deploy-state"
    export HMAIGC_STATE_DIR="$deploy_root/shared/deploy-state/state"
    export HMAIGC_BACKUP_DIR="$deploy_root/shared/deploy-state/backups"

    cd "$release_dir"
    set +e
    bash deploy/hmaigc.sh upgrade "$version"
    exit_code=$?
    set -e
    if ((exit_code == 0)); then
        set +e
        bash deploy/hmaigc.sh status
        exit_code=$?
        set -e
    fi
    if ((exit_code == 0)); then
        if ! {
            printf '%s\n' "$release_dir" >"$current_release_tmp"
            chmod 600 "$current_release_tmp"
            mv -f -- "$current_release_tmp" "$current_release_file"
        }; then
            rm -f -- "$current_release_tmp"
            exit_code=1
        fi
    fi

    write_result "$result_file" "$exit_code"
    trap - EXIT
    exit "$exit_code"
}

stream_operation() {
    local operation_dir="$1"
    local log_file="$operation_dir/output.log"
    local result_file="$operation_dir/result"
    local pid_file="$operation_dir/worker.pid"
    local next_line=1
    local worker_pid result_code line_count

    worker_pid="$(cat "$pid_file")"
    [[ "$worker_pid" =~ ^[0-9]+$ ]] || fail "invalid release worker pid"

    while true; do
        if [[ -f "$log_file" ]]; then
            line_count="$(wc -l <"$log_file")"
            if ((line_count >= next_line)); then
                sed -n "${next_line},${line_count}p" "$log_file"
                next_line=$((line_count + 1))
            fi
        fi

        if [[ -f "$result_file" ]]; then
            result_code="$(cat "$result_file")"
            [[ "$result_code" =~ ^[0-9]+$ ]] || fail "invalid release worker result"
            return "$result_code"
        fi

        if ! kill -0 "$worker_pid" 2>/dev/null; then
            sleep 1
            if [[ ! -f "$result_file" ]]; then
                printf 'release worker exited without a durable result; operator inspection is required\n' >>"$log_file"
                write_result "$result_file" 125
            fi
            continue
        fi
        sleep 2
    done
}

launch_or_attach() {
    local version="$1"
    local deploy_root="$2"
    local script_path release_dir operation_dir log_file result_file pid_file worker_pid
    script_path="$(realpath -e "${BASH_SOURCE[0]}")"
    release_dir="$deploy_root/releases/$version"
    operation_dir="$deploy_root/shared/deploy-state/actions/$version"
    log_file="$operation_dir/output.log"
    result_file="$operation_dir/result"
    pid_file="$operation_dir/worker.pid"

    [[ -f "$release_dir/deploy/hmaigc.sh" ]] || fail "release bundle is incomplete: $release_dir"
    [[ -f "$deploy_root/shared/production.env" ]] || fail "production environment is missing"
    mkdir -p "$operation_dir"
    chmod 700 "$deploy_root/shared/deploy-state" "$deploy_root/shared/deploy-state/actions" "$operation_dir"

    exec 9>"$operation_dir/launch.lock"
    flock 9
    if [[ -f "$result_file" ]]; then
        flock -u 9
        [[ ! -f "$log_file" ]] || cat "$log_file"
        local existing_result
        existing_result="$(cat "$result_file")"
        [[ "$existing_result" =~ ^[0-9]+$ ]] || fail "invalid existing release result"
        return "$existing_result"
    fi

    if [[ -f "$pid_file" ]]; then
        worker_pid="$(cat "$pid_file")"
        if [[ ! "$worker_pid" =~ ^[0-9]+$ ]] || ! kill -0 "$worker_pid" 2>/dev/null; then
            printf 'previous release worker disappeared without a durable result; operator inspection is required\n' >>"$log_file"
            write_result "$result_file" 125
        fi
    else
        : >"$log_file"
        nohup bash "$script_path" --worker "$version" "$deploy_root" "$operation_dir" \
            >>"$log_file" 2>&1 </dev/null &
        worker_pid=$!
        printf '%s\n' "$worker_pid" >"$pid_file.tmp.$$"
        chmod 600 "$pid_file.tmp.$$"
        mv -f -- "$pid_file.tmp.$$" "$pid_file"
    fi
    flock -u 9

    stream_operation "$operation_dir"
}

main() {
    if [[ "${1:-}" == --worker ]]; then
        [[ $# -eq 4 ]] || fail "worker requires version, deploy root and operation directory"
        validate_inputs "$2" "$3"
        run_worker "$2" "$3" "$4"
        return
    fi

    [[ $# -eq 2 ]] || fail "usage: hmaigc-release-runner.sh <version> <deploy-root>"
    validate_inputs "$1" "$2"
    command -v flock >/dev/null 2>&1 || fail "flock is required"
    command -v realpath >/dev/null 2>&1 || fail "realpath is required"
    launch_or_attach "$1" "$2"
}

main "$@"
