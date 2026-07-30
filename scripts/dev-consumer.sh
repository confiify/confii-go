#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

action=${1:-}
consumer_input=${2:-}
repo_input=${3:-}
scope=${4:-core}
release_version=${5:-}

usage() {
	echo "usage: $0 <link|unlink> <consumer-dir> <confii-repo-dir> <core|cloud|all> [release-version]" >&2
}

if [ "$action" != "link" ] && [ "$action" != "unlink" ]; then
	usage
	exit 2
fi
if [ -z "$consumer_input" ] || [ -z "$repo_input" ]; then
	usage
	exit 2
fi
if [ "$scope" != "core" ] && [ "$scope" != "cloud" ] && [ "$scope" != "all" ]; then
	usage
	exit 2
fi
if [ ! -d "$consumer_input" ] || [ ! -f "$consumer_input/go.mod" ]; then
	echo "CONSUMER_DIR must identify an existing Go module containing go.mod: $consumer_input" >&2
	exit 2
fi
if [ ! -d "$repo_input" ] || [ ! -f "$repo_input/go.mod" ]; then
	echo "Confii checkout does not contain go.mod: $repo_input" >&2
	exit 2
fi

consumer=$(CDPATH= cd -- "$consumer_input" && pwd -P)
repo=$(CDPATH= cd -- "$repo_input" && pwd -P)
if [ "$consumer" = "$repo" ]; then
	echo "CONSUMER_DIR must be a different Go module from the Confii checkout" >&2
	exit 2
fi

root_module=github.com/confiify/confii-go/v2
loader_module=github.com/confiify/confii-go/loader/cloud/v2
secret_module=github.com/confiify/confii-go/secret/cloud/v2
zero_version=v2.0.0-00010101000000-000000000000

if [ "$action" = "link" ]; then
	(
		cd "$consumer"
		go mod edit -replace="$root_module=$repo"
		if [ "$scope" = "cloud" ] || [ "$scope" = "all" ]; then
			go mod edit -replace="$loader_module=$repo/loader/cloud"
			go mod edit -replace="$secret_module=$repo/secret/cloud"
		fi
	)
	echo "Linked Confii development checkout into $consumer"
	echo "  $root_module => $repo"
	if [ "$scope" = "cloud" ] || [ "$scope" = "all" ]; then
		echo "  $loader_module => $repo/loader/cloud"
		echo "  $secret_module => $repo/secret/cloud"
	fi
	echo "Run 'go mod tidy' and your project tests inside the consumer module."
	exit 0
fi

(
	cd "$consumer"
	go mod edit -dropreplace="$root_module"
	go mod edit -dropreplace="$loader_module"
	go mod edit -dropreplace="$secret_module"

	if [ -n "$release_version" ]; then
		if [ "$release_version" = "$zero_version" ]; then
			echo "CONFII_VERSION must identify a real release or revision, not $zero_version" >&2
			exit 2
		fi
		if [ "$scope" = "cloud" ] || [ "$scope" = "all" ]; then
			go mod edit \
				-require="$root_module@$release_version" \
				-require="$loader_module@$release_version" \
				-require="$secret_module@$release_version"
		else
			go mod edit -require="$root_module@$release_version"
		fi
	fi

	# A local replacement has no module version. If `go get` is run while the
	# replacement is active, Go can persist its zero pseudo-version in require.
	# Once the replacement is removed that revision is invalid, so drop only
	# those synthetic requirements and let the consumer's next tidy restore the
	# modules selected by its imports.
	for module in "$root_module" "$loader_module" "$secret_module"; do
		if awk -v module="$module" -v version="$zero_version" \
			'$1 == module && $2 == version { found = 1 } END { exit !found }' go.mod; then
			go mod edit -droprequire="$module"
			printf 'Removed stale zero-version requirement for %s\n' "$module"
		fi
	done
)
echo "Removed local Confii replacements from $consumer"
if [ -n "$release_version" ]; then
	echo "Pinned Confii root and selected optional modules to $release_version"
fi
echo "Run 'go mod tidy' inside the consumer module to restore its normal module graph."
