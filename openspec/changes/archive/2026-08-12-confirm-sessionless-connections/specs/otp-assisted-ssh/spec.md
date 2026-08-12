## MODIFIED Requirements

### Requirement: OTP secrecy and cleanup
JumpOTP MUST NOT place an OTP or TOTP seed in configuration, argv, environment variables, logs, error messages, telemetry, update requests, clipboard contents, persistent files, tmux paste buffers, tmux commands, captured pane output, or runtime lease metadata. Workspace OTP delivery SHALL use only in-memory broker and PTY byte streams. Implementations SHALL minimize code lifetime, overwrite owned buffers when practical, and close broker connections promptly. Because a remote endpoint may echo submitted characters outside JumpOTP's control, the wrapper SHALL clear its own visible screen and scrollback once it confirms that the connection is established, using terminal control sequences written to its own output stream. Clearing MUST NOT use `capture-pane`, `send-keys`, or any other tmux buffer or command mechanism. Clearing MUST NOT be triggered by a local submission write, because an endpoint's echo arrives after that write completes and would survive the clear. An attempt that never reaches a confirmed connection MUST NOT clear, so that its failure evidence remains visible.

#### Scenario: Debug logging enabled
- **WHEN** diagnostic logging is enabled during OTP retrieval and injection
- **THEN** logs contain redacted state transitions but no OTP value, seed, session value, raw item reference, or prompt transcript

#### Scenario: Process terminated
- **WHEN** a broker or wrapper exits normally, fails, or is interrupted
- **THEN** its socket connections close, owned OTP byte buffers are discarded, and no tmux buffer or persistent file requires cleanup

#### Scenario: Endpoint echoes the submitted code
- **WHEN** the remote endpoint echoes a submitted code back to the target window and the connection then becomes established
- **THEN** the wrapper clears the visible screen and scrollback at establishment, so the echoed value does not persist in a long-lived window

#### Scenario: Echo arriving after the submission write is still cleared
- **WHEN** the endpoint's echo reaches the window only after the local submission write has returned
- **THEN** the clear still removes it, because clearing is triggered by confirmed establishment rather than by the write

#### Scenario: Failed authentication keeps its evidence
- **WHEN** an attempt ends without a confirmed connection, including while an operator is still being prompted for manual entry
- **THEN** the wrapper does not clear the screen, preserving the prompt, retry guidance, and failure lines
