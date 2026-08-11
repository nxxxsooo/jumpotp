## ADDED Requirements

### Requirement: Best-effort Bitwarden readiness before automatic connections
Before launching an automatic direct connection, JumpOTP SHALL execute `bw status` with discrete arguments to warm and validate the existing Bitwarden CLI environment. Before preparing targets for a workspace with no validated live broker, JumpOTP SHALL perform the same readiness check. The readiness check MUST NOT receive an item reference, request a TOTP, log in, unlock the vault directly, manage `BW_SESSION`, or forward raw command output. A readiness failure SHALL produce one redacted warning and SHALL NOT prevent the connection from continuing to its configured prompt-time fallback. Manual mode and reattachment to a validated live broker SHALL skip readiness.

#### Scenario: Automatic direct connection is warmed before launch
- **WHEN** an operator starts an automatic direct connection and `bw status` reports `unlocked`
- **THEN** JumpOTP completes readiness silently before launching the configured SSH or SSHM child

#### Scenario: Readiness failure preserves prompt-time fallback
- **WHEN** readiness is locked, unavailable, malformed, interrupted, or timed out
- **THEN** JumpOTP prints one redacted readiness warning, launches the connection, and preserves the configured `prompt` or `fail` behavior at the MFA prompt

#### Scenario: Manual mode skips Bitwarden readiness
- **WHEN** an operator starts a direct connection or workspace with `--manual`
- **THEN** JumpOTP does not execute `bw status` and does not request a provider-generated code

#### Scenario: Existing workspace broker skips readiness
- **WHEN** an operator reattaches to a workspace whose broker socket and current-user lease identify a validated live broker
- **THEN** the reattaching invocation does not execute `bw status` and reuses the existing broker

#### Scenario: Readiness diagnostics remain secret-free
- **WHEN** `bw status` emits structured status data or an error containing deployment details
- **THEN** JumpOTP discards the raw output and exposes only a bounded readiness category without an item reference, server URL, session value, password, or OTP

### Requirement: Provider and broker latency budgets
JumpOTP SHALL apply a twenty-second deadline independently to each Bitwarden readiness or TOTP retrieval command. Workspace broker clients SHALL use a twenty-two-second request deadline, and broker server connections SHALL use a twenty-five-second deadline, so normal broker framing remains outside the provider budget. Expiry SHALL retain the existing visible fallback or fail behavior and MUST NOT trigger an automatic retry after remote rejection.

#### Scenario: Guarded provider completes after the former deadline
- **WHEN** a guarded readiness or TOTP command completes successfully after ten seconds but before twenty seconds
- **THEN** JumpOTP accepts the result instead of classifying it as timed out

#### Scenario: Provider exceeds the bounded deadline
- **WHEN** a readiness or TOTP command does not complete within twenty seconds
- **THEN** JumpOTP terminates the bounded operation, reports the corresponding redacted timeout category, and follows the configured continuation or fallback behavior

#### Scenario: Broker budget covers provider execution
- **WHEN** a workspace target requests a code from a healthy broker
- **THEN** the broker client and connection deadlines remain longer than the twenty-second provider deadline by their specified margins
