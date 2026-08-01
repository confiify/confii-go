#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

usage() {
	cat >&2 <<'USAGE'
usage: sh scripts/release-watch-pr.sh <pr-number-or-url>

Watches PR checks to completion and prints a focused failure summary when any
check fails.

Environment:
  RELEASE_WATCH_INTERVAL  polling interval in seconds (default: 15)
USAGE
	exit 2
}

pr=${1:-}
[ -n "$pr" ] || usage
interval=${RELEASE_WATCH_INTERVAL:-15}

echo "Watching PR checks for $pr..."
if gh pr checks "$pr" --watch --interval "$interval" --fail-fast; then
	echo ""
	echo "All PR checks passed."
	gh pr view "$pr" --json state,reviewDecision,mergeStateStatus,url \
		--template 'state={{.state}} review={{.reviewDecision}} merge={{.mergeStateStatus}} url={{.url}}{{"\n"}}'
	exit 0
fi

echo ""
echo "One or more PR checks failed. Current check summary:"
gh pr checks "$pr" || true

echo ""
echo "Recent failed workflow runs for this branch:"
branch=$(gh pr view "$pr" --json headRefName --template '{{.headRefName}}')
gh run list --branch "$branch" --limit 10 --json databaseId,name,status,conclusion,url \
	--template '{{range .}}{{if eq .conclusion "failure"}}- {{.databaseId}} {{.name}} {{.url}}{{"\n"}}{{end}}{{end}}' || true

echo ""
echo "Inspect a failed run with:"
echo "  sh scripts/release-diagnose-run.sh <run-id>"
exit 1
