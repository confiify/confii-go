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
   make supply-chain-check
   mkdocs build --strict
   ```

4. Open a pull request and require the CI, CodeQL, dependency-review,
   govulncheck, docs, and Scorecard policies configured for the repository.
5. Merge without bypassing branch protection, then verify the merge commit on
   `main` has passed all required checks.

## Tag and publish

Create signed, annotated tags on the same verified commit. For `v1.4.0`:

```bash
git tag -s loader/cloud/v1.4.0 -m "loader/cloud v1.4.0"
git tag -s secret/cloud/v1.4.0 -m "secret/cloud v1.4.0"
git tag -s v1.4.0 -m "confii-go v1.4.0"
git push --atomic origin loader/cloud/v1.4.0 secret/cloud/v1.4.0 v1.4.0
```

The root tag starts the release workflow. It re-runs module, race, and cloud
tests and checks all three tags and internal versions. A version-pinned
GoReleaser toolchain then cross-compiles the CLI, creates an SPDX JSON SBOM for
each archive, includes the SBOMs and applicable OpenVEX documents in the
release checksum manifest, and stages the GitHub release as a draft. The
release gate imports and re-exports every SBOM with commit-pinned
[bomctl](https://github.com/bomctl/bomctl), proving that the documents are
semantically consumable through the protobom model rather than merely valid
JSON. The workflow rejects incomplete release metadata before publication, generates
SLSA provenance for every checksummed archive, attaches the signed Sigstore bundle as
`confii-<tag>.intoto.jsonl`, and only then publishes the release.

After the workflow succeeds, verify one archive and verify all modules through
a clean consumer:

```bash
gh release download v1.4.0 \
  --pattern 'confii-v1.4.0-linux-amd64.tar.gz' \
  --pattern 'confii-v1.4.0-linux-amd64.tar.gz.sbom.json' \
  --pattern 'checksums.txt' \
  --pattern '*.openvex.json' \
  --pattern 'confii-v1.4.0.intoto.jsonl'
sha256sum --check --ignore-missing checksums.txt
jq -e '.spdxVersion == "SPDX-2.3" and (.packages | length > 0)' \
  confii-v1.4.0-linux-amd64.tar.gz.sbom.json
gh attestation verify confii-v1.4.0-linux-amd64.tar.gz \
  --repo confiify/confii-go \
  --bundle confii-v1.4.0.intoto.jsonl \
  --signer-workflow confiify/confii-go/.github/workflows/release.yaml
go list -m github.com/confiify/confii-go@v1.4.0
go list -m github.com/confiify/confii-go/loader/cloud@v1.4.0
go list -m github.com/confiify/confii-go/secret/cloud@v1.4.0
```

Tags and published releases are immutable. If a release is defective, publish
a new patch version; never move or replace a published tag.

## Continuity drill

The manual `Maintainer continuity drill` workflow verifies release-environment
approval, module checks, race tests, and a GoReleaser snapshot without creating
a tag or publishing a release. The snapshot must contain all expected archives,
per-archive SPDX SBOMs, OpenVEX documents, and checksum entries. The continuity
maintainer runs and approves this workflow as described in
[`CONTINUITY.md`](CONTINUITY.md). It is evidence that the release tooling remains
operable, not a substitute for the full production release procedure above.
