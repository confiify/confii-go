// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package dotenvparse provides the shared dotenv parsing path used by both
// explicit and declarative file loaders.
package dotenvparse

import (
	"fmt"
	"sort"
	"strings"

	"github.com/confiify/confii-go/v2/configmap"
	"github.com/confiify/confii-go/v2/internal/typecoerce"
	"github.com/joho/godotenv"
)

// Issue describes one dotenv record that could not be parsed or mapped into
// Confii's nested configuration representation.
type Issue struct {
	Line int
	Err  error
}

// IssueHandler decides whether a malformed record should abort parsing. A nil
// return skips the record; a non-nil return is returned to the caller. This
// lets loaders retain their raise, warn, and ignore policies without owning a
// second dotenv grammar implementation.
type IssueHandler func(Issue) error

// Parse decodes dotenv input with godotenv, converts scalar values, and maps
// dot-separated names into nested configuration keys. Records are admitted
// incrementally so a caller may skip a malformed record while preserving
// variable expansion against preceding valid records.
func Parse(data []byte, onIssue IssueHandler) (map[string]any, error) {
	records := logicalRecords(string(data))
	accepted := make([]string, 0, len(records))
	values := make(map[string]string)

	for _, record := range records {
		trimmed := strings.TrimSpace(record.text)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			key, ok := assignmentKey(trimmed)
			if !ok || key == "" {
				detail := "missing '=' separator"
				if ok {
					detail = "empty variable name"
				}
				if handleErr := handle(onIssue, Issue{
					Line: record.line,
					Err:  fmt.Errorf("%s", detail),
				}); handleErr != nil {
					return nil, handleErr
				}
				continue
			}
		}
		recordText := normalizeDoubleQuotedTabs(record.text)
		candidate := append(append([]string(nil), accepted...), recordText)
		parsed, err := godotenv.Unmarshal(strings.Join(candidate, "\n"))
		if err != nil {
			if handleErr := handle(onIssue, Issue{Line: record.line, Err: fmt.Errorf("invalid dotenv syntax")}); handleErr != nil {
				return nil, handleErr
			}
			continue
		}
		accepted = append(accepted, recordText)
		values = parsed
	}

	if len(values) == 0 {
		return nil, nil
	}
	result := make(map[string]any, len(values))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := configmap.Set(result, key, typecoerce.ParseScalar(values[key], false)); err != nil {
			if handleErr := handle(onIssue, Issue{Err: fmt.Errorf("invalid key %q: %w", key, err)}); handleErr != nil {
				return nil, handleErr
			}
		}
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func assignmentKey(record string) (string, bool) {
	if strings.HasPrefix(record, "export ") {
		record = strings.TrimSpace(strings.TrimPrefix(record, "export "))
	}
	key, _, ok := strings.Cut(record, "=")
	return strings.TrimSpace(key), ok
}

func normalizeDoubleQuotedTabs(record string) string {
	var builder strings.Builder
	builder.Grow(len(record))
	inDoubleQuote := false
	for index := 0; index < len(record); index++ {
		char := record[index]
		if char == '"' && (index == 0 || record[index-1] != '\\') {
			inDoubleQuote = !inDoubleQuote
		}
		if inDoubleQuote && char == '\\' && index+1 < len(record) && record[index+1] == 't' {
			builder.WriteByte('\t')
			index++
			continue
		}
		builder.WriteByte(char)
	}
	return builder.String()
}

type logicalRecord struct {
	line int
	text string
}

func logicalRecords(input string) []logicalRecord {
	lines := strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n")
	records := make([]logicalRecord, 0, len(lines))
	for index := 0; index < len(lines); {
		start := index
		parts := []string{lines[index]}
		quote := openQuote(lines[index], 0)
		index++
		for quote != 0 && index < len(lines) {
			parts = append(parts, lines[index])
			quote = openQuote(lines[index], quote)
			index++
		}
		records = append(records, logicalRecord{line: start + 1, text: strings.Join(parts, "\n")})
	}
	return records
}

func openQuote(line string, quote byte) byte {
	escaped := false
	for index := 0; index < len(line); index++ {
		char := line[index]
		if quote == '"' && escaped {
			escaped = false
			continue
		}
		if quote == '"' && char == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '#' {
			break
		}
		if char == '\'' || char == '"' {
			quote = char
		}
	}
	return quote
}

func handle(onIssue IssueHandler, issue Issue) error {
	if onIssue != nil {
		return onIssue(issue)
	}
	if issue.Line > 0 {
		return fmt.Errorf("dotenv line %d: %w", issue.Line, issue.Err)
	}
	return issue.Err
}
