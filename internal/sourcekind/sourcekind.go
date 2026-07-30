// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package sourcekind classifies Loader source identifiers as local filesystem
// paths or non-file sources. The shared predicate keeps file watching, source
// tracking, and loader orchestration consistent.
//
// Classification uses source-identifier prefixes because loaders may expose
// non-URL identifiers such as "environment:APP" and "ssm:/service".
package sourcekind

import "strings"

// nonFileSchemePrefixes contains source identifiers that cannot be handled as
// local filesystem paths. Entries are matched case-insensitively. Loaders
// that return a non-file Source identifier must add its prefix here so file
// watching and source tracking classify it consistently.
//
// Recognized schemes (and the loader/store that produces each):
//
//   - "http://", "https://"   - HTTPLoader and similar URL-fetch loaders
//   - "environment:"          - EnvironmentLoader / envPrefixAutoLoader
//   - "env:"                  - environment-source marker
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
// The empty string is treated as an unbound source and returns false.
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
