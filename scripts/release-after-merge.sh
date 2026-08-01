#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

usage() {
	cat >&2 <<'USAGE'
usage: sh scripts/release-after-merge.sh vX.Y.Z [pr-number-or-url]

After the release PR is merged, syncs main, creates signed annotated root and
nested module tags on the merge commit, pushes them atomically, then watches
the release workflow to completion.

Environment:
  RELEASE_REMOTE          remote to fetch/push (default: origin)
  RELEASE_MAIN_BRANCH     main branch name (default: main)
  RELEASE_WATCH_INTERVAL  polling interval in seconds (default: 15)
  RELEASE_PUSH_TAGS       set to 0 to create and verify tags without pushing
USAGE
	exit 2
}

version=${1:-}
pr=${2:-}
case "$version" in
v[0-9]*.[0-9]*.[0-9]*) ;;
*) usage ;;
esac

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
remote=${RELEASE_REMOTE:-origin}
main_branch=${RELEASE_MAIN_BRANCH:-main}
interval=${RELEASE_WATCH_INTERVAL:-15}
push_tags=${RELEASE_PUSH_TAGS:-1}

loader_tag="loader/cloud/$version"
secret_tag="secret/cloud/$version"
root_tag="$version"

run() {
	echo ""
	echo "==> $*"
	"$@"
}

tag_exists() {
	git show-ref --verify --quiet "refs/tags/$1"
}

cd "$repo_root"

if [ -n "$(git status --porcelain)" ]; then
	echo "working tree must be clean before release tagging" >&2
	exit 1
fi

if [ -n "$pr" ]; then
	state=$(gh pr view "$pr" --json state --template '{{.state}}')
	if [ "$state" != "MERGED" ]; then
		echo "PR $pr is $state; release tags must be created only after merge" >&2
		exit 1
	fi
	merge_commit=$(gh pr view "$pr" --json mergeCommit --template '{{.mergeCommit.oid}}')
else
	merge_commit=""
fi

run git fetch "$remote"
run git switch "$main_branch"
run git merge --ff-only "$remote/$main_branch"

main_head=$(git rev-parse HEAD)
if [ -n "$merge_commit" ] && [ "$main_head" != "$merge_commit" ]; then
	echo "main HEAD $main_head does not match PR merge commit $merge_commit" >&2
	exit 1
fi

for tag in "$loader_tag" "$secret_tag" "$root_tag"; do
	if tag_exists "$tag"; then
		echo "tag $tag already exists locally" >&2
		exit 1
	fi
	if git ls-remote --exit-code --tags "$remote" "$tag" >/dev/null 2>&1; then
		echo "tag $tag already exists on $remote" >&2
		exit 1
	fi
done

run git tag -s "$loader_tag" -m "loader/cloud $version" "$main_head"
run git tag -s "$secret_tag" -m "secret/cloud $version" "$main_head"
run git tag -s "$root_tag" -m "confii-go $version" "$main_head"

for tag in "$loader_tag" "$secret_tag" "$root_tag"; do
	resolved=$(git rev-list -n 1 "$tag")
	if [ "$resolved" != "$main_head" ]; then
		echo "tag $tag resolves to $resolved, expected $main_head" >&2
		exit 1
	fi
done

run git tag -v "$root_tag"

if [ "$push_tags" = "0" ]; then
	echo ""
	echo "Tags created and verified locally. RELEASE_PUSH_TAGS=0, so nothing was pushed."
	exit 0
fi

run git push --atomic "$remote" "$loader_tag" "$secret_tag" "$root_tag"

echo ""
echo "Waiting for release workflow for $root_tag..."
run_id=""
i=0
while [ "$i" -lt 20 ]; do
	run_id=$(gh run list --workflow release.yaml --limit 10 --json databaseId,headBranch,headSha,event \
		--template "{{range .}}{{if and (eq .headBranch \"$root_tag\") (eq .headSha \"$main_head\")}}{{.databaseId}}{{\"\\n\"}}{{end}}{{end}}" | head -n 1)
	if [ -n "$run_id" ]; then
		break
	fi
	i=$((i + 1))
	sleep 6
done

if [ -z "$run_id" ]; then
	echo "could not find release workflow run for $root_tag on $main_head" >&2
	exit 1
fi

run gh run watch "$run_id" --interval "$interval" --exit-status
run gh release view "$root_tag" --json tagName,name,url,isDraft,isPrerelease,publishedAt

echo ""
echo "Release $version completed."
