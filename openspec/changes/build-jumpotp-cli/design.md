## Context

See `proposal.md` for motivation. This is a new public project informed by a private shell proof of concept that validated the user flow but also exposed failure modes around shared tmux servers, pane inspection, shell error propagation, OTP buffers, and deployment-specific constants.

The design must preserve OpenSSH and Bitwarden as authorities. JumpOTP receives SSH aliases and Bitwarden item references, but never becomes an SSH configuration manager, credential store, TOTP implementation, or Bitwarden session manager. All public code, planning artifacts, fixtures, documentation, packages, archives, and candidate history must remain synthetic.

## Goals / Non-Goals

**Goals:**

- Deliver one Go-native target wrapper that safely proxies an interactive PTY, detects a strict MFA prompt, obtains a broker-generated or direct provider-generated OTP, and supports visible manual entry.
- Compose those wrappers into an optional persistent multi-target tmux workspace without observing pane contents or touching the user's default tmux server.
- Group concurrently ready workspace targets through an ephemeral current-user Unix-socket broker without tmux paste buffers or persistent OTP state.
- Make every external execution argv-based, timeout-aware, and testable through synthetic fake executables.
- Make npm the primary installation path through a dependency-free JavaScript platform selector and prebuilt Go binaries.

**Non-Goals:**

- Managing SSH configuration, SSH keys, passwords, ProxyJump rules, agents, ControlMaster lifetime, Bitwarden login or unlock, or self-hosted Bitwarden setup.
- Supporting non-Bitwarden providers, Windows, WebAuthn, push, SMS, email OTP, ambiguous generic OTP prompts, remote command execution through `connect`, file transfer, tunnels, or embedded SSHM TUI selection in v0.1.
- Reimplementing TOTP generation, shipping a secret seed, capturing target pane contents, or persisting OTPs.
- Shipping or documenting any private deployment profile or compatibility adapter in the public repository.

## Decisions

### 1. Repository and process architecture

Use one repository containing:

```text
cmd/jumpotp/                 Go entry point
internal/cli/                command parsing and output contracts
internal/config/             strict YAML schema and validation
internal/launcher/           ssh and sshm argv builders
internal/provider/           provider interface and Bitwarden implementation
internal/mfa/                prompt presets and bounded stream matching
internal/terminal/           PTY proxy, resize, signals, echo transitions
internal/broker/             workspace readiness and in-memory OTP grouping
internal/runtime/            secure directories, leases, process identity
internal/workspace/          dedicated tmux server and session lifecycle
internal/probes/             optional catalog and scheduler
internal/doctor/             non-mutating dependency diagnostics
packages/npm/jumpotp/        root JavaScript launcher package
packages/npm/platform/*/     four native npm packages
scripts/                     deterministic build, pack, scrub, and release checks
```

The Go process calls external executables through `os/exec` with discrete arguments. Narrow runner and clock interfaces exist only at process, terminal, filesystem, and time boundaries so tests can substitute fakes. Provider and launcher interfaces are compile-time extension points; v0.1 has no plugin runtime or arbitrary command provider.

Alternatives rejected:

- Bash core: direct PTY restoration, broker framing, bounded timeouts, and safe process recovery are too fragile.
- Embedded SSH library: it would duplicate OpenSSH configuration, agent, and vendor-specific bastion behavior.
- Arbitrary provider command: it turns trusted configuration into a local command-execution surface.

### 2. CLI parsing and exact strict configuration

Implement exactly the public command forms and YAML field model in `cli-and-configuration/spec.md`. Use a Go YAML decoder that exposes nodes so duplicate keys can be rejected before typed strict decoding. Validate the entire document and every cross-reference before starting a child process.

Configuration resolution is:

1. Explicit global `--config`.
2. `$XDG_CONFIG_HOME/jumpotp/config.yaml`.
3. `~/.config/jumpotp/config.yaml`.

`config validate PATH` is a one-shot positional override and conflicts with global `--config`. Do not merge files, evaluate YAML tags, expand environment variables, or interpolate shell text. Preserve probe ordering with slices rather than maps. `config init` writes the full six-command synthetic Linux catalog and health remains disabled.

### 3. Launcher composition with OpenSSH and SSHM

The launcher interface returns executable plus argv:

- `ssh`: `ssh <alias>`.
- `sshm`: `sshm <alias>`.

SSHM direct-target mode is optional and is treated as compatible only when its process replacement preserves the parent PTY. Target selection remains explicit because an embedded SSHM TUI would not reveal the selected alias early enough for target-level OTP resolution. `doctor` uses `ssh -G <alias>` only as a non-connecting expansion check and does not claim it proves a Host stanza exists.

Do not accept free-form launcher commands, SSH argument fragments, or remote commands in direct connection configuration. Users express SSH behavior in OpenSSH configuration.

### 4. One target wrapper owns PTY and manual fallback

Direct connect starts the launcher in a pseudo-terminal, places the controlling terminal in raw mode, and copies bytes bidirectionally without rewriting ANSI output. Window-size changes and termination signals propagate to the child. Restoration is registered immediately after raw mode succeeds and runs on every catchable exit path.

