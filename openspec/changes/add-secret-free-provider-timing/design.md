## Context

See `proposal.md` for motivation and `specs/otp-assisted-ssh/spec.md` for observable requirements. The Bitwarden provider currently returns category-only errors. Readiness errors carry an internal operation marker so safe messages can distinguish `bw status` from TOTP retrieval. Direct terminal fallback renders those safe messages locally. Workspace retrieval crosses a version-one JSON broker protocol whose server emits a safe `message`, but whose client intentionally ignores that text and reconstructs an error from a validated category.

The provider owns the only trustworthy boundary around the PATH-resolved external command, so it is also the only component that can measure wrapper-inclusive execution without exposing the item or captured output. Existing provider execution is bounded at twenty seconds; broker clients and connections remain bounded at twenty-two and twenty-five seconds.

## Goals / Non-Goals

**Goals:**

- Attach low-resolution elapsed timing only to errors measured around an external Bitwarden provider invocation.
- Preserve the measurement through workspace brokering without weakening category validation or raw-message isolation.
- Keep deterministic tests independent of actual wall-clock delays and live Bitwarden state.
- Preserve compatibility with existing version-one broker peers in both directions.

**Non-Goals:**

- Timing successful calls, parsing time, broker queue time, network framing, terminal arbitration, or wrapper sub-stages.
- Adding debug modes, configuration, persistent logging, telemetry, login, unlock, session management, or a protocol-version bump.
- Changing the twenty-second provider budget, existing fallback behavior, or public exit codes.

## Decisions

### Measure the external runner and attach timing only on failure

The provider will sample time immediately before and after its discrete-argument runner call. All provider-generated failures after that call—including command failure, timeout, locked or malformed readiness status, and invalid TOTP output—will retain the measured execution duration. Successful readiness and retrieval will discard the measurement and remain silent. Errors generated outside this measured boundary will not gain a duration.

`Bitwarden` will receive an internal clock seam, analogous to its runner and timeout seams, so tests can advance deterministic monotonic timestamps without sleeping. The production default remains `time.Now`.

Alternative considered: measure in CLI and terminal callers. Rejected because this would omit broker-owned retrieval, mix provider time with orchestration, and duplicate security-sensitive formatting logic.

### Normalize elapsed time into a low-resolution bounded bucket

Provider errors will distinguish “unmeasured” from a measured duration below one second. Safe rendering uses `<1s` for the sub-second bucket and the nearest whole second otherwise. Before attachment, elapsed time is clamped to the inclusive range from zero through the twenty-second provider deadline, preventing scheduler overshoot or malformed transport from expanding the diagnostic domain.

The elapsed value remains structured data on the provider error; callers never concatenate raw runner output. Readiness conversion preserves both the validated category and the elapsed bucket.

Alternative considered: retain millisecond precision. Rejected because it is unnecessary for diagnosing multi-second wrappers and exposes more operational detail than the Product Brief permits.

### Extend broker version one with an optional numeric field

Error responses will add optional `elapsed_seconds`. A pointer/optional representation distinguishes an absent measurement from the valid zero bucket used for `<1s`. The server copies the field only from a measured provider error. The client accepts only integer values from zero through twenty and reconstructs a category-plus-timing provider error; absent or invalid values are ignored. The existing `message` remains unused by the client and must not influence diagnostics.

The protocol version stays at one. JSON peers ignore unknown fields, so old clients remain compatible with new servers; new clients treat an omitted field from old servers as an unmeasured error.

Alternative considered: send a fully rendered message. Rejected because it would make a broker-controlled string part of the terminal security boundary and could carry item or provider output.

### Keep rendering centralized in provider safe-message functions

Provider safe-message rendering will append ` after <bucket>` only when the error contains an accepted measurement. Readiness-specific rendering continues to select readiness wording; retrieval rendering continues to use the existing TOTP categories. CLI and terminal callers remain unaware of formatting details, and broker clients reconstruct structured errors before those existing surfaces render them.

Alternative considered: add timing at each CLI warning and fallback site. Rejected because direct and workspace paths could diverge and generic errors could accidentally acquire invented timing.

## Risks / Trade-offs

- **[A twenty-second timeout can overshoot slightly in wall time]** → Clamp the diagnostic bucket to the provider deadline; retain the real context deadline for enforcement.
- **[Nearest-second rounding hides small latency differences]** → Accept the loss of precision as a privacy trade-off; `<1s` still distinguishes fast failures from slow wrapper paths.
- **[A malformed broker sends arbitrary timing]** → Accept only optional integers in the fixed zero-to-twenty range and otherwise omit timing while preserving validated category handling.
- **[Existing tests assert exact messages]** → Use deterministic clock fixtures and update only assertions whose provider errors are explicitly measured.
- **[A wrapper's internal stage remains unknown]** → Keep that boundary intentional; the diagnostic identifies only readiness versus TOTP retrieval and total external-command time.

## Migration Plan

No configuration or persistent-state migration is required. The optional version-one broker field permits rolling compatibility with an already-running old broker or older pane client. Ship through the normal patch release process after focused provider, broker, CLI, and terminal tests; full formatting, vet, test, race, cross-build, strict OpenSpec validation, and privacy scrub. Rollback removes the optional field and timing attachment; category-only version-one behavior remains valid.
