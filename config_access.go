// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"fmt"
	"github.com/confiify/confii-go/internal/dictutil"
	"github.com/confiify/confii-go/validate"
	"math"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Get retrieves a value by dot-separated key path. Hooks are applied
// uniformly: scalar leaves run through the hook pipeline (env expansion,
// secret resolution, type casting, custom hooks), and when keyPath
// addresses a sub-tree the returned map is a hook-applied deep copy
// whose every leaf has been processed by the same pipeline.
//
// Get is equivalent to calling [Config.GetCtx] with [context.Background].
// Legacy [hook.Func] hooks have no way to surface errors and run as
// before. Context-aware [hook.FuncCtx] hooks (for example, a
// [secret.Resolver] registered via [Resolver.HookCtx]) execute under
// [context.Background] and any error they return is propagated back to
// the caller of Get. Callers that need to honor per-request deadlines
// or thread caller context through to remote stores should use
// [Config.GetCtx] directly.
//
// G11: prior to Wave 8, only scalar Get applied hooks; whole-map Get,
// [Config.Typed], [Config.ToDict], and [Config.Export] returned raw
// (unhooked) values. The pipeline is now applied uniformly across every
// access mode so that a service calling cfg.Get("db.password") and one
// calling cfg.Typed[Config]() see the same effective value.
func (c *Config[T]) Get(keyPath string) (any, error) {
	return c.GetCtx(context.Background(), keyPath)
}

// GetCtx retrieves a value by dot-separated key path, threading ctx through
// any registered context-aware hooks.
//
// The supplied ctx is passed to every [hook.FuncCtx] registered on the
// processor (see [hook.Processor.RegisterGlobalHookCtx]). If a hook
// returns an error — for example, a [secret.Resolver] configured with
// [secret.WithResolverFailOnMissing](true) when a referenced secret is
// not found — that error is returned to the caller and the partially
// resolved value is discarded. Hooks of the legacy [hook.Func] signature
// continue to run unchanged and cannot raise errors.
//
// When keyPath resolves to a sub-tree (a nested map) the returned value
// is a hook-applied deep copy: every leaf in the returned map has been
// passed through the hook pipeline with its absolute key path. Mutating
// the returned map is safe and does not affect live config state.
//
// G10 (Wave 12): the read-side deep-copy contract is now symmetric with
// the write-side contract closed in Wave 11 (Set deep-copies inputs via
// dictutil.DeepCopyValue). Whole-map Get already flowed through
// applyHooksRecursive, which produces a deep copy. For the scalar /
// slice leaf path this method now defensively deep-copies the
// hook-processor output via dictutil.DeepCopyValue before returning, so
// a caller cannot mutate a returned []any (or a map embedded in a hook
// result) and have those mutations leak back into envConfig — which
// would otherwise let users bypass Freeze and break the documented
// thread-safety claim.
func (c *Config[T]) GetCtx(ctx context.Context, keyPath string) (result any, err error) {
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
				processed, err := c.hookProcessor.ProcessCtx(ctx, keyPath, envVal)
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
	// Snapshot the selected value while it is protected, then release the
	// Config lock before running user hooks. Hooks may perform I/O or call
	// back into Config.Set/Get; executing them under c.mu would block writers
	// and makes a re-entrant write deadlock.
	val = dictutil.DeepCopyValue(val)
	c.mu.RUnlock()

	if subMap, isMap := val.(map[string]any); isMap {
		// G11: whole-map Get used to bypass hooks entirely. Apply the
		// hook pipeline to every leaf in a deep copy so the caller sees
		// resolved (env-expanded, secret-substituted, type-cast) values
		// regardless of whether they fetched a scalar or a sub-tree.
		// applyHooksRecursive already produces a deep copy (G11 +
		// Wave 12 G10 read-side deep-copy contract).
		return c.applyHooksRecursive(ctx, keyPath, subMap)
	}
	if subSlice, isSlice := val.([]any); isSlice {
		// G10 (Wave 12): a leaf []any in envConfig was previously
		// returned to the caller as a live reference — mutation of
		// any element would alias into Config state, breaking Freeze
		// and racing against concurrent writers. Apply the hook
		// pipeline element-by-element (matching ToDict / Export) and
		// return the resulting fresh slice, which is by construction
		// a deep copy.
		return c.applyHooksToSlice(ctx, keyPath, subSlice)
	}
	processed, err := c.hookProcessor.ProcessCtx(ctx, keyPath, val)
	if err != nil {
		return nil, err
	}
	// G10 (Wave 12): scalar leaves are typically immutable (string,
	// int, bool, nil), but a custom hook is free to materialize a
	// map/slice. Run the result through DeepCopyValue so the contract
	// holds uniformly: nothing returned from Get aliases live state.
	return dictutil.DeepCopyValue(processed), nil
}

// GetOr retrieves a value by key path, returning the default if not found.
func (c *Config[T]) GetOr(keyPath string, defaultVal any) any {
	val, err := c.Get(keyPath)
	if err != nil {
		return defaultVal
	}
	return val
}

