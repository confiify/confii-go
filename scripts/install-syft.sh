#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

install_dir=${1:?usage: install-syft.sh INSTALL_DIR}
version=1.49.0
archive="syft_${version}_linux_amd64.tar.gz"
expected_sha256=7aa2f03ee92739cf643279ba3990548b9925d4e22cae13f46831ee62821147fe
url="https://github.com/anchore/syft/releases/download/v${version}/${archive}"

command -v curl >/dev/null 2>&1 || {
	echo "curl is required to install Syft" >&2
	exit 1
}
command -v sha256sum >/dev/null 2>&1 || {
	echo "sha256sum is required to verify Syft" >&2
	exit 1
}

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

curl --fail --show-error --silent --location \
	--proto '=https' --tlsv1.2 \
	--retry 3 --retry-all-errors \
	--output "$tmp_dir/$archive" "$url"

printf '%s  %s\n' "$expected_sha256" "$tmp_dir/$archive" | sha256sum --check --status
tar -xzf "$tmp_dir/$archive" -C "$tmp_dir" syft
mkdir -p "$install_dir"
install -m 0755 "$tmp_dir/syft" "$install_dir/syft"
"$install_dir/syft" version
