## Why

JumpOTP safely reports why Bitwarden readiness or TOTP retrieval failed, but it does not report how long the bounded provider operation ran. When a PATH-resolved wrapper performs session checks or recovery, users cannot distinguish a fast state failure from exhaustion of the provider budget without unsafe external tracing.

## What Changes

- Measure only failed Bitwarden readiness and TOTP retrieval operations and append a rounded whole-second elapsed duration to their existing redacted diagnostics.
- Preserve provider-measured elapsed time across the workspace broker as a separately bounded numeric field rather than forwarding command output or arbitrary provider messages.
- Keep successful operations silent and retain the existing CLI, configuration, timeout, fallback, broker, PTY, and one-submission behavior.
- Add deterministic timing, rendering, broker-transport, compatibility, and privacy regression coverage without accessing a live vault item or OTP.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `otp-assisted-ssh`: Add failure-only, stage-specific elapsed timing to redacted Bitwarden diagnostics in direct and workspace flows.

## Impact

- Affects the internal provider error representation, Bitwarden provider timing, safe-message rendering, the optional fields of the internal broker response, focused tests, and user documentation.
- Does not add a public flag, environment variable, YAML field, persistent log, telemetry, provider command, dependency, protocol-version bump, or release-version change.
- Does not expose item references, command arguments, stdout, stderr, vault metadata, session material, passwords, OTPs, or wrapper sub-stage behavior.
