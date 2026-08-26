#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=deploy/lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"
# shellcheck source=deploy/lib/backup.sh
source "$SCRIPT_DIR/lib/backup.sh"

usage() {
    cat <<'EOF'
HMaigc 生产发布工具

用法：
  ./deploy/hmaigc.sh install [vX.Y.Z]   首次安装
  ./deploy/hmaigc.sh upgrade vX.Y.Z     备份后升级，失败自动恢复
  ./deploy/hmaigc.sh rollback            回滚到升级前版本及同一数据恢复点
  ./deploy/hmaigc.sh backup              停写并创建一致性备份
  ./deploy/hmaigc.sh verify              校验当前版本、依赖和 SPA 深链接
  ./deploy/hmaigc.sh status              查看版本状态与容器状态

可通过 HMAIGC_ENV_FILE 指定生产环境文件，默认读取仓库根目录 .env.production。
EOF
}

configured_release_version() {
    env_value HMAIGC_VERSION
}

require_installed_state() {
    [[ -f "$STATE_FILE" ]] || fail "尚未记录已安装版本，请先执行 install"
    local current current_backend current_web previous rollback_backup previous_backend previous_web
    current="$(state_value CURRENT_VERSION)"
    current_backend="$(state_value CURRENT_BACKEND_IMAGE)"
    current_web="$(state_value CURRENT_WEB_IMAGE)"
    previous="$(state_value PREVIOUS_VERSION)"
    rollback_backup="$(state_value ROLLBACK_BACKUP)"
    previous_backend="$(state_value PREVIOUS_BACKEND_IMAGE)"
    previous_web="$(state_value PREVIOUS_WEB_IMAGE)"
    validate_release_version "$current"
    require_immutable_image "$current_backend"
    require_immutable_image "$current_web"
    if [[ -n "$previous" ]]; then
        validate_release_version "$previous"
        [[ -n "$rollback_backup" ]] || fail "部署状态缺少上一版本恢复点"
        require_immutable_image "$previous_backend"
        require_immutable_image "$previous_web"
    elif [[ -n "$rollback_backup" || -n "$previous_backend" || -n "$previous_web" ]]; then
        fail "部署状态包含孤立的上一版本恢复事实"
    fi
}

install_release() {
    local target="${1:-$(configured_release_version)}"
    configure_deploy_runtime "$target"
    start_operation_log install
    acquire_deploy_lock
    preflight
    [[ ! -f "$STATE_FILE" ]] || fail "系统已经安装；升级请使用 upgrade"

    pull_release "$target"
    if ! start_release "$target"; then
        stop_application
        fail "首次安装未通过健康检查，应用容器已停止，数据库和备份卷保留"
    fi
    write_release_state \
        "$target" "" "" \
        "$RESOLVED_BACKEND_IMAGE" "$RESOLVED_WEB_IMAGE" "" ""
    log "安装完成：$target"
}

