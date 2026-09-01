#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

# Run golangci-lint over every module in the repository, under every build tag
# that gates a provider.
#
# The hosted lint job invokes golangci-lint once in the repository root, which
# lints the root module alone; everything behind a build tag was outside the
# gate, including the Vault-tagged secret provider. This closes that.
#
# The matrix below is declared, but it is not trusted. A hand-maintained list of
# modules is the same drift-prone construct as a hand-maintained list of
# configuration keys: the first version of this script omitted examples/cloud
# and still reported that all modules were clean. So the declared matrix is
# reconciled against what the repository actually contains, in both directions,
# before any linting happens. A module or provider tag that nobody has claimed
# fails the run; so does a claim for something that no longer exists.
set -eu

REQUIRED_LINT_VERSION="v2.11.4"

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

LINT="${GOLANGCI_LINT:-golangci-lint}"

fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/confii-lint-modfiles.XXXXXX")
# A single cleanup, registered once. Setting trap again for the same signal
# replaces the previous handler rather than adding to it, so a second trap
# later in the script would silently drop the module restore and leave the
# caller's go.mod files rewritten.
cleanup() {
	restore_modules 2>/dev/null || true
	rm -rf "$fixture_dir"
}
trap cleanup EXIT HUP INT TERM

# --- the declared matrix ------------------------------------------------------
# One line per lint invocation: "<module dir><tab><comma-separated tags>".
# An empty tag field lints the module with no build tags.
declared_matrix() {
	printf '%s\t%s\n' \
		'.' '' \
		'tools/security-insights-check' '' \
		'loader/cloud' '' \
		'loader/cloud' 'aws' \
		'loader/cloud' 'azure' \
		'loader/cloud' 'gcp' \
		'loader/cloud' 'ibm' \
		'loader/cloud' 'aws,azure,gcp,ibm' \
		'secret/cloud' 'aws' \
		'secret/cloud' 'azure' \
		'secret/cloud' 'gcp' \
		'secret/cloud' 'vault' \
		'secret/cloud' 'aws,azure,gcp,vault' \
		'examples/cloud' 'aws' \
		'examples/cloud' 'azure' \
		'examples/cloud' 'gcp' \
		'examples/cloud' 'ibm' \
		'examples/cloud' 'vault' \
		'examples/cloud' 'aws,azure,gcp,ibm,vault'
}

declared_modules() { declared_matrix | cut -f1 | sort -u; }

# Tags this module is linted under, one per line, single tags only.
declared_tags_for() {
	declared_matrix | awk -F'\t' -v m="$1" '$1 == m && $2 != "" { print $2 }' |
		tr ',' '\n' | sort -u
}

# --- what the repository actually contains ------------------------------------
# Every discovery below runs through a checked assignment before anything is
# parsed. A pipeline reports only its last command's status, so `git ls-files |
# sed | sort` succeeds even when git fails, and discovery that silently returns
# nothing is how a gate reports that everything is clean while examining nothing.
if ! tracked_gomod=$(git ls-files '*go.mod' 'go.mod'); then
	printf 'git ls-files failed; modules cannot be discovered\n' >&2
	exit 1
fi
if [ -z "$tracked_gomod" ]; then
	printf 'no tracked go.mod files were found, which cannot be true here;\n' >&2
	printf 'discovery is broken and the matrix cannot be reconciled against it\n' >&2
	exit 1
fi
if ! tracked_go=$(git ls-files '*.go'); then
	printf 'git ls-files failed; build tags cannot be discovered\n' >&2
	exit 1
fi
if [ -z "$tracked_go" ]; then
	printf 'no tracked Go sources were found; discovery is broken\n' >&2
	exit 1
fi

discovered_modules_list=$(printf '%s\n' "$tracked_gomod" |
	sed 's#go\.mod$##; s#/$##; s#^$#.#' | sort -u)

discovered_modules() { printf '%s\n' "$discovered_modules_list"; }

# Provider tags a module's own sources declare. Build constraints also carry
# platform and toolchain terms that are not provider tags; those are named here
# so an unexpected term surfaces rather than being quietly dropped.
non_provider_tags='^(go[0-9.]+|unix|linux|darwin|windows|js|wasm|cgo|race|ignore|purego|test|integration)$'

