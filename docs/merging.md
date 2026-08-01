# Merge Strategies

Later sources have higher precedence. Confii v2 uses one merge model with a
default strategy and optional dotted-path overrides.

![Confii composition and merge flow](assets/composition-merge.svg)

## Self-config

```yaml
merge:
  default: deep_merge
  paths:
    database.replicas: append
    features: union
```

The canonical strategy names are:

| Name | Behavior |
| --- | --- |
| `replace` | Discard the governed base value and use the overlay. |
| `shallow_merge` | Preserve top-level base keys; shared keys are replaced without recursive map merging. |
| `deep_merge` | Recursively combine maps; overlay leaves win. This is the default. |
| `append` | Append overlay list elements after base elements. |
| `prepend` | Put overlay list elements before base elements. |
| `intersection` | Keep common equal values and recursively intersect maps. |
| `union` | Keep values from both sides and recursively merge common maps. |

Unknown names fail startup. The self-configuration schema accepts only the
canonical names listed above.

## Go options

```go
cfg, err := confii.New[AppConfig](
    confii.WithLoaders(base, overlay),
    confii.WithMergeStrategy(confii.StrategyMerge),
    confii.WithMergeStrategyMap(map[string]confii.MergeStrategy{
        "database.replicas": confii.StrategyAppend,
        "features":          confii.StrategyUnion,
    }),
)
```

Use `StrategyShallowMerge` when a later source should replace a nested map as
one top-level value. `WithDeepMerge`, `EnableDeepMerge`, and
`DisableDeepMerge` were removed in v2 because they created a parallel merge
control surface.

The builder provides `WithMergeStrategy`. Per-path options can be supplied via
the constructor when the builder is not otherwise needed.

## Type mismatches

`deep_merge`, `append`, `prepend`, and `union` fall back to the overlay when
their operands do not have the required compatible shape. `intersection`
omits incompatible values. Source data is copied into the candidate snapshot;
merging does not mutate caller-owned maps or slices.
