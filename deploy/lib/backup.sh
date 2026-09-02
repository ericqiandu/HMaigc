#!/usr/bin/env bash

set -Eeuo pipefail

backup_manifest_value() {
    local backup_path="$1"
    local key="$2"
    awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print; exit }' "$backup_path/manifest.env"
}

verify_backup() {
    local backup_path="$1"
    [[ -d "$backup_path" ]] || fail "备份目录不存在：$backup_path"
    [[ -s "$backup_path/postgres.dump" ]] || fail "PostgreSQL 备份为空：$backup_path"
    [[ -s "$backup_path/backend-data.tgz" ]] || fail "资源卷备份为空：$backup_path"
    [[ -s "$backup_path/manifest.env" ]] || fail "备份清单不存在：$backup_path"
    (
        cd "$backup_path"
        sha256sum -c SHA256SUMS >&2
    )
    compose exec -T postgres pg_restore --list <"$backup_path/postgres.dump" >/dev/null
}

prune_backups_if_configured() {
    local protected_path="${1:-}"
    local retention="${HMAIGC_BACKUP_RETENTION:-$(env_value HMAIGC_BACKUP_RETENTION)}"
    retention="${retention:-0}"
    [[ "$retention" =~ ^[0-9]+$ ]] || fail "HMAIGC_BACKUP_RETENTION 必须是非负整数"
    ((retention > 0)) || return 0

    local index=0 backup_path
    while IFS= read -r backup_path; do
        index=$((index + 1))
        if [[ -n "$protected_path" && "$backup_path" == "$protected_path" ]]; then
            continue
        fi
        if ((index > retention)); then
            log "按已配置保留策略删除旧备份：$backup_path"
            rm -rf -- "$backup_path"
        fi
    done < <(find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -type d -name '*--v*' -print | sort -r)
}

create_backup() {
    local version="$1"
    local protected_path="${2:-}"
    local stamp backup_path temporary temporary_relative
    validate_release_version "$version"
    stamp="$(date -u +'%Y%m%d-%H%M%S')-$$"
    backup_path="$BACKUP_DIR/${stamp}--${version}"
    temporary="${backup_path}.tmp"
    temporary_relative="$(backup_directory_relative_path "$temporary")"
    mkdir -p "$temporary"
    chmod 700 "$temporary"

    log "创建 PostgreSQL 恢复点"
    compose exec -T postgres sh -ceu \
        'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom' \
        >"$temporary/postgres.dump"

    docker volume inspect "$HMAIGC_BACKEND_DATA_VOLUME" >/dev/null
    log "创建后端资源卷恢复点"
    docker run --rm \
        --mount "type=volume,src=$HMAIGC_BACKEND_DATA_VOLUME,dst=/source,readonly" \
        --mount "type=bind,src=$BACKUP_DIR,dst=/backups" \
        --env "BACKUP_RELATIVE=$temporary_relative" \
        "$BACKUP_HELPER_IMAGE" \
        sh -ceu 'tar -czf "/backups/$BACKUP_RELATIVE/backend-data.tgz" -C /source .'

    {
        printf 'VERSION=%s\n' "$version"
        printf 'CREATED_AT=%s\n' "$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
        printf 'POSTGRES_VOLUME=%s\n' "$HMAIGC_POSTGRES_DATA_VOLUME"
        printf 'BACKEND_VOLUME=%s\n' "$HMAIGC_BACKEND_DATA_VOLUME"
    } >"$temporary/manifest.env"
    (
        cd "$temporary"
        sha256sum postgres.dump backend-data.tgz manifest.env >SHA256SUMS
    )
    verify_backup "$temporary"
    mv "$temporary" "$backup_path"
    prune_backups_if_configured "$protected_path"
    printf '%s\n' "$backup_path"
}

restore_backup() {
    local backup_path="$1"
    local expected_version backup_version backup_postgres_volume backup_backend_volume backup_relative
    expected_version="$2"
    verify_backup "$backup_path"
    backup_version="$(backup_manifest_value "$backup_path" VERSION)"
    [[ "$backup_version" == "$expected_version" ]] ||
        fail "备份版本不匹配：期望 $expected_version，备份属于 ${backup_version:-未知版本}"
    backup_postgres_volume="$(backup_manifest_value "$backup_path" POSTGRES_VOLUME)"
    backup_backend_volume="$(backup_manifest_value "$backup_path" BACKEND_VOLUME)"
    backup_relative="$(backup_directory_relative_path "$backup_path")"
    [[ "$backup_postgres_volume" == "$HMAIGC_POSTGRES_DATA_VOLUME" ]] ||
        fail "PostgreSQL 卷不匹配：当前 $HMAIGC_POSTGRES_DATA_VOLUME，备份属于 $backup_postgres_volume"
    [[ "$backup_backend_volume" == "$HMAIGC_BACKEND_DATA_VOLUME" ]] ||
        fail "后端资源卷不匹配：当前 $HMAIGC_BACKEND_DATA_VOLUME，备份属于 $backup_backend_volume"

    stop_application
    start_infrastructure
    log "恢复 PostgreSQL：$backup_path"
    compose exec -T postgres sh -ceu '
        dropdb -U "$POSTGRES_USER" --if-exists --force "$POSTGRES_DB"
        createdb -U "$POSTGRES_USER" "$POSTGRES_DB"
    '
    compose exec -T postgres sh -ceu '
        pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
            --exit-on-error --no-owner --no-privileges
    ' <"$backup_path/postgres.dump"

    log "恢复后端资源卷：$backup_path"
    docker run --rm \
        --mount "type=volume,src=$HMAIGC_BACKEND_DATA_VOLUME,dst=/target" \
        --mount "type=bind,src=$BACKUP_DIR,dst=/backups,readonly" \
        --env "BACKUP_RELATIVE=$backup_relative" \
        "$BACKUP_HELPER_IMAGE" \
        sh -ceu '
            find /target -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
            tar -xzf "/backups/$BACKUP_RELATIVE/backend-data.tgz" -C /target
        '
}
