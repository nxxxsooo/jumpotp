## Purpose

Defines an optional persistent multi-host tmux workspace that is isolated from the user's ordinary tmux server, uses pane-local PTY wrappers for MFA, and fails closed when server ownership cannot be proven.

## ADDED Requirements

### Requirement: JumpOTP-owned tmux isolation
Workspace mode SHALL use a dedicated JumpOTP tmux server rather than the default tmux server. Each profile SHALL map to exactly one ordinary tmux session containing one target window per configured target and, only when health probing is enabled, one health-summary window. Every target window SHALL run the JumpOTP PTY wrapper around the selected launcher rather than a raw launcher command.

#### Scenario: Ordinary tmux remains untouched
- **WHEN** a user creates, attaches, stops, or repairs a JumpOTP workspace
- **THEN** sessions, paste buffers, sockets, processes, and clients on the user's default tmux server are not listed, modified, detached, or killed

#### Scenario: Workspace created
- **WHEN** `jumpotp workspace production` runs and no production session exists
- **THEN** JumpOTP creates one profile session and starts one wrapped target window for every configured production target

### Requirement: One ephemeral broker per active profile invocation
An active `workspace PROFILE` command SHALL own or reuse exactly one validated broker for that profile while it prepares and presents the workspace. The broker SHALL remain available while that command is attached or waiting on a same-server client switch, and SHALL close its socket when the owning command ends. A later workspace invocation SHALL safely create a new broker and existing target wrappers SHALL be able to reconnect without restarting their SSH children. Broker absence SHALL not terminate the tmux session.

#### Scenario: Detach and reattach
- **WHEN** the user detaches, causing the owning workspace command and broker to exit, and later runs the same workspace command
- **THEN** the existing tmux session remains, a new broker is created, target wrappers reconnect, and the user reattaches without restarting healthy SSH children

#### Scenario: Concurrent workspace invocation
- **WHEN** another process requests the same profile while a validated broker is already active
- **THEN** it reuses the active profile broker for target readiness instead of creating a competing OTP source

### Requirement: Explicit persistent lifecycle
`workspace` SHALL persist after terminal closure or tmux detach and SHALL reattach on the next identical command. `stop PROFILE` SHALL terminate only the named profile session and SHALL not issue `ssh -O exit`, delete OpenSSH ControlPath sockets, or otherwise close ControlMaster connections.

#### Scenario: Terminal tab closed
- **WHEN** the terminal client and workspace broker disappear without `stop`
- **THEN** the profile tmux session and its target wrappers continue in the JumpOTP tmux server

#### Scenario: Stop one profile
- **WHEN** `jumpotp stop production` runs while another profile workspace exists
- **THEN** only the production session is terminated and the other profile remains available

#### Scenario: ControlMaster after stop
- **WHEN** stopping a workspace leaves an OpenSSH ControlMaster alive
- **THEN** JumpOTP reports that OpenSSH controls its remaining lifetime and does not close it

### Requirement: Safe attachment behavior
JumpOTP SHALL attach from a normal terminal, switch the current client when invoked inside its own tmux server, and refuse implicit nested attachment when invoked inside a different tmux server. Broker ownership SHALL remain valid for the duration of either attachment form. A nested-attachment refusal SHALL explain how to detach first.

#### Scenario: Invocation inside unrelated tmux
- **WHEN** workspace attachment is requested from a client on another tmux server
- **THEN** JumpOTP exits with code `5` without nesting or switching the unrelated client

#### Scenario: Invocation inside JumpOTP tmux
- **WHEN** workspace attachment is requested from a client already connected to the JumpOTP server
- **THEN** JumpOTP switches that client to the requested profile and keeps the invoking process available as the profile broker until the client leaves

### Requirement: Current-user runtime namespace and leases
JumpOTP SHALL use a current-user-only runtime directory with mode `0700` for non-secret socket and lease metadata. It SHALL maintain mode-`0600` leases for the dedicated tmux server and active profile brokers. A server lease SHALL record the canonical socket path, user identity, tmux executable identity, PID, and operating-system process start identity. A broker lease SHALL record only profile, socket path, PID, and process start identity. Leases MUST NOT contain configuration values, SSH expansion, Bitwarden references, session values, or OTPs.

#### Scenario: Runtime directory has unsafe permissions
- **WHEN** the selected runtime directory is not owned by the current user or is accessible to another user
- **THEN** workspace operations fail with code `5` before creating a tmux server or broker

#### Scenario: PID reused
- **WHEN** a lease PID exists but its start identity or executable does not match the lease
- **THEN** JumpOTP treats the lease as stale evidence and refuses to signal that process

