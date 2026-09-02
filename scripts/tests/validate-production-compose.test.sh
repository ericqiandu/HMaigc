#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

bash "$REPO_ROOT/scripts/validate-production-compose.sh"

if grep -Eq 'HMAIGC_OPS_SOCKET|HMAIGC_OPS_SHARED_SECRET_FILE|ops-state' "$REPO_ROOT/docker-compose.production.yml"; then
    echo "production Compose must not depend on the retired operations controller" >&2
    exit 1
fi
