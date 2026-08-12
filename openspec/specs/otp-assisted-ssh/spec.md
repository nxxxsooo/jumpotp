# otp-assisted-ssh Specification

## Purpose

Defines how JumpOTP safely obtains a short-lived TOTP from an already configured Bitwarden CLI and injects it into an authorized interactive SSH MFA prompt without exporting the seed or persisting the code.
## Requirements
### Requirement: Bitwarden is the only v0.1 OTP provider
JumpOTP SHALL accept only `provider: bitwarden` in v0.1. Direct connect mode and the active workspace broker SHALL obtain a code by executing `bw get totp <item>` with discrete arguments and MUST NOT invoke a local shell. An item MAY be an exact ID or a unique literal string; ambiguous string lookup SHALL be treated as provider failure.

#### Scenario: Existing self-hosted Bitwarden configuration
- **WHEN** the user's `bw` installation is already configured for a self-hosted Bitwarden-compatible server
- **THEN** JumpOTP uses that existing CLI configuration without reading or changing its server URL

#### Scenario: Duplicate matching items
- **WHEN** `bw get totp` reports that a literal item reference matches more than one vault item
- **THEN** JumpOTP reports an ambiguous provider lookup and follows the configured fallback without selecting either item

#### Scenario: Unsupported provider
- **WHEN** configuration names `1password`, `pass`, `rbw`, or an arbitrary command provider
- **THEN** validation fails as unsupported in v0.1

### Requirement: JumpOTP does not manage Bitwarden authentication
JumpOTP MUST NOT perform `bw login`, `bw unlock`, `bw config server`, manage `BW_SESSION`, request a master password, persist Bitwarden API credentials, or place the existing Bitwarden session value into tmux server state. It SHALL rely on the invoking user's existing `bw` environment and authentication behavior.

#### Scenario: Vault is locked
- **WHEN** `bw get totp` fails because the vault is locked or unavailable
- **THEN** JumpOTP explains the provider failure and follows the configured fallback without attempting to unlock the vault

### Requirement: Strict bounded MFA prompt matching
JumpOTP SHALL retrieve or request an OTP only after the child PTY output fully matches the configured `jumpserver-koko`, `generic-totp`, or `custom` matcher. Matching SHALL observe a bounded rolling buffer, normalize terminal control sequences for comparison without changing displayed output, and MUST NOT provide a broad `generic-otp` preset or match ordinary password, SMS, email, push, or WebAuthn prompts. A custom expression SHALL use Go RE2 syntax, be nonempty, and be limited to 4096 UTF-8 bytes.

#### Scenario: JumpServer KoKo prompt
- **WHEN** output matches the complete `jumpserver-koko` six-digit code-prompt family
- **THEN** JumpOTP retrieves the effective target OTP and submits it once

#### Scenario: Password prompt
- **WHEN** output contains `Password:` but no configured MFA match
- **THEN** JumpOTP passes the interaction through unchanged and does not call Bitwarden

#### Scenario: ANSI-formatted prompt
- **WHEN** a valid configured MFA prompt is split by ANSI control sequences or PTY reads
- **THEN** the observer matches the normalized bounded stream while the user receives the original bytes unchanged

#### Scenario: Custom pattern
- **WHEN** a profile selects `custom` and supplies a valid bounded RE2-compatible expression
- **THEN** that exact expression is used as the only automatic OTP trigger for the profile

### Requirement: One automatic submission per target connection
JumpOTP SHALL automatically submit at most one provider-generated code per distinct strict-matched MFA prompt, and at most two automatic submissions per connection attempt. A second automatic submission SHALL occur only after the remote endpoint rejects the first and SHALL use a code obtained after waiting for the next epoch-aligned TOTP window boundary; a previously submitted code value MUST NOT be resubmitted. After the second rejection, or when no fresh code can be obtained, JumpOTP MUST NOT loop and SHALL follow the configured fallback. Each supervised reconnection attempt is a new connection attempt with reset submission state.

#### Scenario: Automatic code rejected
- **WHEN** the remote endpoint rejects an automatically submitted code and shows another strict-matched prompt
- **THEN** JumpOTP waits for the next TOTP window, submits one fresh code automatically, and preserves all redaction and zeroization guarantees

#### Scenario: Automatic submissions exhausted
- **WHEN** the remote endpoint rejects a second automatically submitted code and shows another prompt
- **THEN** JumpOTP leaves the target interactive, explains that automatic submission is exhausted, and allows only manual interaction for that connection attempt

#### Scenario: Stale code is never repeated
- **WHEN** a retry becomes possible while the rejected code's TOTP window is still current
- **THEN** JumpOTP delays the retry past the window boundary rather than resubmitting the same value

### Requirement: Visible manual fallback in the target terminal
OTP fallback SHALL support `prompt` and `fail`, with `prompt` as the default. Under `prompt`, the target PTY wrapper SHALL print a concise redacted failure explanation in the same controlling terminal, temporarily restore local line discipline and visible echo, read only a numeric line from that controlling terminal, restore proxy mode, and submit the value once. Under `fail`, or when no controlling terminal exists, JumpOTP SHALL not read from piped stdin and SHALL exit the affected target with code `4`.

