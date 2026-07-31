# Error Handling

Confii returns structured `*confii.ConfigError` values for configuration
failures. The structure separates a stable machine-readable category from the
concrete operational cause and operator-facing message.

```go
value, err := cfg.Get("database.host")
if err != nil {
    if errors.Is(err, confii.ErrConfigNotFound) {
        // Handle a missing key without parsing the message.
    }

    var configErr *confii.ConfigError
    if errors.As(err, &configErr) {
        slog.Error("configuration operation failed",
            "operation", configErr.Op,
            "code", configErr.Code,
            "source", configErr.Source,
            "key", configErr.Key,
        )
    }
}
```

## Stable categories

`ConfigError.Code` uses `ConfigErrorCode`. Each code corresponds to a sentinel
accepted by `errors.Is`:

| Code | Sentinel | Meaning |
| --- | --- | --- |
| `config_load` | `ErrConfigLoad` | Source loading, initialization, or materialization failed |
| `config_format` | `ErrConfigFormat` | A source format or declared format/path combination is invalid |
| `config_validation` | `ErrConfigValidation` | The candidate failed schema, typed, or custom validation |
| `config_not_found` | `ErrConfigNotFound` | A requested key does not exist |
| `config_merge` | `ErrConfigMerge` | Configuration values cannot be merged under the selected strategy |
| `config_frozen` | `ErrConfigFrozen` | A mutation was attempted after freezing |
| `config_closed` | `ErrConfigClosed` | A mutation was attempted after closing |
| `config_access` | `ErrConfigAccess` | A value cannot be returned as the requested access type |
| `config_invalid` | `ErrConfigInvalid` | An operation argument or mutation path is invalid |

Use `errors.Is` for control flow and `ConfigError.Code` for structured logging,
telemetry, or transport. Do not compare `Error()` strings; they are intended for
operators and may gain additional context.

## Concrete causes and diagnostic context

`ConfigError.Err` is the single wrapped operational cause. Standard
`errors.Is` and `errors.As` traversal can therefore find context cancellation,
deadlines, filesystem failures, SDK errors, and application validator errors.

`ConfigError.Context` contains bounded, structured diagnostic data. For
example, key-not-found errors retain the complete `available_keys` slice even
though the rendered message limits the list. Validation errors similarly keep
structured violations without embedding configuration values in the message.
