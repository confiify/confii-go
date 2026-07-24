#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

url=${1:-}
if [ -z "$url" ]; then
	echo "usage: $0 https://documentation.example/" >&2
	exit 2
fi

case "$url" in
	https://*) ;;
	*)
		echo "documentation URL must use https: $url" >&2
		exit 2
		;;
esac

headers_file=$(mktemp)
trap 'rm -f "$headers_file"' EXIT HUP INT TERM

effective_url=$(curl --fail --silent --show-error --location --head \
	--max-redirs 5 --output /dev/null --write-out '%{url_effective}' "$url")
curl --fail --silent --show-error --head --output "$headers_file" "$effective_url"

header_value() {
	name=$1
	awk -v wanted="$name" '
		tolower(substr($0, 1, length(wanted) + 1)) == tolower(wanted ":") {
			sub(/^[^:]+:[[:space:]]*/, "")
			sub(/\r$/, "")
			print
		}' "$headers_file" | tail -n 1
}

require_header() {
	name=$1
	value=$(header_value "$name")
	if [ -z "$value" ]; then
		echo "missing required response header: $name" >&2
		exit 1
	fi
	printf '%s: %s\n' "$name" "$value"
}

csp=$(header_value "Content-Security-Policy")
if [ -z "$csp" ]; then
	echo "missing required response header: Content-Security-Policy" >&2
	exit 1
fi
for directive in "default-src 'self'" "object-src 'none'" "frame-ancestors 'none'"; do
	case "$csp" in
		*"$directive"*) ;;
		*)
			echo "Content-Security-Policy is missing: $directive" >&2
			exit 1
			;;
	esac
done
printf '%s\n' "Content-Security-Policy: $csp"

nosniff=$(header_value "X-Content-Type-Options" | tr '[:upper:]' '[:lower:]')
if [ "$nosniff" != "nosniff" ]; then
	echo "X-Content-Type-Options must be nosniff" >&2
	exit 1
fi
printf '%s\n' "X-Content-Type-Options: $nosniff"

frame_options=$(header_value "X-Frame-Options" | tr '[:lower:]' '[:upper:]')
case "$frame_options" in
	DENY|SAMEORIGIN) ;;
	*)
		echo "X-Frame-Options must be DENY or SAMEORIGIN" >&2
		exit 1
		;;
esac
printf '%s\n' "X-Frame-Options: $frame_options"

require_header "Strict-Transport-Security"
require_header "Referrer-Policy"
require_header "Permissions-Policy"

echo "documentation response headers passed at $effective_url"
