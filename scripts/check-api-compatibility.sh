#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

if [ -z "${APIDIFF:-}" ] || [ ! -x "$APIDIFF" ]; then
  echo "APIDIFF must name an executable apidiff binary" >&2
  exit 2
fi

repo=$(git rev-parse --show-toplevel)
current_module=$(awk '$1 == "module" { print $2; exit }' "$repo/go.mod")
case "$current_module" in
  */v[2-9]|*/v[1-9][0-9]*) current_major=${current_module##*/v} ;;
  *) current_major=1 ;;
esac
baseline=${API_BASELINE_TAG:-}
if [ -z "$baseline" ]; then
	baseline=$(git tag --merged HEAD --list "v${current_major}.*" --sort=-v:refname |
		grep -E "^v${current_major}\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$" |
		head -n 1)
fi

# A new major intentionally has no compatible predecessor. The first v2 pull
# request therefore has no same-major API artifact to compare; after v2.0.0 is
# tagged, every subsequent v2 change is checked against the latest v2 release.
if [ -z "$baseline" ]; then
	echo "API compatibility: no released v${current_major} baseline exists (first release of this major)"
	exit 0
fi

if ! printf '%s\n' "$baseline" |
	grep -Eq "^v${current_major}\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$"; then
	echo "API baseline must be a stable v${current_major} semantic-version tag; got: $baseline" >&2
  exit 2
fi

baseline_commit=$(git rev-parse --verify "refs/tags/${baseline}^{commit}")
for nested_tag in "loader/cloud/${baseline}" "secret/cloud/${baseline}"; do
  nested_commit=$(git rev-parse --verify "refs/tags/${nested_tag}^{commit}")
  if [ "$nested_commit" != "$baseline_commit" ]; then
    echo "API baseline tags do not identify the same commit: ${baseline}, ${nested_tag}" >&2
    exit 1
  fi
done

tmp=$(mktemp -d /tmp/confii-api-compat.XXXXXX)
cleanup() {
  case "$tmp" in
    /tmp/confii-api-compat.*) rm -rf -- "$tmp" ;;
    *) echo "Refusing to remove unexpected temporary path: $tmp" >&2 ;;
  esac
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$tmp/old"
git archive "$baseline" | tar -x -C "$tmp/old"

# Release-preparation branches intentionally make the nested modules require
# the version being prepared before its tag exists. Model the current source
# tree through an isolated workspace so a clean CI runner resolves that module
# edge locally without modifying go.mod/go.sum or requiring a premature tag.
workspace_dir="$tmp/workspace"
mkdir -p "$workspace_dir"
(
  cd "$workspace_dir"
  go work init \
    "$repo" \
    "$repo/loader/cloud" \
    "$repo/secret/cloud"

  loader_root_version=$(awk -v module="$current_module" '$1 == module { print $2; exit }' \
    "$repo/loader/cloud/go.mod")
  secret_root_version=$(awk -v module="$current_module" '$1 == module { print $2; exit }' \
    "$repo/secret/cloud/go.mod")
  if [ -z "$loader_root_version" ] || [ "$loader_root_version" != "$secret_root_version" ]; then
    echo "Cloud modules must require the same root-module version" >&2
    exit 1
  fi
  go work edit \
    -replace="$current_module@$loader_root_version=$repo"
)
current_workspace="$workspace_dir/go.work"

check_module() {
  relative_dir=$1
  artifact_name=$2
  build_sets=$3

  current_dir="$repo"
  old_dir="$tmp/old"
  if [ "$relative_dir" != "." ]; then
    current_dir="$repo/$relative_dir"
    old_dir="$tmp/old/$relative_dir"
  fi

  module=$(awk '$1 == "module" { print $2; exit }' "$current_dir/go.mod")
  old_module=$(awk '$1 == "module" { print $2; exit }' "$old_dir/go.mod")
  if [ -z "$module" ] || [ "$module" != "$old_module" ]; then
    echo "Module path changed in $relative_dir: $old_module -> $module" >&2
    exit 1
  fi

  if [ "$relative_dir" != "." ]; then
    # Resolve the baseline nested module against the root source extracted
    # from the same signed tag. This keeps compatibility checks independent of
    # module-proxy propagation and changes only the disposable archive copy.
    old_root_version=$(awk -v module="$current_module" '$1 == module { print $2; exit }' \
      "$old_dir/go.mod")
    if [ -z "$old_root_version" ]; then
      echo "Baseline cloud module does not require the root module: $old_dir" >&2
      exit 1
    fi
    (
      cd "$old_dir"
      GOWORK=off go mod edit \
        -replace="$current_module@$old_root_version=$tmp/old"
    )
  fi

  for build_tags in $build_sets; do
    display_tags=$build_tags
    goflags="-tags=$build_tags"
    if [ "$build_tags" = "base" ]; then
      display_tags="default"
      goflags=
    fi
    set_name=$(printf '%s' "$build_tags" | tr ',' '-')

    echo "Checking $module ($display_tags tags) against $baseline"
    (
      cd "$old_dir"
      GOWORK=off GOFLAGS="$goflags" GOCACHE="$tmp/cache-old" \
        "$APIDIFF" -m -w "$tmp/${artifact_name}-${set_name}-old.api" "$module"
    )
    (
      cd "$current_dir"
      GOWORK="$current_workspace" GOFLAGS="$goflags" GOCACHE="$tmp/cache-current" \
        "$APIDIFF" -m -w "$tmp/${artifact_name}-${set_name}-new.api" "$module"
    )
    incompatible=$("$APIDIFF" -m -incompatible \
      "$tmp/${artifact_name}-${set_name}-old.api" \
      "$tmp/${artifact_name}-${set_name}-new.api")
    if [ -n "$incompatible" ]; then
      printf '%s\n' "$incompatible" >&2
      echo "Incompatible public API change detected in $module ($display_tags tags)" >&2
      exit 1
    fi
  done
}

check_module . root "base"
check_module loader/cloud loader-cloud "base aws azure gcp ibm aws,azure,gcp,ibm"
check_module secret/cloud secret-cloud "aws azure gcp vault aws,azure,gcp,vault"

echo "API compatibility: all released modules are compatible with $baseline"
