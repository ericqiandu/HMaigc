#!/usr/bin/env bash

set -Eeuo pipefail

readonly DIST_DIR="${1:?用法：verify-static-release-assets.sh <dist-dir> <release-url>}"
readonly RELEASE_URL="${2:?用法：verify-static-release-assets.sh <dist-dir> <release-url>}"
readonly INDEX_FILE="$DIST_DIR/index.html"

[[ -f "$INDEX_FILE" ]] || {
    printf '静态发布入口不存在：%s\n' "$INDEX_FILE" >&2
    exit 1
}
[[ "$RELEASE_URL" == https://* && "$RELEASE_URL" != */ ]] || {
    printf '静态发布地址必须是无尾斜杠的 HTTPS URL：%s\n' "$RELEASE_URL" >&2
    exit 1
}

entry_asset_count=0
while IFS= read -r asset_url; do
    [[ -n "$asset_url" ]] || continue
    entry_asset_count=$((entry_asset_count + 1))
    case "$asset_url" in
        "$RELEASE_URL"/*) ;;
        *)
            printf '入口资源未指向当前不可变发布目录：%s\n' "$asset_url" >&2
            exit 1
            ;;
    esac

    asset_path="${asset_url#"$RELEASE_URL"/}"
    asset_path="${asset_path%%\?*}"
    [[ -f "$DIST_DIR/$asset_path" ]] || {
        printf '入口引用的资源不在本地构建产物中：%s\n' "$asset_path" >&2
        exit 1
    }

    curl \
        --fail \
        --silent \
        --show-error \
        --location \
        --retry 8 \
        --retry-all-errors \
        --retry-delay 5 \
        --output /dev/null \
        "$asset_url" || {
            printf '入口资源无法通过公网访问：%s\n' "$asset_url" >&2
            exit 1
        }
done < <(
    grep -Eo '(src|href)="[^"]+\.(js|css)(\?[^"]*)?"' "$INDEX_FILE" |
        sed -E 's/^[^=]+="([^"]+)"$/\1/'
)

(( entry_asset_count > 0 )) || {
    printf '静态发布入口未声明任何 JS/CSS 资源：%s\n' "$INDEX_FILE" >&2
    exit 1
}

printf '静态发布入口资源验证通过，共 %s 个资源\n' "$entry_asset_count"