The observer keeps a bounded normalized matching buffer separate from displayed bytes. It strips or normalizes terminal control sequences for comparison, handles prompts split across reads, and recognizes only the configured preset or bounded RE2 pattern. The connection state machine is:

```text
waiting → matched → provider-or-broker/manual → submitted → passthrough
```

A connection can enter `submitted` automatically only once. Password prompts and unmatched output remain untouched.

For visible manual input, pause user-to-child copying, restore canonical mode and echo on the controlling terminal, print a redacted explanation, read one bounded numeric line from that terminal, return to raw mode, and resume copying. Workspace target panes run this identical wrapper, so manual behavior belongs to the affected pane rather than the outer workspace process.

### 5. Ephemeral broker replaces pane capture and tmux buffers

Each target pane runs a hidden internal wrapper command with only the config path, profile ID, target ID, launcher choice, and broker socket path in argv. It never receives an OTP, item reference, or Bitwarden session in argv or environment.

The public `workspace PROFILE` process validates configuration, creates or validates one profile broker socket, and owns Bitwarden retrieval. A wrapper that reaches its MFA prompt sends a framed readiness message containing only protocol version, profile ID, target ID, and connection nonce. The broker maps that identifier to the already validated effective provider and item, briefly aggregates ready targets with the same group key, calls `bw get totp` once, and returns a length-delimited code over each authenticated current-user socket connection.

Socket framing has fixed size limits and deadlines. Runtime directory ownership and socket permissions are the trust boundary; the threat model assumes processes already running as the same local user are trusted. Codes never enter tmux commands, tmux buffers, pane capture, files, or logs.

While no broker exists, a wrapper at an MFA prompt waits for either a new broker connection or attended manual input. A later `workspace` invocation recreates the stable profile socket and wrappers reconnect without restarting SSH. `--manual` bypasses broker registration.

Alternatives rejected:

- `capture-pane` plus `load-buffer`: it observes remote screen contents and cannot guarantee buffer deletion after `SIGKILL`.
- Independent `bw` calls inside every pane: tmux would have to retain or refresh Bitwarden session environment and concurrent grouping would be lost.
- A permanent daemon: OTP brokering is needed only while the workspace command is active.

### 6. Workspace attachment and broker lifetime

Use one dedicated tmux server socket and one ordinary session per profile, with one target-wrapper window per target and an optional health-summary window.

Outside tmux, keep the broker in the parent process, start `tmux attach-session` as a child with inherited terminal I/O, and close the broker when attachment ends. Inside the same JumpOTP server, identify the current client, execute `switch-client`, and keep the invoking process blocked as broker owner until that client leaves the requested session or the command is interrupted. Inside another tmux server, refuse nesting.

The session and target wrappers persist after broker or terminal loss. A second invocation may use an existing validated broker rather than race it. Broker leases and connection nonces prevent accidental cross-profile routing; ambiguous broker ownership fails closed.

### 7. Dedicated tmux runtime leases and bounded recovery

Use an explicit tmux socket under a secure runtime directory:

1. A current-user-owned, mode-`0700` `$XDG_RUNTIME_DIR/jumpotp` when available.
2. Otherwise a no-symlink, current-user-owned, mode-`0700` `jumpotp-<uid>` directory under the platform temporary directory.

Create files with no-follow and exclusive semantics, validate every ancestor controlled by JumpOTP, and keep Unix socket paths below platform length limits.

After starting tmux, query its server PID and write an atomic mode-`0600` lease containing canonical socket path, UID, canonical tmux executable identity, PID, and OS process start identity. Linux reads UID, executable, and start ticks from `/proc`; Darwin uses native process metadata behind a build-tagged implementation. Broker leases use the same PID/start-identity protection but contain no executable or configuration data.

All tmux commands use the explicit socket and bounded context deadlines. A missing server is empty state. For an unhealthy server, retry first. Recovery may signal only when socket ownership, lease, UID, PID, start identity, and executable identity agree. Try graceful termination, wait, then force only that validated PID. Quarantine the single stale socket and lease by atomic rename; never delete a runtime tree or use process-name matching. Conflicting evidence yields manual guidance.

### 8. Health probes are opt-in, visible, and reuse-only

`config init` writes the exact six Linux definitions from the workspace spec, each individually editable, while profile health remains disabled. A profile chooses an ordered list of enabled IDs. The optional health window runs one visible internal scheduler process that rotates target and probe, prints bounded redacted results, and exits with its profile session.

Before each probe, execute `ssh -O check <alias>`. Only success permits `ssh -o BatchMode=yes -o ConnectTimeout=<n> <alias> <remote-command>`. The trusted remote command is one argv element passed to OpenSSH and is never evaluated by a local shell. Missing masters and unsupported remote exec are skipped or failed without calling the broker or touching target panes.

### 9. State, logging, and secret lifetime

Persistent user-authored state is only YAML. Runtime state is limited to tmux's dedicated socket/session state plus non-secret leases and broker sockets. No database or credential cache exists.

Human and JSON diagnostics identify only public state categories and configured profile/target IDs. Bitwarden item references are redacted. OTP values live in the smallest practical byte slices, are overwritten when practical, and never enter structured errors. The remote endpoint may echo submitted characters; documentation must state that terminal display is outside JumpOTP's secrecy guarantee.

