# Release runbook

No step in this document grants authority to create a remote repository, make
it public, publish packages, create tags, or create Releases.

## One-time bootstrap

1. Complete all tests and the whole-tree privacy scrub.
2. With separate authorization, make the repository public.
3. Recheck all five package names immediately before mutation.
4. Build functional `0.1.0-rc.0` packages.
5. With interactive npm account 2FA, publish four platform packages under
   `next`, verify them, then publish the root prerelease.
6. Configure the exact `release.yml` GitHub Actions Trusted Publisher on
   every existing package.
7. On every package's npm Settings page, set Publishing access to
   `Require two-factor authentication and disallow tokens`. The CLI
   `mfa=publish` mode is not a substitute for this token restriction.
8. From an authenticated maintainer terminal, run `npm run trust:check` and
   complete the single npm proof-of-presence flow if requested. The release
   workflow intentionally carries no account token, so this registry-authority
   check must pass before the stable tag is created.

Empty reservation packages and stored npm automation tokens are prohibited.

## Stable release

The `release.yml` workflow checks tag, manifest, repository, runner, and trust
configuration; builds and verifies all native binaries; publishes platform
packages first through OIDC; publishes the root package last; and creates the
matching GitHub Release with `SHA256SUMS`.

Any partial npm publication is immutable state and must be reported. Do not
silently republish or move a conflicting tag.
