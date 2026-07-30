// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Sentinel errors classify failures for use with [errors.Is]. Public APIs may
// wrap both a category and an operational cause; use [errors.As] to inspect a
// [ConfigError] and its structured fields.
var (
	ErrConfigLoad       = errors.New("config load error")
	ErrConfigFormat     = errors.New("config format error")
	ErrConfigValidation = errors.New("config validation error")
	ErrConfigNotFound   = errors.New("config key not found")
	ErrConfigMerge      = errors.New("config merge conflict")
	ErrConfigFrozen     = errors.New("config is frozen")
	ErrConfigClosed     = errors.New("config is closed")
	ErrConfigAccess     = errors.New("config access error")
	// ErrConfigInvalid is returned when a runtime mutation API ([Config.Set],
	// [Config.Override]) is invoked with an argument that the config rejects:
	// most commonly a dot-separated key path that traverses through a
	// non-map intermediate (e.g. setting "service.host" when "service" is
	// already bound to a string). These errors must be surfaced rather than
	// swallowed, which would leave caller and config state out of sync. Wrap with
	// [NewInvalidError] so callers can detect via [errors.Is].
	ErrConfigInvalid    = errors.New("config invalid mutation")
	ErrSecretNotFound   = errors.New("secret not found")
	ErrSecretAccess     = errors.New("secret access error")
	ErrSecretStore      = errors.New("secret store error")
	ErrSecretValidation = errors.New("secret validation error")
	ErrVaultAuth        = errors.New("vault authentication error")
)

// availableKeysCap is the maximum number of available keys included in the
// rendered string form of a not-found error. The keys are sorted
// alphabetically and any beyond the cap are summarized as "(N more...)".
// The full slice is preserved on the ConfigError.Context map so callers can
// still inspect every candidate programmatically.
const availableKeysCap = 10

// ConfigError is a structured error that captures the failing operation,
// the source and key involved, and an underlying sentinel error. Use
// [errors.Is] or [errors.As] to inspect the chain.
//
// The Context field carries the full structured payload (e.g. the complete
// list of available keys for not-found errors); the Error string form is
// deliberately bounded and deterministic — it sorts context keys
// alphabetically, summarizes nested collections, and caps long key lists.
// To read the unbounded data, inspect Context directly.
type ConfigError struct {
	Op     string // operation that failed (e.g., "Load", "Get", "Set")
	Source string // source identifier (e.g., file path, URL)
	Key    string // config key involved, if applicable
	Err    error  // underlying error (wraps a sentinel)
	// Cause preserves the concrete operational failure separately from Err's
	// stable Confii category. Error renders Err for operators, while
	// errors.Is/errors.As can still discover cancellation, deadline, SDK, and
	// filesystem causes through Unwrap.
	Cause   error
	Context map[string]any
}

// Error renders a stable, operator-friendly message of the form
//
//	<Op>: [<Source>] key "<Key>": <underlying error> [k1=v1, k2=v2, ...]
//
// Context keys are sorted alphabetically so the output is byte-deterministic
// across invocations (Go's map iteration order is randomized). Nested maps
// and slices are summarized as "<map: N entries>" / "<slice: N items>" to
// keep the message bounded — full structured data is available via the
// Context field. Message text is operator-oriented, not a machine interface;
// consumers should use errors.Is/errors.As and the structured fields.
func (e *ConfigError) Error() string {
	var b strings.Builder
	if e.Op != "" {
		b.WriteString(e.Op)
		b.WriteString(": ")
	}
	if e.Source != "" {
		fmt.Fprintf(&b, "[%s] ", e.Source)
	}
	if e.Key != "" {
		fmt.Fprintf(&b, "key %q: ", e.Key)
	}
	if e.Err != nil {
		b.WriteString(e.Err.Error())
	}
	if len(e.Context) > 0 {
		b.WriteString(" ")
		b.WriteString(formatContext(e.Context))
	}
	return b.String()
}

// Unwrap returns the stable Confii category and, when distinct, the concrete
// operational cause. This permits errors.Is/errors.As to match either one.
func (e *ConfigError) Unwrap() []error {
	if e == nil {
		return nil
	}
	if e.Cause == nil || e.Cause == e.Err {
		if e.Err == nil {
			return nil
		}
		return []error{e.Err}
	}
	if e.Err == nil {
		return []error{e.Cause}
	}
	return []error{e.Err, e.Cause}
}

// formatContext renders ctx as a deterministic, single-line, comma-separated
// list of "key=value" pairs wrapped in "[... ]". Keys are sorted
// alphabetically so output is reproducible. The special key
// "available_keys" (used by NewNotFoundError) is rendered alphabetically
// sorted and capped at availableKeysCap entries; overflow is summarized as
// "(N more...)". Other map / slice values are summarized as
// "<map: N entries>" / "<slice: N items>" rather than expanded inline so
// the message stays bounded for large configs.
func formatContext(ctx map[string]any) string {
	keys := make([]string, 0, len(ctx))
	for k := range ctx {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteByte('[')
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(formatContextValue(k, ctx[k]))
	}
	b.WriteByte(']')
	return b.String()
}

