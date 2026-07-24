#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

minimum="${BRANCH_COVERAGE_MIN:-80}"
gobco="${GOBCO:-gobco}"

case "$minimum" in
  ''|*[!0-9]*)
    echo "BRANCH_COVERAGE_MIN must be an integer percentage" >&2
    exit 2
    ;;
esac

if ! command -v "$gobco" >/dev/null 2>&1; then
  echo "gobco is required; install the pinned version with:" >&2
  echo "  go install github.com/rillig/gobco@v1.3.4" >&2
  exit 2
fi

root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
work="$(mktemp -d "${TMPDIR:-/tmp}/confii-branch-coverage.XXXXXX")"
trap 'rm -rf "$work"' EXIT HUP INT TERM

(
  cd "$root"
  go list -f '{{.ImportPath}}|{{.Dir}}' ./...
) >"$work/packages"

covered=0
conditions=0
packages=0

while IFS='|' read -r import_path package_dir; do
  case "$package_dir" in
    "$root"/examples/*|"$root"/integration)
      continue
      ;;
  esac

  packages=$((packages + 1))
  output="$work/package-$packages.log"
  # Gobco invokes `go test`; disconnect stdin so a test cannot consume the
  # remaining package-list records that drive this loop.
  if ! (cd "$package_dir" && "$gobco") </dev/null >"$output" 2>&1; then
    cat "$output" >&2
    echo "branch coverage failed for $import_path" >&2
    exit 1
  fi

  result="$(sed -n 's/^Condition coverage: \([0-9][0-9]*\)\/\([0-9][0-9]*\)$/\1 \2/p' "$output" | tail -n 1)"
  if [ -z "$result" ]; then
    cat "$output" >&2
    echo "gobco did not report condition coverage for $import_path" >&2
    exit 1
  fi

  package_covered=${result%% *}
  package_conditions=${result##* }
  covered=$((covered + package_covered))
  conditions=$((conditions + package_conditions))
  printf '%-62s %s/%s\n' "$import_path" "$package_covered" "$package_conditions"
done <"$work/packages"

if [ "$conditions" -eq 0 ]; then
  echo "branch coverage failed: no measurable conditions found" >&2
  exit 1
fi

percentage="$(awk -v covered="$covered" -v total="$conditions" 'BEGIN { printf "%.2f", 100 * covered / total }')"
printf 'Aggregate condition coverage: %s/%s (%s%%; required %s%%)\n' \
  "$covered" "$conditions" "$percentage" "$minimum"

if [ $((covered * 100)) -lt $((conditions * minimum)) ]; then
  echo "branch coverage is below the required threshold" >&2
  exit 1
fi
