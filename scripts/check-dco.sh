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
  message=$(git show -s --format='%B' "$commit")
  trailers=$(printf '%s\n' "$message" | git interpret-trailers --parse)
  last_nonempty=$(printf '%s\n' "$message" | awk 'NF { line = $0 } END { print line }')

  valid_signoff=0
  if printf '%s\n' "$trailers" |
    grep -F -i -x -- "Signed-off-by: ${author}" >/dev/null; then
    valid_signoff=1
  fi

  # GitHub's Dependabot authors commits with its noreply address but uses its
  # canonical support address in the generated DCO trailer. Both addresses
  # are GitHub-managed identities for the same bot; accept only that exact
  # name/address pair as the final non-empty message line rather than exempting
  # automated pull requests wholesale. Dependabot's embedded metadata prevents
  # git interpret-trailers from recognizing the otherwise valid final trailer.
  if [ "$author" = 'dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>' ] &&
    [ "$last_nonempty" = 'Signed-off-by: dependabot[bot] <support@github.com>' ]; then
    valid_signoff=1
  fi

  if [ "$valid_signoff" -ne 1 ]; then
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
