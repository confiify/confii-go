# Release process

Confii is a multi-module repository. A release is complete only when the root,
cloud-loader, and cloud-secret tags all identify the same reviewed commit.

## Prepare

1. Work from a clean release branch based on `origin/main`.
2. Update `CHANGELOG.md` and the internal module requirements to the intended
   version. The root module uses `vX.Y.Z`; nested module tags use their module
   directory prefix.
3. Run the release gates:

   ```bash
   make ci-full
   make lint
   make vulncheck
   mkdocs build --strict
   ```

4. Open a pull request and require the CI, CodeQL, dependency-review,
   govulncheck, docs, and Scorecard policies configured for the repository.
5. Merge without bypassing branch protection, then verify the merge commit on
   `main` has passed all required checks.

## Tag and publish

Create signed, annotated tags on the same verified commit. For `v1.1.0`:

```bash
git tag -s loader/cloud/v1.1.0 -m "loader/cloud v1.1.0"
git tag -s secret/cloud/v1.1.0 -m "secret/cloud v1.1.0"
git tag -s v1.1.0 -m "confii-go v1.1.0"
git push --atomic origin loader/cloud/v1.1.0 secret/cloud/v1.1.0 v1.1.0
```

The root tag starts the release workflow. It re-runs module, race, and cloud
tests; checks all three tags and internal versions; cross-compiles the CLI;
publishes checksums; and records GitHub artifact attestations.

After the workflow succeeds, verify one archive and verify all modules through
a clean consumer:

```bash
gh attestation verify confii-v1.1.0-linux-amd64.tar.gz --repo confiify/confii-go
go list -m github.com/confiify/confii-go@v1.1.0
go list -m github.com/confiify/confii-go/loader/cloud@v1.1.0
go list -m github.com/confiify/confii-go/secret/cloud@v1.1.0
```

Tags and published releases are immutable. If a release is defective, publish
a new patch version; never move or replace a published tag.
