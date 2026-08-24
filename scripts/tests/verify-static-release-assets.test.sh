#!/usr/bin/env bash

set -Eeuo pipefail

readonly TEST_ROOT="$(mktemp -d)"
trap 'rm -rf -- "$TEST_ROOT"' EXIT
mkdir -p "$TEST_ROOT/bin" "$TEST_ROOT/dist/assets"
mkdir -p "$TEST_ROOT/direct-oss-dist/assets"

cat >"$TEST_ROOT/dist/index.html" <<'EOF'
<html>
  <head><link rel="stylesheet" href="https://static.hm.kunagent.com/hmaigc/web/releases/v1.0.14/assets/app.css"></head>
  <body><div id="root"></div><script type="module" src="https://static.hm.kunagent.com/hmaigc/web/releases/v1.0.14/assets/app.js"></script></body>
</html>
EOF
printf 'body{}' >"$TEST_ROOT/dist/assets/app.css"
{
    printf '/*'
    head -c 1019 /dev/zero | tr '\0' 'x'
    printf '*/\n'
} >"$TEST_ROOT/dist/assets/app.js"
cp "$TEST_ROOT/dist/assets/app.css" "$TEST_ROOT/direct-oss-dist/assets/app.css"
cp "$TEST_ROOT/dist/assets/app.js" "$TEST_ROOT/direct-oss-dist/assets/app.js"
sed 's#https://static.hm.kunagent.com/hmaigc/web#https://hmaigc-prod-static.oss-cn-hongkong.aliyuncs.com/hmaigc/web#g' \
    "$TEST_ROOT/dist/index.html" >"$TEST_ROOT/direct-oss-dist/index.html"

cat >"$TEST_ROOT/bin/curl" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail

headers_file=""
url=""
write_out=""
while (($# > 0)); do
    case "$1" in
        --dump-header|-D)
            headers_file="$2"
            shift 2
            ;;
        --write-out|-w)
            write_out="$2"
            shift 2
            ;;
        --output|-o|--header|-H)
            shift 2
            ;;
        --fail|--silent|--show-error|--location|--retry-all-errors)
            shift
            ;;
        --retry|--retry-delay)
            shift 2
            ;;
        *)
            url="$1"
            shift
            ;;
    esac
done

[[ "$url" != *"/assets/app.js" || "${FAKE_MISSING_ENTRY_ASSET:-false}" != "true" ]]

if [[ "$url" == *.css ]]; then
    content_type="${FAKE_CONTENT_TYPE:-text/css}"
else
    content_type="${FAKE_CONTENT_TYPE:-application/javascript}"
fi
content_encoding="${FAKE_CONTENT_ENCODING:-br}"
if [[ "$url" == *.css && "${FAKE_SMALL_ASSET_IDENTITY:-false}" == "true" ]]; then
    content_encoding=""
fi

if [[ -n "$headers_file" ]]; then
    cat >"$headers_file" <<HEADERS
HTTP/2 200
content-type: $content_type
content-encoding: $content_encoding
cache-control: ${FAKE_CACHE_CONTROL:-public,max-age=31536000,immutable}
access-control-allow-origin: ${FAKE_CORS_ORIGIN:-https://hm.kunagent.com}
access-control-allow-methods: ${FAKE_CORS_METHODS:-GET,HEAD,OPTIONS}

HEADERS
fi
if [[ "$write_out" == *url_effective* ]]; then
    printf '%s|%s' "${FAKE_HTTP_VERSION:-2}" "${FAKE_EFFECTIVE_URL:-$url}"
else
    printf '%s' "${FAKE_HTTP_VERSION:-2}"
fi
EOF
chmod +x "$TEST_ROOT/bin/curl"

readonly REPOSITORY_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly VERIFY_COMMAND="$REPOSITORY_ROOT/scripts/verify-static-release-assets.sh"
readonly RELEASE_URL="https://static.hm.kunagent.com/hmaigc/web/releases/v1.0.14"

assert_rejected() {
    local label="$1"
    shift
    if PATH="$TEST_ROOT/bin:$PATH" "$@" bash "$VERIFY_COMMAND" "$TEST_ROOT/dist" "$RELEASE_URL"; then
        printf 'static release verification unexpectedly accepted %s\n' "$label" >&2
        exit 1
    fi
}

PATH="$TEST_ROOT/bin:$PATH" bash "$VERIFY_COMMAND" "$TEST_ROOT/dist" "$RELEASE_URL"
PATH="$TEST_ROOT/bin:$PATH" FAKE_SMALL_ASSET_IDENTITY=true bash "$VERIFY_COMMAND" "$TEST_ROOT/dist" "$RELEASE_URL"
cp "$TEST_ROOT/dist/assets/app.js" "$TEST_ROOT/eligible-app.js"
dd if=/dev/zero of="$TEST_ROOT/dist/assets/app.js" bs=1 count=0 seek=10485760 status=none
assert_rejected "an uncompressed JavaScript response at the 10 MiB boundary" env FAKE_CONTENT_ENCODING=identity
dd if=/dev/zero of="$TEST_ROOT/dist/assets/app.js" bs=1 count=0 seek=10485761 status=none
PATH="$TEST_ROOT/bin:$PATH" FAKE_CONTENT_ENCODING=identity bash "$VERIFY_COMMAND" "$TEST_ROOT/dist" "$RELEASE_URL"
mv "$TEST_ROOT/eligible-app.js" "$TEST_ROOT/dist/assets/app.js"

assert_rejected "a missing entry asset" env FAKE_MISSING_ENTRY_ASSET=true
assert_rejected "HTTP/1.1 delivery" env FAKE_HTTP_VERSION=1.1
assert_rejected "an uncompressed JavaScript response" env FAKE_CONTENT_ENCODING=identity
assert_rejected "an unsupported content encoding" env FAKE_CONTENT_ENCODING=compress
assert_rejected "a mutable cache policy" env FAKE_CACHE_CONTROL=no-cache
assert_rejected "a shared-cache-only policy" env FAKE_CACHE_CONTROL=public,s-max-age=31536000,immutable
assert_rejected "an unrelated CORS origin" env FAKE_CORS_ORIGIN=https://example.invalid
assert_rejected "a CORS write method" env FAKE_CORS_METHODS=GET,HEAD,OPTIONS,POST
assert_rejected "an off-origin redirect" env FAKE_EFFECTIVE_URL=https://hmaigc-prod-static.oss-cn-hongkong.aliyuncs.com/hmaigc/web/releases/v1.0.14/assets/app.js

if PATH="$TEST_ROOT/bin:$PATH" bash "$VERIFY_COMMAND" "$TEST_ROOT/direct-oss-dist" \
    "https://hmaigc-prod-static.oss-cn-hongkong.aliyuncs.com/hmaigc/web/releases/v1.0.14"; then
    printf 'static release verification unexpectedly accepted a direct OSS delivery URL\n' >&2
    exit 1
fi

printf 'static release entry asset verification test passed\n'
