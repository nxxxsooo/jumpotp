## 1. Repository and Build Foundation

- [x] 1.1 Initialize a local Git repository without creating a remote, add Apache-2.0 licensing and ignore rules, and verify the initial tracked tree contains only synthetic public material.
- [x] 1.2 Initialize the Go module and the `cmd/jumpotp` plus internal package layout from the design.
- [x] 1.3 Add and pin the minimal CLI, strict-YAML, PTY, terminal, and platform-process dependencies; document Go and Node support and run clean dependency audits.
- [x] 1.4 Create deterministic build commands with one version source for `darwin-arm64`, `darwin-x64`, `linux-arm64`, and `linux-x64`.
- [x] 1.5 Add baseline CI for formatting, vetting, unit tests, race tests where supported, four-platform cross-builds, and synthetic-only fixtures.

## 2. CLI and Strict Configuration

- [x] 2.1 Implement the exact `connect`, `workspace`, `status`, `stop`, `doctor`, `config init`, `config validate`, `version`, and `help` command and argument contract with the specified exit classes.
- [x] 2.2 Implement XDG and `--config` discovery plus the explicit conflict between global `--config` and positional `config validate PATH`.
- [x] 2.3 Implement YAML node-level duplicate-key rejection followed by strict typed version-1 decoding with no tags, interpolation, merging, or unknown fields.
- [x] 2.4 Implement every top-level, defaults, profile, MFA, OTP, target, workspace-health, and probe field with the specified types, defaults, enums, bounds, and cross-reference validation.
- [x] 2.5 Implement launcher, OTP, fallback, item, target, manual-mode, health-order, and interval inheritance or override rules.
- [x] 2.6 Implement `config init` with a commented synthetic profile, the exact six-command Linux probe catalog, global health disabled, and overwrite protection.
- [x] 2.7 Implement stable redacted human errors and schema-versioned JSON for `version`, `config validate`, `doctor`, and `status`.
- [x] 2.8 Add table-driven configuration tests for valid examples, duplicate keys, unknown fields, every invalid enum and bound, item strings with spaces, missing references, path ambiguity, and deterministic error ordering.

## 3. OpenSSH, SSHM, and Diagnostics

- [x] 3.1 Implement argv-only `ssh <alias>` and optional `sshm <alias>` launchers with no local shell, free-form launcher, SSH argument fragments, or remote-command acceptance.
- [x] 3.2 Implement non-connecting `ssh -G` expansion diagnostics without claiming that OpenSSH proves a Host stanza exists.
- [x] 3.3 Implement dependency and compatible-version checks for `ssh`, `bw`, optional `sshm`, and workspace-only `tmux`.
- [x] 3.4 Implement non-mutating `doctor [PROFILE] [--json]` without Bitwarden authentication or TOTP retrieval, broker creation, SSH connection, or tmux mutation.
- [x] 3.5 Add fake-executable tests covering launcher selection, missing dependencies, invalid aliases, exact argv boundaries, SSHM process replacement, and redacted diagnostics.

## 4. OTP Provider and Prompt Matching

- [x] 4.1 Define the compile-time provider interface and implement bounded `bw get totp <item>` execution with stdout captured only in owned memory.
- [x] 4.2 Classify locked vault, unavailable CLI or server, missing item, ambiguous item, invalid output, timeout, and interruption without invoking login or unlock behavior.
- [x] 4.3 Implement `jumpserver-koko`, strict `generic-totp`, and bounded Go RE2 `custom` matchers; reject `generic-otp`, password, SMS, email, push, and WebAuthn near-matches.
- [x] 4.4 Implement ANSI-normalized, split-read-aware matching over a bounded rolling observer buffer while forwarding original PTY bytes unchanged.
- [x] 4.5 Implement numeric OTP validation, optional digit narrowing, and exact six-digit enforcement for `jumpserver-koko`.
- [x] 4.6 Implement the one-automatic-submission state machine and prohibit automatic retry after a rejected code.
- [x] 4.7 Add matcher and provider tests using marked synthetic codes and public prompt shapes without retaining raw transcripts.

## 5. Target PTY Wrapper

