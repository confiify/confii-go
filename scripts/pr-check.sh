#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

base="${PR_BASE:-origin/main}"
head="${PR_HEAD:-HEAD}"
level="${PR_CHECK_LEVEL:-fast}"
fuzztime="${PR_FUZZTIME:-5s}"
coverage_profile="${PR_COVERAGE_PROFILE:-coverage.out}"

run() {
	echo ""
	echo "==> $*"
	"$@"
}

run_sh() {
	echo ""
	echo "==> $*"
	sh -c "$*"
}

case "$level" in
	fast | full) ;;
	*)
		echo "PR_CHECK_LEVEL must be fast or full" >&2
		exit 2
		;;
esac

echo "Confii PR check"
echo "  base:  $base"
echo "  head:  $head"
echo "  level: $level"
echo "  fuzz:  $fuzztime"

if [ -n "$(git status --porcelain)" ]; then
	echo ""
	echo "warning: working tree has uncommitted changes; range checks use committed $head"
fi

run git diff --check "$base...$head"
run git diff --check
run git diff --cached --check
run sh scripts/check-dco.sh "$base" "$head"
run make lint
run go test -shuffle=on -timeout 120s ./...
run go test -race -shuffle=on -timeout 120s ./...
run make fuzz FUZZTIME="$fuzztime"
run go test -coverprofile="$coverage_profile" -covermode=atomic -timeout 120s ./...
run sh scripts/check-statement-coverage.sh "$coverage_profile"
PATCH_COVERAGE_BASE="$base" PATCH_COVERAGE_HEAD="$head" run sh scripts/check-patch-coverage.sh "$base" "$head" "$coverage_profile"

if git diff --name-only "$base...$head" | grep -Eq '^(README\.md|llms\.txt|docs/|mkdocs\.yml|Makefile|scripts/check-docs-artifact\.sh|scripts/check-onboarding-docs\.sh|scripts/check-site-headers\.sh)'; then
	run make docs-check
else
	echo ""
	echo "==> docs-check skipped; no docs-triggering files changed"
fi

if [ "$level" = "full" ]; then
	run make mod-verify
	run make test-integration
	run make test-cloud
	run make test-branch-cover
	run make reproducible-build-check
	run make api-compat
	run make reuse-lint
	run make supply-chain-check
fi

echo ""
echo "PR check passed."