upgrade_release() {
    local target="${1:-}"
    [[ -n "$target" ]] || fail "upgrade 必须显式指定目标版本，例如 v1.0.12"
    configure_deploy_runtime "$target"
    start_operation_log upgrade
    acquire_deploy_lock
    preflight
    require_installed_state

    local current backup_path original_previous original_rollback_backup
    local current_backend_image current_web_image
    local original_previous_backend_image original_previous_web_image
    local target_backend_image target_web_image
    current="$(state_value CURRENT_VERSION)"
    current_backend_image="$HMAIGC_BACKEND_IMAGE"
    current_web_image="$HMAIGC_WEB_IMAGE"
    original_previous="$(state_value PREVIOUS_VERSION)"
    original_rollback_backup="$(state_value ROLLBACK_BACKUP)"
    original_previous_backend_image="$(state_value PREVIOUS_BACKEND_IMAGE)"
    original_previous_web_image="$(state_value PREVIOUS_WEB_IMAGE)"
    [[ "$target" != "$current" ]] || fail "目标版本与当前版本相同：$target"
    verify_running_release "$current" ||
        fail "当前运行版本与发布状态不一致或健康检查失败，拒绝自动升级"
    pull_release "$target"
    target_backend_image="$RESOLVED_BACKEND_IMAGE"
    target_web_image="$RESOLVED_WEB_IMAGE"
    if ! audit_target_agent_runtime_upgrade "$target" online; then
        fail "目标版本 Agent Runtime 在线只读预检失败；当前版本保持运行，未进入停写或备份"
    fi
    docker pull "$BACKUP_HELPER_IMAGE" >/dev/null

    stop_application
    if ! audit_target_agent_runtime_upgrade "$target" quiesced; then
        export HMAIGC_VERSION="$current"
        use_release_images "$current_backend_image" "$current_web_image"
        if start_release "$current"; then
            fail "目标版本 Agent Runtime 停写后只读预检失败；已恢复当前版本，未创建备份或启动目标版本"
        fi
        fail "目标版本 Agent Runtime 停写后只读预检失败，且当前版本恢复启动失败，请立即人工检查"
    fi
    if ! backup_path="$(create_backup "$current" "$original_rollback_backup")"; then
        export HMAIGC_VERSION="$current"
        use_release_images "$current_backend_image" "$current_web_image"
        start_release "$current" || fail "备份失败且当前版本恢复启动失败，请立即人工检查"
        fail "升级前备份失败，已恢复当前版本，未执行升级"
    fi

    if start_release "$target"; then
        write_release_state \
            "$target" "$current" "$backup_path" \
            "$target_backend_image" "$target_web_image" \
            "$current_backend_image" "$current_web_image"
        prune_backups_if_configured "$backup_path"
        log "升级完成：$current -> $target"
        return 0
    fi

    log "新版本未通过验收，开始自动恢复 $current"
    export HMAIGC_VERSION="$current"
    use_release_images "$current_backend_image" "$current_web_image"
    restore_backup "$backup_path" "$current"
    if start_release "$current"; then
        write_release_state \
            "$current" "$original_previous" "$original_rollback_backup" \
            "$current_backend_image" "$current_web_image" \
            "$original_previous_backend_image" "$original_previous_web_image"
        prune_backups_if_configured "$original_rollback_backup"
        fail "升级失败，已自动恢复到 $current；请检查 $LOG_DIR 与容器日志"
    fi
    fail "升级和自动恢复均未通过验收，当前服务状态未知；恢复点位于：$backup_path，请立即检查容器状态与日志"
}

