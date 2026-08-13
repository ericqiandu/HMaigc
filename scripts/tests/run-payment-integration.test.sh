#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
runner="$repo_root/scripts/tests/run-payment-integration.sh"
test_root="$(mktemp -d)"

cleanup() {
	rm -r -- "$test_root"
}
trap cleanup EXIT

docker() {
	printf '%s\n' "$*" >>"$PAYMENT_RUNNER_CAPTURE/docker-commands"
	case "$*" in
		*' port postgres 5432')
			printf '127.0.0.1:55432\n'
			;;
		*' port redis 6379')
			printf '127.0.0.1:56379\n'
			;;
	esac
}

go() {
	if [[ " $* " == *' -list '* ]]; then
		printf '%s\n' TestPostgresWatermarkPublicationSerializesConcurrentVersions TestPostgresWatermarkPreferenceAndConsentRollbackTogether TestPostgresTaskCreateFreezesWatermarkWithBillingAndProviderRuntime TestPostgresTaskCreateRollsBackWhenWatermarkLogFails TestPostgresWatermarkTaskFreezeAllowsConcurrentReaders
		return
	fi
	printf '%s\n' "$PWD" >"$PAYMENT_RUNNER_CAPTURE/working-directory"
	printf '%s\n' "$@" >"$PAYMENT_RUNNER_CAPTURE/go-arguments"
	printf '%s\n' \
		"CANVAS_REQUIRE_INTEGRATION_TESTS=$CANVAS_REQUIRE_INTEGRATION_TESTS" \
		"DATABASE_URL=$DATABASE_URL" \
		"REDIS_URL=$REDIS_URL" >"$PAYMENT_RUNNER_CAPTURE/environment"
}

export -f docker go

all_capture="$test_root/all"
mkdir -p "$all_capture"
PAYMENT_RUNNER_CAPTURE="$all_capture" bash "$runner" --all

mapfile -t all_arguments <"$all_capture/go-arguments"
expected_all_arguments=(
	test
	./internal/database
	./internal/repository
	./internal/service
	-count=1
)
if [[ "${all_arguments[*]}" != "${expected_all_arguments[*]}" ]]; then
	printf 'unexpected --all Go arguments: %q\n' "${all_arguments[*]}" >&2
	exit 1
fi

expected_backend_directory="$repo_root/backend"
actual_backend_directory="$(<"$all_capture/working-directory")"
if [[ "$actual_backend_directory" != "$expected_backend_directory" ]]; then
	printf 'runner used working directory %q, want %q\n' \
		"$actual_backend_directory" "$expected_backend_directory" >&2
	exit 1
fi

expected_environment="$test_root/expected-environment"
printf '%s\n' \
	'CANVAS_REQUIRE_INTEGRATION_TESTS=1' \
	'DATABASE_URL=postgres://hmaigc:integration-only-password@127.0.0.1:55432/hmaigc_payment_integration?sslmode=disable' \
	'REDIS_URL=redis://127.0.0.1:56379/0' >"$expected_environment"
if ! cmp -s "$expected_environment" "$all_capture/environment"; then
	printf '%s\n' 'runner did not enforce the isolated PostgreSQL and Redis environment' >&2
	diff -u "$expected_environment" "$all_capture/environment" >&2 || true
	exit 1
fi

if ! grep -Fq 'down --volumes --remove-orphans' "$all_capture/docker-commands"; then
	printf '%s\n' 'runner did not clean up its isolated Compose project' >&2
	exit 1
fi

for arguments in '--all --run TestPayment' '--run TestPayment --all'; do
	conflict_capture="$test_root/conflict-${arguments//[^[:alnum:]]/-}"
	mkdir -p "$conflict_capture"
	read -r -a conflict_arguments <<<"$arguments"
	set +e
	PAYMENT_RUNNER_CAPTURE="$conflict_capture" bash "$runner" "${conflict_arguments[@]}" \
		>"$conflict_capture/stdout" 2>"$conflict_capture/stderr"
	status=$?
	set -e
	if [[ "$status" -ne 2 ]]; then
		printf 'conflicting arguments %q exited %d, want 2\n' "$arguments" "$status" >&2
		exit 1
	fi
	if [[ -e "$conflict_capture/go-arguments" ]]; then
		printf 'conflicting arguments %q unexpectedly started Go tests\n' "$arguments" >&2
		exit 1
	fi
done

required_capture="$test_root/required"
mkdir -p "$required_capture"
PAYMENT_RUNNER_CAPTURE="$required_capture" bash "$runner" \
	--run '^TestPostgresWatermark' \
	--require TestPostgresWatermarkPublicationSerializesConcurrentVersions \
	--require TestPostgresWatermarkPreferenceAndConsentRollbackTogether \
	--require TestPostgresTaskCreateFreezesWatermarkWithBillingAndProviderRuntime \
	--require TestPostgresTaskCreateRollsBackWhenWatermarkLogFails \
	--require TestPostgresWatermarkTaskFreezeAllowsConcurrentReaders

missing_capture="$test_root/missing"
mkdir -p "$missing_capture"
set +e
PAYMENT_RUNNER_CAPTURE="$missing_capture" bash "$runner" --run '^TestPostgresWatermark' --require TestPostgresWatermarkMissing \
	>"$missing_capture/stdout" 2>"$missing_capture/stderr"
missing_status=$?
set -e
if [[ "$missing_status" -ne 1 ]] || ! grep -Fq 'required Go test not found: TestPostgresWatermarkMissing' "$missing_capture/stderr"; then
	printf 'missing required test gate exited %d or returned the wrong error\n' "$missing_status" >&2
	exit 1
fi

printf '%s\n' 'payment integration runner contract test passed'
