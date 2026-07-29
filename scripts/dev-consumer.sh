#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

action=${1:-}
consumer_input=${2:-}
repo_input=${3:-}
scope=${4:-core}

usage() {
	echo "usage: $0 <link|unlink> <consumer-dir> <confii-repo-dir> <core|cloud|all>" >&2
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

root_module=github.com/confiify/confii-go
loader_module=$root_module/loader/cloud
secret_module=$root_module/secret/cloud

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
)
echo "Removed local Confii replacements from $consumer"
echo "Run 'go mod tidy' inside the consumer module to restore its normal module graph."
