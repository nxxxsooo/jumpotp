## Context

See `proposal.md` for motivation. The provider currently applies one ten-second context to the entire PATH-resolved `bw get totp` process. A guarded executable may run one or more full Bitwarden status or recovery operations before the requested command. Direct connections instantiate the provider in the CLI handler; workspace owners instantiate it in the ephemeral broker. The public contract requires prompt-gated TOTP retrieval, no Bitwarden authentication management, bounded execution, redacted diagnostics, and persistent workspace reattachment.

The existing completed OpenSpec change remains untouched. The user-owned untracked `openspec/specs/` tree is read as the capability source but is not edited by this change.

## Goals / Non-Goals

**Goals:**

- Warm the existing Bitwarden environment before new automatic connections without requesting a TOTP early.
- Give guarded provider operations enough bounded time to complete their observed cold path.
- Keep readiness failure advisory so manual access remains available.
- Make direct and workspace orchestration testable without a live vault, OTP, SSH endpoint, or tmux server.

**Non-Goals:**

- Adding configuration fields, providers, telemetry, remote-rejection retries, or Bitwarden session management.
- Changing the external `bw` wrapper, release version, package state, or active runtime sessions.

## Decisions

### Add readiness to the provider boundary

`provider.Bitwarden` will implement an internal readiness interface with `Ready(context.Context) error`. It will invoke the existing runner with exactly `bw status`, capture bounded stdout and stderr, parse only the JSON `status` field, and accept only `unlocked`. Captured bytes will be cleared after parsing. Readiness-specific safe messages will reuse provider error kinds without describing a status failure as a TOTP failure.

This keeps Bitwarden command semantics in the provider rather than the terminal proxy. Calling `bw status` through PATH deliberately exercises the same configured executable wrapper that later handles `get totp`; JumpOTP still never calls `login` or `unlock` directly.

### Use one twenty-second provider budget

The existing provider timeout field will remain the test seam for both readiness and code retrieval, while its default increases to twenty seconds. Broker client and server deadlines remain derived constants, becoming twenty-two and twenty-five seconds. This is preferred over a new YAML option because the issue is a safe implementation default, not a per-profile product choice.

### Keep readiness best effort and outside the PTY

The direct CLI handler will run readiness before starting the terminal proxy unless the resolved target is manual. Success is silent. Failure prints one safe warning to stderr and continues with the same provider so the prompt-time call can still succeed.

The workspace handler will avoid needless status commands by probing for a socket and lease that identify a validated live broker. A confirmed live broker skips readiness and follows the existing reattachment path. Missing broker state runs readiness before broker and target preparation. Ambiguous state skips readiness and lets existing broker creation validation return the authoritative lifecycle error.

The probe is read-only and introduces no new lease format. Simultaneous first-start contenders may both perform the harmless readiness check before the existing atomic socket creation selects one owner.

### Preserve prompt-time arbitration

Readiness does not change terminal matching, input suppression, manual fallback, broker grouping, reconnect arbitration, or the one-submission rule. TOTP retrieval remains inside the existing prompt-matched provider call. A provider timeout at the prompt continues through the established safe fallback paths.

### Test observable behavior without wall-clock delays

Provider tests will use the existing runner seam to inspect the real context deadline, exact arguments, parsing, and safe classification. Orchestration tests will use deterministic fakes at the external provider, launcher, and broker boundaries and assert connection ordering and user-visible outcomes rather than mock existence. No regression test will wait ten real seconds or access live secrets.

## Risks / Trade-offs

- **[A wrapper still exceeds twenty seconds]** → Retain bounded timeout classification and existing fallback.
- **[Readiness succeeds but later retrieval fails]** → Treat readiness as advisory and keep prompt-time error handling authoritative.
- **[Concurrent workspace starters duplicate readiness]** → Accept a non-secret bounded duplicate rather than add persistent locking or publish an unserved broker socket.
- **[Broker state is malformed or hostile]** → Skip readiness and preserve the existing ownership and lifecycle validation error.
- **[Status JSON contains private deployment metadata]** → Parse only the bounded status field, never forward raw buffers, and clear captured bytes.
- **[Longer prompt wait delays fallback]** → Keep the limit fixed at twenty seconds and document the residual remote-prompt risk.

## Migration Plan

No configuration or data migration is required. Ship the behavior as a backward-compatible patch after OpenSpec validation, focused and full Go verification, cross-builds, and privacy scrub. Rollback restores the previous provider timeout and removes readiness orchestration; existing configuration and workspace state remain compatible in either direction.
