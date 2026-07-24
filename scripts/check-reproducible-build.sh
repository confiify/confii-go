#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

first="$(mktemp -d "${TMPDIR:-/tmp}/confii-repro-first.XXXXXX")"
second="$(mktemp -d "${TMPDIR:-/tmp}/confii-repro-second.XXXXXX")"
trap 'rm -rf "$first" "$second"' EXIT HUP INT TERM

toolchain="$(sed -n 's/^toolchain //p' go.mod)"
if [ -z "$toolchain" ]; then
  echo "reproducible build check failed: go.mod must pin a toolchain" >&2
  exit 1
fi

build() {
  output="$1"
  mkdir -p "$output/cache" "$output/bin"
  CGO_ENABLED=0 \
  GOARCH=amd64 \
  GOCACHE="$output/cache" \
  GOOS=linux \
  GOTOOLCHAIN="$toolchain" \
  GOWORK=off \
  LC_ALL=C \
  TZ=UTC \
  go build \
    -mod=readonly \
    -trimpath \
    -buildvcs=false \
    -ldflags='-s -w -X main.version=reproducible' \
    -o "$output/bin/confii" \
    ./confii
}

build "$first"
build "$second"

if ! cmp -s "$first/bin/confii" "$second/bin/confii"; then
  echo "reproducible build check failed: binaries differ" >&2
  exit 1
fi

echo "Reproducible linux/amd64 build check passed with $toolchain and independent build caches."
