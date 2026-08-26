#!/usr/bin/env bash

set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNNER="$REPO_ROOT/deploy/hmaigc-release-runner.sh"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf -- "$TEST_ROOT"' EXIT

if ! command -v flock >/dev/null 2>&1; then
    mkdir -p "$TEST_ROOT/test-bin"
    cat >"$TEST_ROOT/test-bin/flock" <<'FAKE_FLOCK'
#!/usr/bin/env bash
exit 0
FAKE_FLOCK
    chmod +x "$TEST_ROOT/test-bin/flock"
    export PATH="$TEST_ROOT/test-bin:$PATH"
fi

VERSION=v1.2.3
RELEASE_DIR="$TEST_ROOT/releases/$VERSION"
mkdir -p "$RELEASE_DIR/deploy" "$TEST_ROOT/shared"
printf 'test-only\n' >"$TEST_ROOT/shared/production.env"

cat >"$RELEASE_DIR/deploy/hmaigc.sh" <<'FAKE_DEPLOY'
#!/usr/bin/env bash
set -Eeuo pipefail
printf '%s\n' "$1" >>"${FAKE_RELEASE_CALLS:?}"
if [[ "$1" == upgrade ]]; then
    sleep 2
    if [[ "$2" == "${FAKE_FAILED_RELEASE_VERSION:-never}" ]]; then
        exit 42
    fi
    printf '%s\n' "$2" >"${FAKE_UPGRADED_VERSION:?}"
fi
FAKE_DEPLOY
chmod +x "$RELEASE_DIR/deploy/hmaigc.sh"

export FAKE_RELEASE_CALLS="$TEST_ROOT/release-calls.log"
export FAKE_UPGRADED_VERSION="$TEST_ROOT/upgraded-version"

bash "$RUNNER" "$VERSION" "$TEST_ROOT" >"$TEST_ROOT/first-client.log" 2>&1 &
client_pid=$!
sleep 1
kill "$client_pid" 2>/dev/null || true
wait "$client_pid" 2>/dev/null || true

result_file="$TEST_ROOT/shared/deploy-state/actions/$VERSION/result"
deadline=$((SECONDS + 10))
while [[ ! -f "$result_file" && $SECONDS -lt $deadline ]]; do
    sleep 1
done

[[ -f "$result_file" ]] || {
    printf 'detached release worker did not finish after client disconnect\n' >&2
    cat "$TEST_ROOT/first-client.log" >&2 || true
    cat "$TEST_ROOT/shared/deploy-state/actions/$VERSION/output.log" >&2 || true
    exit 1
}
[[ "$(cat "$result_file")" == 0 ]]
[[ "$(cat "$FAKE_UPGRADED_VERSION")" == "$VERSION" ]]
[[ "$(cat "$TEST_ROOT/shared/deploy-state/current-release")" == "$RELEASE_DIR" ]]

bash "$RUNNER" "$VERSION" "$TEST_ROOT" >"$TEST_ROOT/second-client.log"
[[ "$(grep -c '^upgrade$' "$FAKE_RELEASE_CALLS")" -eq 1 ]]
[[ "$(grep -c '^status$' "$FAKE_RELEASE_CALLS")" -eq 1 ]]

FAILED_VERSION=v1.2.4
FAILED_RELEASE_DIR="$TEST_ROOT/releases/$FAILED_VERSION"
mkdir -p "$FAILED_RELEASE_DIR/deploy"
cp "$RELEASE_DIR/deploy/hmaigc.sh" "$FAILED_RELEASE_DIR/deploy/hmaigc.sh"
export FAKE_FAILED_RELEASE_VERSION="$FAILED_VERSION"

if bash "$RUNNER" "$FAILED_VERSION" "$TEST_ROOT" >"$TEST_ROOT/failed-client.log" 2>&1; then
    printf 'failed release unexpectedly succeeded\n' >&2
    exit 1
else
    failed_exit=$?
fi
[[ "$failed_exit" -eq 42 ]]
[[ "$(cat "$TEST_ROOT/shared/deploy-state/actions/$FAILED_VERSION/result")" == 42 ]]
[[ "$(cat "$TEST_ROOT/shared/deploy-state/current-release")" == "$RELEASE_DIR" ]]

if bash "$RUNNER" "$FAILED_VERSION" "$TEST_ROOT" >"$TEST_ROOT/failed-client-retry.log" 2>&1; then
    printf 'failed release retry unexpectedly succeeded\n' >&2
    exit 1
else
    failed_retry_exit=$?
fi
[[ "$failed_retry_exit" -eq 42 ]]
[[ "$(grep -c '^upgrade$' "$FAKE_RELEASE_CALLS")" -eq 2 ]]

printf 'detached source release runner smoke test passed\n'
