# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| latest  | Yes                |

## Reporting a Vulnerability

We take security seriously. If you discover a vulnerability in Confii, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please report vulnerabilities by emailing:

**confii.connect@gmail.com**

Include the following in your report:

- Description of the vulnerability
- Steps to reproduce
- Affected versions
- Potential impact
- Suggested fix (if any)

## Response Timeline

- **Acknowledgment:** Within 48 hours of receiving your report
- **Initial assessment:** Within 5 business days
- **Fix timeline:** We aim to release a patch within 30 days for confirmed vulnerabilities
- **Disclosure:** We will coordinate public disclosure with you after a fix is available

## Scope

The following are in scope:

- The `confii-go` library code (`github.com/confiify/confii-go`)
- Secret store integrations (credential handling, caching, resolution)
- Configuration parsing (injection via crafted config files)
- The CLI tool (`confii`)

The following are out of scope:

- Vulnerabilities in third-party dependencies (report these upstream)
- Issues that require physical access to the machine
- Denial of service via extremely large config files (expected behavior)

## Recognition

We appreciate security researchers who help keep Confii safe. With your permission, we will acknowledge your contribution in the release notes for the fix.

## Security Best Practices for Users

- Never commit secrets or credentials in configuration files
- Use `${secret:key}` placeholders with a proper secret store in production
- Enable `WithFreezeOnLoad(true)` in production to prevent runtime config mutation
- Use build tags to include only the cloud providers you need
- Keep your Go toolchain and Confii version up to date
