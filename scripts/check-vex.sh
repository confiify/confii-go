#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
vex_dir="$repo_root/.openvex"

command -v jq >/dev/null 2>&1 || {
	echo "jq is required to validate OpenVEX documents" >&2
	exit 1
}

set -- "$vex_dir"/*.openvex.json
[ -f "$1" ] || {
	echo "no OpenVEX documents found in $vex_dir" >&2
	exit 1
}

for vex_file do
	jq -e '
		.["@context"] == "https://openvex.dev/ns/v0.2.0" and
		(.["@id"] | type == "string" and length > 0) and
		(.author | type == "string" and length > 0) and
		(.timestamp | fromdateiso8601 | type == "number") and
		(.version | type == "number" and . >= 1) and
		(.statements | type == "array" and length > 0) and
		(.statements | all(
			(.vulnerability.name | type == "string" and length > 0) and
			(.status == "not_affected" or .status == "affected" or
			 .status == "fixed" or .status == "under_investigation") and
			(.products | type == "array" and length > 0) and
			(if .status == "not_affected" then
				((.justification | type == "string" and length > 0) or
				 (.impact_statement | type == "string" and length > 0))
			 else true end) and
			(if .status == "affected" then
				(.action_statement | type == "string" and length > 0)
			 else true end)
		))
	' "$vex_file" >/dev/null
done

ignored_ids=$(
	find "$repo_root" -name osv-scanner.toml -not -path '*/graphify-out/*' -exec \
		awk -F '"' '$1 ~ /^[[:space:]]*id[[:space:]]*=/ { print $2 }' {} + |
		sort -u
)

[ -n "$ignored_ids" ] || {
	echo "no OSV suppressions found to reconcile with VEX" >&2
	exit 1
}

for vuln_id in $ignored_ids; do
	statement_count=0
	for vex_file in "$vex_dir"/*.openvex.json; do
		count=$(jq --arg id "$vuln_id" \
			'[.statements[] | select(.vulnerability.name == $id)] | length' \
			"$vex_file")
		statement_count=$((statement_count + count))
	done
	[ "$statement_count" -ge 1 ] || {
		echo "OSV suppression $vuln_id has no matching OpenVEX statement" >&2
		exit 1
	}
done

go_2026_vex="$vex_dir/GO-2026-5932.openvex.json"
[ -f "$go_2026_vex" ] || {
	echo "GO-2026-5932 requires $go_2026_vex" >&2
	exit 1
}

jq -e '
	[.statements[] |
	 select(.vulnerability.name == "GO-2026-5932" and
	        .status == "not_affected" and
	        .justification == "vulnerable_code_not_present")]
	| length == 1
' "$go_2026_vex" >/dev/null

crypto_version=""
for manifest in \
	"$repo_root/go.mod" \
	"$repo_root/loader/cloud/go.mod" \
	"$repo_root/secret/cloud/go.mod" \
	"$repo_root/examples/cloud/go.mod"
do
	version=$(awk '$1 == "golang.org/x/crypto" { print $2; exit }' "$manifest")
	[ -n "$version" ] || {
		echo "$manifest does not declare golang.org/x/crypto" >&2
		exit 1
	}
	if [ -z "$crypto_version" ]; then
		crypto_version=$version
	elif [ "$version" != "$crypto_version" ]; then
		echo "golang.org/x/crypto versions are not synchronized across modules" >&2
		exit 1
	fi
done

component_purl="pkg:golang/golang.org/x/crypto@$crypto_version"
for product_purl in \
	"pkg:golang/github.com/confiify/confii-go" \
	"pkg:golang/github.com/confiify/confii-go/loader/cloud" \
	"pkg:golang/github.com/confiify/confii-go/secret/cloud" \
	"pkg:golang/github.com/confiify/confii-go/examples/cloud"
do
	jq -e --arg product "$product_purl" --arg component "$component_purl" '
		any(.statements[] |
			select(.vulnerability.name == "GO-2026-5932") |
			.products[]?;
			.["@id"] == $product and
			any(.subcomponents[]?; .["@id"] == $component))
	' "$go_2026_vex" >/dev/null || {
		echo "GO-2026-5932 VEX is stale for $product_purl and $component_purl" >&2
		exit 1
	}
done

check_no_openpgp_dependency() {
	module_dir=$1
	tags=$2
	if [ -n "$tags" ]; then
		deps=$(cd "$module_dir" && go list -deps -tags "$tags" ./...)
	else
		deps=$(cd "$module_dir" && go list -deps ./...)
	fi
	if printf '%s\n' "$deps" | grep -Eq '^golang\.org/x/crypto/openpgp(/|$)'; then
		echo "OpenPGP vulnerable code is present in $module_dir dependency graph" >&2
		exit 1
	fi
}

check_no_openpgp_dependency "$repo_root" ""
check_no_openpgp_dependency "$repo_root/loader/cloud" "aws,azure,gcp,ibm"
check_no_openpgp_dependency "$repo_root/secret/cloud" "aws,azure,gcp,vault"
check_no_openpgp_dependency "$repo_root/examples/cloud" "aws,azure,gcp,vault,ibm"

echo "OpenVEX metadata is valid and every OSV suppression is accounted for"
