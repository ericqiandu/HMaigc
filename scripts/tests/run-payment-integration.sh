#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="$repo_root/deploy/tests/docker-compose.payment-integration.yml"
run_pattern='Test.*(PaymentIntegrity|MembershipOrderIdempotency|PaymentCheckoutSession)'
run_all=false
run_pattern_overridden=false
required_tests=()

while (($# > 0)); do
    case "$1" in
        --run)
            (($# >= 2)) || { echo "--run requires a Go test pattern" >&2; exit 2; }
			[[ "$run_all" == false ]] || { echo "--all and --run are mutually exclusive" >&2; exit 2; }
			[[ "$run_pattern_overridden" == false ]] || { echo "--run may only be specified once" >&2; exit 2; }
            run_pattern="$2"
			run_pattern_overridden=true
            shift 2
            ;;
		--all)
			[[ "$run_pattern_overridden" == false ]] || { echo "--all and --run are mutually exclusive" >&2; exit 2; }
			[[ "$run_all" == false ]] || { echo "--all may only be specified once" >&2; exit 2; }
			run_all=true
			shift
			;;
		--require)
			(($# >= 2)) || { echo "--require needs an exact Go test name" >&2; exit 2; }
			required_tests+=("$2")
			shift 2
			;;
        *)
            echo "unknown argument: $1" >&2
            exit 2
            ;;
    esac
done
if [[ "$run_all" == true && ${#required_tests[@]} -gt 0 ]]; then
	echo "--require must be used with --run" >&2
	exit 2
fi

command -v docker >/dev/null 2>&1 || { echo "docker is required for payment integration tests" >&2; exit 1; }
suffix="$(date +%s)-${RANDOM:-0}-$$"
project="hmaigc-payment-integration-$suffix"

cleanup() {
	local status=$?
	trap - EXIT INT TERM
	if ! docker compose --project-name "$project" --file "$compose_file" down --volumes --remove-orphans >/dev/null; then
		echo "failed to remove payment integration project $project" >&2
		status=1
	fi
	exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker compose --project-name "$project" --file "$compose_file" up --detach --wait
postgres_port="$(docker compose --project-name "$project" --file "$compose_file" port postgres 5432 | awk -F: 'END {print $NF}')"
redis_port="$(docker compose --project-name "$project" --file "$compose_file" port redis 6379 | awk -F: 'END {print $NF}')"
[[ "$postgres_port" =~ ^[0-9]+$ ]] || { echo "failed to resolve PostgreSQL test port" >&2; exit 1; }
[[ "$redis_port" =~ ^[0-9]+$ ]] || { echo "failed to resolve Redis test port" >&2; exit 1; }

export DATABASE_URL="postgres://hmaigc:integration-only-password@127.0.0.1:${postgres_port}/hmaigc_payment_integration?sslmode=disable"
export REDIS_URL="redis://127.0.0.1:${redis_port}/0"
export CANVAS_REQUIRE_INTEGRATION_TESTS=1

cd "$repo_root/backend"
test_packages=(
	./internal/database
	./internal/repository
	./internal/service
)
if [[ "$run_all" == false ]]; then
	matched_tests="$(go test "${test_packages[@]}" -list "$run_pattern")"
	if ! grep -Eq '^Test' <<<"$matched_tests"; then
		echo "required integration test pattern matched zero tests: $run_pattern" >&2
		exit 1
	fi
	for required_test in "${required_tests[@]}"; do
		if ! grep -Fxq "$required_test" <<<"$matched_tests"; then
			echo "required integration test was not matched: $required_test" >&2
			exit 1
		fi
	done
fi
go_test_arguments=(
	test
	"${test_packages[@]}"
)
if [[ "$run_all" == false ]]; then
	go_test_arguments+=(--run "$run_pattern")
fi
go_test_arguments+=(-count=1)
go "${go_test_arguments[@]}"
