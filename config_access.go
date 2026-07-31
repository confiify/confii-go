// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/confiify/confii-go/v2/internal/dictutil"
	"github.com/confiify/confii-go/v2/validate"
)

// Get retrieves an already-materialized value by dot-separated key path.
// When keyPath addresses a sub-tree, the returned map is a deep copy.
//
// Get is equivalent to calling [Config.GetWithContext] with Confii's implicit runtime
// context, bounded by [WithOperationTimeout].
// Callers that need request deadlines, cancellation, or values should use
// [Config.GetWithContext] directly.
func (c *Config[T]) Get(keyPath string) (any, error) {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.GetWithContext(ctx, keyPath)
}

// GetWithContext retrieves an already-materialized value by dot-separated key
// path. The context controls this operation and is checked before the snapshot
// is read; hooks and providers run when snapshots are built, not during reads.
//
// When keyPath resolves to a sub-tree (a nested map) the returned value
// is a deep copy. Mutating the returned value is safe and does not affect live
// config state. Scalar container values are also copied defensively.
func (c *Config[T]) GetWithContext(ctx context.Context, keyPath string) (result any, err error) {
	if ctx == nil {
		return nil, &ConfigError{Op: "Get", Key: keyPath, Code: ConfigErrorCodeInvalid, Err: errors.New("nil context")}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	started := time.Now()
	c.mu.RLock()
	observer := c.observer
	defer func() {
		if err == nil && observer != nil {
			observer.RecordAccess(keyPath, time.Since(started))
		}
	}()
	val, ok := dictutil.GetNested(c.envConfig, keyPath)
	if !ok {
		if c.opts.SysenvFallback {
			if envVal, found := c.lookupSysenv(keyPath); found {
				c.mu.RUnlock()
				processed, err := c.hookProcessor.Process(ctx, keyPath, envVal)
				if err != nil {
					return nil, err
				}
				// Sysenv values are strings, but a hook may produce a
				// reference type. Defensive copy keeps the contract
				// uniform.
				return dictutil.DeepCopyValue(processed), nil
			}
		}
		availableKeys := dictutil.FlatKeys(c.envConfig)
		c.mu.RUnlock()
		return nil, NewNotFoundError(keyPath, availableKeys)
	}
	// Snapshot the already-materialized value while it is protected.
	val = dictutil.DeepCopyValue(val)
	c.mu.RUnlock()
	return val, nil
}

// GetOr retrieves a value by key path, returning the default if the read fails.
// Use Get when hook, cancellation, and not-found errors must be distinguished.
func (c *Config[T]) GetOr(keyPath string, defaultVal any) any {
	val, err := c.Get(keyPath)
	if err != nil {
		return defaultVal
	}
	return val
}

// GetString retrieves a value by key path and returns its string form. Native
// strings are returned unchanged; other values are formatted with fmt.Sprint.
// Lookup failures, including cancellation and [ErrConfigNotFound], are returned.
func (c *Config[T]) GetString(keyPath string) (string, error) {
	val, err := c.Get(keyPath)
	if err != nil {
		return "", err
	}
	if s, ok := val.(string); ok {
		return s, nil
	}
	return fmt.Sprintf("%v", val), nil
}

// GetStringOr retrieves a string value, returning the default if the read or
// conversion fails. Use GetString when the error must be observed.
func (c *Config[T]) GetStringOr(keyPath, defaultVal string) string {
	s, err := c.GetString(keyPath)
	if err != nil {
		return defaultVal
	}
	return s
}

// GetInt retrieves an int value by key path.
//
// Native int values are returned unchanged. int64 and finite, integral float64
// values are converted when they fit the platform int range. Fractional,
// non-finite, overflowing, and unsupported values return a typed [*ConfigError]
// rather than being truncated.
func (c *Config[T]) GetInt(keyPath string) (int, error) {
	val, err := c.Get(keyPath)
	if err != nil {
		return 0, err
	}
	switch v := val.(type) {
	case int:
		return v, nil
	case int64:
		if v > int64(math.MaxInt) || v < int64(math.MinInt) {
			return 0, &ConfigError{
				Op:   "GetInt",
				Key:  keyPath,
				Code: ConfigErrorCodeInvalid,
				Err:  fmt.Errorf("int64 value %d overflows int", v),
			}
		}
		return int(v), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, &ConfigError{
				Op:   "GetInt",
				Key:  keyPath,
				Code: ConfigErrorCodeInvalid,
				Err:  fmt.Errorf("cannot convert non-finite float64 (%v) to int", v),
			}
		}
		if math.Trunc(v) != v {
			return 0, &ConfigError{
				Op:   "GetInt",
				Key:  keyPath,
				Code: ConfigErrorCodeInvalid,
				Err:  fmt.Errorf("float64 value %v has non-zero fractional part; refusing to truncate to int", v),
			}
		}
		// Range-check before narrowing to prevent silent wrapping on
		// architectures where int is 32 bits or values that exceed
		// int64 range when cast.
		if v > float64(math.MaxInt) || v < float64(math.MinInt) {
			return 0, &ConfigError{
				Op:   "GetInt",
				Key:  keyPath,
				Code: ConfigErrorCodeInvalid,
				Err:  fmt.Errorf("float64 value %v overflows int", v),
			}
		}
		return int(v), nil
	default:
		return 0, &ConfigError{Op: "GetInt", Key: keyPath, Code: ConfigErrorCodeAccess, Err: fmt.Errorf("cannot convert %T to int", val)}
	}
}

