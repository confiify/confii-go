#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

# Prove that scripts/lint-modules.sh actually rejects what it claims to reject.
#
# A gate that passes tells you nothing on its own: it passes identically when it
# is examining the code and when it is examining nothing. The first version of
# that script omitted a whole module and still reported every module clean, so
# the gate is checked against three distinct failures and one success:
#
#   1. a lint defect inside a build-tagged file        (the tag rows work)
#   2. a new module that no lint row claims            (module discovery works)
#   3. a lint row naming a module that does not exist  (the reverse works)
#   4. a new provider build tag no lint row claims     (tag discovery works)
#   5. a linter that reports the right version and     (producer status is
#      then exits non-zero                              checked, not assumed)
#   6. the unmodified tree                             (it is not simply broken)
#
# Case 6 is what stops the others from being satisfied by a gate that always
# fails. Cases 4 and 5 exist because each was a real false-green: the tag
# reconciliation was claimed but never exercised, and the version check captured
# the linter's output through a pipeline that reported only sed's status.
#
# Everything happens in a throwaway fixture. An earlier version mutated a
# tracked file in the caller's checkout and restored it from a trap, which
# leaves the working tree altered if the process is killed rather than
# interrupted.
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/confii-lint-gate.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

# The fixture is built from the working tree, not from HEAD: the subject of this
# check is the tree about to be committed. Tracked files carry their
# working-tree content and untracked-but-not-ignored files come along too, so a
# newly added module or script is present exactly as it would be once committed.
( cd "$repo_root" && git ls-files --cached --others --exclude-standard ) |
	while IFS= read -r file; do
		[ -f "$repo_root/$file" ] || continue
		mkdir -p "$fixture/$(dirname "$file")"
		cp "$repo_root/$file" "$fixture/$file"
	done

# git ls-files drives module and tag discovery, so the fixture needs an index,
# and a commit gives every probe below a clean point to restore to.
git -C "$fixture" init --quiet
git -C "$fixture" add -A >/dev/null
git -C "$fixture" -c user.name=gate -c user.email=gate@invalid \
	commit --quiet -m 'lint gate fixture' >/dev/null

restore() { git -C "$fixture" reset --hard --quiet HEAD; git -C "$fixture" clean -qfd; }
gate() { ( cd "$fixture" && sh scripts/lint-modules.sh >/dev/null 2>&1 ); }

expect_reject() {
	if gate; then
		printf 'FAIL: the lint gate accepted %s\n' "$1" >&2
		exit 1
	fi
	printf 'rejects %s\n' "$1"
	restore
}

# 1. A defect visible only under a build tag.
cat >> "$fixture/secret/cloud/vault.go" <<'GO'

func confiiLintGateProbe() {
	var s *VaultStore
	s.Close()
}
GO
expect_reject "a defect inside a vault-tagged file"

# 2. A module nothing in the matrix claims.
mkdir -p "$fixture/probe-module"
printf 'module example.com/probe\n\ngo 1.25.0\n' > "$fixture/probe-module/go.mod"
printf 'package probe\n' > "$fixture/probe-module/probe.go"
git -C "$fixture" add probe-module >/dev/null
expect_reject "a newly added module that no lint row covers"

# 3. A matrix row for a module that is gone.
git -C "$fixture" rm -r --quiet --force tools/security-insights-check >/dev/null
expect_reject "a lint row naming a module that no longer exists"

# 4. A provider build tag that no lint row covers. Module discovery working
# does not mean tag discovery does; they are separate reconciliations.
cat > "$fixture/secret/cloud/probe_tagged.go" <<'GO'
//go:build newprovider

package cloud
GO
git -C "$fixture" add secret/cloud/probe_tagged.go >/dev/null
expect_reject "a new provider build tag that no lint row covers"

# 5. A linter that prints exactly the required version and then fails. Without
# checking the command's status this passes, and every row afterwards runs
# against a binary already known to be broken.
fake_lint="$fixture/fake-golangci-lint"
{
	printf '#!/bin/sh\n'
	printf 'if [ "$1" = version ]; then\n'
	printf '  echo "golangci-lint has version %s built with go1.25.14"\n' "v2.11.4"
	printf '  exit 7\n'
	printf 'fi\n'
	printf 'exit 0\n'
} > "$fake_lint"
chmod +x "$fake_lint"
if ( cd "$fixture" && GOLANGCI_LINT="$fake_lint" sh scripts/lint-modules.sh >/dev/null 2>&1 ); then
	printf 'FAIL: the lint gate accepted a linter that reported the right version and exited non-zero\n' >&2
	exit 1
fi
printf 'rejects a linter that reports the right version and then fails\n'
rm -f "$fake_lint"
restore

# 6. The untouched tree.
if ! gate; then
	printf 'FAIL: the lint gate rejects the unmodified tree\n' >&2
	( cd "$fixture" && sh scripts/lint-modules.sh 2>&1 | tail -20 ) >&2
	exit 1
fi
printf 'accepts the unmodified tree\n'
