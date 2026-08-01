#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

usage() {
	cat >&2 <<'USAGE'
usage: sh scripts/release-diagnose-run.sh <run-id>

Shows failed jobs and recent log lines for a GitHub Actions run.
USAGE
	exit 2
}

run_id=${1:-}
[ -n "$run_id" ] || usage

echo "Run summary:"
gh run view "$run_id" --json name,status,conclusion,url,createdAt,updatedAt \
	--template 'name={{.name}} status={{.status}} conclusion={{.conclusion}} url={{.url}}{{"\n"}}'

echo ""
echo "Failed jobs:"
gh run view "$run_id" --json jobs \
	--template '{{range .jobs}}{{if eq .conclusion "failure"}}- {{.name}} ({{.databaseId}}){{"\n"}}{{end}}{{end}}'

echo ""
echo "Failure-oriented log excerpts:"
gh run view "$run_id" --log-failed | awk '
	/::error|FAIL:|FAIL$|failed|Failure|panic:|Error:|exit status/ {
		print
		count++
		if (count >= 120) exit
	}
	END {
		if (count == 0) {
			print "No common failure markers found in failed-job logs."
			print "Run: gh run view " run_id " --log-failed"
		}
	}
' run_id="$run_id"
