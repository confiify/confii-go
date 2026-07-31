// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"fmt"
	"sort"
	"strings"

	"github.com/confiify/confii-go/v2/diff"
)

func sensitivePathsFromConfig(config map[string]any) map[string]struct{} {
	found := make(map[string]struct{})
	collectSecretReferenceKeys("", config, found)
	return found
}

func sensitivePathsForConfig(config map[string]any, declared []string) map[string]struct{} {
	found := sensitivePathsFromConfig(config)
	for _, path := range declared {
		found[path] = struct{}{}
	}
	return found
}

func normalizeSensitivePathList(paths []string) ([]string, error) {
	if paths == nil {
		return nil, nil
	}
	unique := make(map[string]struct{}, len(paths))
	for index, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			return nil, fmt.Errorf("sensitive path %d is empty", index)
		}
		if trimmed != path {
			return nil, fmt.Errorf("sensitive path %q contains surrounding whitespace", path)
		}
		for _, segment := range strings.Split(path, ".") {
			if segment == "" {
				return nil, fmt.Errorf("sensitive path %q contains an empty segment", path)
			}
		}
		unique[path] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for path := range unique {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

func cloneSensitivePaths(paths map[string]struct{}) map[string]struct{} {
	clone := make(map[string]struct{}, len(paths))
	for path := range paths {
		clone[path] = struct{}{}
	}
	return clone
}

func sensitivePathList(paths map[string]struct{}) []string {
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func sensitivePathSet(paths []string) map[string]struct{} {
	result := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path != "" {
			result[path] = struct{}{}
		}
	}
	return result
}

func pathIsSensitive(path string, paths map[string]struct{}) bool {
	for sensitive := range paths {
		if path == sensitive || strings.HasPrefix(path, sensitive+".") || strings.HasPrefix(sensitive, path+".") {
			return true
		}
	}
	return false
}

func redactConfigDiffs(diffs []diff.ConfigDiff, paths map[string]struct{}) []diff.ConfigDiff {
	return diff.Redact(diffs, sensitivePathList(paths), redactedSecretValue)
}
