#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

base=${1:-${DCO_BASE:-origin/main}}
head=${2:-${DCO_HEAD:-HEAD}}

git rev-parse --verify "${base}^{commit}" >/dev/null
git rev-parse --verify "${head}^{commit}" >/dev/null

commits=$(git rev-list --reverse "${base}..${head}")
if [ -z "$commits" ]; then
  echo "DCO check: no commits found in ${base}..${head}" >&2
  exit 1
fi

failed=0
for commit in $commits; do
  author=$(git show -s --format='%an <%ae>' "$commit")
  trailers=$(git show -s --format='%B' "$commit" | git interpret-trailers --parse)

  if ! printf '%s\n' "$trailers" |
    grep -F -i -x -- "Signed-off-by: ${author}" >/dev/null; then
    echo "DCO check: ${commit} is missing 'Signed-off-by: ${author}'" >&2
    failed=1
  fi
done

if [ "$failed" -ne 0 ]; then
  cat >&2 <<'EOF'

Every proposed commit must certify the Developer Certificate of Origin.
Amend each reported commit with `git commit --amend --signoff` and rebase as
needed. See CONTRIBUTING.md#developer-certificate-of-origin.
EOF
  exit 1
fi

echo "DCO check: all proposed commits are signed off (${base}..${head})"
