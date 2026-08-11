# Changelog

All notable changes follow Semantic Versioning.

## [Unreleased]

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
