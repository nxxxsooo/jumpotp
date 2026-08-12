# Changelog

All notable changes follow Semantic Versioning.

## [0.2.1] - 2026-08-12

- Report `master established` in each workspace target window once a reusable ControlMaster is confirmed, naming the alias to use for interactive work. A sessionless target produces no remote output after authentication, so a healthy connection previously looked identical to a hung one.
- Clear the target window and its scrollback at that same confirmed-establishment moment, removing the authentication exchange — including the code the endpoint echoes back, which JumpOTP never prints but cannot prevent — from a window that may sit unattended for days. Clearing writes only terminal control sequences to JumpOTP's own output stream and still never uses `capture-pane`, `send-keys`, or any tmux buffer mechanism. An attempt that never confirms a master never clears, preserving its failure evidence.

## [0.2.0] - 2026-08-12

- **Breaking:** workspace target windows no longer present an interactive remote shell. Each target window now holds a sessionless ControlMaster (the wrapper launches `<launcher> -N -o ServerAliveInterval=60 -o ServerAliveCountMax=3 -o ControlPersist=no <alias>`) that performs MFA and keeps the connection alive without a remote session for a bastion's interactive-idle reaper to reclaim. Interactive work now goes through an ordinary `ssh <alias>` (or `sshm <alias>`), which reuses the same ControlMaster. No configuration changes are required to migrate; stop typing into target windows and use `ssh <alias>` instead.
- Supervise each workspace target's launcher child with gated, backed-off reconnection: on a non-operator exit the wrapper retries with exponential backoff (5s doubling to a 300s cap, bounded jitter) and only dials again once a reusable ControlMaster or a validated active broker is confirmed, so an unattended workspace never generates a failed MFA attempt.
- Guard broker retrieval against the TOTP rotation boundary: a group retrieval that would start with fewer than 8 seconds left in the current epoch-aligned 30-second window now waits for the next window before calling Bitwarden, so every delivered code keeps usable submission runway.
- Bound automatic OTP resubmission to at most two submissions per connection attempt: a rejected code triggers exactly one fresh, never-repeated retry after the next TOTP window boundary, then falls back to the configured manual/fail behavior.
- Derive and report a `connecting` target state in `status` when a target wrapper is alive but its ControlMaster is not yet confirmed.
- Warm the existing Bitwarden CLI path with a bounded, redacted `bw status` readiness check before new automatic SSH flows.
- Increase the fixed provider deadline from 10 to 20 seconds, with derived 22-second broker client and 25-second connection budgets.
- Skip readiness for manual invocations and validated active-workspace reattachments while preserving existing fallback and broker lifecycle behavior.
- Add failure-only, stage-specific Bitwarden elapsed timing using `<1s` or bounded whole-second buckets without exposing provider output or item references.
- Preserve workspace timing through an optional bounded version-one broker field while retaining compatibility with peers that omit or ignore it.

## [0.1.3] - 2026-08-06

- Prevent terminal input typed during automatic OTP retrieval from corrupting the submitted code.
- Discard stale queued input before visible manual fallback while preserving interrupt controls.
- Handle control-D and buffered manual retries without hanging the target pane.

## [0.1.2] - 2026-07-31

- Initial strict YAML configuration and CLI surface.
- Bitwarden-backed direct PTY TOTP assistance with visible manual fallback.
- Isolated tmux workspaces with ephemeral grouped OTP broker.
- Opt-in ControlMaster-only health probes.
- npm-first native packaging for four platforms.
- Public README, project imagery, and stable release surfaces.
- Registry verification now tolerates bounded npm propagation delay and safely skips versions already published by a partial run.

## [0.1.1] - 2026-07-31

- Published signed `0.1.1` platform packages through OIDC.
- The root package and GitHub Release were withheld after immediate registry verification observed stale data.