// GetIntOr retrieves an int value, returning the default if the read or
// conversion fails. Use GetInt when the error must be observed.
func (c *Config[T]) GetIntOr(keyPath string, defaultVal int) int {
	v, err := c.GetInt(keyPath)
	if err != nil {
		return defaultVal
	}
	return v
}

// GetBool retrieves a bool value by key path.
//
// In addition to a native bool, GetBool accepts the canonical string
// boolean spellings documented in docs/access.md and produced by the
// type-casting hook (see [WithTypeCasting]):
//
//   - true:  "true",  "1", "yes", "on"
//   - false: "false", "0", "no",  "off"
//
// Matching is case-insensitive and surrounding whitespace is trimmed.
// Numeric forms 0 and 1 (as int / int64 / float64) also coerce, so a
// JSON value `1` and a YAML value `true` are treated equivalently. Any
// other value returns a typed *ConfigError.
//
// The accepted forms match the type-casting behavior documented in
// docs/access.md.
func (c *Config[T]) GetBool(keyPath string) (bool, error) {
	val, err := c.Get(keyPath)
	if err != nil {
		return false, err
	}
	switch v := val.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "on":
			return true, nil
		case "false", "0", "no", "off":
			return false, nil
		}
		return false, &ConfigError{
			Op:   "GetBool",
			Key:  keyPath,
			Code: ConfigErrorCodeAccess,
			Err:  fmt.Errorf("cannot convert string %q to bool (accepted: true/false, 1/0, yes/no, on/off)", v),
		}
	case int:
		switch v {
		case 0:
			return false, nil
		case 1:
			return true, nil
		}
	case int64:
		switch v {
		case 0:
			return false, nil
		case 1:
			return true, nil
		}
	case float64:
		switch v {
		case 0:
			return false, nil
		case 1:
			return true, nil
		}
	}
	return false, &ConfigError{Op: "GetBool", Key: keyPath, Code: ConfigErrorCodeAccess, Err: fmt.Errorf("cannot convert %T to bool", val)}
}

// GetBoolOr retrieves a bool value, returning the default if the read or
// conversion fails. Use GetBool when the error must be observed.
func (c *Config[T]) GetBoolOr(keyPath string, defaultVal bool) bool {
	v, err := c.GetBool(keyPath)
	if err != nil {
		return defaultVal
	}
	return v
}

