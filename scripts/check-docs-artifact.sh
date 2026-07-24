#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

headers_file=${1:-site/_headers}

if [ ! -f "$headers_file" ]; then
	echo "missing rendered security-header policy: $headers_file" >&2
	exit 1
fi

require_literal() {
	value=$1
	if ! grep -Fq "$value" "$headers_file"; then
		echo "missing required header policy in $headers_file: $value" >&2
		exit 1
	fi
}

require_literal "Content-Security-Policy: default-src 'self'"
require_literal "object-src 'none'"
require_literal "frame-ancestors 'none'"
require_literal "X-Content-Type-Options: nosniff"
require_literal "X-Frame-Options: DENY"
require_literal "Referrer-Policy: strict-origin-when-cross-origin"
require_literal "Permissions-Policy:"
require_literal "Strict-Transport-Security:"

echo "documentation security-header artifact is complete"
