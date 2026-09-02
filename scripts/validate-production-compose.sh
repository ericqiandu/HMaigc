#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

CANVAS_DATA_PATH="${CANVAS_DATA_PATH:-./.local/ci-data}" \
    docker compose -f docker-compose.yml config -q

docker compose \
    --env-file deploy/tests/fixtures/production.env \
    -f docker-compose.production.yml \
    config -q

docker compose \
    --env-file deploy/tests/fixtures/ops.env \
    -f deploy/docker-compose.ops.yml \
    config -q
