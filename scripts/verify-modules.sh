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

	while [ "$#" -gt 0 ]; do
		go mod edit -modfile="$fixture_dir/$name.mod" -replace="$1"
		shift
	done

	cd "$module_dir"
	GOWORK=off go mod tidy -modfile="$fixture_dir/$name.mod"

	# Local replacements are a pre-release verification aid, never release
	# metadata. Remove them before comparing the generated manifests.
	case "$name" in
	loader|secret)
		go mod edit -modfile="$fixture_dir/$name.mod" \
			-dropreplace=github.com/confiify/confii-go
		;;
	example)
		go mod edit -modfile="$fixture_dir/$name.mod" \
			-dropreplace=github.com/confiify/confii-go \
			-dropreplace=github.com/confiify/confii-go/loader/cloud \
			-dropreplace=github.com/confiify/confii-go/secret/cloud
		;;
	esac

	cmp "$module_dir/go.mod" "$fixture_dir/$name.mod"
	cmp "$module_dir/go.sum" "$fixture_dir/$name.sum"
}

verify_root
verify_nested loader "$repo_root/loader/cloud" \
	"github.com/confiify/confii-go=$repo_root"
verify_nested secret "$repo_root/secret/cloud" \
	"github.com/confiify/confii-go=$repo_root"
verify_nested example "$repo_root/examples/cloud" \
	"github.com/confiify/confii-go=$repo_root" \
	"github.com/confiify/confii-go/loader/cloud=$repo_root/loader/cloud" \
	"github.com/confiify/confii-go/secret/cloud=$repo_root/secret/cloud"

echo "All module manifests are tidy and verified."
