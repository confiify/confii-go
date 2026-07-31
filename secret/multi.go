// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package secret

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	confii "github.com/confiify/confii-go/v2"
)

// MultiStore tries multiple stores in priority order.
//
// Error semantics:
//
//   - [MultiStore.GetSecret] returns the first successful value. It continues
//     only after [confii.ErrSecretNotFound]. A cancellation or non-not-found
//     backend failure stops the search so an outage cannot silently fall back
//     to a lower-authority store. The error includes prior misses and the
//     failing backend. If every store misses, the result wraps
//     [confii.ErrSecretNotFound].
//
//   - [MultiStore.ListSecrets] returns the union of keys from every store
//     that succeeded. If at least one store errored, the partial inventory
//     is still returned alongside a [MultiStoreError] describing each
//     backing failure; callers must check both the slice and the error.
//
// Per-store errors remain available through [errors.Is] and [errors.As], so
// callers can distinguish an absent secret from authentication, network, or
// provider failures.
type MultiStore struct {
	stores       []confii.SecretStore
	writeToFirst bool
	logger       *slog.Logger
}

// MultiStoreOption configures a MultiStore.
type MultiStoreOption func(*MultiStore)

// WithWriteToFirst controls write and delete fan-out. The default true sends
// the operation only to the highest-priority store. False applies stores in
// order and stops at the first error; already completed writes are not rolled
// back.
func WithWriteToFirst(v bool) MultiStoreOption {
	return func(s *MultiStore) { s.writeToFirst = v }
}

