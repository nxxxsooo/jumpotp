## ADDED Requirements

### Requirement: Failure-only Bitwarden elapsed diagnostics
JumpOTP SHALL measure the external execution time of failed Bitwarden readiness and TOTP retrieval operations and append a stage-specific, rounded elapsed duration to the existing redacted diagnostic. Durations below one second SHALL be rendered as `<1s`; longer durations SHALL be rounded to the nearest whole second and bounded by the provider deadline. Successful operations MUST remain silent. Diagnostics MUST NOT contain an item reference, command arguments, stdout, stderr, vault metadata, session material, password, OTP, or wrapper-internal sub-stage data.

#### Scenario: Readiness failure includes bounded elapsed time
- **WHEN** `bw status` returns a locked, unavailable, malformed, interrupted, failed, or timed-out result
- **THEN** the readiness warning identifies the readiness stage, includes only the rounded provider-execution duration, and continues with the existing configured MFA behavior

#### Scenario: Direct retrieval failure includes bounded elapsed time
- **WHEN** prompt-gated `bw get totp` retrieval fails during a direct connection
- **THEN** the existing fail or visible-fallback diagnostic identifies TOTP retrieval and includes only the rounded provider-execution duration

#### Scenario: Fast failure uses a low-resolution bucket
- **WHEN** a failed Bitwarden operation returns in less than one second
- **THEN** JumpOTP reports the elapsed duration as `<1s` rather than exposing sub-second precision

#### Scenario: Successful provider operation remains silent
- **WHEN** readiness or TOTP retrieval succeeds
- **THEN** JumpOTP emits no timing diagnostic and preserves the existing success behavior

#### Scenario: Non-provider failure has no invented timing
- **WHEN** JumpOTP receives an error for which it did not measure provider execution
- **THEN** the safe diagnostic remains generic and omits elapsed timing

### Requirement: Secret-free workspace timing transport
For workspace TOTP failures, JumpOTP SHALL carry the server-measured elapsed-time bucket as an optional bounded numeric broker response field. The broker MUST NOT use arbitrary provider messages to transport timing, and the client MUST ignore absent or out-of-range timing values while retaining the validated error category. Adding the optional field MUST remain compatible with protocol-version-one peers that do not send or consume it.

#### Scenario: Workspace retrieval preserves measured timing
- **WHEN** the workspace broker receives a timed provider failure
- **THEN** each waiting client receives the validated error category and bounded elapsed-time bucket and renders the same redacted timing diagnostic as a direct connection

#### Scenario: Older peer omits timing
- **WHEN** a protocol-version-one broker response contains a valid error category without an elapsed-time field
- **THEN** the client preserves the existing safe error behavior without adding a duration

#### Scenario: Broker timing is outside the accepted bound
- **WHEN** a broker response contains a negative or above-deadline elapsed-time value
- **THEN** the client ignores the timing value and does not render it

#### Scenario: Arbitrary broker message is not surfaced
- **WHEN** a broker response includes a message containing private provider details
- **THEN** the client constructs its diagnostic only from the validated category and accepted numeric timing field
