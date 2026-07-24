# Project Roadmap

Confii is developed in public through focused issues and reviewed pull
requests. This roadmap records direction, not a promise of dates or scope.
Security and compatibility fixes take priority over planned feature work.

## Current priorities

- Keep the core module small while maintaining separately selectable cloud
  integrations.
- Maintain the configuration-source, environment, secret-resolution,
  introspection, and lifecycle contracts documented by the project.
- Preserve release integrity through signed tags, verified commits, immutable
  releases, dependency review, provenance, and reproducible-build checks.
- Complete and maintain the OpenSSF Best Practices evidence set without
  overstating practices that depend on contributor or operating history.

## Next

- Expand realistic end-to-end examples for cloud loaders and secret stores.
- Improve diagnostics around configuration plans, source precedence, and
  provider selection.
- Continue compatibility testing across supported Go releases and operating
  systems.
- Turn confirmed defects and security findings into permanent regression
  tests.

## Later and exploratory

- Evaluate additional opt-in providers when they solve demonstrated user
  needs without increasing the core module's dependency footprint.
- Improve machine-readable supply-chain and security evidence as OpenSSF and
  Go ecosystem standards evolve.

## Proposing roadmap changes

Open a focused feature request describing the user problem, alternatives,
compatibility impact, security implications, and maintenance cost. A roadmap
item is not accepted until a maintainer-approved pull request or issue records
that decision. The roadmap is reviewed for currency during each release.
