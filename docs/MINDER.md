# Minder continuous policy monitoring

Confii provides an audit-only Minder profile at
`.minder/confii-profile.yaml`. It monitors the repository controls already
required by project policy: secret scanning and push protection, protected
pull-request review, signed commits, required checks, least-privilege and
commit-pinned workflows, CodeQL, Dependabot, Security Insights, the security
policy, and the absence of checked-in executables.

The profile deliberately sets both `alert` and `remediate` to `off`. Applying
it creates continuous dashboard evidence without letting a third-party service
change repository settings or open security advisories. Maintainers may enable
alerts after the first clean evaluation. Automatic remediation requires a
separate security review and is not part of this integration.

## Hosted service activation

Confii targets the [Custcodian-hosted Minder service](https://custcodian.dev/hosted/),
which is free for public open-source repositories and is the current default
server for the Minder CLI. A repository file cannot grant an external service
GitHub access, so an authorized maintainer must perform these steps once after
this change reaches `main`:

1. Install Minder (`brew install minder` on macOS) and authenticate:

   ```bash
   minder auth login --grpc-host api.custcodian.dev
   ```

2. Run `minder quickstart`, authorize the GitHub App only for
   `confiify/confii-go`, and register that repository. If the provider is
   already enrolled, use:

   ```bash
   minder repo register --name confiify/confii-go
   ```

3. Clone the official rule catalog at the reviewed commit and create the rule
   types referenced by the profile:

   ```bash
   git clone https://github.com/mindersec/minder-rules-and-profiles.git
   cd minder-rules-and-profiles
   git checkout 76ddcca7f6d662e9d2b59f846c5d96f193b717de
   ```

   For each `type` in `.minder/confii-profile.yaml`, create the corresponding
   `rule-types/github/<type>.yaml` if it is not already present in the Minder
   project. `minder ruletype list` shows installed types; create a missing one
   with `minder ruletype create -f <file>`.

4. Return to this repository and apply the reviewed profile:

   ```bash
   minder profile apply -f .minder/confii-profile.yaml
   minder profile status list --name confii-repository-security --detailed
   ```

Treat Minder as monitoring evidence, not as the source of truth. GitHub
rulesets and checked-in workflows remain authoritative. Reapply the profile
after changing it and review the Minder GitHub App permissions quarterly under
the project credential policy.
