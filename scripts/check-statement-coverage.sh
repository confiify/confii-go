#!/bin/sh
# Copyright 2026 The Confii Contributors
# SPDX-License-Identifier: MIT

set -eu

profile="${1:-coverage.out}"
minimum="${COVERAGE_MINIMUM:-90}"

awk -v minimum="$minimum" '
  NR > 1 {
    split($1, path, ":")
    if (path[1] !~ /\/examples\// && path[1] !~ /\/integration\//) {
      total += $2
      if ($3 > 0) {
        covered += $2
      }
    }
  }
  END {
    if (total == 0) {
      print "statement coverage check failed: no statements measured" > "/dev/stderr"
      exit 1
    }
    percentage = 100 * covered / total
    printf "Non-example statement coverage: %.2f%% (%d/%d); required: %.2f%%\n", percentage, covered, total, minimum
    if (percentage + 0.000001 < minimum) {
      exit 1
    }
  }
' "$profile"