- [x] 5.1 Implement bidirectional PTY proxying with raw mode, ANSI fidelity, child exit propagation, resize propagation, signal forwarding, and terminal restoration on every catchable exit path.
- [x] 5.2 Implement direct provider retrieval after strict readiness and write the validated code plus newline only to the child PTY.
- [x] 5.3 Implement pane-local visible manual fallback by pausing input forwarding, restoring canonical echo, reading one bounded numeric line from the controlling terminal, and restoring proxy mode.
- [x] 5.4 Implement `--manual`, `fallback: fail`, no-controlling-TTY failure, and manual interruption behavior.
- [x] 5.5 Add real PTY integration fixtures for split prompts, ANSI prompts, resize, concurrent child output, provider delay, visible fallback, rejected codes, interrupts, child crashes, and post-authentication input.
- [x] 5.6 Add security assertions proving synthetic OTPs never appear in argv, environment, logs, structured errors, files, clipboard operations, or process diagnostics.

## 6. Ephemeral Workspace Broker

- [x] 6.1 Implement secure runtime-directory and mode-`0600` broker socket creation with no-follow checks, bounded path length, atomic ownership, and profile-scoped leases.
- [x] 6.2 Implement a versioned, length-bounded Unix-socket protocol for target readiness and in-memory OTP delivery using only profile ID, target ID, connection nonce, status codes, and code bytes.
- [x] 6.3 Resolve provider and item exclusively inside the broker from validated configuration so item references and session values never enter wrapper argv or tmux environment.
- [x] 6.4 Implement effective provider-item grouping, a short ready-member aggregation window, one retrieval per ready batch, fresh retrieval for late members, and one delivery per connection.
- [x] 6.5 Implement wrapper reconnection to a later broker, broker reuse for concurrent workspace invocations, and broker-loss transition to attended pane-local manual input without restarting SSH.
- [x] 6.6 Implement prompt readiness arbitration between broker delivery and manual terminal input while ensuring only one path can submit.
- [x] 6.7 Add broker tests for permissions, framing abuse, wrong profile or target, duplicate readiness, target override isolation, concurrent batches, provider failure, stale lease, disconnect, reconnect, interruption, and zero persistent OTP state.

## 7. Isolated Workspace and tmux Recovery

- [x] 7.1 Implement a JumpOTP-dedicated explicit tmux socket with bounded command contexts and validation of every returned session, window, pane, and client identifier.
- [x] 7.2 Implement one profile session with one wrapped target window per configured target and an optional health-summary window, rejecting unexpected existing panes instead of replacing them.
- [x] 7.3 Implement normal attachment with a broker-owning parent, same-server client switching with broker lifetime tracking, unrelated-tmux nesting refusal, detach persistence, and reattach.
- [x] 7.4 Implement `status [PROFILE] [--json]` and `stop PROFILE`, ensuring stop affects only the named session and never closes OpenSSH ControlMasters.
- [x] 7.5 Implement atomic mode-`0600` tmux server leases containing socket, UID, executable identity, PID, and OS process start identity, with Linux and Darwin identity readers.
- [x] 7.6 Implement absent-versus-unhealthy classification, bounded retries, full lease and process validation, graceful then forced termination of only one proven process, and atomic stale-socket quarantine.
- [x] 7.7 Refuse recovery on wrong ownership, symlinks, missing or conflicting leases, PID reuse, executable mismatch, multiple candidates, or unsupported identity inspection.
- [x] 7.8 Add isolated tmux tests for create, duplicate invocation, detach and reattach, same-server switching, nested refusal, partial target failure, stop, stale socket, graceful and forced recovery, ambiguous evidence, and preservation of the default tmux server.
- [x] 7.9 Assert workspace OTP paths never invoke `capture-pane`, `load-buffer`, `set-buffer`, `paste-buffer`, or `send-keys`.

## 8. Optional Health Probes

- [x] 8.1 Implement the exact six generated Linux definitions and strict configuration for enablement, editing, addition, deletion, ordering, interval, labels, platform hints, and reference validation.
- [x] 8.2 Implement one visible health-window scheduler that rotates effective probes across targets and exits with its profile session.
- [x] 8.3 Require successful `ssh -O check` before every probe and execute only with bounded `BatchMode=yes` behavior and one discrete trusted remote-command argument.
- [x] 8.4 Skip missing masters and surface unsupported remote-exec failures without contacting the OTP broker, initiating MFA, or modifying target panes.
- [x] 8.5 Add tests proving health is globally disabled in generated configuration, only enabled ordered definitions rotate, no local shell is used, no OTP is requested, and failures leave target windows unchanged.

## 9. npm and GitHub Release Packaging

