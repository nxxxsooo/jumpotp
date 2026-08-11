## Why

JumpOTP currently gives the entire PATH-resolved Bitwarden command chain ten seconds to return a TOTP. Guarded `bw` wrappers may need to validate or recover a cold session before executing the real request, which can exhaust that budget and force avoidable manual MFA fallback.

## What Changes

- Perform a bounded, non-secret Bitwarden readiness check before automatic direct connections and new workspace broker starts.
- Treat readiness as best effort: warn safely on failure and preserve the configured prompt-time fallback.
- Increase readiness and TOTP retrieval budgets from ten to twenty seconds while retaining derived broker deadlines.
- Skip readiness in manual mode and when reattaching to an already validated live broker.
- Add deterministic latency, orchestration, redaction, and broker-state regression coverage without using a real vault item or OTP.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `otp-assisted-ssh`: Add best-effort pre-connection Bitwarden readiness and a longer bounded provider budget without weakening prompt-gated TOTP retrieval, manual fallback, or secret handling.

## Impact

- Affects the internal Bitwarden provider, direct CLI orchestration, workspace broker startup detection, provider and broker deadlines, tests, and user documentation.
- Does not change the public CLI, YAML schema, exit codes, provider selection, item resolution, or release version.
- Does not log in, unlock Bitwarden directly, manage `BW_SESSION`, publish a release, or interact with live SSH/tmux sessions.
