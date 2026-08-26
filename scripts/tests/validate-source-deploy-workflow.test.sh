#!/usr/bin/env bash

set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORKFLOW="$REPO_ROOT/.github/workflows/publish-images.yml"
RELEASE_RUNNER="$REPO_ROOT/deploy/hmaigc-release-runner.sh"

require_text() {
    local text="$1"
    grep -Fq -- "$text" "$WORKFLOW" || {
        printf 'source deployment workflow is missing: %s\n' "$text" >&2
        exit 1
    }
}

require_runner_text() {
    local text="$1"
    grep -Fq -- "$text" "$RELEASE_RUNNER" || {
        printf 'source release runner is missing: %s\n' "$text" >&2
        exit 1
    }
}

reject_text() {
    local text="$1"
    if grep -Fq -- "$text" "$WORKFLOW"; then
        printf 'source deployment workflow still contains retired behavior: %s\n' "$text" >&2
        exit 1
    fi
}

require_text 'deploy-production:'
require_text 'environment: production'
require_text 'group: hmaigc-production'
require_text 'cancel-in-progress: false'
require_text 'HMAIGC_PRODUCTION_SSH_HOST_KEY'
require_text 'HMAIGC_PRODUCTION_DEPLOY_ROOT'
require_text 'production tag must be a shell-safe semantic version'
require_text 'production deploy root must be a dedicated absolute path'
require_text 'hmaigc-release-runner.sh'
require_text 'StrictHostKeyChecking=yes'
require_runner_text 'export HMAIGC_DEPLOY_STATE_DIR="$deploy_root/shared/deploy-state"'
require_runner_text 'export HMAIGC_BACKUP_DIR="$deploy_root/shared/deploy-state/backups"'
reject_text 'hmaigc-ops-controller'
reject_text 'StrictHostKeyChecking=no'
reject_text 'git pull'

printf 'source deployment workflow contract passed\n'