### Requirement: Bounded workspace server recovery
JumpOTP SHALL distinguish an absent tmux server from an unhealthy JumpOTP-owned server. After bounded retries and command timeouts, it MAY terminate an unresponsive server only after the canonical socket, current-user ownership, runtime lease, PID, process start identity, and tmux executable identity all agree. Recovery MUST NOT use broad process matching. Because recovery resets every JumpOTP workspace, JumpOTP SHALL print that impact before recovery and rebuild only the requested profile. Ambiguous or incomplete evidence SHALL produce manual remediation guidance without signaling a process.

#### Scenario: No server exists
- **WHEN** workspace creation finds neither a live JumpOTP server nor a conflicting socket
- **THEN** it starts a new server and writes a new validated lease without reporting corruption

#### Scenario: Server exited unexpectedly
- **WHEN** repeated bounded health commands fail and the socket, lease, owner, PID, start identity, and executable all identify one unresponsive JumpOTP tmux process
- **THEN** JumpOTP attempts graceful termination, uses forced termination only after a bounded wait, quarantines only the stale dedicated socket and lease, and rebuilds the requested workspace

#### Scenario: Ownership cannot be validated
- **WHEN** any socket, lease, process, owner, executable, or start-identity evidence conflicts or is unavailable
- **THEN** JumpOTP refuses destructive recovery and exits with code `5` with exact manual remediation guidance

### Requirement: Pane and window failures fail closed
JumpOTP SHALL verify every tmux create, query, respawn, selection, and lifecycle operation. Empty pane identifiers or failed queries MUST be treated as workspace errors and MUST NOT be reinterpreted as a healthy or replaceable target pane. JumpOTP MUST NOT use `capture-pane`, `load-buffer`, `set-buffer`, `paste-buffer`, or `send-keys` to observe or submit an OTP.

#### Scenario: Target wrapper exits during creation
- **WHEN** a new target window terminates before JumpOTP can identify its wrapper pane
- **THEN** workspace creation exits with code `5`, names the synthetic target identifier, and never substitutes an empty pane

#### Scenario: Existing non-wrapper pane
- **WHEN** a target window exists but cannot prove it is running the expected JumpOTP wrapper for that profile and target
- **THEN** JumpOTP preserves the pane and reports a conflict instead of replacing it

### Requirement: Generated rotating health-probe catalog
`config init` SHALL generate the following ordered Linux probe catalog, with each probe enabled in the catalog but all workspace health execution globally disabled:

| ID | Label | Remote command |
|---|---|---|
| `uptime` | Time, load, and uptime | `date '+%F %T %Z'; uptime` |
| `memory` | Memory and swap | `free -h` |
| `root-filesystem` | Root filesystem | `df -h /` |
| `failed-units` | Failed systemd units | `systemctl --failed --no-legend --no-pager` |
| `network-summary` | Network summary | `ss -s` |
| `top-cpu` | Top CPU processes | `LC_ALL=C ps -eo pid,comm,%cpu,%mem,etime --sort=-%cpu | head -n 8` |

Users SHALL be able to enable or disable definitions, edit commands and labels, add uniquely named trusted definitions, delete unreferenced definitions, and select or reorder probe IDs per profile. When profile health is enabled, one scheduler SHALL rotate through its effective ordered commands at the configured interval and render results in one health-summary window.

#### Scenario: Default public configuration
- **WHEN** a user initializes and uses a new configuration without editing health settings
- **THEN** the six definitions are visible but no remote health command runs

#### Scenario: Customized rotation
- **WHEN** a user disables one definition, adds another trusted command, and orders the remaining IDs in a profile
- **THEN** validation accepts valid references and the scheduler rotates only that effective order

### Requirement: Probes reuse existing authenticated connections only
Before every probe, JumpOTP SHALL require a successful `ssh -O check <alias>` and SHALL then invoke `ssh -o BatchMode=yes -o ConnectTimeout=<bounded> <alias> <remote-command>` using discrete local arguments. It MUST NOT evaluate the command in a local shell, initiate interactive authentication, request an OTP, or inject commands into target panes. Unsupported RemoteCommand behavior SHALL be reported as skipped or failed without disrupting target windows.

#### Scenario: No ControlMaster
- **WHEN** a probe becomes due but `ssh -O check` does not confirm a live master for the target
- **THEN** the probe is skipped and no new SSH login starts

#### Scenario: Bastion rejects RemoteCommand
- **WHEN** a batch probe fails because the endpoint does not support SSH exec requests
- **THEN** the failure appears in the health summary and target windows remain unchanged

### Requirement: Workspace status reporting
`status` SHALL report profile session presence, attached client count, broker state, target wrapper and child state, health-window state, and whether target ControlMasters appear available. It SHALL not capture or print pane contents, raw process arguments, item references, or runtime lease contents.

#### Scenario: Human-readable status
- **WHEN** the user runs `jumpotp status production`
- **THEN** the output distinguishes running, detached, stopped, failed, broker-unavailable, and probe-skipped states without exposing remote terminal content