// formatContextValue produces the bounded, deterministic string form of a
// single context value. The key name is passed in so that "available_keys"
// (and any other []string value) gets the sorted-and-capped list treatment.
func formatContextValue(_ string, v any) string {
	switch val := v.(type) {
	case nil:
		return "<nil>"
	case string:
		return val
	case []string:
		return formatStringSlice(val)
	case map[string]any:
		return fmt.Sprintf("<map: %d entries>", len(val))
	}

	// Summarize common collection types to keep error messages bounded.
	switch val := v.(type) {
	case []any:
		return fmt.Sprintf("<slice: %d items>", len(val))
	case []int:
		return fmt.Sprintf("<slice: %d items>", len(val))
	case []float64:
		return fmt.Sprintf("<slice: %d items>", len(val))
	case map[string]string:
		return fmt.Sprintf("<map: %d entries>", len(val))
	case map[string]int:
		return fmt.Sprintf("<map: %d entries>", len(val))
	}

	return fmt.Sprintf("%v", v)
}

// formatStringSlice renders a []string deterministically: alphabetical sort,
// capped at availableKeysCap entries, with an "(N more...)" suffix if the
// list was truncated. This is the primary mechanism for taming the
// available-keys list on not-found errors against large configs.
func formatStringSlice(items []string) string {
	sorted := make([]string, len(items))
	copy(sorted, items)
	sort.Strings(sorted)

	if len(sorted) <= availableKeysCap {
		return "[" + strings.Join(sorted, ", ") + "]"
	}
	shown := sorted[:availableKeysCap]
	remaining := len(sorted) - availableKeysCap
	return "[" + strings.Join(shown, ", ") + fmt.Sprintf(" (%d more...)]", remaining)
}

// NewLoadError returns a [ConfigError] categorized as [ErrConfigLoad]. source
// should be the loader's stable, non-sensitive identifier. The supplied cause
// remains discoverable with [errors.Is] and [errors.As].
func NewLoadError(source string, err error) error {
	return &ConfigError{
		Op:     "Load",
		Source: source,
		Err:    fmt.Errorf("%w: %v", ErrConfigLoad, err),
		Cause:  err,
	}
}

// NewFormatError returns a [ConfigError] categorized as [ErrConfigFormat]. It
// records source and formatType for diagnostics and preserves err in the error
// chain.
func NewFormatError(source, formatType string, err error) error {
	return &ConfigError{
		Op:     "Parse",
		Source: source,
		Err:    fmt.Errorf("%w: %v", ErrConfigFormat, err),
		Cause:  err,
		Context: map[string]any{
			"format_type": formatType,
		},
	}
}

// NewNotFoundError creates a key-not-found error with the full list of
// available keys preserved on Context["available_keys"]. The string form
// returned by Error() sorts the keys alphabetically and caps the rendered
// list at availableKeysCap, appending an "(N more...)" suffix when
// truncated; this keeps operator-facing messages bounded for large configs
// while still letting programmatic callers read every candidate via the
// Context field.
func NewNotFoundError(key string, availableKeys []string) error {
	return &ConfigError{
		Op:  "Get",
		Key: key,
		Err: ErrConfigNotFound,
		Context: map[string]any{
			"available_keys": availableKeys,
		},
	}
}

// NewValidationError returns a [ConfigError] categorized as
// [ErrConfigValidation]. errs is retained in Context["validation_errors"] for
// programmatic reporting, while original remains in the error chain.
func NewValidationError(errs []string, original error) error {
	return &ConfigError{
		Op:    "Validate",
		Err:   fmt.Errorf("%w: %v", ErrConfigValidation, original),
		Cause: original,
		Context: map[string]any{
			"validation_errors": errs,
		},
	}
}

// NewFrozenError returns a [ConfigError] categorized as [ErrConfigFrozen] and
// records the rejected operation name.
func NewFrozenError(op string) error {
	return &ConfigError{
		Op:  op,
		Err: ErrConfigFrozen,
	}
}

// NewClosedError returns a [ConfigError] categorized as [ErrConfigClosed] for a
// mutation attempted after [Config.Close].
func NewClosedError(op string) error {
	return &ConfigError{Op: op, Err: ErrConfigClosed}
}

// NewInvalidError returns a [ConfigError] categorized as [ErrConfigInvalid].
// op and key identify the rejected operation and key path, while err remains
// available through the error chain.
func NewInvalidError(op, key string, err error) error {
	return &ConfigError{
		Op:    op,
		Key:   key,
		Err:   fmt.Errorf("%w: %v", ErrConfigInvalid, err),
		Cause: err,
	}
}
