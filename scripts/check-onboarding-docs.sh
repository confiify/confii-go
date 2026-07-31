#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

extract_section() {
	file=$1
	start=$2
	end=$3
	output=$4

	awk -v start="$start" -v end="$end" '
		$0 == start { in_section = 1 }
		in_section && $0 == end { exit }
		in_section { print }
	' "$file" >"$output"

	if [ ! -s "$output" ]; then
		echo "missing onboarding section $start in $file" >&2
		exit 1
	fi
}

require_ordered_workflow() {
	file=$1
	previous=0
	shift

	for command in "$@"; do
		line=$(awk -v command="$command" '$0 == command { print NR; exit }' "$file")
		if [ -z "$line" ]; then
			echo "missing onboarding command in $file: $command" >&2
			exit 1
		fi
		if [ "$line" -le "$previous" ]; then
			echo "onboarding command is out of order in $file: $command" >&2
			exit 1
		fi
		previous=$line
	done
}

extract_section README.md "## Quick Start" "## Environment Strategies" "$tmp_dir/readme"
extract_section docs/index.md "## Quick Start" "## Documentation" "$tmp_dir/index"
extract_section docs/quickstart.md "# Quick Start" "## 4. Define a typed configuration" "$tmp_dir/quickstart"
extract_section llms.txt "## Quick Start" "### Self-configured usage" "$tmp_dir/llms"

for section in "$tmp_dir/readme" "$tmp_dir/index" "$tmp_dir/quickstart" "$tmp_dir/llms"; do
	require_ordered_workflow "$section" \
		"mkdir my-service" \
		"go mod init example.com/my-service" \
		"go get github.com/confiify/confii-go/v2@latest" \
		"go install github.com/confiify/confii-go/v2/confii@latest" \
		"confii --version" \
		"confii init"
done

echo "onboarding documentation is consistent"
