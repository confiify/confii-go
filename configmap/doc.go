// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

// Package configmap provides key-path operations for Confii-compatible
// map[string]any values.
//
// A key path contains one or more non-empty segments separated by dots. Map
// keys therefore must not contain literal dots when they are intended to be
// addressed through this package. The package is dependency-free so custom
// loaders and independently versioned Confii modules can share the same path
// semantics without importing the root confii package.
//
// The functions do not synchronize access to maps. Callers must not read and
// mutate the same map concurrently without external synchronization.
package configmap
