# Security Policy

Report vulnerabilities privately through GitHub's security advisory feature for
this repository. Do not include connector tokens, panel credentials, device
identifiers, or public tunnel URLs in an issue.

## Release controls

- Update GitHub Actions and Go dependencies only through reviewed pull
  requests; Dependabot opens routine update proposals.
- Review `BINARY_SOURCES.md`, pinned digests, licenses, and release notes for
  every upstream payload update.
- Build artifacts include an SPDX document and Go module inventory. The normal
  CI ZIP is validation-only because upstream Android runtime binaries are not
  committed to this repository.
- Use `tools/prepare-upstream-binaries.ps1` with a reviewed CA Extract digest
  before creating a flashable release package.
