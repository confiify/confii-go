// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package sourcekind centralizes the predicate that classifies a Loader
// source string as either a local-filesystem path or a non-file source
// (HTTP/HTTPS URL, env-prefix marker, cloud-store identifier, secret-store
// identifier). Prior to D-W20-01 (Wave 22) three packages each carried their
// own copy of the allowlist:
//
//   - watch/watcher.go        -> nonFileSchemes
//   - sourcetrack/filetracker -> nonFileSchemePrefixes
//   - confii (root)           -> nonFileLoaderSchemes
//
// The lists drifted (sourcetrack carried "env:" but lacked "ssm:"/"git:";
// confii lacked "env:"; watch lacked "env:") so a new Loader scheme had to
// be registered in three places by hand or fsnotify, sourcetrack, or the
// loader-watch wiring would silently misclassify it. This package owns the
// canonical union and exposes a single predicate ([IsNonFileSource]) for the
// three call sites.
//
// The predicate is intentionally string-prefix based (no URL parse, no
// allocation) so it is cheap enough to be called inside the FileTracker
// HasChanged hot path on every gate check.
package sourcekind

import "strings"

// nonFileSchemePrefixes is the canonical union of every non-file scheme or
// marker prefix that has historically been recognized by any of the three
// pre-D-W20-01 lists. Each entry is matched case-insensitively against the
// start of a source string. New Loader implementations whose Source()
// identifier is not a filesystem path MUST add their prefix here so that
// the watch / sourcetrack / loader-watch wiring all classify the new
// source uniformly.
//
// Recognized schemes (and the loader/store that produces each):
//
//   - "http://", "https://"   - HTTPLoader and similar URL-fetch loaders
//   - "environment:"          - EnvironmentLoader / envPrefixAutoLoader (G03)
//   - "env:"                  - alternative env-marker prefix (sourcetrack legacy)
//   - "s3://"                 - S3Loader
//   - "ssm:"                  - SSMLoader (AWS Parameter Store)
//   - "gs://"                 - GCSLoader
//   - "azure://"              - AzureBlobLoader
//   - "ibmcos://"             - IBMCOSLoader
//   - "git:"                  - GitLoader
//   - "consul://"             - ConsulLoader
//   - "vault:" / "vault://"   - Vault-backed loaders / secret stores
//   - "file://"               - remote-file URI marker (NOT auto-stripped;
//     classified non-file so callers may decide)
var nonFileSchemePrefixes = []string{
	"http://",
	"https://",
	"environment:",
	"env:",
	"s3://",
	"ssm:",
	"gs://",
	"azure://",
	"ibmcos://",
	"git:",
	"consul://",
	"vault://",
	"vault:",
	"file://",
}

// IsNonFileSource reports whether the given source string identifies
// something other than a local filesystem path. Non-file sources include
// HTTP/HTTPS URLs, env-prefix markers, and cloud-store identifiers; they
// cannot be watched via fsnotify, source-tracked via os.Stat, or otherwise
// treated as files.
//
// The empty string is treated as a file source (returns false) because
// callers historically use "" as the "not yet bound to any source" sentinel
// and we must not classify it as untrackable.
//
// Comparison is case-insensitive at the prefix.
func IsNonFileSource(source string) bool {
	if source == "" {
		return false
	}
	lower := strings.ToLower(source)
	for _, p := range nonFileSchemePrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}