- [x] 9.1 Create the dependency-free `jumpotp` JavaScript launcher with Node `>=22.14.0` and four platform package manifests with exact versions, `os`, `cpu`, `bin`, and executable permissions.
- [x] 9.2 Implement exact platform selection, inherited stdio and signals, exit propagation, and actionable errors for unsupported platforms or omitted optional dependencies.
- [x] 9.3 Add deterministic package generation and `npm pack` audits rejecting install lifecycle scripts, unexpected files, mismatched versions, missing binaries, incorrect modes, private data, and oversized artifacts.
- [x] 9.4 Add consumer tests that install locally packed packages on supported runners and execute the real `jumpotp version --json`, upgrade, repair, and uninstall paths.
- [x] 9.5 Add release checks for five-name availability, exact public repository URLs, Git tag and version agreement, GitHub-hosted runner identity, current npm Trusted Publishing minimums, and all five trust configurations.
- [x] 9.6 Add a stable OIDC workflow that builds and tests all binaries, publishes and verifies four platform packages and provenance first, and publishes the root package last without an npm token.
- [x] 9.7 Add GitHub Release packaging for the same four stable binaries or archives plus SHA256 manifest and downloaded-asset verification.

## 10. Documentation, Privacy, and Verification Gates

- [x] 10.1 Write English README and Chinese documentation covering threat model, same-user broker trust, authorized-use positioning, prerequisites, Bitwarden Password Manager versus standalone Authenticator, self-hosted CLI compatibility, exact commands, YAML, SSHM pairing, tmux detach and stop semantics, and visible fallback.
- [x] 10.2 Document npm install, update, uninstall, optional-dependency repair, prerelease removal, GitHub fallback, checksums, no telemetry, no automatic updates, and remote-echo limitations.
- [x] 10.3 Add SECURITY, CONTRIBUTING, CHANGELOG, Apache-2.0 notices, synthetic examples, and non-affiliation statements for Bitwarden, JumpServer, and SSHM.
- [x] 10.4 Implement a whole-tree scrub covering `openspec/`, tracked source, tests, docs, workflows, manifests, package tarballs, release archives, checksums, and candidate Git history with explicit synthetic allowlists.
- [x] 10.5 Add tests that the scrub blocks internal hosts, non-example IPs, private selectors, real item references, credentials, account mappings, realistic OTPs, deployment names, and operational data.
- [x] 10.6 Run strict OpenSpec validation, formatting, unit and race tests, PTY and broker tests, isolated tmux tests, cross-builds, npm pack/install audits, dependency review, privacy scrub, and manual artifact inspection.

## 11. Private Deployment Validation

- [x] 11.1 Build and install a development binary without changing any existing private adapter, then create one private YAML outside the repository using existing OpenSSH aliases and one existing Bitwarden item.
- [x] 11.2 With explicit service-access authorization, verify real direct connect, grouped workspace OTP, visible manual fallback, optional SSHM launcher, detach and reattach, ControlMaster reuse, opt-in probes, stop semantics, and validated broken-tmux recovery.
- [x] 11.3 Convert every discovered defect into a synthetic public regression test without copying private identifiers, prompt transcripts, configuration, or operational output into the repository.
- [x] 11.4 Record only redacted pass, fail, platform, version, and verification-category evidence; keep private configuration and detailed operational evidence outside the repository.

## 12. External Repository and Publication Gates

- [x] 12.1 With separate authorization, create a private remote repository and run CI without publishing, pushing tags, creating Releases, or changing visibility.
- [x] 12.2 Run the whole-tree and candidate-history scrub plus human review; with separate authorization, make the repository public before any provenance-bearing publication.
- [x] 12.3 Immediately recheck all five npm names; with separate interactive publication authorization, publish and verify functional `0.1.0-rc.0` platform packages under `next`, then publish and verify the root prerelease using account 2FA and no stored automation token.
- [x] 12.4 Configure and verify one exact Trusted Publisher for each existing npm package before preparing the stable tag.
- [x] 12.5 With separate stable-release authorization, preserve the failed `v0.1.0` tag and partial `v0.1.1` publication, publish `v0.1.2` platform packages then root through OIDC, create the matching GitHub Release, and verify provenance, versions, dist-tags, checksums, clean installation, update, and uninstall.
- [ ] 12.6 Only after stable private validation, migrate any external private compatibility adapter under a separate local-operation authorization and retain a reversible backup outside the public repository.
