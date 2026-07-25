#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
report_dir=${1:-"$repo_root/bin/fuzz-introspector-report"}
report="$report_dir/fuzz_report.html"
functions="$report_dir/all-fuzz-introspector-functions.json"

[ -s "$report" ] || {
	echo "Fuzz Introspector did not produce $report" >&2
	exit 1
}
[ -s "$functions" ] || {
	echo "Fuzz Introspector did not produce $functions" >&2
	exit 1
}

rg --no-config --no-heading --with-filename --line-number \
	'^func Fuzz[A-Za-z0-9_]+' --glob '*.go' "$repo_root" |
while IFS=: read -r file _ declaration; do
	fn=$(printf '%s\n' "$declaration" | sed -E 's/^func (Fuzz[A-Za-z0-9_]+).*/\1/')
	relative=${file#"$repo_root"/}
	pkg=./$(dirname "$relative")
	target="$pkg:$fn"

	grep -F "$target" "$repo_root/Makefile" >/dev/null || {
		echo "fuzz target missing from Makefile FUZZ_TARGETS: $target" >&2
		exit 1
	}
	grep -F "\"$target\"" "$repo_root/.github/workflows/ci.yaml" >/dev/null || {
		echo "fuzz target missing from CI matrix: $target" >&2
		exit 1
	}
	grep -F "\"Func name\": \"$fn\"" "$functions" >/dev/null || {
		echo "Fuzz Introspector report did not index $fn" >&2
		exit 1
	}
done

target_count=$(rg --no-config -c '^func Fuzz[A-Za-z0-9_]+' --glob '*.go' "$repo_root" | awk -F: '{ total += $2 } END { print total + 0 }')
[ "$target_count" -gt 0 ] || {
	echo "no native Go fuzz targets found" >&2
	exit 1
}

echo "Fuzz Introspector indexed all $target_count native Go fuzz targets"
