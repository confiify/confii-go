# Project credential management

This policy governs credentials controlled by the Confii project, including
GitHub authentication and signing keys, repository and environment secrets,
CI/CD tokens, documentation-hosting credentials, release credentials, and
security-response accounts. It does not require users of the library to adopt a
specific secret manager, but the same principles are recommended for production
deployments.

## Storage and transmission

- Credentials must never be committed to this repository, placed in examples,
  copied into issue or pull-request text, printed in logs, or included in build
  and release artifacts.
- CI/CD secrets belong in GitHub Actions repository or environment secrets, or
  in the external provider's encrypted credential store. Personal SSH and
  signing keys remain in the maintainer's protected operating-system key store
  or hardware-backed authenticator and are never shared.
- Workflows prefer the short-lived, repository-scoped `GITHUB_TOKEN` and OIDC
  identity tokens over personal access tokens or static cloud keys. External
  integrations should use GitHub Apps or provider-native workload identity when
  available.
- Credentials may be transmitted only over authenticated encrypted channels.
  Recovery material must be stored separately from the credential it recovers.

## Access and approval

- Access is granted to a named person or workload, never to a shared human
  account, and is limited to the minimum repository, environment, resource, and
  duration required.
- A maintainer other than the recipient reviews any grant of escalated access.
  The public role and responsibility record is maintained in
  [`MAINTAINERS.md`](https://github.com/confiify/confii-go/blob/main/MAINTAINERS.md)
  through a reviewed pull request.
- The `release` environment is restricted to designated release and continuity
  maintainers. Routine workflows receive read-only permissions; jobs that
  publish security results or releases declare only their required write
  permissions.
- Maintainers must use phishing-resistant or authenticator-application 2FA.
  SMS-only authentication is not sufficient for privileged project accounts.
- Credentials must not be repurposed across people, services, or environments.
  Project access is removed promptly when a role ends or no longer requires it.

## Inventory, review, and rotation

The lead and continuity maintainers maintain a private inventory containing the
credential owner, system, purpose, privileges, creation date, expiry or review
date, and recovery owner. The inventory records metadata only, never secret
values.

- The inventory and effective access are reviewed at least quarterly and before
  every release. Unused credentials are revoked rather than retained.
- Expiring credentials are rotated before expiry. Non-expiring machine tokens
  and static CI/CD secrets are rotated at least every 90 days.
- Maintainer SSH authentication and signing keys are reviewed quarterly and
  rotated at least annually, or sooner when the hosting provider or key policy
  requires it.
- Provider-managed GitHub App, OIDC, and ephemeral workflow credentials do not
  require manual periodic rotation; their installation permissions and trust
  policies are still reviewed quarterly.
- Rotation must update the protected store, confirm the replacement works,
  revoke the predecessor, and record the date and responsible maintainer without
  recording either credential value.

## Exposure and emergency response

A credential is treated as compromised if it appears in source, logs, artifacts,
chat, issue content, an untrusted device, or an unexpected authentication event.
The responding maintainer must:

1. revoke or disable it immediately and stop affected automation if necessary;
2. rotate related credentials that could have been derived or reused;
3. review provider audit logs and repository activity;
4. remove public exposure without rewriting protected history unless GitHub
   Security advises that history removal is necessary;
5. report any product impact through the private process in
   [the security policy](https://github.com/confiify/confii-go/security/policy); and
6. document the incident, containment, replacement, and follow-up controls in a
   private security record.

GitHub secret scanning, push protection, and the required Gitleaks job are
preventive controls, not substitutes for revocation and incident review.

## Continuity

Continuity uses separately controlled maintainer accounts and scoped repository
authority, not copies of another maintainer's private credentials. The tested
process is documented in [`CONTINUITY.md`](CONTINUITY.md). Emergency access does
not permit disabling required checks, publishing unsigned releases, or sharing
personal key material.
