#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

first="$(mktemp -d "${TMPDIR:-/tmp}/confii-repro-first.XXXXXX")"
second="$(mktemp -d "${TMPDIR:-/tmp}/confii-repro-second.XXXXXX")"
trap 'rm -rf "$first" "$second"' EXIT HUP INT TERM

build() {
  output="$1"
  go build \
    -trimpath \
    -buildvcs=false \
    -ldflags='-s -w -X main.version=reproducible' \
    -o "$output/confii" \
    ./confii
}

build "$first"
build "$second"

if ! cmp -s "$first/confii" "$second/confii"; then
  echo "reproducible build check failed: binaries differ" >&2
  exit 1
fi

echo "Reproducible build check passed."
