#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

# Verify that every publishable module is tidy without requiring release tags
# to exist first. Nested modules are checked through temporary modfiles whose
# only extra state is a local replacement for not-yet-published sibling modules.

set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/confii-module-verify.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM
GOCACHE="$fixture_dir/go-build"
export GOCACHE

verify_root() {
	cd "$repo_root"
	GOWORK=off go mod tidy -diff
	GOWORK=off go mod verify
}

verify_nested() {
	name=$1
	module_dir=$2
	shift 2

	cp "$module_dir/go.mod" "$fixture_dir/$name.mod"
	cp "$module_dir/go.sum" "$fixture_dir/$name.sum"
	: >"$fixture_dir/$name.replacements"

	while [ "$#" -gt 0 ]; do
		replacement=$1
		printf '%s\n' "${replacement%%=*}" >>"$fixture_dir/$name.replacements"
		go mod edit -modfile="$fixture_dir/$name.mod" -replace="$replacement"
		shift
	done

	cd "$module_dir"
	GOWORK=off go mod tidy -modfile="$fixture_dir/$name.mod"

	# Local replacements are a pre-release verification aid, never release
	# metadata. Remove them before comparing the generated manifests.
	case "$name" in
	loader|secret)
		go mod edit -modfile="$fixture_dir/$name.mod" \
			-dropreplace=github.com/confiify/confii-go/v2
		;;
	example)
		go mod edit -modfile="$fixture_dir/$name.mod" \
			-dropreplace=github.com/confiify/confii-go/v2 \
			-dropreplace=github.com/confiify/confii-go/loader/cloud/v2 \
			-dropreplace=github.com/confiify/confii-go/secret/cloud/v2
		;;
	esac

	# Tidy removes checksums for locally replaced modules. Those checksums are
	# still valid—and required by an ordinary consumer once a referenced
	# internal release exists—so preserve only the entries corresponding to
	# replacements introduced by this verifier. All other sum drift remains a
	# hard failure.
	while IFS= read -r replaced_module; do
		LC_ALL=C awk -v module="$replaced_module" '
			FNR == NR {
				if ($1 == module) preserved[++preserved_count] = $0
				next
			}
			$1 == module { next }
			!inserted && preserved_count > 0 && $1 > module {
				for (i = 1; i <= preserved_count; i++) print preserved[i]
				inserted = 1
			}
			{ print }
			END {
				if (!inserted) {
					for (i = 1; i <= preserved_count; i++) print preserved[i]
				}
			}
		' "$module_dir/go.sum" "$fixture_dir/$name.sum" \
			>"$fixture_dir/$name.sum.merged"
		mv "$fixture_dir/$name.sum.merged" "$fixture_dir/$name.sum"
	done <"$fixture_dir/$name.replacements"

	diff -u "$module_dir/go.mod" "$fixture_dir/$name.mod"
	diff -u "$module_dir/go.sum" "$fixture_dir/$name.sum"
}

verify_root
verify_nested loader "$repo_root/loader/cloud" \
	"github.com/confiify/confii-go/v2=$repo_root"
verify_nested secret "$repo_root/secret/cloud" \
	"github.com/confiify/confii-go/v2=$repo_root"
verify_nested example "$repo_root/examples/cloud" \
	"github.com/confiify/confii-go/v2=$repo_root" \
	"github.com/confiify/confii-go/loader/cloud/v2=$repo_root/loader/cloud" \
	"github.com/confiify/confii-go/secret/cloud/v2=$repo_root/secret/cloud"
verify_nested security_insights "$repo_root/tools/security-insights-check"

echo "All module manifests are tidy and verified."
