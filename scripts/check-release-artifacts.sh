#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
dist_dir=${1:-"$repo_root/dist"}
checksum_file="$dist_dir/checksums.txt"

command -v jq >/dev/null 2>&1 || {
	echo "jq is required to validate release SBOMs" >&2
	exit 1
}
[ -s "$checksum_file" ] || {
	echo "missing release checksum file: $checksum_file" >&2
	exit 1
}

checksum_contains_once() {
	artifact_name=$1
	awk -v name="$artifact_name" '$2 == name { count++ } END { exit count != 1 }' \
		"$checksum_file"
}

awk '
	NF != 2 || length($1) != 64 || $1 ~ /[^0-9a-f]/ { exit 1 }
	seen[$2]++ { exit 1 }
' "$checksum_file" || {
	echo "checksums.txt contains malformed or duplicate entries" >&2
	exit 1
}

archive_count=0
sbom_count=0
for archive in "$dist_dir"/confii-*.tar.gz "$dist_dir"/confii-*.zip; do
	[ -f "$archive" ] || continue
	archive_count=$((archive_count + 1))
	archive_name=$(basename "$archive")
	sbom="$archive.sbom.json"
	sbom_name=$(basename "$sbom")

	[ -s "$sbom" ] || {
		echo "missing SPDX SBOM for $archive_name" >&2
		exit 1
	}
	jq -e '
		.spdxVersion == "SPDX-2.3" and
		(.SPDXID | type == "string" and length > 0) and
		(.name | type == "string" and length > 0) and
		(.creationInfo.created | type == "string" and length > 0) and
		(.packages | type == "array" and length > 0)
	' "$sbom" >/dev/null

	checksum_contains_once "$archive_name" || {
		echo "$archive_name is missing from checksums.txt" >&2
		exit 1
	}
	checksum_contains_once "$sbom_name" || {
		echo "$sbom_name is missing from checksums.txt" >&2
		exit 1
	}
	sbom_count=$((sbom_count + 1))
done

[ "$archive_count" -eq 5 ] || {
	echo "expected 5 compiled release archives, found $archive_count" >&2
	exit 1
}
[ "$sbom_count" -eq "$archive_count" ] || {
	echo "every compiled archive must have exactly one SPDX SBOM" >&2
	exit 1
}

for vex_file in "$repo_root"/.openvex/*.openvex.json; do
	[ -f "$vex_file" ] || continue
	vex_name=$(basename "$vex_file")
	checksum_contains_once "$vex_name" || {
		echo "$vex_name is missing from checksums.txt" >&2
		exit 1
	}
done

echo "release artifacts include five valid SPDX SBOMs and checksummed VEX metadata"
