## Purpose

Defines a reproducible npm-first release contract for the Go-native JumpOTP CLI, including the one-time package bootstrap required before OIDC Trusted Publishing and checksum-verifiable GitHub Release fallback assets.

## ADDED Requirements

### Requirement: Supported runtime platforms
JumpOTP v0.1 SHALL publish native binaries for `darwin-arm64`, `darwin-x64`, `linux-arm64`, and `linux-x64`. The npm launcher SHALL require Node.js `22.14.0` or newer. Unsupported operating-system, architecture, or Node combinations SHALL fail with an actionable message rather than executing a mismatched binary.

#### Scenario: Supported npm installation
- **WHEN** a user globally installs JumpOTP with a supported Node version on one of the four native platforms
- **THEN** npm installs the matching native package and the `jumpotp` command executes that binary

#### Scenario: Unsupported Windows platform
- **WHEN** the JavaScript launcher runs on Windows in v0.1
- **THEN** it reports that Windows is unsupported and points to supported-platform documentation

### Requirement: npm is the primary distribution surface
The primary installation command SHALL be `npm install -g jumpotp`. The unscoped `jumpotp` package SHALL contain a dependency-free JavaScript launcher and exact-version optional dependencies on `jumpotp-darwin-arm64`, `jumpotp-darwin-x64`, `jumpotp-linux-arm64`, and `jumpotp-linux-x64`. The launcher SHALL map `process.platform` and `process.arch` to exactly one package and forward inherited stdio, arguments, signals, and exit status to the native binary.

#### Scenario: Optional dependencies omitted
- **WHEN** installation used `--omit=optional` or the matching platform package is otherwise absent
- **THEN** the launcher exits clearly with npm repair instructions and the GitHub Release fallback rather than downloading or compiling code

### Requirement: No install-time downloader
No JumpOTP npm package SHALL use `preinstall`, `install`, or `postinstall` scripts to download, compile, execute, chmod, or modify the native binary. Package selection SHALL rely on npm `os`, `cpu`, `bin`, and optional-dependency metadata, and packed platform binaries SHALL already have executable mode.

#### Scenario: Package audit
- **WHEN** a maintainer inspects all five packed manifests and tarballs
- **THEN** no install lifecycle script, unexpected executable, external download URL, or missing executable bit is present

### Requirement: One-time functional prerelease bootstrap
Because npm Trusted Publishing can be configured only after a package exists, the first release SHALL use a separately authorized interactive bootstrap. Immediately before bootstrap, all five package names SHALL be checked for availability. From a privacy-scrubbed public repository and verified release candidate, an owner using npm account 2FA SHALL publish the four functional platform packages at `0.1.0-rc.0` with dist-tag `next`, verify them, and then publish the functional root package at the same prerelease version and tag. JumpOTP MUST NOT publish an empty reservation package or store an npm automation token.

#### Scenario: Name becomes unavailable
- **WHEN** any required package name is no longer available before the bootstrap
- **THEN** publication stops before creating any of the five packages and the product naming decision returns to review

#### Scenario: Platform bootstrap fails
- **WHEN** any `0.1.0-rc.0` platform package fails to publish or verify
- **THEN** the root prerelease package is not published and the partial registry state is reported

### Requirement: Trusted Publisher registration
After all five functional prerelease packages exist, an owner SHALL configure one GitHub Actions Trusted Publisher for each package, with exact public repository and workflow identity and permission to publish. The standard release path SHALL use GitHub-hosted runners, `id-token: write`, Node.js and npm versions meeting npm's current Trusted Publishing minimums, and no write-capable npm token. Repository URLs in all package manifests SHALL exactly match the public repository.

#### Scenario: Missing package trust
- **WHEN** any of the five packages lacks the exact expected Trusted Publisher configuration
- **THEN** the formal release workflow fails before publishing any stable package

#### Scenario: Self-hosted release runner
- **WHEN** the formal publish job runs on a self-hosted runner in v0.1
- **THEN** the workflow refuses publication because the trusted release contract requires a supported GitHub-hosted runner

### Requirement: Atomic stable multi-package release order
All five npm packages SHALL share one semantic version. A stable release workflow SHALL build and verify all native artifacts, publish the four platform packages first through OIDC, verify their registry versions and provenance, and publish the root launcher last. If any platform publish or verification fails, the root launcher for that stable version MUST NOT be published.

#### Scenario: Partial platform publish failure
- **WHEN** one stable platform package fails to publish or verify
- **THEN** the workflow stops before publishing the root package and reports the immutable partial registry state

#### Scenario: Version tag mismatch
- **WHEN** the Git tag does not equal `v` plus the Go version and every package manifest version
- **THEN** the workflow fails before registry mutation

### Requirement: Public provenance
Stable npm publication SHALL use OIDC Trusted Publishing with automatically generated public-package provenance. The repository SHALL be public and the release commit, workflow file, and package `repository.url` SHALL be reachable before publication. Making a repository public remains a separately authorized external action.

#### Scenario: Repository remains private
- **WHEN** the candidate repository is private
- **THEN** the stable publication workflow refuses release rather than publishing without the required provenance

### Requirement: GitHub Release fallback
Every stable version SHALL also publish the four native binaries or archives and a SHA256 checksum manifest in a GitHub Release whose tag matches the npm version. Documentation SHALL distinguish npm as the primary install path and GitHub Release assets as the manual fallback.

#### Scenario: Verify fallback asset
- **WHEN** a user downloads a GitHub Release binary
- **THEN** the documented procedure verifies it against the published SHA256 manifest before installation

### Requirement: Version and consumer lifecycle consistency
The Go binary, JavaScript launcher, all platform package manifests, npm `latest` tag, Git tag, and GitHub Release SHALL report the same stable version. Documentation SHALL include first install, update, uninstall, npm optional-dependency repair, prerelease removal, and manual GitHub fallback instructions.

#### Scenario: Version query after npm install
- **WHEN** the user runs `jumpotp version --json` after stable installation
- **THEN** the reported version matches the installed root and platform package versions

### Requirement: Whole-repository privacy and integrity gate
Before the repository is made public, before bootstrap, and before every stable release, validation SHALL inspect the complete tracked tree including OpenSpec artifacts, source, tests, examples, configuration, documentation, workflows, generated npm tarballs, release archives, checksums, and the candidate Git history. It SHALL block internal hostnames, non-synthetic IP addresses, private SSH selectors, real Bitwarden item names or IDs, credentials, account mappings, realistic OTP values, private deployment names, and private operational data.

#### Scenario: Private reference detected in planning artifacts
- **WHEN** the scrub finds a private deployment reference in an OpenSpec artifact
- **THEN** publication stops until the artifact and candidate history are cleaned and every generated artifact is rebuilt

#### Scenario: Synthetic fixture
- **WHEN** a test uses an explicitly allowlisted documentation-range address, reserved example hostname, and marked synthetic OTP
- **THEN** the scrub accepts it while continuing to reject unmarked realistic values

### Requirement: License and update behavior
JumpOTP SHALL be licensed under Apache-2.0, SHALL collect no telemetry, and SHALL perform no automatic update checks in v0.1.

#### Scenario: Offline invocation
- **WHEN** a user runs JumpOTP after installation without network access other than the requested SSH and existing Bitwarden behavior
- **THEN** JumpOTP does not contact an analytics or update service