// GetFloat64 retrieves a numeric value by key path. It accepts float64, int,
// and int64 values; all other types return a typed [*ConfigError]. Converting a
// sufficiently large integer may lose precision according to Go's normal
// integer-to-float conversion rules.
func (c *Config[T]) GetFloat64(keyPath string) (float64, error) {
	val, err := c.Get(keyPath)
	if err != nil {
		return 0, err
	}
	switch v := val.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	default:
		return 0, &ConfigError{Op: "GetFloat64", Key: keyPath, Code: ConfigErrorCodeAccess, Err: fmt.Errorf("cannot convert %T to float64", val)}
	}
}

// GetFloat64Or retrieves a float64 value, returning the default if the read or
// conversion fails. Use GetFloat64 when the error must be observed.
func (c *Config[T]) GetFloat64Or(keyPath string, defaultVal float64) float64 {
	v, err := c.GetFloat64(keyPath)
	if err != nil {
		return defaultVal
	}
	return v
}

// MustGet retrieves a value and panics with the underlying error when lookup
// fails. It is appropriate for startup invariants and tests where absence is a
// programming error; request-handling code should normally use [Config.Get].
func (c *Config[T]) MustGet(keyPath string) any {
	val, err := c.Get(keyPath)
	if err != nil {
		panic(err)
	}
	return val
}

// Has reports whether keyPath is reachable through any resolution path
// that [Config.Get] would consult.
//
// Specifically Has returns true when:
//
//   - keyPath is present in the loaded/merged config map; OR
//   - sysenv fallback is enabled with [WithSysenvFallback]
//     AND a process-environment variable named after keyPath (uppercased,
//     dots replaced with underscores, optionally prefixed with the
//     configured [WithEnvPrefix]) is set.
//
// Has and Get use the same lookup paths: if Has reports true, Get
// will not return ErrConfigNotFound (modulo concurrent mutation); if Has
// reports false, Get returns ErrConfigNotFound.
func (c *Config[T]) Has(keyPath string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if dictutil.HasNested(c.envConfig, keyPath) {
		return true
	}
	if c.opts.SysenvFallback {
		if _, found := c.lookupSysenv(keyPath); found {
			return true
		}
	}
	return false
}

