## Purpose

Defines how JumpOTP safely obtains a short-lived TOTP from an already configured Bitwarden CLI and injects it into an authorized interactive SSH MFA prompt without exporting the seed or persisting the code.

## ADDED Requirements

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
JumpOTP SHALL automatically submit at most one provider-generated code to each target connection. It MUST NOT loop on rejection or silently fetch and submit another code.

#### Scenario: Automatic code rejected
- **WHEN** the remote endpoint rejects an automatically submitted code and shows another prompt
- **THEN** JumpOTP leaves the target interactive, explains that automatic submission is exhausted, and allows only manual interaction for that connection

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
Each active `workspace PROFILE` invocation SHALL create one current-user-only Unix-domain broker socket in a mode-`0700` runtime directory and remove it when the invocation ends. Target wrappers SHALL register only their profile and target identifiers; the broker SHALL resolve effective provider and item values from its own validated configuration. Targets with the same provider and item SHALL form one group. The broker SHALL retrieve only after at least one group member is ready, briefly aggregate concurrently ready members, and send one in-memory code to each ready wrapper over the socket. Later members SHALL trigger a fresh retrieval.

#### Scenario: Two targets ready together
- **WHEN** two targets in the same group reach their prompts within the aggregation window while the broker is active
- **THEN** the broker calls Bitwarden once and sends the result once to each ready target wrapper

#### Scenario: Late target
- **WHEN** another target in the group reaches its prompt after the earlier batch was submitted
- **THEN** the broker performs a fresh provider retrieval for the late target

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