// GetString retrieves a string value by key path.
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

// GetStringOr retrieves a string value, returning the default if not found.
func (c *Config[T]) GetStringOr(keyPath, defaultVal string) string {
	s, err := c.GetString(keyPath)
	if err != nil {
		return defaultVal
	}
	return s
}

// GetInt retrieves an int value by key path.
//
// G30 (Wave 21) — behavior tightening (no API signature change). For a
// stored float64 (the natural type produced by JSON / YAML decoding of
// numeric literals), GetInt now requires the value to have a zero
// fractional part (i.e. math.Trunc(v) == v). When the fractional part is
// non-zero the call returns a typed *ConfigError wrapping
// [ErrConfigInvalid] instead of silently truncating: pre-Wave 21
// behavior turned `cfg.Set("port", 3.14); cfg.GetInt("port")` into the
// surprise return value (3, nil), which masked configuration bugs.
// Integer-valued floats (3.0, -7.0, 1e6) continue to convert cleanly.
// The float64 → int conversion also guards against int-range overflow.
func (c *Config[T]) GetInt(keyPath string) (int, error) {
	val, err := c.Get(keyPath)
	if err != nil {
		return 0, err
	}
	switch v := val.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, &ConfigError{
				Op:  "GetInt",
				Key: keyPath,
				Err: fmt.Errorf("%w: cannot convert non-finite float64 (%v) to int", ErrConfigInvalid, v),
			}
		}
		if math.Trunc(v) != v {
			return 0, &ConfigError{
				Op:  "GetInt",
				Key: keyPath,
				Err: fmt.Errorf("%w: float64 value %v has non-zero fractional part; refusing to truncate to int", ErrConfigInvalid, v),
			}
		}
		// Range-check before narrowing so we don't silently wrap on
		// architectures where int is 32 bits or values that exceed
		// int64 range when cast.
		if v > float64(math.MaxInt) || v < float64(math.MinInt) {
			return 0, &ConfigError{
				Op:  "GetInt",
				Key: keyPath,
				Err: fmt.Errorf("%w: float64 value %v overflows int", ErrConfigInvalid, v),
			}
		}
		return int(v), nil
	default:
		return 0, &ConfigError{Op: "GetInt", Key: keyPath, Err: fmt.Errorf("cannot convert %T to int", val)}
	}
}

// GetIntOr retrieves an int value, returning the default if not found.
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
// G30 (Wave 21) — behavior tightening (no API signature change). Prior
// to Wave 21, GetBool only accepted a native Go bool, so a stored string
// "1" or "yes" — which docs/access.md and the type-casting hook docs
// both claim are valid bool forms — failed with "cannot convert string
// to bool". The accepted-form set now matches the documented contract.
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
			Op:  "GetBool",
			Key: keyPath,
			Err: fmt.Errorf("cannot convert string %q to bool (accepted: true/false, 1/0, yes/no, on/off)", v),
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
	return false, &ConfigError{Op: "GetBool", Key: keyPath, Err: fmt.Errorf("cannot convert %T to bool", val)}
}

// GetBoolOr retrieves a bool value, returning the default if not found.
func (c *Config[T]) GetBoolOr(keyPath string, defaultVal bool) bool {
	v, err := c.GetBool(keyPath)
	if err != nil {
		return defaultVal
	}
	return v
}

// GetFloat64 retrieves a float64 value by key path.
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
		return 0, &ConfigError{Op: "GetFloat64", Key: keyPath, Err: fmt.Errorf("cannot convert %T to float64", val)}
	}
}

// MustGet retrieves a value and panics on error. Intended for tests.
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
//   - sysenv fallback is enabled (the default; see [WithSysenvFallback])
//     AND a process-environment variable named after keyPath (uppercased,
//     dots replaced with underscores, optionally prefixed with the
//     configured [WithEnvPrefix]) is set.
//
// G30 (Wave 21) — behavior tightening (no API signature change). Prior
// to Wave 21, Has consulted only the in-memory config map and ignored
// sysenv fallback, so a caller could observe Has("db.password") == false
// while Get("db.password") returned a real value resolved via
// $APP_DB_PASSWORD. The two helpers now agree: if Has reports true, Get
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
// G30 (Wave 21) — behavior tightening (no API signature change). Prior to
// Wave 21, Keys(prefix) stripped the prefix from each returned key (e.g.
// returned "host" / "port" for prefix "db"), which conflicted with the
// documented `for _, k := range cfg.Keys(p) { cfg.Get(k) }` iteration
// pattern: callers had to manually re-prepend the prefix before Get/Has
// would resolve. Keys now returns fully-qualified paths so callers can
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