No telemetry, background update service, or automatic update check exists.

### 10. npm-first packaging and truthful first release

Build reproducible binaries for `darwin-arm64`, `darwin-x64`, `linux-arm64`, and `linux-x64`. Each platform npm package contains one pre-permissioned executable plus `os` and `cpu` metadata. The root package contains a dependency-free Node launcher, requires Node `>=22.14.0`, and declares exact-version optional dependencies on all four packages.

There are two release phases:

1. **Bootstrap:** after separate authorization, make the scrubbed repository public, build functional `0.1.0-rc.0` artifacts, publish four platform packages interactively with account 2FA under `next`, verify them, then publish the root prerelease. This establishes real packages without empty reservations or stored automation tokens.
2. **Stable:** configure one exact Trusted Publisher per package, then use a GitHub-hosted OIDC workflow to publish and verify four `0.1.0` platform packages before publishing the root package. Provenance must resolve to the public release commit.

GitHub Release receives the same four stable artifacts plus a SHA256 manifest. Any immutable partial npm release is reported rather than overwritten or hidden.

### 11. Verification and privacy are release inputs

Synthetic fake executables cover SSH, SSHM, Bitwarden, tmux, process identity, and prompt variants. PTY integration tests run real pseudo-terminals for raw mode, visible fallback, resize, interrupts, crashes, split ANSI prompts, and broker loss. Isolated tmux tests use a test-only socket and process namespace.

The privacy scrub runs before public visibility, bootstrap, and stable publication. It scans the complete tracked tree including `openspec/`, package tarballs, release archives, checksums, workflows, and candidate history. Only explicitly allowlisted documentation domains, reserved example IP ranges, and marked synthetic OTP fixtures pass. A final human inspection remains required because regex scanning cannot prove semantic privacy.

## Risks / Trade-offs

- **[Automating a second factor reduces factor separation]** → Document the threat model; never store seeds or manage Bitwarden authentication; provide `--manual` and `fallback: fail`.
- **[A compromised endpoint can imitate an MFA prompt]** → Preserve OpenSSH host-key verification, bind items to explicit aliases, use narrow matchers, and submit once.
- **[Same-user malware can access broker traffic or Bitwarden state]** → State the same-user trust boundary; use mode-`0700` runtime directories, mode-`0600` sockets and leases, short deadlines, and no persistent code state.
- **[Prompt text varies across endpoint versions]** → Keep presets narrow, support bounded custom RE2 patterns, and maintain only synthetic or public prompt fixtures.
- **[TOTP rotates between retrieval and verification]** → Retrieve after readiness, keep aggregation brief, never automatically retry, and return control on rejection.
- **[Manual echo briefly changes terminal mode]** → Pause proxy copying, bound the read, restore mode on every catchable exit, and test interrupts and child output races.
- **[Automatic tmux recovery can destroy healthy workspaces]** → Isolate the socket, record PID start identity, retry first, and refuse recovery on any ambiguity.
- **[Broker loss while detached prevents automatic reauthentication]** → Keep wrappers and SSH alive, allow broker reconnection on the next workspace invocation, and retain pane-local manual input.
- **[Remote health commands are not portable]** → Disable globally, label platform, require a live ControlMaster, and allow full catalog editing.
- **[Five npm packages complicate atomicity]** → Bootstrap and stable platform packages first, publish root last, and report partial immutable state.
- **[Package names may be claimed before bootstrap]** → Recheck all five immediately before mutation and return naming to review if any is unavailable.
- **[Privacy scanners have false negatives]** → Scan every artifact and candidate history, use synthetic data only, and require human review before public visibility.

## Migration Plan

1. Initialize the local Git repository and implement entirely with synthetic configuration, fixtures, hosts, item references, and OTPs.
2. Run unit, race, PTY, broker, isolated tmux, cross-build, npm pack/install, and privacy tests locally.
3. With separate authorization, create a private remote repository and run private CI without publishing.
4. Create one private deployment configuration outside the repository and, with separate service-access authorization, validate real `connect`, `workspace`, fallback, detach/reattach, launcher composition, probes, ControlMaster reuse, and recovery. Keep existing private adapters unchanged during this validation.
5. Fix defects in the public implementation using synthetic regression fixtures; never copy private transcripts or identifiers into the repository.
6. Run whole-tree and candidate-history scrub plus human review. With separate authorization, make the repository public.
7. Recheck package names; with separate publication authorization, perform the functional `0.1.0-rc.0` interactive bootstrap and configure all five Trusted Publishers.
8. Publish stable `v0.1.0` through OIDC, create the matching GitHub Release, and verify provenance, checksums, installation, update, and uninstall.
9. Only after a stable private validation may an external private adapter be migrated. Rollback restores that adapter and uninstalls JumpOTP without changing SSH configuration, Bitwarden state, or ControlMaster sockets.

## Open Questions

None. Remote creation, public visibility, service access, interactive package bootstrap, stable publication, and private adapter migration are authority gates rather than unresolved product behavior.