# Files belonging to a module: those under its directory but not under any
# nested module. Without the second half, the root module appears to declare
# every provider tag in the repository, because every nested module's sources
# live under it.
files_of_module() {
	module="$1"
	if [ "$module" = "." ]; then
		prefix=""
	else
		prefix="$module/"
	fi
	printf '%s\n' "$tracked_go" | while read -r file; do
		case "$file" in
			"$prefix"*) ;;
			*) continue ;;
		esac
		owner="."
		for candidate in $discovered_modules_list; do
			[ "$candidate" = "." ] && continue
			case "$file" in
				"$candidate"/*) [ ${#candidate} -gt ${#owner} ] && owner="$candidate" ;;
			esac
		done
		[ "$owner" = "$module" ] && printf '%s\n' "$file"
	done
}

discovered_tags_for() {
	# grep exits 1 when a module has no build constraints at all, which is a
	# legitimate answer of "none" rather than a failure, so its status is not
	# checked here; the file list it reads was checked above.
	files_of_module "$1" |
		xargs grep -h '^//go:build' 2>/dev/null |
		sed 's#^//go:build ##' |
		tr ' |()&!' '\n' |
		grep -v '^$' |
		grep -Ev "$non_provider_tags" |
		sort -u
}

# --- reconciliation -----------------------------------------------------------
problems=""
note() { problems="$problems
  $1"; }

discovered_modules > "$fixture_dir/discovered"
declared_modules > "$fixture_dir/declared"

for module in $(comm -23 "$fixture_dir/discovered" "$fixture_dir/declared"); do
	note "module $module exists but no lint row claims it"
done
for module in $(comm -13 "$fixture_dir/discovered" "$fixture_dir/declared"); do
	note "lint row claims module $module, which does not exist"
done

# Does the module have sources outside every build constraint? A module whose
# files are all tagged has nothing for an untagged run to analyze, and
# golangci-lint fails outright rather than reporting zero issues. A module that
# does have untagged sources must have an untagged row, or that code is
# unlinted.
has_untagged_files() {
	files_of_module "$1" | while read -r file; do
		grep -q '^//go:build' "$file" || { printf 'yes\n'; break; }
	done
}

has_untagged_row() {
	declared_matrix | awk -F'\t' -v m="$1" '$1 == m && $2 == "" { found = 1 } END { exit !found }'
}

for module in $(declared_modules); do
	[ -d "$module" ] || continue
	if [ -n "$(has_untagged_files "$module")" ]; then
		has_untagged_row "$module" ||
			note "$module has untagged sources but no untagged lint row"
	else
		has_untagged_row "$module" &&
			note "$module has no untagged sources, so its untagged lint row analyzes nothing"
	fi
	found=$(discovered_tags_for "$module")
	claimed=$(declared_tags_for "$module")
	for tag in $found; do
		if ! printf '%s\n' "$claimed" | grep -qx "$tag"; then
			note "$module declares build tag '$tag', which no lint row covers"
		fi
	done
	for tag in $claimed; do
		if ! printf '%s\n' "$found" | grep -qx "$tag"; then
			note "lint row covers $module tag '$tag', which no source declares"
		fi
	done
done

if [ -n "$problems" ]; then
	printf 'The lint matrix disagrees with the repository:%s\n' "$problems" >&2
	exit 1
fi

# --- toolchain and linter identity --------------------------------------------
# The repository already knows that this linter behaves differently under a
# newer Go than the one go.mod pins: under a Homebrew Go 1.27 it fails to load
# standard-library export data and reports phantom typecheck errors. Pin it.
required_toolchain=$(awk '/^toolchain /{print $2; exit}' go.mod)
if [ -n "$required_toolchain" ]; then
	GOTOOLCHAIN="$required_toolchain"
	export GOTOOLCHAIN
fi

# GOLANGCI_LINT may name any executable, so both halves are checked: that the
# command succeeds, and that what it printed is the required version. Capturing
# them in one pipeline reported only sed's status, so a linter that printed the
# right version and then exited non-zero was accepted.
if ! lint_version_output=$("$LINT" version 2>&1); then
	printf '%s failed to report its version:\n%s\n' "$LINT" "$lint_version_output" >&2
	exit 1
fi
actual_lint_version=$(printf '%s\n' "$lint_version_output" |
	sed -n 's/.*version \(v\{0,1\}[0-9][^ ]*\).*/\1/p' | head -1)
