#!/usr/bin/env bash

set -Eeuo pipefail

readonly VERSION="${1:-}"
readonly REPOSITORY_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly CHANGELOG_PATH="${2:-$REPOSITORY_ROOT/CHANGELOG.md}"

[[ -n "$VERSION" ]] || {
    printf 'usage: %s vX.Y.Z [CHANGELOG_PATH]\n' "$0" >&2
    exit 1
}
[[ -f "$CHANGELOG_PATH" ]] || {
    printf 'changelog not found: %s\n' "$CHANGELOG_PATH" >&2
    exit 1
}

awk -v heading="## $VERSION" '
    $0 == heading || index($0, heading " - ") == 1 {
        found = 1
        next
    }
    found && /^## / {
        exit
    }
    found {
        lines[++count] = $0
        if ($0 ~ /[^[:space:]]/) {
            nonempty = 1
        }
    }
    END {
        if (!found) {
            print "release section not found: " heading > "/dev/stderr"
            exit 2
        }
        if (!nonempty) {
            print "release section is empty: " heading > "/dev/stderr"
            exit 3
        }
        for (line_number = 1; line_number <= count; line_number++) {
            print lines[line_number]
        }
    }
' "$CHANGELOG_PATH"
