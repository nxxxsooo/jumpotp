## MODIFIED Requirements

### Requirement: Supervised target reconnection
The target wrapper SHALL supervise its launcher child. When the child exits for any reason other than an operator stop, the wrapper SHALL reconnect with exponential backoff starting at 5 seconds, doubling to a 300-second ceiling, with bounded jitter, and SHALL reset per-connection submission state for each attempt. Before dialing, the wrapper SHALL require either a confirmed reusable ControlMaster for the alias or a validated active profile broker; while neither is available it SHALL wait and re-check on a bounded interval without initiating SSH authentication. SIGTERM, SIGHUP, and interrupt SHALL end the wrapper without reconnecting. The wrapper SHALL print redacted connection, established, exit, backoff, and waiting states to its own window.

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

## ADDED Requirements

### Requirement: Established sessionless connections are reported
A sessionless target produces no remote output once authentication succeeds, so the wrapper SHALL confirm establishment itself and report it. While an attempt is running, the wrapper SHALL probe for a reusable ControlMaster on a bounded interval using the same read-only `ssh -O check` mechanism used by the reconnect gate, and on first confirmation SHALL print one status line naming the target and the alias to use for interactive work. It SHALL report establishment at most once per attempt and MUST NOT infer establishment from remote terminal output. An attempt that never reaches a confirmed master SHALL leave its existing exit, backoff, or waiting lines as the last state on screen.

#### Scenario: Sessionless master reports itself
- **WHEN** a target wrapper authenticates and its sessionless launcher child holds the connection without producing further output
- **THEN** the window shows a status line confirming the master is established and naming the alias for interactive use, instead of ending on the authentication exchange

#### Scenario: Failed attempt is not reported as established
- **WHEN** an attempt ends before a ControlMaster can be confirmed
- **THEN** no establishment line is printed and the exit and backoff lines remain the last visible state

#### Scenario: Reported once per attempt
- **WHEN** a confirmed master remains available for the life of an attempt
- **THEN** the wrapper prints the establishment line once and does not repeat it on later probes
