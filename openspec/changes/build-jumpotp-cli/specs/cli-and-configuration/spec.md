## Purpose

Defines the stable JumpOTP command-line and complete configuration contract so users, scripts, launchers, and future private adapters can select SSH targets without duplicating OpenSSH configuration or storing credentials.

## ADDED Requirements

### Requirement: Stable command surface
JumpOTP SHALL expose the following v0.1 command forms and SHALL reject unknown commands, unknown flags, missing operands, and excess positional operands with exit code `2` and command-specific usage:

```text
jumpotp [--config PATH] connect PROFILE/TARGET [--launcher ssh|sshm] [--manual]
jumpotp [--config PATH] workspace PROFILE [--launcher ssh|sshm] [--manual]
jumpotp [--config PATH] status [PROFILE] [--json]
jumpotp [--config PATH] stop PROFILE
jumpotp [--config PATH] doctor [PROFILE] [--json]
jumpotp [--config PATH] config init [--force]
jumpotp [--config PATH] config validate [PATH] [--json]
jumpotp version [--json]
jumpotp help [COMMAND]
```

#### Scenario: Connect to an explicit target
- **WHEN** the user runs `jumpotp connect production/app-01`
- **THEN** JumpOTP resolves profile `production` and target `app-01` and starts one interactive connection

#### Scenario: Create or reattach a workspace
- **WHEN** the user runs `jumpotp workspace production`
- **THEN** JumpOTP creates the profile workspace if absent or attaches to the existing profile workspace if present

#### Scenario: Reject unsupported remote command arguments
- **WHEN** the user appends a remote command after a `connect` target
- **THEN** JumpOTP exits with code `2` because remote command execution is not part of the v0.1 interactive connection contract

### Requirement: Unambiguous profile and target addressing
Interactive connections SHALL use `PROFILE/TARGET` addressing. Profile names, target names, and configured OpenSSH aliases MUST match `[A-Za-z0-9][A-Za-z0-9._-]*`, and each target name MUST be unique within its profile.

#### Scenario: Duplicate target names
- **WHEN** a profile contains two targets with the same name
- **THEN** configuration validation fails before any external process starts

#### Scenario: Missing profile
- **WHEN** a user addresses a profile that is not configured
- **THEN** JumpOTP exits with code `2` and names the missing profile without printing configuration secrets

### Requirement: Complete strict YAML version-1 schema
JumpOTP SHALL accept exactly the following YAML version-1 field model and SHALL reject unknown fields, duplicate mapping keys, invalid types, empty required strings, invalid enums, invalid references, and unsupported versions before starting any external process:

```yaml
version: 1

defaults:
  launcher: ssh
  otp:
    provider: bitwarden
    fallback: prompt

probes:
  - id: uptime
    enabled: true
    label: Time, load, and uptime
    platform: linux
    command: "date '+%F %T %Z'; uptime"

profiles:
  production:
    launcher: ssh
    mfa:
      preset: jumpserver-koko
    otp:
      provider: bitwarden
      item: Example Login
      fallback: prompt
    targets:
      app-01:
        ssh: production-app-01
      app-02:
        ssh: production-app-02
        otp:
          item: Example Login Override
    workspace:
      health:
        enabled: false
        interval: 4m
        commands:
          - uptime
```

The allowed top-level fields SHALL be:

- `version`: required integer and exactly `1`.
- `defaults`: optional mapping containing only `launcher` and `otp`.
- `probes`: optional ordered sequence of probe definitions.
- `profiles`: required nonempty mapping keyed by profile identifier.

`defaults.launcher` SHALL be `ssh` or `sshm` and SHALL default to `ssh`. `defaults.otp` SHALL contain only optional `provider` and `fallback`; the only v0.1 provider is `bitwarden`, and fallback SHALL be `prompt` or `fail` with default `prompt`.

Each profile SHALL contain only `launcher`, `mfa`, `otp`, `targets`, and `workspace`. `mfa`, `otp.item`, and a nonempty `targets` mapping are required. Profile `otp` SHALL contain only `provider`, `item`, and `fallback`, with provider and fallback inheriting their defaults when omitted. `mfa` SHALL contain `preset`, optional `pattern`, and optional `digits`; `preset` SHALL be `jumpserver-koko`, `generic-totp`, or `custom`; `pattern` is required only for `custom`; and `digits`, when present, SHALL be an integer from 5 through 10. The `jumpserver-koko` preset SHALL reject any `digits` value other than 6.

Each target SHALL contain only required `ssh` and optional `otp`. Target `otp` SHALL contain only `item`, allowing the profile item to be replaced for that target. Bitwarden item references are nonempty literal strings and MAY contain spaces; JumpOTP SHALL perform no shell, environment-variable, or template expansion on them.

Each probe definition SHALL contain exactly `id`, `enabled`, `label`, `platform`, and `command`; IDs are unique identifiers, `platform` SHALL be `linux`, `darwin`, or `any`, and `command` is a nonempty trusted remote-command string. Profile `workspace.health` SHALL contain only `enabled`, `interval`, and `commands`; `enabled` defaults to `false`, `interval` defaults to `4m` and MUST be from `30s` through `24h`, and `commands` is an ordered unique sequence of defined, enabled probe IDs. Omission of `commands` SHALL select all globally enabled probes in definition order.