// Keys returns all dot-separated leaf key paths.
//
// When called with a prefix, only keys that start with that prefix are
// returned, and the keys retain their FULL dot-separated path (the prefix
// is NOT stripped). For example:
//
//	cfg.Set("db.host", "localhost")
//	cfg.Set("db.port", 5432)
//	cfg.Keys("db") // []string{"db.host", "db.port"}
//	for _, k := range cfg.Keys("db") {
//	    v, _ := cfg.Get(k) // each key works directly with Get
//	}
//
// Keys returns fully-qualified paths so callers can
// feed each key directly back into any access helper. Callers who need
// the suffix-only form can derive it with strings.TrimPrefix at the call
// site.
//
// The prefix accepts either "db" or "db." (the trailing dot is added if
// absent); both forms are equivalent.
func (c *Config[T]) Keys(prefix ...string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p := ""
	if len(prefix) > 0 {
		p = prefix[0]
	}
	if p == "" {
		keys := dictutil.FlatKeys(c.envConfig)
		sort.Strings(keys)
		return keys
	}
	// Normalize so callers can pass either "db" or "db." — both should
	// match keys "db.host", "db.port", etc.
	matchPrefix := p
	if !strings.HasSuffix(matchPrefix, ".") {
		matchPrefix += "."
	}
	all := dictutil.FlatKeys(c.envConfig)
	keys := make([]string, 0, len(all))
	for _, k := range all {
		if strings.HasPrefix(k, matchPrefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// ToDict returns the materialized configuration as a deep copy.
//
// ToDict uses the implicit runtime context bounded by [WithOperationTimeout].
// Transformations and secret resolution do not rerun during reads. Mutating
// the returned map cannot modify Config state or bypass [Config.Freeze].
func (c *Config[T]) ToDict() (map[string]any, error) {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.ToDictWithContext(ctx)
}

// ToDictWithContext returns a deep copy of the materialized configuration.
// It returns immediately when ctx is canceled. Transformations and remote
// secret resolution occur during snapshot materialization, not during reads.
func (c *Config[T]) ToDictWithContext(ctx context.Context) (map[string]any, error) {
	if ctx == nil {
		return nil, &ConfigError{Op: "ToDict", Code: ConfigErrorCodeInvalid, Err: errors.New("nil context")}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.RLock()
	src := c.envConfig
	if src == nil {
		src = c.mergedConfig
	}
	snapshot := dictutil.DeepCopy(src)
	c.mu.RUnlock()
	return snapshot, nil
}

// Typed decodes the configuration into a typed struct T and validates it.
//
// Typed uses the implicit runtime context bounded by [WithOperationTimeout].
// Materialization and secret errors are reported before a snapshot is
// published; Typed only decodes and validates that published snapshot.
//
// The decoded pointer is cached until the Config publishes another snapshot.
// Repeated Typed calls may therefore return the same pointer. Mutating it
// changes that typed view but does not write back to Config.Get or ToDict;
// use [Config.Set] for a configuration mutation. Call TypedWithContext when an
// independently decoded value is required.
//
//	type AppConfig struct {
//		Server struct {
//			Host string `confii:"host" validate:"required"`
//			Port int    `confii:"port" validate:"min=1,max=65535"`
//		} `confii:"server"`
//	}
//	app, err := cfg.Typed()
//	if err != nil { return err }
//	fmt.Println(app.Server.Host, app.Server.Port)
func (c *Config[T]) Typed() (*T, error) {
	ctx, cancel := c.implicitOperationContext()
	defer cancel()
	return c.typedCtx(ctx, true)
}

// TypedWithContext decodes and validates the published configuration snapshot
// into a new *T on every call. The returned value is independent from the
// Config and other typed results. It returns immediately when ctx is canceled.
// The context does not trigger transformation hooks or remote secret
// resolution during the read.
func (c *Config[T]) TypedWithContext(ctx context.Context) (*T, error) {
	return c.typedCtx(ctx, false)
}

func (c *Config[T]) typedCtx(ctx context.Context, cacheable bool) (*T, error) {
	if ctx == nil {
		return nil, NewValidationError([]string{"nil context"}, errors.New("nil context"))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.RLock()
	if cacheable && c.validatedModel != nil {
		model := c.validatedModel
		c.mu.RUnlock()
		return model, nil
	}
	snapshot := dictutil.DeepCopy(c.envConfig)
	c.mu.RUnlock()

	model, err := validate.DecodeAndValidate[T](snapshot)
	if err != nil {
		return nil, NewValidationError([]string{err.Error()}, err)
	}
	if !cacheable {
		return model, nil
	}

	// Publish the context-free cache only if live state still matches the
	// snapshot that was resolved. A concurrent mutation may have invalidated the
	// snapshot while decoding; caching that stale model would make future Typed
	// calls return data older than envConfig. Concurrent decoders of the same
	// snapshot may replace one equivalent cached pointer with another.
	c.mu.Lock()
	if reflect.DeepEqual(snapshot, c.envConfig) {
		c.validatedModel = model
	}
	c.mu.Unlock()
	return model, nil
}

// String returns a human-readable summary of the Config, including its
// environment, key count, loader sources, and frozen state.
func (c *Config[T]) String() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	sources := make([]string, 0, len(c.loaders))
	for _, l := range c.loaders {
		sources = append(sources, l.Source())
	}
	frozen := ""
	if c.frozen {
		frozen = ", frozen"
	}
	return fmt.Sprintf("Config(env=%q, keys=%d, sources=%v%s)",
		c.env, len(dictutil.FlatKeys(c.envConfig)), sources, frozen)
}

func (c *Config[T]) lookupSysenv(keyPath string) (any, bool) {
	envName := strings.ToUpper(strings.ReplaceAll(keyPath, ".", "_"))
	if c.opts.EnvPrefix != "" {
		envName = strings.ToUpper(c.opts.EnvPrefix) + "_" + envName
	}
	val, ok := os.LookupEnv(envName)
	if !ok {
		return nil, false
	}
	return val, true
}
