#!/usr/bin/env bash

set -Eeuo pipefail

readonly TEST_ROOT="$(mktemp -d)"
trap 'rm -rf -- "$TEST_ROOT"' EXIT
mkdir -p "$TEST_ROOT/bin" "$TEST_ROOT/dist/assets"

cat >"$TEST_ROOT/dist/index.html" <<'EOF'
<html>
  <head><link rel="stylesheet" href="https://static.example.invalid/releases/v1.0.14/assets/app.css"></head>
  <body><div id="root"></div><script type="module" src="https://static.example.invalid/releases/v1.0.14/assets/app.js"></script></body>
</html>
EOF
printf 'body{}' >"$TEST_ROOT/dist/assets/app.css"
printf 'console.log("ready")' >"$TEST_ROOT/dist/assets/app.js"

cat >"$TEST_ROOT/bin/curl" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
url="${*: -1}"
[[ "$url" != *"/assets/app.js" || "${FAKE_MISSING_ENTRY_ASSET:-false}" != "true" ]]
EOF
chmod +x "$TEST_ROOT/bin/curl"

readonly REPOSITORY_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
readonly VERIFY_COMMAND="$REPOSITORY_ROOT/scripts/verify-static-release-assets.sh"
readonly RELEASE_URL="https://static.example.invalid/releases/v1.0.14"

PATH="$TEST_ROOT/bin:$PATH" bash "$VERIFY_COMMAND" "$TEST_ROOT/dist" "$RELEASE_URL"

if PATH="$TEST_ROOT/bin:$PATH" FAKE_MISSING_ENTRY_ASSET=true \
    bash "$VERIFY_COMMAND" "$TEST_ROOT/dist" "$RELEASE_URL"; then
    printf 'static release verification unexpectedly accepted a missing entry asset\n' >&2
    exit 1
fi

printf 'static release entry asset verification test passed\n'
