#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

usage() {
	cat >&2 <<'USAGE'
usage: sh scripts/release-prepare-pr.sh vX.Y.Z

Prepares a release branch, updates internal module pins, runs release gates,
commits the release prep, pushes the branch, and opens a GitHub pull request.

Environment:
  RELEASE_BASE        base branch to release from (default: origin/main)
  RELEASE_REMOTE      remote to push to (default: origin)
  RELEASE_RUN_GATES   set to 0 to skip local release gates
  RELEASE_OPEN_PR     set to 0 to skip gh pr create
USAGE
	exit 2
}

version=${1:-}
case "$version" in
v[0-9]*.[0-9]*.[0-9]*) ;;
*) usage ;;
esac

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
base=${RELEASE_BASE:-origin/main}
remote=${RELEASE_REMOTE:-origin}
branch="release-$version"
run_gates=${RELEASE_RUN_GATES:-1}
open_pr=${RELEASE_OPEN_PR:-1}

run() {
	echo ""
	echo "==> $*"
	"$@"
}

replace_version() {
	file=$1
	module=$2
	tmp="${file}.tmp.$$"
	awk -v module="$module" -v version="$version" '
		# Rewrite only the version token. Assigning to $2 would rebuild the
		# record with OFS and strip the leading tab go.mod requires.
		$1 == module {
			sub(/v[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?/, version)
		}
		{ print }
	' "$file" >"$tmp"
	mv "$tmp" "$file"
}

ensure_changelog_entry() {
	if grep -q "^## \\[$(printf '%s' "$version" | sed 's/^v//')\\]" CHANGELOG.md; then
		return
	fi

	tmp="CHANGELOG.md.tmp.$$"
	awk -v plain="$(printf '%s' "$version" | sed 's/^v//')" '
		BEGIN { inserted = 0 }
		# Keep [Unreleased] at the top; the release section belongs
		# directly above the previous release.
		/^## \[/ && $0 !~ /^## \[Unreleased\]/ && inserted == 0 {
			print "## [" plain "] - YYYY-MM-DD"
			print ""
			print "- TODO: summarize user-facing release changes."
			print ""
			inserted = 1
		}
		{ print }
		END {
			if (inserted == 0) {
				print "## [" plain "] - YYYY-MM-DD"
				print ""
				print "- TODO: summarize user-facing release changes."
			}
		}
	' CHANGELOG.md >"$tmp"
	mv "$tmp" CHANGELOG.md
	echo "Inserted a CHANGELOG.md stub for $version; edit it before merging if needed." >&2
}

cd "$repo_root"

if [ -n "$(git status --porcelain)" ]; then
	echo "working tree must be clean before release preparation" >&2
	exit 1
fi

run git fetch "$remote"

if git show-ref --verify --quiet "refs/heads/$branch"; then
	echo "local branch $branch already exists" >&2
	exit 1
fi

if git ls-remote --exit-code --heads "$remote" "$branch" >/dev/null 2>&1; then
	echo "remote branch $remote/$branch already exists" >&2
	exit 1
fi

run git switch -c "$branch" "$base"

replace_version loader/cloud/go.mod github.com/confiify/confii-go/v2
replace_version secret/cloud/go.mod github.com/confiify/confii-go/v2
replace_version examples/cloud/go.mod github.com/confiify/confii-go/v2
replace_version examples/cloud/go.mod github.com/confiify/confii-go/loader/cloud/v2
replace_version examples/cloud/go.mod github.com/confiify/confii-go/secret/cloud/v2

tmp_script="scripts/test-cloud-consumer.sh.tmp.$$"
awk -v version="$version" '
	/^confii_version=\$\{CONFII_VERSION:-v[0-9]+\.[0-9]+\.[0-9]+\}/ {
		print "confii_version=${CONFII_VERSION:-" version "}"
		next
	}
	{ print }
' scripts/test-cloud-consumer.sh >"$tmp_script"
mv "$tmp_script" scripts/test-cloud-consumer.sh

ensure_changelog_entry

run git diff --check
run sh scripts/verify-modules.sh

if [ "$run_gates" != "0" ]; then
	run make ci-full
	run make lint
	run make vulncheck
	run make supply-chain-check
	run make docs-check
fi

run git add CHANGELOG.md docs/changelog.md loader/cloud/go.mod secret/cloud/go.mod examples/cloud/go.mod scripts/test-cloud-consumer.sh
run git commit -s -m "release: prepare $version"
run git push -u "$remote" "$branch"

if [ "$open_pr" != "0" ]; then
	run gh pr create \
		--base main \
		--head "$branch" \
		--title "release: prepare $version" \
		--body "Prepare Confii Go $version release metadata and module pins."
fi

echo ""
echo "Release PR preparation completed for $version."
echo "Next: sh scripts/release-watch-pr.sh <pr-number-or-url>"