#### Scenario: Direct manual fallback
- **WHEN** provider retrieval fails during `connect` and fallback is `prompt`
- **THEN** the user sees the reason in the local terminal, sees the digits while typing, and JumpOTP submits the validated line once

#### Scenario: Workspace manual fallback
- **WHEN** a workspace broker is unavailable or reports provider failure
- **THEN** the affected target pane itself explains the condition and performs visible manual entry without using the workspace command's outer stdin

#### Scenario: Manual fallback without a TTY
- **WHEN** provider retrieval fails but the target wrapper has no controlling terminal
- **THEN** it exits with code `4` without reading a secret from standard input

#### Scenario: Manual-only invocation
- **WHEN** the user supplies `--manual`
- **THEN** every affected target skips broker and Bitwarden retrieval and requests visible manual input only after its strict MFA prompt matches

### Requirement: OTP value validation
Provider and manual values MUST be numeric and 5 through 10 digits unless the selected preset or configured `digits` value imposes a narrower length. The `jumpserver-koko` preset SHALL require exactly six digits. Invalid values MUST NOT be injected.

#### Scenario: Unexpected provider output
- **WHEN** `bw get totp` emits nonnumeric output or a value outside the allowed length
- **THEN** JumpOTP treats retrieval as failed, never logs the value, and follows the configured fallback

### Requirement: Ephemeral grouped workspace broker
Each active `workspace PROFILE` invocation SHALL create one current-user-only Unix-domain broker socket in a mode-`0700` runtime directory and remove it when the invocation ends. Target wrappers SHALL register only their profile and target identifiers; the broker SHALL resolve effective provider and item values from its own validated configuration. Targets with the same provider and item SHALL form one group. The broker SHALL retrieve only after at least one group member is ready, briefly aggregate concurrently ready members, and send one in-memory code to each ready wrapper over the socket. Later members SHALL trigger a fresh retrieval. When a group retrieval would begin with fewer than 8 seconds remaining in the current epoch-aligned 30-second TOTP window, the broker SHALL delay the retrieval until the next window begins so every delivered code retains usable submission runway.

#### Scenario: Two targets ready together
- **WHEN** two targets in the same group reach their prompts within the aggregation window while the broker is active
- **THEN** the broker calls Bitwarden once and sends the result once to each ready target wrapper

#### Scenario: Late target
- **WHEN** another target in the group reaches its prompt after the earlier batch was submitted
- **THEN** the broker performs a fresh provider retrieval for the late target

#### Scenario: Retrieval near the rotation boundary
- **WHEN** a group becomes ready inside the final guard interval of the current TOTP window
- **THEN** the broker waits for the next window boundary before invoking the provider and then delivers the fresh-window code to every aggregated waiter

#### Scenario: Target-level OTP override
- **WHEN** one target overrides the profile Bitwarden item
- **THEN** its code is never delivered to targets using the profile item

#### Scenario: Broker disappears
- **WHEN** the workspace command detaches, terminates, or loses its broker while target panes remain
- **THEN** target sessions persist, pending wrappers reconnect to a later broker invocation, and a wrapper with an attended prompt can still use visible manual fallback

### Requirement: Interactive terminal fidelity
Every target wrapper SHALL preserve bidirectional terminal behavior, ANSI output, window resizing, user signals, and terminal restoration before and after MFA handling. Direct mode SHALL proxy the selected launcher directly; workspace mode SHALL run the same wrapper inside each target tmux pane.

#### Scenario: Terminal resize
- **WHEN** the controlling terminal size changes during a target connection
- **THEN** the SSH child PTY receives the updated dimensions without terminating the session

#### Scenario: Interrupted manual prompt
- **WHEN** the user interrupts while proxy mode is temporarily suspended for visible manual input
- **THEN** JumpOTP restores terminal state and returns the interrupt exit class

### Requirement: OTP secrecy and cleanup
JumpOTP MUST NOT place an OTP or TOTP seed in configuration, argv, environment variables, logs, error messages, telemetry, update requests, clipboard contents, persistent files, tmux paste buffers, tmux commands, captured pane output, or runtime lease metadata. Workspace OTP delivery SHALL use only in-memory broker and PTY byte streams. Implementations SHALL minimize code lifetime, overwrite owned buffers when practical, close broker connections promptly, and document that the remote endpoint may visibly echo submitted characters outside JumpOTP's control.

#### Scenario: Debug logging enabled
- **WHEN** diagnostic logging is enabled during OTP retrieval and injection
- **THEN** logs contain redacted state transitions but no OTP value, seed, session value, raw item reference, or prompt transcript

#### Scenario: Process terminated
- **WHEN** a broker or wrapper exits normally, fails, or is interrupted
- **THEN** its socket connections close, owned OTP byte buffers are discarded, and no tmux buffer or persistent file requires cleanup

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

