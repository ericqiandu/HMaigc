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
	printf '%s\n' "$PWD" >"$PAYMENT_RUNNER_CAPTURE/working-directory"
	printf '%s\n' "$@" >"$PAYMENT_RUNNER_CAPTURE/go-arguments"
	printf '%s\n' \
		"CANVAS_REQUIRE_INTEGRATION_TESTS=$CANVAS_REQUIRE_INTEGRATION_TESTS" \
		"DATABASE_URL=$DATABASE_URL" \
		"REDIS_URL=$REDIS_URL" >"$PAYMENT_RUNNER_CAPTURE/environment"
	if [[ " $* " == *' -list '* ]]; then
		printf '%s\n' "${PAYMENT_RUNNER_LIST_OUTPUT:-TestPaymentIntegrity}"
	fi
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

zero_capture="$test_root/zero-match"
mkdir -p "$zero_capture"
set +e
PAYMENT_RUNNER_CAPTURE="$zero_capture" PAYMENT_RUNNER_LIST_OUTPUT="NO_MATCH" bash "$runner" --run '^TestDefinitelyMissing$' \
	>"$zero_capture/stdout" 2>"$zero_capture/stderr"
status=$?
set -e
if [[ "$status" -ne 1 ]]; then
	printf 'zero-match run exited %d, want 1\n' "$status" >&2
	exit 1
fi
if ! grep -Fq 'matched zero tests' "$zero_capture/stderr"; then
	printf '%s\n' 'zero-match run did not report the missing required tests' >&2
	exit 1
fi

required_capture="$test_root/required-match"
mkdir -p "$required_capture"
PAYMENT_RUNNER_CAPTURE="$required_capture" PAYMENT_RUNNER_LIST_OUTPUT=$'TestRequiredOne\nTestRequiredTwo' \
	bash "$runner" --run '^TestRequired' --require TestRequiredOne --require TestRequiredTwo

missing_required_capture="$test_root/required-missing"
mkdir -p "$missing_required_capture"
set +e
PAYMENT_RUNNER_CAPTURE="$missing_required_capture" PAYMENT_RUNNER_LIST_OUTPUT='TestRequiredOne' \
	bash "$runner" --run '^TestRequired' --require TestRequiredOne --require TestRequiredTwo \
	>"$missing_required_capture/stdout" 2>"$missing_required_capture/stderr"
status=$?
set -e
if [[ "$status" -ne 1 ]]; then
	printf 'missing required test exited %d, want 1\n' "$status" >&2
	exit 1
fi
if ! grep -Fq 'required integration test was not matched: TestRequiredTwo' "$missing_required_capture/stderr"; then
	printf '%s\n' 'missing required test was not reported exactly' >&2
	exit 1
fi

printf '%s\n' 'payment integration runner contract test passed'
