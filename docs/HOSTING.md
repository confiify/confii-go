# Documentation hosting

The documentation is built as a static MkDocs site and published at
<https://confii-go-docs.pages.dev/>. The repository produces a host-neutral
`site/` artifact containing a
[Cloudflare Pages compatible `_headers` policy](https://developers.cloudflare.com/pages/configuration/headers/).

The production URL passed the repository's live response-header test on
2026-07-25. Maintainers can repeat the test after any hosting or policy change.

## Build

From a clean checkout:

```bash
python -m pip install --require-hashes --requirement docs/requirements.txt
make docs-check
```

The command builds `site/` and verifies that `site/_headers` contains the
required policy. The policy restricts default content to the site origin,
disables object embedding and framing, prevents MIME sniffing, restricts
browser capabilities, and enables HSTS.

## Recommended Cloudflare Pages configuration

Connect the `confiify/confii-go` repository to a project controlled by the
same maintainers as the source repository, then configure:

| Setting | Value |
| --- | --- |
| Production branch | `main` |
| Build command | `pip install --require-hashes -r docs/requirements.txt && make docs-check` |
| Build output directory | `site` |
| Root directory | repository root |

Cloudflare Pages reads the generated `_headers` file from the output
directory. Preview deployments should be reviewed before the production URL
or DNS record is changed.

## Production acceptance

After deployment, run:

```bash
make docs-live-headers URL=https://<production-documentation-host>/
```

The current production deployment was accepted with:

```bash
make docs-live-headers URL=https://confii-go-docs.pages.dev/
```

The check verified nonpermissive `Content-Security-Policy`,
`X-Content-Type-Options`, `X-Frame-Options`, `Strict-Transport-Security`,
`Referrer-Policy`, and `Permissions-Policy` response headers.

Also verify the site in a browser, including navigation, search, theme
switching, code-copy buttons, fonts, badges, and external links. After a host
migration, maintainers should:

1. update `site_url` in `mkdocs.yml`;
2. update the repository website URL and badge evidence;
3. redirect or retire the old GitHub Pages URL; and
4. mark `hardened_site` Met with the tested production URL.

The hosting account must also follow the continuity model: at least two
independently controlled maintainers need appropriate administrative or
recovery access, and each must use non-SMS cryptographic 2FA.