// NewMultiStore creates a priority chain. The stores slice is retained and
// must not be mutated while in use. Nil entries are unsupported. An empty
// chain reports not found on reads and treats writes and deletes as no-ops.
func NewMultiStore(stores []confii.SecretStore, opts ...MultiStoreOption) *MultiStore {
	s := &MultiStore{
		stores:       stores,
		writeToFirst: true,
		logger:       slog.Default(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// MultiStoreError aggregates per-store errors from a [MultiStore]
// operation. It implements `Unwrap() []error` so [errors.Is] and
// [errors.As] traverse every wrapped error — for example, after a
// [MultiStore.GetSecret] call where the only failure mode across every
// store was a missing key, `errors.Is(err, confii.ErrSecretNotFound)`
// still reports true. Each entry is rendered on its own line in
// [MultiStoreError.Error()] with the store index (and store name when the
// underlying type implements `Name() string`) so operators can identify
// which backend produced which failure.
type MultiStoreError struct {
	// Op is the operation that produced the failures (e.g. "GetSecret",
	// "ListSecrets"). Included verbatim in the rendered string.
	Op string
	// Key is the secret key the operation was scoped to, or the prefix
	// for ListSecrets. May be empty.
	Key string
	// Errs is the slice of per-store errors, one per failing backend, in
	// the order the backends were attempted. The Index field of each
	// entry corresponds to the position in the [MultiStore]'s store list.
	Errs []MultiStoreErrorEntry
}

// MultiStoreErrorEntry is a single per-store failure within a
// [MultiStoreError].
type MultiStoreErrorEntry struct {
	// Index is the zero-based position of the failing store in the
	// MultiStore's underlying slice.
	Index int
	// Name is a best-effort identifier for the store. It is sourced from
	// a `Name() string` method on the store implementation when present;
	// otherwise it is empty.
	Name string
	// Err is the error returned by the store.
	Err error
}

// Error renders one failure per line, prefixed with operation, key, and
// store identity. The message is bounded by the number of stores so it
// remains operator-friendly even when every backend fails.
func (e *MultiStoreError) Error() string {
	if e == nil || len(e.Errs) == 0 {
		return "multi-store error: (no entries)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "multi-store %s", e.Op)
	if e.Key != "" {
		fmt.Fprintf(&b, " %q", e.Key)
	}
	fmt.Fprintf(&b, " failed across %d store(s):", len(e.Errs))
	for _, entry := range e.Errs {
		b.WriteString("\n  - ")
		if entry.Name != "" {
			fmt.Fprintf(&b, "store[%d] (%s): ", entry.Index, entry.Name)
		} else {
			fmt.Fprintf(&b, "store[%d]: ", entry.Index)
		}
		if entry.Err != nil {
			b.WriteString(entry.Err.Error())
		} else {
			b.WriteString("<nil error>")
		}
	}
	return b.String()
}

// Unwrap exposes per-store errors for [errors.Is] / [errors.As]
// traversal. The aggregate matches [confii.ErrSecretNotFound] only when every
// store reported missing; otherwise
// the unwrap surface elides the not-found entries whenever at least one
// real (non not-found) error is present. In other words:
//
//   - If every entry is a not-found error, Unwrap returns all of them so
//     `errors.Is(err, confii.ErrSecretNotFound)` keeps reporting true,
//     preserving the typed not-found contract.
//
//   - If at least one entry is a real backend error (network/auth/etc),
//     Unwrap returns only the real errors. The not-found entries are
//     still rendered by [MultiStoreError.Error()] for operator visibility
//     but they no longer let `errors.Is(err, confii.ErrSecretNotFound)`
//     mask a partial outage.
//
// In both cases backend-specific sentinels remain matchable via
// `errors.Is(err, backendSentinel)` because the real-error entries are
// unwrapped.
func (e *MultiStoreError) Unwrap() []error {
	if e == nil {
		return nil
	}
	hasReal := false
	for _, entry := range e.Errs {
		if entry.Err != nil && !errors.Is(entry.Err, confii.ErrSecretNotFound) {
			hasReal = true
			break
		}
	}
	out := make([]error, 0, len(e.Errs))
	for _, entry := range e.Errs {
		if entry.Err == nil {
			continue
		}
		if hasReal && errors.Is(entry.Err, confii.ErrSecretNotFound) {
			continue
		}
		out = append(out, entry.Err)
	}
	return out
}

// storeNamer is the optional interface used to label store entries in
// rendered MultiStoreError messages. Stores that wish to surface a
// human-readable identifier (e.g. "aws-secretsmanager", "vault-prod") may
// implement it; the MultiStore does not require it.
type storeNamer interface {
	Name() string
}

func storeName(store confii.SecretStore) string {
	if n, ok := store.(storeNamer); ok {
		return n.Name()
	}
	return ""
}

// GetSecret tries stores in priority order and returns the first non-nil value.
// A nil value with no error is treated as not found. Search continues only for
// not-found results:
//
//   - If every error wraps [confii.ErrSecretNotFound] (or every store
//     returned `(nil, nil)`) the call returns a single typed not-found error.
//
//   - The first cancellation or non-not-found error stops traversal and returns
//     a *[MultiStoreError] containing that failure and earlier misses. Its error
//     chain intentionally excludes those earlier not-found errors so
//     errors.Is(err, confii.ErrSecretNotFound) cannot mask an outage.
func (s *MultiStore) GetSecret(ctx context.Context, key string, opts ...confii.SecretOption) (any, error) {
	var entries []MultiStoreErrorEntry
	for i, store := range s.stores {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		val, err := store.GetSecret(ctx, key, opts...)
		if err == nil && val != nil {
			return val, nil
		}
		if err == nil {
			err = fmt.Errorf("%w: %s", confii.ErrSecretNotFound, key)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		entry := MultiStoreErrorEntry{
			Index: i,
			Name:  storeName(store),
			Err:   err,
		}
		entries = append(entries, entry)
		if !errors.Is(err, confii.ErrSecretNotFound) {
			s.logger.Warn("secret store error",
				slog.String("key", key),
				slog.Int("store_index", i),
				slog.String("store_name", entry.Name),
				slog.String("error", err.Error()),
			)
			return nil, &MultiStoreError{Op: "GetSecret", Key: key, Errs: entries}
		}
	}
	return nil, fmt.Errorf("%w: %s (tried %d stores)", confii.ErrSecretNotFound, key, len(s.stores))
}

// SetSecret writes to the first store by default. WithWriteToFirst(false)
// writes to every store in order and stops at the first error; the operation is
// not transactional across stores.
func (s *MultiStore) SetSecret(ctx context.Context, key string, value any, opts ...confii.SecretOption) error {
	if s.writeToFirst && len(s.stores) > 0 {
		return s.stores[0].SetSecret(ctx, key, value, opts...)
	}
	for _, store := range s.stores {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := store.SetSecret(ctx, key, value, opts...); err != nil {
			return err
		}
	}
	return nil
}

// DeleteSecret deletes from the first store by default. WithWriteToFirst(false)
// deletes from every store in order and stops at the first error; the operation
// is not transactional across stores.
func (s *MultiStore) DeleteSecret(ctx context.Context, key string, opts ...confii.SecretOption) error {
	if s.writeToFirst && len(s.stores) > 0 {
		return s.stores[0].DeleteSecret(ctx, key, opts...)
	}
	for _, store := range s.stores {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := store.DeleteSecret(ctx, key, opts...); err != nil {
			return err
		}
	}
	return nil
}

// ListSecrets aggregates secret keys from all stores, deduplicating the
// results. When one or more stores return an error, the returned slice
// still contains the union of keys from the stores that did succeed and
// the returned error is a *[MultiStoreError] wrapping each backing
// failure. Callers MUST check both the slice and the error: a non-nil
// error does not imply an empty inventory, and an empty inventory with a
// nil error means no store had any matching keys.
func (s *MultiStore) ListSecrets(ctx context.Context, prefix string) ([]string, error) {
	seen := make(map[string]struct{})
	var result []string
	var entries []MultiStoreErrorEntry
	for i, store := range s.stores {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		keys, err := store.ListSecrets(ctx, prefix)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				if ctx.Err() != nil {
					return result, ctx.Err()
				}
				return result, err
			}
			entry := MultiStoreErrorEntry{
				Index: i,
				Name:  storeName(store),
				Err:   err,
			}
			entries = append(entries, entry)
			s.logger.Warn("secret store list error",
				slog.String("prefix", prefix),
				slog.Int("store_index", i),
				slog.String("store_name", entry.Name),
				slog.String("error", err.Error()),
			)
			continue
		}
		for _, k := range keys {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				result = append(result, k)
			}
		}
	}
	if len(entries) > 0 {
		return result, &MultiStoreError{
			Op:   "ListSecrets",
			Key:  prefix,
			Errs: entries,
		}
	}
	return result, nil
}
