#!/usr/bin/env bash

set -Eeuo pipefail

readonly IMAGE_DIGEST_PATTERN='^.+@sha256:[0-9a-f]{64}$'

require_immutable_image() {
    local image="$1"
    [[ "$image" =~ $IMAGE_DIGEST_PATTERN ]] || fail "镜像必须使用不可变 SHA-256 摘要：${image:-未配置}"
}

resolve_image_digest() {
    local tagged="$1"
    local repository repo_digest
    [[ "$tagged" != *@sha256:* ]] || fail "解析摘要时必须提供带版本标签的镜像：$tagged"
    repository="${tagged%:*}"
    [[ -n "$repository" && "$repository" != "$tagged" ]] || fail "镜像必须包含显式版本标签：$tagged"

    docker pull "$tagged" >/dev/null
    repo_digest="$({
        docker image inspect "$tagged" --format '{{range .RepoDigests}}{{println .}}{{end}}'
    } | awk -v repository="$repository" '
        index($0, repository "@sha256:") == 1 { print; exit }
    ')"
    require_immutable_image "$repo_digest"
    printf '%s\n' "$repo_digest"
}

resolve_release_images() {
    local version="$1"
    validate_release_version "$version"
    : "${HMAIGC_IMAGE_REGISTRY:?生产配置必须填写 HMAIGC_IMAGE_REGISTRY}"
    RESOLVED_BACKEND_IMAGE="$(resolve_image_digest "$HMAIGC_IMAGE_REGISTRY/hmaigc-backend:$version")"
    RESOLVED_WEB_IMAGE="$(resolve_image_digest "$HMAIGC_IMAGE_REGISTRY/hmaigc-web:$version")"
    export RESOLVED_BACKEND_IMAGE RESOLVED_WEB_IMAGE
}

use_release_images() {
    local backend_image="$1"
    local web_image="$2"
    require_immutable_image "$backend_image"
    require_immutable_image "$web_image"
    HMAIGC_BACKEND_IMAGE="$backend_image"
    HMAIGC_WEB_IMAGE="$web_image"
    export HMAIGC_BACKEND_IMAGE HMAIGC_WEB_IMAGE
}