// ToDict returns the effective configuration as a hook-applied deep copy.
//
// ToDict is equivalent to [Config.ToDictCtx] with [context.Background].
// Errors raised by context-aware hooks are silently discarded — the
// pre-hook value is retained in their place. Callers that need to
// observe hook errors (for example, [secret.WithResolverFailOnMissing]
// fail-fast behavior) should use [Config.ToDictCtx].
//
// G11: prior to Wave 8 ToDict returned the live envConfig pointer
// without applying hooks, so callers saw raw `${secret:…}` placeholders
// and unexpanded `${ENV_VAR}` strings. The returned map is now both
// hook-applied and a defensive deep copy so callers can mutate it
// freely.
//
// G10 (Wave 12): the deep-copy contract is the read-side counterpart of
// the Wave 11 [Config.Set] write-side contract. Mutating the returned
// map cannot bleed into envConfig, cannot race against concurrent
// writers, and cannot be used to bypass [Config.Freeze]. The source is
// deep-copied while c.mu.RLock is held, then hooks run after the lock is
// released so user hook code may safely call back into Config APIs.
func (c *Config[T]) ToDict() map[string]any {
	d, _ := c.ToDictCtx(context.Background())
	return d
}

// ToDictCtx returns the effective configuration as a hook-applied deep
// copy, threading ctx through any registered context-aware hooks.
//
// Every leaf in the returned map is passed through the hook pipeline
// (env expansion, secret resolution, type casting, custom hooks) with
// its full dot-separated key path. The first error returned by a
// context-aware hook is propagated to the caller; subsequent leaves are
// not processed and the partially-resolved map is discarded. Legacy
// [hook.Func] hooks continue to run as before and cannot raise errors.
//
// The returned map is a deep copy of the live config: callers may
// mutate it without affecting the Config's state.
func (c *Config[T]) ToDictCtx(ctx context.Context) (map[string]any, error) {
	c.mu.RLock()
	src := c.envConfig
	if src == nil {
		src = c.mergedConfig
	}
	snapshot := dictutil.DeepCopy(src)
	c.mu.RUnlock()
	return c.applyHooksRecursive(ctx, "", snapshot)
}

// Typed decodes the configuration into a typed struct T and validates it.
//
// Typed is equivalent to [Config.TypedCtx] with [context.Background].
// Hook errors raised by context-aware hooks (for example a
// [secret.Resolver] configured with [secret.WithResolverFailOnMissing])
// are propagated as a [*ConfigError] wrapping [ErrConfigValidation].
//
// G11: prior to Wave 8 Typed decoded raw `c.envConfig` straight into T
// without applying hooks, so a `${secret:db/password}` placeholder ended
// up as the literal string in the typed struct while [Config.Get]
// returned the resolved secret. Typed now applies the hook pipeline to
// a deep copy of the source map before decoding so the typed model and
// scalar Get agree on the effective configuration.
func (c *Config[T]) Typed() (*T, error) {
	return c.TypedCtx(context.Background())
}

// TypedCtx decodes the configuration into a typed struct T and validates
// it, applying the hook pipeline to every leaf before decoding.
//
// The supplied ctx is threaded through every registered context-aware
// hook. The first error returned by a hook (or by validation) is
// propagated; the validated-model cache is only populated when the
// caller passes a context-free invocation (i.e. via [Config.Typed]).
// Callers passing a request-scoped ctx always re-run hooks so that
// per-request context (deadlines, cancellation, propagated values)
// affects the materialized model.
func (c *Config[T]) TypedCtx(ctx context.Context) (*T, error) {
	// Cache fast-path: only valid for the canonical context-free call,
	// which is signalled by ctx == context.Background(). A caller-supplied
	// ctx may carry per-request values that the resolver depends on, so
	// re-run the pipeline rather than returning a possibly-stale model.
	cacheable := ctx == context.Background()

	c.mu.RLock()
	if cacheable && c.validatedModel != nil {
		model := c.validatedModel
		c.mu.RUnlock()
		return model, nil
	}
	snapshot := dictutil.DeepCopy(c.envConfig)
	c.mu.RUnlock()

	// Hooks are user code and may perform I/O or re-enter Config. Run them
	// against the private snapshot without holding c.mu. This also keeps a
	// slow resolver from blocking unrelated readers and writers.
	resolved, err := c.applyHooksRecursive(ctx, "", snapshot)
	if err != nil {
		return nil, NewValidationError([]string{err.Error()}, err)
	}

	model, err := validate.DecodeAndValidate[T](resolved)
	if err != nil {
		return nil, NewValidationError([]string{err.Error()}, err)
	}
	if !cacheable {
		return model, nil
	}

	// Publish the context-free cache only if live state still matches the
	// snapshot that was resolved. A hook may have re-entered Set/Reload while
	// we were unlocked; caching that now-stale model would make future Typed
	// calls return data older than envConfig. If another caller populated a
	// valid cache first, prefer it.
	c.mu.Lock()
	if c.validatedModel != nil {
		model = c.validatedModel
	} else if reflect.DeepEqual(snapshot, c.envConfig) {
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
