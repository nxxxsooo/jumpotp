## MODIFIED Requirements

### Requirement: JumpOTP-owned tmux isolation
Workspace mode SHALL use a dedicated JumpOTP tmux server rather than the default tmux server. Each profile SHALL map to exactly one ordinary tmux session containing one target window per configured target and, only when health probing is enabled, one health-summary window. Every target window SHALL run the JumpOTP PTY wrapper around the selected launcher in sessionless master mode rather than a raw launcher command or an interactive remote shell.

#### Scenario: Ordinary tmux remains untouched
- **WHEN** a user creates, attaches, stops, or repairs a JumpOTP workspace
- **THEN** sessions, paste buffers, sockets, processes, and clients on the user's default tmux server are not listed, modified, detached, or killed

#### Scenario: Workspace created
- **WHEN** `jumpotp workspace production` runs and no production session exists
- **THEN** JumpOTP creates one profile session and starts one wrapped sessionless-master target window for every configured production target

### Requirement: Workspace status reporting
`status` SHALL report profile session presence, attached client count, broker state, target wrapper and child state, health-window state, and whether target ControlMasters appear available. A target whose wrapper window is alive but whose ControlMaster is not confirmed SHALL be reported as `connecting` rather than `running`. It SHALL not capture or print pane contents, raw process arguments, item references, or runtime lease contents.

#### Scenario: Human-readable status
- **WHEN** the user runs `jumpotp status production`
- **THEN** the output distinguishes running, connecting, detached, stopped, failed, broker-unavailable, and probe-skipped states without exposing remote terminal content

#### Scenario: Target reconnecting after connection loss
- **WHEN** a target wrapper is waiting or reconnecting and its ControlMaster is not confirmed
- **THEN** `status` reports that target as `connecting` while the session, wrapper, and health states remain individually reported

## ADDED Requirements

### Requirement: Sessionless target masters
Every workspace target window SHALL hold its connection without a remote session: the wrapper SHALL invoke the selected launcher with `-N`, bounded keepalive options (`ServerAliveInterval` 60 seconds, `ServerAliveCountMax` 3), and `ControlPersist no` followed by the target alias, and MUST NOT request a remote shell, remote command, or session channel. Disabling ControlPersist for the wrapper's own invocation keeps the master process inside the window so its lifetime remains window-owned even when the user's OpenSSH configuration enables ControlPersist for bare invocations. ControlMaster and ControlPath behavior SHALL remain delegated to the user's OpenSSH configuration. When an externally owned ControlMaster already serves the alias, the sessionless client SHALL attach through it without error and SHALL become the master only after a later attempt finds no live master. Interactive remote work SHALL NOT be a workspace window responsibility.

#### Scenario: Idle workspace survives interactive-session reaping
- **WHEN** a workspace target master has been established and no interactive use occurs for longer than the bastion's idle-session reclamation cycle
- **THEN** the sessionless connection and its ControlMaster remain available and ordinary `ssh <alias>` invocations continue to reuse them without new MFA

#### Scenario: External master already present
- **WHEN** a target window starts while a ControlPersist master from a prior bare invocation still owns the ControlPath
- **THEN** the wrapper's sessionless client runs without MFA through the existing master, and after that master exits a later supervised attempt establishes a new master

### Requirement: Supervised target reconnection
The target wrapper SHALL supervise its launcher child. When the child exits for any reason other than an operator stop, the wrapper SHALL reconnect with exponential backoff starting at 5 seconds, doubling to a 300-second ceiling, with bounded jitter, and SHALL reset per-connection submission state for each attempt. Before dialing, the wrapper SHALL require either a confirmed reusable ControlMaster for the alias or a validated active profile broker; while neither is available it SHALL wait and re-check on a bounded interval without initiating SSH authentication. SIGTERM, SIGHUP, and interrupt SHALL end the wrapper without reconnecting. The wrapper SHALL print redacted connection, exit, backoff, and waiting states to its own window.

#### Scenario: Connection lost while detached
- **WHEN** a target master dies while the workspace is detached and no broker is active
- **THEN** the window persists, the wrapper waits without dialing SSH, and the next `workspace` invocation's broker allows the wrapper to reconnect automatically without manual window repair

#### Scenario: Reconnect reuses an existing master
- **WHEN** the launcher child exits but `ssh -O check` still confirms a live master for the alias
- **THEN** the wrapper reconnects immediately through the existing master without requesting an OTP

#### Scenario: No unattended authentication noise
- **WHEN** the reconnect gate finds neither a reusable master nor a validated broker
- **THEN** the wrapper initiates no SSH connection and no MFA interaction until a later gate check passes

#### Scenario: Operator stop is final
- **WHEN** `stop` kills the profile session or the user closes a target window or interrupts the wrapper
- **THEN** the wrapper exits without spawning another launcher child