rollback_release() {
    local configured_version
    configured_version="${1:-$(configured_release_version)}"
    configure_deploy_runtime "$configured_version"
    start_operation_log rollback
    acquire_deploy_lock
    preflight
    require_installed_state

    local current previous rollback_backup safety_backup
    local current_backend_image current_web_image
    local previous_backend_image previous_web_image
    current="$(state_value CURRENT_VERSION)"
    current_backend_image="$HMAIGC_BACKEND_IMAGE"
    current_web_image="$HMAIGC_WEB_IMAGE"
    previous="$(state_value PREVIOUS_VERSION)"
    previous_backend_image="$(state_value PREVIOUS_BACKEND_IMAGE)"
    previous_web_image="$(state_value PREVIOUS_WEB_IMAGE)"
    rollback_backup="$(state_value ROLLBACK_BACKUP)"
    [[ -n "$previous" && -n "$rollback_backup" ]] || fail "当前没有可执行的一键回滚恢复点"
    validate_release_version "$previous"
    require_immutable_image "$previous_backend_image"
    require_immutable_image "$previous_web_image"
    verify_running_release "$current" ||
        fail "当前运行版本与发布状态不一致或健康检查失败，拒绝自动回滚"
    export HMAIGC_VERSION="$previous"
    use_release_images "$previous_backend_image" "$previous_web_image"
    docker pull "$previous_backend_image" >/dev/null
    docker pull "$previous_web_image" >/dev/null
    docker pull "$BACKUP_HELPER_IMAGE" >/dev/null

    stop_application
    if ! safety_backup="$(create_backup "$current" "$rollback_backup")"; then
        export HMAIGC_VERSION="$current"
        use_release_images "$current_backend_image" "$current_web_image"
        start_release "$current" || fail "回滚前安全备份失败且当前版本恢复启动失败"
        fail "回滚前安全备份失败，已恢复当前版本"
    fi

    restore_backup "$rollback_backup" "$previous"
    if start_release "$previous"; then
        write_release_state \
            "$previous" "$current" "$safety_backup" \
            "$previous_backend_image" "$previous_web_image" \
            "$current_backend_image" "$current_web_image"
        prune_backups_if_configured "$safety_backup"
        log "回滚完成：$current -> $previous"
        return 0
    fi

    log "目标回滚版本未通过验收，恢复回滚前版本 $current"
    export HMAIGC_VERSION="$current"
    use_release_images "$current_backend_image" "$current_web_image"
    restore_backup "$safety_backup" "$current"
    if start_release "$current"; then
        write_release_state \
            "$current" "$previous" "$rollback_backup" \
            "$current_backend_image" "$current_web_image" \
            "$previous_backend_image" "$previous_web_image"
        prune_backups_if_configured "$rollback_backup"
        fail "回滚失败，已恢复到回滚前版本 $current"
    fi
    fail "回滚和安全恢复均失败，服务保持停止状态；恢复点：$safety_backup"
}

backup_release() {
    local configured_version
    configured_version="$(configured_release_version)"
    configure_deploy_runtime "$configured_version"
    start_operation_log backup
    acquire_deploy_lock
    preflight
    require_installed_state

    local current backup_path rollback_backup
    current="$(state_value CURRENT_VERSION)"
    rollback_backup="$(state_value ROLLBACK_BACKUP)"
    verify_running_release "$current" ||
        fail "当前运行版本与发布状态不一致或健康检查失败，拒绝自动备份"
    docker pull "$BACKUP_HELPER_IMAGE" >/dev/null
    stop_application
    if ! backup_path="$(create_backup "$current" "$rollback_backup")"; then
        start_release "$current" || fail "备份失败且当前版本恢复启动失败"
        fail "备份失败，已恢复当前版本"
    fi
    start_release "$current" || fail "备份成功，但当前版本恢复启动失败：$backup_path"
    prune_backups_if_configured "$rollback_backup"
    log "备份完成：$backup_path"
}

verify_release() {
    local configured_version
    configured_version="$(configured_release_version)"
    configure_deploy_runtime "$configured_version"
    start_operation_log verify
    preflight
    require_installed_state

    local current
    current="$(state_value CURRENT_VERSION)"
    verify_backend_release "$current"
    verify_web_release "$current" public
    log "版本与健康检查通过：$current"
}

status_release() {
    local configured_version
    configured_version="$(configured_release_version)"
    configure_deploy_runtime "$configured_version"
    start_operation_log status
    preflight
    printf 'current=%s\nprevious=%s\nrollback_backup=%s\n' \
        "$(state_value CURRENT_VERSION)" \
        "$(state_value PREVIOUS_VERSION)" \
        "$(state_value ROLLBACK_BACKUP)"
    compose ps
}

main() {
    local command="${1:-help}"
    shift || true
    case "$command" in
        install) install_release "$@" ;;
        upgrade) upgrade_release "$@" ;;
        rollback) rollback_release "$@" ;;
        backup) backup_release "$@" ;;
        verify) verify_release "$@" ;;
        status) status_release "$@" ;;
        help | --help | -h) usage ;;
        *) usage >&2; fail "未知命令：$command" ;;
    esac
}

main "$@"
