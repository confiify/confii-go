// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/confiify/confii-go/v2/internal/dictutil"
)

// validateAndOwnOptions validates the complete effective construction plan and
// detaches every mutable container supplied by the caller or self-config. It
// runs before source or provider I/O so invalid admission never creates a
// partially initialized Config.
func validateAndOwnOptions(opts *options) error {
	if opts.Logger == nil {
		return invalidConstructionOption("logger must not be nil")
	}
	if !validErrorPolicy(opts.OnError) {
		return invalidConstructionOption(fmt.Sprintf("invalid error policy %q", opts.OnError))
	}
	if opts.StartupTimeout < 0 {
		return invalidConstructionOption("startup timeout must not be negative")
	}
	if opts.OperationTimeout < 0 {
		return invalidConstructionOption("operation timeout must not be negative")
	}
	if opts.ReloadDebounce < 0 {
		return invalidConstructionOption("reload debounce must not be negative")
	}
	if opts.SecretResolutionConcurrency < 1 {
		return invalidConstructionOption("secret resolution concurrency must be at least 1")
	}
	if opts.FreezeOnLoad && opts.DynamicReloading {
		return invalidConstructionOption("WithFreezeOnLoad(true) and WithDynamicReloading(true) are mutually exclusive")
	}
	if !validMergeStrategy(opts.MergeStrategy) {
		return invalidConstructionOption(fmt.Sprintf("invalid default merge strategy %d", opts.MergeStrategy))
	}
	for path, strategy := range opts.MergeStrategyMap {
		if err := validateMergePath(path); err != nil {
			return invalidConstructionOption(err.Error())
		}
		if !validMergeStrategy(strategy) {
			return invalidConstructionOption(fmt.Sprintf("invalid merge strategy %d for path %q", strategy, path))
		}
	}
	for index, loader := range opts.Loaders {
		if isNilExtension(loader) {
			return invalidConstructionOption(fmt.Sprintf("loader %d is nil", index))
		}
	}
	if opts.isSet("secret_hook") && opts.SecretResolver != nil {
		if isNilExtension(opts.SecretResolver) {
			return invalidConstructionOption("secret resolver is nil")
		}
		h, err := managedResolverHook(opts.SecretResolver)
		if err != nil {
			return invalidConstructionOption(err.Error())
		}
		if h == nil {
			return invalidConstructionOption("secret resolver returned a nil hook")
		}
		opts.SecretHook = h
	} else if opts.isSet("secret_hook") && opts.SecretHook == nil {
		return invalidConstructionOption("secret hook is nil")
	}
	for index, setup := range opts.hookSetups {
		if setup.hook == nil {
			return invalidConstructionOption(fmt.Sprintf("hook %d is nil", index))
		}
		if setup.kind == hookSetupCondition && setup.condition == nil {
			return invalidConstructionOption(fmt.Sprintf("condition hook %d has a nil condition", index))
		}
		if setup.kind == hookSetupKey && strings.TrimSpace(setup.key) == "" {
			return invalidConstructionOption(fmt.Sprintf("key hook %d has an empty key", index))
		}
	}

	// Take ownership of mutable input containers. Collaborator implementations
	// remain shared because arbitrary Loader/Exporter/Validator interfaces cannot
	// be cloned; only their containing slices are detached.
	opts.Loaders = append([]Loader(nil), opts.Loaders...)
	opts.MergeStrategyMap = maps.Clone(opts.MergeStrategyMap)
	sensitivePaths, err := normalizeSensitivePathList(opts.SensitivePaths)
	if err != nil {
		return invalidConstructionOption(err.Error())
	}
	opts.SensitivePaths = sensitivePaths
	opts.Exporters = append([]Exporter(nil), opts.Exporters...)
	opts.Validators = append([]Validator(nil), opts.Validators...)
	opts.hookSetups = append([]hookSetup(nil), opts.hookSetups...)
	if schema, ok := opts.Schema.(map[string]any); ok {
		opts.Schema = dictutil.DeepCopy(schema)
	}
	if opts.selfConfigSources != nil {
		sources := make([]map[string]any, len(opts.selfConfigSources))
		for index, source := range opts.selfConfigSources {
			sources[index] = dictutil.DeepCopy(source)
		}
		opts.selfConfigSources = sources
	}
	if opts.selfConfigSecrets != nil {
		opts.selfConfigSecrets = dictutil.DeepCopy(opts.selfConfigSecrets)
	}
	return nil
}

func applyConstructionOption(option Option, opts *options, index int) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = invalidConstructionOption(fmt.Sprintf("option %d panicked: %v", index, recovered))
		}
	}()
	option(opts)
	return nil
}

func managedResolverHook(resolver ManagedSecretResolver) (result func(context.Context, string, any) (any, error), err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("secret resolver Hook panicked: %v", recovered)
		}
	}()
	return resolver.Hook(), nil
}

func invalidConstructionOption(message string) error {
	return NewInvalidError("New", "", errors.New(message))
}

func validErrorPolicy(policy ErrorPolicy) bool {
	switch policy {
	case ErrorPolicyRaise, ErrorPolicyWarn, ErrorPolicyIgnore:
		return true
	default:
		return false
	}
}

func validMergeStrategy(strategy MergeStrategy) bool {
	switch strategy {
	case StrategyReplace, StrategyShallowMerge, StrategyMerge, StrategyAppend,
		StrategyPrepend, StrategyIntersection, StrategyUnion:
		return true
	default:
		return false
	}
}

func validateMergePath(path string) error {
	if path == "" || path != strings.TrimSpace(path) {
		return fmt.Errorf("merge strategy path %q must be non-empty without surrounding whitespace", path)
	}
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			return fmt.Errorf("merge strategy path %q contains an empty segment", path)
		}
	}
	return nil
}
