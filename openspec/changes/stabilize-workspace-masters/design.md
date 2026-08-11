# Design: Stabilize Workspace Masters

## Context And Evidence

- The bastion (JumpServer koko) reclaims idle interactive sessions on a roughly 30-minute cycle. Observed 2026-08-11 against production: target windows created 21:02 died at ~21:29 (ecs-02, caught live) and earlier rounds died within 14–37 minutes; the machine did not sleep during any observed loss.
- A ControlMaster with no attached session survived 4+ hours against the same bastion under the previous bare `ControlPersist 4h` model. This is the load-bearing precedent for the sessionless (-N) design: keyboard-interactive MFA completes at the authentication layer before any session channel exists, and a session-free connection gives the bastion's interactive-idle reaper nothing to reclaim.
- Concurrent same-code TOTP submission is accepted by the bastion (two of three targets routinely succeed on one batched code). The recurring single-target failure matches a rotation-boundary rejection: a code fetched late in its 30-second window is stale by submission time. Documented once in production on 2026-08-06 and consistent with every observed "one loser per rebuild round".
- `broker.Active(profile)` (validated lease + bounded socket dial) already exists in the completed `harden-bitwarden-readiness` work and is the reconnect gate primitive.

## Decisions

### D1: Sessionless master windows

The workspace target wrapper launches `<launcher> -N -o ServerAliveInterval=60 -o ServerAliveCountMax=3 <alias>` instead of `<launcher> <alias>`. `ControlMaster auto`/`ControlPath` remain the user's ssh_config responsibility exactly as today. Effects:

- No remote shell, no PTY session on the asset, nothing for the interactive-idle reaper to kill.
- The keepalives bound dead-TCP detection to ~3 minutes so the supervisor notices real network loss.
- If an external ControlMaster already owns the path (e.g. a leftover `ControlPersist` master), the `-N` client attaches as a session-free mux client; when that master exits, the client exits and the supervisor's next attempt becomes the new master. No special-case code.
- The `connect` command and health probes are unchanged; probes already run over `-O check` + batch exec.

Rejected alternative — keep interactive shells and reconnect through the ~30-minute reaping: burns roughly two TOTP codes per target per hour, multiplies boundary races, and turns MFA volume into a bastion-audit anomaly.

### D2: Supervised reconnection in the target wrapper

`__target` wraps the existing single-shot connect flow in a supervision loop:

- Child exits → print a redacted status line (exit code, timestamp, next delay) → backoff 5s doubling to a 300s cap with ±20% jitter → gate check → reconnect. Submission bookkeeping (automatic-submission counter, matcher state) resets per attempt.
- Gate: attempt only when `ssh -O check <alias>` confirms a reusable master (connect immediately; no MFA needed) or `broker.Active(profile)` reports a validated live broker. Otherwise poll the gate every 10 seconds without dialing SSH — an unattended workspace generates zero failed MFA attempts and zero bastion connection noise.
- Operator stop wins: SIGTERM/SIGHUP/interrupt (tmux `kill-session` via `stop`, window close, Ctrl-C) ends the wrapper without reconnecting. Only child-initiated exits reconnect.
- Manual fallback remains available on an attended prompt exactly as today; fallback `fail` still exits the attempt with code 4, and the supervisor then waits gated as above.

Rejected alternatives — manager-side respawn (loses in-window continuity and fights `reconcileSession`'s fail-closed pane rules); broker daemonization (new lifecycle surface, contradicts the ephemeral-broker requirement); direct Bitwarden access from wrappers (expands the credential exposure surface into the tmux environment; explicitly rejected by the product decision).

### D3: TOTP rotation-boundary guard and bounded resubmission

- Broker: before a group retrieval, compute the position inside the current 30-second TOTP window from the wall clock (TOTP windows are epoch-aligned). If fewer than 8 seconds remain, wait for the next boundary plus a small skew allowance, then fetch. All grouped waiters receive a code with usable runway. Constants live beside the existing 150ms aggregation window; the 30-second period is asserted as a named constant (the `jumpserver-koko` preset is 6-digit/30s TOTP).
- Wrapper: on a rejected automatic submission (strict matcher fires again), wait for the next epoch-aligned TOTP boundary, request a fresh code, and submit at most one more time. Never resubmit a code from the same window; cap two automatic submissions per connection attempt; then manual/fail fallback as configured. No broker protocol change — freshness is achieved by client-side boundary waiting plus the server-side guard.

Rejected alternative — strict per-target serialization across TOTP windows: adds 30–60 seconds to every cold start while the boundary guard removes the observed failure directly.

### D4: Status derivation

`status` derives per-target display state without new plumbing: wrapper window alive + ControlMaster available → `running`; wrapper window alive + ControlMaster unavailable → `connecting`; window absent → `stopped`; existing `failed` rules unchanged. No pane content is ever read.

## Security And Observability

- Unchanged: no `send-keys`/`capture-pane` for OTP, codes stay in memory and are zeroized, no code or item reference is logged, manual fallback echoes digits only in the target's own terminal.
- Reconnect gating is itself a security control: no unattended SSH dialing means no unattended MFA failures and no lockout risk.
- The wrapper's status lines (connect, exit, backoff, waiting-for-broker) are the observability surface, alongside the derived `connecting` state in `status`.

## Validation Experiment E1 (gates release, not implementation)

Live check that the bastion tolerates sessionless masters: establish `ssh -N` with a dedicated throwaway ControlPath against one production target, leave it unattended for 40+ minutes, then require `ssh -O check` success and a mux exec (`ssh <alias> true`) success. Run by the supervising agent with the operator's authorization; consumes one TOTP. If E1 fails, apply the proposal's pivot: revert D1's `-N` argument decision, keep D2–D4.

## Migration, Compatibility, Rollback

- Config schema, broker protocol, leases, tmux ownership, health probes, and `stop` are untouched. Existing workspaces pick up the new behavior on the next `workspace` run after upgrade.
- Docs must state the breaking change: workspace windows are connection holders, not shells; use `ssh <alias>` for interactive work.
- Rollback = install the previous binary and rerun `workspace`; no persistent state migrates in either direction.
