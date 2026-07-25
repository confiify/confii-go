#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

if [ -z "${APIDIFF:-}" ] || [ ! -x "$APIDIFF" ]; then
  echo "APIDIFF must name an executable apidiff binary" >&2
  exit 2
fi

repo=$(git rev-parse --show-toplevel)
baseline=${API_BASELINE_TAG:-}
if [ -z "$baseline" ]; then
  baseline=$(git tag --merged HEAD --list 'v[0-9]*' --sort=-v:refname |
    grep -E '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' |
    head -n 1)
fi

if ! printf '%s\n' "$baseline" |
  grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'; then
  echo "API baseline must be a stable semantic-version tag; got: ${baseline:-<empty>}" >&2
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
    # v1.2.0 was published before its nested-module sums included the root
    # module. Populate only the temporary archive; never rewrite a release or
    # the current worktree.
    (cd "$old_dir" && GOWORK=off go mod download github.com/confiify/confii-go)
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
      GOFLAGS="$goflags" GOCACHE="$tmp/cache-old" \
        "$APIDIFF" -m -w "$tmp/${artifact_name}-${set_name}-old.api" "$module"
    )
    (
      cd "$current_dir"
      GOFLAGS="$goflags" GOCACHE="$tmp/cache-current" \
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