if [ -z "$actual_lint_version" ]; then
	printf 'could not read a version from %s:\n%s\n' "$LINT" "$lint_version_output" >&2
	exit 1
fi
case "$actual_lint_version" in
	v*) ;;
	*) actual_lint_version="v$actual_lint_version" ;;
esac
if [ "$actual_lint_version" != "$REQUIRED_LINT_VERSION" ]; then
	printf 'golangci-lint %s is required, found %s (from %s).\n' \
		"$REQUIRED_LINT_VERSION" "$actual_lint_version" "$LINT" >&2
	exit 1
fi

printf 'golangci-lint %s, GOTOOLCHAIN=%s, %s modules, %s lint runs\n\n' \
	"$actual_lint_version" "${GOTOOLCHAIN:-default}" \
	"$(declared_modules | wc -l | tr -d ' ')" \
	"$(declared_matrix | wc -l | tr -d ' ')"

# --- local sibling resolution -------------------------------------------------
# The cloud modules depend on the root module by version. On a release-prep
# branch that version is the one being released and does not exist on the proxy
# yet, so nothing can resolve the import and every cloud row fails on typecheck
# errors that say nothing about the code.
#
# They are therefore linted against their local siblings, the same pre-release
# aid scripts/verify-modules.sh uses. The replacement is written into the real
# go.mod because golangci-lint runs the go toolchain in a subprocess that does
# not carry -modfile, and a workspace does not override an explicit require. It
# is removed again by restore_modules, which the EXIT trap also runs, so an
# interrupted lint cannot leave the tree modified.
#
# This also corrects something the published pin quietly hid: the cloud modules
# were previously linted against the last *released* root module rather than the
# root module in the same change, so a root change that broke them passed.
sibling_modules='loader/cloud secret/cloud examples/cloud'

restore_modules() {
	for module in $sibling_modules; do
		name=$(printf '%s' "$module" | tr '/' '-')
		[ -f "$fixture_dir/$name.mod" ] || continue
		cp "$fixture_dir/$name.mod" "$module/go.mod"
		cp "$fixture_dir/$name.sum" "$module/go.sum"
	done
}

use_local_siblings() {
	for module in $sibling_modules; do
		[ -d "$module" ] || continue
		name=$(printf '%s' "$module" | tr '/' '-')
		cp "$module/go.mod" "$fixture_dir/$name.mod"
		cp "$module/go.sum" "$fixture_dir/$name.sum"

		( cd "$module" && go mod edit \
			-replace="github.com/confiify/confii-go/v2=../.." ) || return 1
		if [ "$module" = "examples/cloud" ]; then
			( cd "$module" && go mod edit \
				-replace="github.com/confiify/confii-go/loader/cloud/v2=../../loader/cloud" \
				-replace="github.com/confiify/confii-go/secret/cloud/v2=../../secret/cloud" ) || return 1
		fi
		( cd "$module" && GOWORK=off go mod tidy >/dev/null 2>&1 ) || {
			printf 'could not resolve %s against its local siblings\n' "$module" >&2
			return 1
		}
	done
}

# --- run ----------------------------------------------------------------------
# Failures accumulate so one run reports every module needing attention.
failed=""
declared_matrix | while IFS="$(printf '\t')" read -r module tags; do
	printf '%s\n' "$module	$tags"
done > /dev/null

use_local_siblings || exit 1

declared_matrix > "$fixture_dir/matrix"

while IFS="$(printf '\t')" read -r module tags; do
	label="$module${tags:+ (tags: $tags)}"
	printf 'Linting %s\n' "$label"
	if [ -n "$tags" ]; then
		( cd "$module" && GOWORK=off "$LINT" run --build-tags "$tags" ./... ) || failed="$failed
  $label"
	else
		( cd "$module" && GOWORK=off "$LINT" run ./... ) || failed="$failed
  $label"
	fi
done < "$fixture_dir/matrix"

if [ -n "$failed" ]; then
	printf '\ngolangci-lint reported issues in:%s\n' "$failed" >&2
	exit 1
fi

printf '\nAll modules and tag sets are clean.\n'
