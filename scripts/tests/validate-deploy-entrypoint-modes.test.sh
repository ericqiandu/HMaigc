#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

for entrypoint in \
    deploy/hmaigc.sh \
    deploy/hmaigc-bootstrap.sh \
    deploy/hmaigc-ops.sh \
    deploy/hmaigc-stage.sh; do
    mode="$(git -C "$REPO_ROOT" ls-files --stage -- "$entrypoint" | awk '{print $1}')"
    if [[ "$mode" != "100755" ]]; then
        printf '%s must be committed with mode 100755; got %s\n' "$entrypoint" "${mode:-missing}" >&2
        exit 1
    fi
done
