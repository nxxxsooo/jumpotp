## Why

Operators who already use OpenSSH aliases and Bitwarden TOTP still have to copy short-lived codes into interactive JumpServer or bastion prompts, while ad-hoc automation often leaks OTPs, duplicates SSH configuration, or leaves fragile tmux state behind. JumpOTP provides a narrow, auditable bridge from an existing Bitwarden CLI session to authorized SSH MFA prompts, with an optional resilient multi-target workspace.

## What Changes

- Introduce the `jumpotp` CLI with explicit `connect`, `workspace`, `status`, `stop`, `doctor`, `config`, and `version` command contracts.
- Add a complete strict YAML version-1 schema that references existing OpenSSH aliases, supports profile-level Bitwarden items with target overrides, defines strict MFA matchers, and offers optional `ssh` or `sshm` launchers.
- Add a PTY-based connection wrapper that retrieves TOTP through `bw get totp`, submits at most once after a strict prompt match, and explains failures before returning to visible manual entry.
- Add persistent, isolated tmux workspaces whose target panes run the same PTY wrapper and obtain grouped OTPs from an ephemeral per-invocation Unix-socket broker without capturing pane contents or storing OTPs in tmux buffers.
- Add an opt-in rotating health-probe catalog that reuses confirmed OpenSSH ControlMaster connections without injecting commands into interactive panes.
- Add bounded recovery of the JumpOTP-owned tmux server using a current-user runtime lease and fail-closed process identity checks.
- Add a Go native core for macOS and Linux, a dependency-free JavaScript npm launcher, platform-specific npm packages, and checksum-verified GitHub Release assets.
- Add release privacy gates that cover the complete Git tree, OpenSpec artifacts, generated packages, and archives before any repository is made public.

## Capabilities

### New Capabilities

- `cli-and-configuration`: Exact commands, arguments, profile and target addressing, strict YAML schema, validation, diagnostics, launcher selection, JSON output, and exit behavior.
- `otp-assisted-ssh`: Bitwarden-backed TOTP retrieval, strict MFA prompt matching, grouped in-memory delivery, visible manual fallback, PTY fidelity, and secret-handling guarantees.
- `workspace-management`: Isolated persistent tmux workspaces, pane wrappers, broker lifecycle, ControlMaster boundaries, optional health probes, runtime leases, status reporting, and broken-server recovery.
- `release-distribution`: Go and JavaScript package architecture, supported platforms, npm-first installation, initial package bootstrap, version consistency, provenance, checksums, and GitHub Release fallback.

### Modified Capabilities

None. This is a new project with no existing OpenSpec capabilities.

## Impact

- A new local Git repository and Go module for the native CLI core; remote repository creation remains a separately authorized operation.
- A new strict YAML configuration at `${XDG_CONFIG_HOME:-~/.config}/jumpotp/config.yaml`.
- Runtime integration with existing `ssh`, optional `sshm`, `bw`, and workspace-only `tmux` executables. JumpOTP does not manage OpenSSH or Bitwarden authentication configuration.
- Current-user runtime metadata and broker sockets containing no OTP or credential values.
- Five public npm packages and matching GitHub Release assets. The packages must first be established through a separately authorized, interactive prerelease bootstrap before Trusted Publishing can publish the formal release.
- Private deployment profiles and compatibility adapters remain outside the public repository.
