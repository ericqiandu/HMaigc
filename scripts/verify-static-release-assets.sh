#!/usr/bin/env bash

set -Eeuo pipefail

readonly DIST_DIR="${1:?用法：verify-static-release-assets.sh <dist-dir> <release-url>}"
readonly RELEASE_URL="${2:?用法：verify-static-release-assets.sh <dist-dir> <release-url>}"
readonly INDEX_FILE="$DIST_DIR/index.html"
readonly EXPECTED_RELEASE_PREFIX="https://static.hm.kunagent.com/hmaigc/web/releases/"
readonly PRODUCTION_ORIGIN="https://hm.kunagent.com"
readonly RESPONSE_ROOT="$(mktemp -d)"

trap 'rm -rf -- "$RESPONSE_ROOT"' EXIT

[[ -f "$INDEX_FILE" ]] || {
    printf '静态发布入口不存在：%s\n' "$INDEX_FILE" >&2
    exit 1
}
[[ "$RELEASE_URL" == https://* && "$RELEASE_URL" != */ ]] || {
    printf '静态发布地址必须是无尾斜杠的 HTTPS URL：%s\n' "$RELEASE_URL" >&2
    exit 1
}
[[ "$RELEASE_URL" == "$EXPECTED_RELEASE_PREFIX"* ]] || {
    printf '正式静态资源必须通过中国大陆 CDN 域名读取，禁止直接 OSS 或其他来源：%s\n' "$RELEASE_URL" >&2
    exit 1
}

read_header() {
    local header_name="${1,,}"
    local header_file="$2"
    tr -d '\r' <"$header_file" |
        awk -F ': *' -v expected="$header_name" '
            tolower($1) == expected {
                value = substr($0, index($0, ":") + 1)
                sub(/^[[:space:]]+/, "", value)
            }
            END { print value }
        '
}

verify_delivery_headers() {
    local asset_url="$1"
    local header_file="$2"
    local http_version="$3"
    local content_type content_encoding cache_control cache_directives cors_origin cors_methods

    case "$http_version" in
        2|2.0|3|3.0) ;;
        *)
            printf '入口资源未通过 HTTP/2 或 HTTP/3 交付：%s（协议 %s）\n' "$asset_url" "$http_version" >&2
            return 1
            ;;
    esac

    content_type="$(read_header content-type "$header_file")"
    case "$asset_url" in
        *.js)
            case "${content_type,,}" in
                application/javascript*|application/x-javascript*|text/javascript*) ;;
                *)
                    printf 'JavaScript Content-Type 无效：%s（%s）\n' "$asset_url" "$content_type" >&2
                    return 1
                    ;;
            esac
            ;;
        *.css)
            [[ "${content_type,,}" == text/css* ]] || {
                printf 'CSS Content-Type 无效：%s（%s）\n' "$asset_url" "$content_type" >&2
                return 1
            }
            ;;
    esac

    content_encoding="$(read_header content-encoding "$header_file")"
    case "${content_encoding,,}" in
        br|gzip) ;;
        *)
            printf '入口资源缺少 Brotli/gzip 压缩：%s（Content-Encoding: %s）\n' "$asset_url" "$content_encoding" >&2
            return 1
            ;;
    esac

    cache_control="$(read_header cache-control "$header_file")"
    cache_directives=",$(printf '%s' "${cache_control,,}" | tr -d '[:space:]'),"
    if [[ "$cache_directives" != *,public,* || "$cache_directives" != *,max-age=31536000,* || "$cache_directives" != *,immutable,* ]]; then
        printf '入口资源不可变缓存策略无效：%s（Cache-Control: %s）\n' "$asset_url" "$cache_control" >&2
        return 1
    fi

    cors_origin="$(read_header access-control-allow-origin "$header_file")"
    [[ "$cors_origin" == "$PRODUCTION_ORIGIN" ]] || {
        printf '入口资源 CORS 必须精确允许正式业务 Origin：%s（Access-Control-Allow-Origin: %s）\n' "$asset_url" "$cors_origin" >&2
        return 1
    }

    cors_methods="$(read_header access-control-allow-methods "$header_file")"
    [[ "$(printf '%s' "${cors_methods^^}" | tr -d '[:space:]')" == "GET,HEAD,OPTIONS" ]] || {
        printf '入口资源 CORS 方法必须仅允许 GET、HEAD 与 OPTIONS：%s（Access-Control-Allow-Methods: %s）\n' "$asset_url" "$cors_methods" >&2
        return 1
    }
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

    header_file="$RESPONSE_ROOT/entry-$entry_asset_count.headers"
    if ! delivery_result="$(curl \
        --fail \
        --silent \
        --show-error \
        --location \
        --retry 8 \
        --retry-all-errors \
        --retry-delay 5 \
        --header 'Accept-Encoding: br,gzip' \
        --header "Origin: $PRODUCTION_ORIGIN" \
        --dump-header "$header_file" \
        --output /dev/null \
        --write-out '%{http_version}|%{url_effective}' \
        "$asset_url")"; then
        printf '入口资源无法通过公网访问：%s\n' "$asset_url" >&2
        exit 1
    fi
    IFS='|' read -r http_version effective_url <<<"$delivery_result"
    [[ "$effective_url" == "$asset_url" ]] || {
        printf '入口资源发生重定向，必须由中国大陆 CDN 原地址直接交付：%s（最终地址 %s）\n' "$asset_url" "$effective_url" >&2
        exit 1
    }
    verify_delivery_headers "$asset_url" "$header_file" "$http_version"
done < <(
    grep -Eo '(src|href)="[^"]+\.(js|css)(\?[^"]*)?"' "$INDEX_FILE" |
        sed -E 's/^[^=]+="([^"]+)"$/\1/'
)

(( entry_asset_count > 0 )) || {
    printf '静态发布入口未声明任何 JS/CSS 资源：%s\n' "$INDEX_FILE" >&2
    exit 1
}

printf '静态发布入口资源验证通过，共 %s 个资源\n' "$entry_asset_count"
