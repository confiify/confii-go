#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

base="${1:-${PATCH_COVERAGE_BASE:-origin/main}}"
head="${2:-${PATCH_COVERAGE_HEAD:-HEAD}}"
profile="${3:-${PATCH_COVERAGE_PROFILE:-coverage.out}}"
minimum="${PATCH_COVERAGE_MINIMUM:-90}"

if [ ! -f "$profile" ]; then
	echo "patch coverage check failed: coverage profile not found: $profile" >&2
	exit 2
fi

tmp_diff="$(mktemp "${TMPDIR:-/tmp}/confii-patch-coverage.XXXXXX")"
trap 'rm -f "$tmp_diff"' EXIT

git diff --no-ext-diff --unified=0 "$base...$head" -- '*.go' >"$tmp_diff"

awk -v minimum="$minimum" -v base="$base" -v head="$head" '
	function profile_path(path) {
		sub(/^.*github.com\/confiify\/confii-go\/v2\//, "", path)
		return path
	}

	FNR == NR && FNR > 1 {
		split($1, loc, ":")
		file = profile_path(loc[1])
		split(loc[2], span, ",")
		split(span[1], start, ".")
		split(span[2], finish, ".")
		for (line = start[1]; line <= finish[1]; line++) {
			key = file SUBSEP line
			statement[key] = 1
			if ($3 > 0) {
				covered[key] = 1
			}
		}
		next
	}

	FNR != NR {
		if ($0 ~ /^\+\+\+ b\//) {
			file = substr($0, 7)
			next
		}
		if ($0 ~ /^\+\+\+ \/dev\/null/) {
			file = ""
			next
		}
		if ($0 ~ /^@@ /) {
			if (match($0, /\+[0-9]+(,[0-9]+)?/)) {
				hunk = substr($0, RSTART + 1, RLENGTH - 1)
				split(hunk, parts, ",")
				next_line = parts[1]
			}
			next
		}
		if (file == "") {
			next
		}
		if ($0 ~ /^\+/ && $0 !~ /^\+\+\+/) {
			key = file SUBSEP next_line
			if (statement[key]) {
				total++
				file_total[file]++
				if (covered[key]) {
					hit++
					file_hit[file]++
				} else {
					missing[file] = missing[file] sprintf("%s:%d\n", file, next_line)
				}
			}
			next_line++
			next
		}
		if ($0 !~ /^-/) {
			next_line++
		}
	}

	END {
		if (total == 0) {
			print "Patch coverage: no changed Go statement lines in " base "..." head
			exit 0
		}
		percentage = 100 * hit / total
		printf "Patch coverage: %.2f%% (%d/%d); required: %.2f%%\n", percentage, hit, total, minimum
		for (file in file_total) {
			printf "  %s: %.2f%% (%d/%d)\n", file, 100 * file_hit[file] / file_total[file], file_hit[file], file_total[file]
			if (missing[file] != "") {
				printf "%s", missing[file]
			}
		}
		if (percentage + 0.000001 < minimum) {
			exit 1
		}
	}
' "$profile" "$tmp_diff"
