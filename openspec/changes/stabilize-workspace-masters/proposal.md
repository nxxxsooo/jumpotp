# Stabilize Workspace Masters

## Why

Workspace target windows currently hold an interactive remote shell that doubles as the OpenSSH ControlMaster owner. The bastion reclaims idle interactive sessions on a roughly 30-minute cycle, which silently destroys the window, its wrapper, and the ControlMaster together, and nothing rebuilds them until the user manually reruns `workspace`. Separately, a cold workspace start submits one shared TOTP code for all targets near-simultaneously; a submission that lands across the 30-second rotation boundary is rejected, exhausts the single automatic submission, and strands that target on an unattended manual prompt until the bastion disconnects it. Field evidence (2026-08-11): three consecutive rebuild rounds lost windows after 14–37 minutes; a sessionless ControlMaster under the previous bare-`ssh_config` model routinely survived 4+ hours against the same bastion.

## What Changes

- Run every workspace target window as a sessionless master: the PTY wrapper launches the configured launcher with `-N` plus bounded keepalive options, so the connection performs MFA and holds the ControlMaster without opening a remote shell or any session the bastion can idle-reap. Workspace windows stop being interactive terminals; interactive work uses ordinary `ssh <alias>` through the shared master.
- Supervise the launcher child inside the target wrapper: when the child exits for any reason other than an operator stop, the wrapper reconnects with bounded exponential backoff. Reconnection attempts are gated on an existing ControlMaster or a validated active broker; while neither is available the wrapper waits and polls without dialing SSH, so an unattended workspace never generates failed MFA attempts. The next `workspace` invocation restores full service automatically.
- Guard TOTP retrieval against the rotation boundary: the broker delays a group retrieval that would start inside the final guard interval of the current 30-second TOTP window until the next window begins, so delivered codes always have usable runway.
- Bound automatic resubmission: a wrapper may automatically submit a second, strictly newer-window code after an explicit rejection of the first, at most twice per connection attempt, never repeating a code, then falls back to the existing manual/fail behavior.
- Derive and report a `connecting` target state in `status` when a target wrapper is alive but its ControlMaster is not yet confirmed.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `workspace-management`: Target windows become supervised sessionless masters with reconnect gating and a derived `connecting` status state.
- `otp-assisted-ssh`: Broker retrieval gains a rotation-boundary guard; the one-automatic-submission rule becomes a bounded two-submission rule with strict code-freshness requirements.

## Impact

- Affects the launcher argument builder, the `__target` wrapper loop, the terminal proxy submission bookkeeping, the broker flush scheduling, workspace status derivation, focused tests, integration tests, and user documentation.
- Breaking behavior change: workspace target windows no longer present a remote interactive shell. Requires a minor version bump (0.2.0) and README/Chinese-doc migration notes.
- Does not change the YAML configuration schema, the broker protocol version or fields, runtime lease handling, tmux ownership rules, health probing, `stop` semantics, or the security boundaries around OTP handling (no `send-keys`, no code logging, in-memory zeroization preserved).
- Builds on top of the completed-but-unlanded `harden-bitwarden-readiness` and `add-secret-free-provider-timing` work (notably `broker.Active`), which must be committed and archived first.
- Pivot path: if the live sessionless-master validation experiment fails (bastion reaps sessionless connections too), the launcher keeps interactive mode and every other element of this change still applies unchanged; only the `-N` argument decision reverts.
