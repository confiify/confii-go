package confii

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Sentinel errors for use with errors.Is.
var (
	ErrConfigLoad       = errors.New("config load error")
	ErrConfigFormat     = errors.New("config format error")
	ErrConfigValidation = errors.New("config validation error")
	ErrConfigNotFound   = errors.New("config key not found")
	ErrConfigMerge      = errors.New("config merge conflict")
	ErrConfigFrozen     = errors.New("config is frozen")
	ErrConfigAccess     = errors.New("config access error")
	// ErrConfigInvalid is returned when a runtime mutation API ([Config.Set],
	// [Config.Override]) is invoked with an argument that the config rejects:
	// most commonly a dot-separated key path that traverses through a
	// non-map intermediate (e.g. setting "service.host" when "service" is
	// already bound to a string). Pre-G12 these errors were silently
	// swallowed, leaving caller and config state out of sync. Wrap with
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
	Op      string // operation that failed (e.g., "Load", "Get", "Set")
	Source  string // source identifier (e.g., file path, URL)
	Key     string // config key involved, if applicable
	Err     error  // underlying error (wraps a sentinel)
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
// Context field. The legacy prefixes from the sentinel errors
// (e.g. "config load error:", "config key not found") are preserved so
// existing log pipelines and substring matchers continue to work.
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

func (e *ConfigError) Unwrap() error {
	return e.Err
}

// formatContext renders ctx as a deterministic, single-line, comma-separated
// list of "key=value" pairs wrapped in "[ ... ]". Keys are sorted
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

	// Fall back on reflect-free type detection for the common collection
	// shapes via fmt verbs — but guard against unbounded expansion by
	// summarizing any slice / map we recognize through a type switch on the
	// concrete kinds we care about. For everything else, %v is fine because
	// it's a scalar-shaped value.
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

// NewLoadError creates a config load error.
func NewLoadError(source string, err error) error {
	return &ConfigError{
		Op:     "Load",
		Source: source,
		Err:    fmt.Errorf("%w: %v", ErrConfigLoad, err),
	}
}

// NewFormatError creates a config format/parse error.
func NewFormatError(source, formatType string, err error) error {
	return &ConfigError{
		Op:     "Parse",
		Source: source,
		Err:    fmt.Errorf("%w: %v", ErrConfigFormat, err),
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

// NewValidationError creates a config validation error.
func NewValidationError(errs []string, original error) error {
	return &ConfigError{
		Op:  "Validate",
		Err: fmt.Errorf("%w: %v", ErrConfigValidation, original),
		Context: map[string]any{
			"validation_errors": errs,
		},
	}
}

// NewFrozenError creates an error for operations on frozen config.
func NewFrozenError(op string) error {
	return &ConfigError{
		Op:  op,
		Err: ErrConfigFrozen,
	}
}

// NewInvalidError creates a typed *ConfigError wrapping [ErrConfigInvalid].
// Used by [Config.Set] and [Config.Override] (G12) to surface the path
// errors raised by dictutil.SetNested when a key-path traversal hits a
// non-map intermediate, instead of silently swallowing them.
func NewInvalidError(op, key string, err error) error {
	return &ConfigError{
		Op:  op,
		Key: key,
		Err: fmt.Errorf("%w: %v", ErrConfigInvalid, err),
	}
}