#### Scenario: Valid complete configuration
- **WHEN** a version-1 document satisfies the field model and all cross-references
- **THEN** `jumpotp config validate` exits successfully without opening a network connection

#### Scenario: Misspelled field
- **WHEN** the configuration contains `launcer: ssh`
- **THEN** validation fails and identifies `launcer` as an unknown field

#### Scenario: Duplicate YAML key
- **WHEN** one profile contains two `targets` keys
- **THEN** validation fails instead of silently using either value

#### Scenario: Invalid probe reference
- **WHEN** a profile health command references a probe ID absent from `probes` or disabled there
- **THEN** validation fails before tmux or SSH starts

### Requirement: Configuration discovery and path disambiguation
JumpOTP SHALL use `--config PATH` when supplied; otherwise it SHALL load `${XDG_CONFIG_HOME}/jumpotp/config.yaml` when `XDG_CONFIG_HOME` is set and `~/.config/jumpotp/config.yaml` otherwise. `config init` SHALL write a commented, synthetic, secret-free version-1 example and SHALL refuse to overwrite an existing file unless `--force` is supplied. `config validate PATH` SHALL validate the positional path without changing discovery state, and using both that positional path and global `--config` SHALL fail with exit code `2`.

#### Scenario: Explicit configuration path
- **WHEN** the user supplies `--config /path/to/team.yaml`
- **THEN** JumpOTP uses only that file and does not merge it with the default configuration

#### Scenario: Ambiguous validation paths
- **WHEN** the user supplies both `--config first.yaml` and `config validate second.yaml`
- **THEN** JumpOTP exits with code `2` and names the conflicting path sources

#### Scenario: Existing configuration protection
- **WHEN** `config init` targets an existing file without `--force`
- **THEN** JumpOTP exits nonzero without changing the file

### Requirement: Profiles, inheritance, and invocation overrides
A target SHALL inherit the profile OTP item, provider, fallback, MFA matcher, and effective launcher. A target OTP item SHALL override only the inherited item. Profile launcher SHALL override the global launcher default, and CLI `--launcher` SHALL override the profile launcher for that invocation. CLI `--manual` SHALL affect every target created by that invocation without changing configuration.

#### Scenario: Profile OTP inherited by targets
- **WHEN** two targets omit target-level OTP configuration
- **THEN** both inherit the profile OTP item and belong to the same OTP retrieval group

#### Scenario: Target OTP override
- **WHEN** a target declares a different Bitwarden item
- **THEN** only that target uses the override and it belongs to a separate OTP retrieval group

#### Scenario: SSHM launcher selected
- **WHEN** the effective launcher is `sshm`
- **THEN** JumpOTP launches `sshm <ssh-alias>` and reports a missing `sshm` executable as dependency exit code `3`

### Requirement: OpenSSH remains authoritative
JumpOTP MUST NOT store or synthesize SSH hostnames, usernames, ports, private-key paths, passwords, ProxyJump rules, ControlPath values, or agent configuration. It SHALL pass the configured alias to the selected launcher as a discrete argument without local shell evaluation.

#### Scenario: Non-connecting alias expansion
- **WHEN** `doctor` checks a configured target
- **THEN** it runs a non-connecting OpenSSH expansion check and reports whether expansion succeeded without claiming that OpenSSH proves the alias was explicitly declared

#### Scenario: Shell metacharacters in an alias
- **WHEN** an alias violates the identifier grammar or would require shell interpretation
- **THEN** configuration validation rejects it before launcher execution

### Requirement: Diagnostics are non-mutating by default
`doctor` SHALL validate configuration, executable availability, launcher compatibility, OpenSSH expansion, and workspace prerequisites without logging into Bitwarden, unlocking a vault, obtaining a TOTP, opening SSH connections, creating a broker, or changing tmux state.

#### Scenario: Locked Bitwarden vault during doctor
- **WHEN** the Bitwarden vault is locked but the `bw` executable exists
- **THEN** `doctor` reports the executable as available and does not attempt an unlock or TOTP retrieval

### Requirement: Stable machine-readable output
`status`, `doctor`, `config validate`, and `version` SHALL support `--json`, emit exactly one JSON object to stdout, use `schema_version: 1`, and send human diagnostics to stderr. JSON output MUST exclude OTP values, prompt transcripts, passwords, Bitwarden session values, raw item references, and raw SSH expansion.

#### Scenario: No workspace server
- **WHEN** `jumpotp status --json` runs with no JumpOTP tmux server
- **THEN** it exits successfully with `{"schema_version":1,"workspaces":[]}` plus no unrelated stdout

#### Scenario: Invalid configuration JSON
- **WHEN** `config validate --json` detects multiple errors
- **THEN** it emits one object containing `schema_version`, `valid: false`, and a stable ordered `errors` array of redacted code, path, and message entries

### Requirement: Stable exit classes
JumpOTP SHALL use exit code `0` for success, `2` for usage, selection, or configuration errors, `3` for missing or incompatible local dependencies, `4` for MFA or interactive-input failure, `5` for workspace or tmux failure, and `130` for user interruption.

#### Scenario: Interrupted connection
- **WHEN** the user sends an interrupt during an interactive connection
- **THEN** JumpOTP restores terminal state, terminates the child process as appropriate, and exits with code `130`
