<p align="center">
  <a href="#jumpotp">English</a> · <a href="docs/zh-CN.md">简体中文</a>
</p>

![JumpOTP. A short-lived OTP signal crossing a controlled SSH boundary.](docs/assets/jumpotp-hero.webp)

<h1 id="jumpotp" align="center">JumpOTP</h1>

<p align="center"><strong>One prompt. One code. One authorized SSH session.</strong></p>

<p align="center">
  <a href="https://github.com/nxxxsooo/jumpotp/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/nxxxsooo/jumpotp/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://www.npmjs.com/package/jumpotp"><img alt="npm" src="https://img.shields.io/npm/v/jumpotp?label=npm"></a>
  <a href="https://github.com/nxxxsooo/jumpotp/blob/main/LICENSE"><img alt="Apache-2.0" src="https://img.shields.io/badge/license-Apache--2.0-c27a18"></a>
</p>

<p align="center">
  <a href="https://mjshao.fun/jumpotp/">Website</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#threat-model">Threat model</a>
</p>

JumpOTP assists **authorized interactive SSH sessions** that request a
time-based one-time password (TOTP). It observes a narrow configured prompt,
asks an already authenticated Bitwarden Password Manager CLI for the current
code, and submits that code at most once. An optional isolated tmux workspace
opens several configured SSH aliases without taking ownership of SSH
configuration or ControlMaster lifetime.

> JumpOTP reduces the separation between a password manager and a second
> factor. Use it only on systems you are authorized to access, understand the
> threat model below, and choose `--manual` or `fallback: fail` when stronger
> factor separation matters.

## Install

```sh
npm install -g jumpotp
jumpotp version
```

Supported in v0.1:

- macOS: arm64 and x64
- Linux: arm64 and x64
- Node.js 22.14.0 or newer for the npm launcher

The npm package performs no install-time download or compilation. If optional
dependencies were omitted:

```sh
npm install -g jumpotp --include=optional
```

GitHub Release binaries are the manual fallback. Download the matching asset
and verify it against `SHA256SUMS` before installation.

Update or uninstall:

```sh
npm install -g jumpotp@latest
npm uninstall -g jumpotp
```

To remove a prerelease:

```sh
npm uninstall -g jumpotp
npm install -g jumpotp@latest
```

## Prerequisites

- OpenSSH with all hosts, users, ports, keys, ProxyJump, and ControlMaster
  behavior already expressed as named aliases in `~/.ssh/config`.
- Bitwarden Password Manager CLI (`bw`) already logged in or unlocked in the
  invoking environment.
- Optional SSHM for `launcher: sshm`.
- tmux only for `workspace`.

`bw get totp` reads integrated authenticator data stored on a Bitwarden
Password Manager item. JumpOTP does not read standalone Bitwarden Authenticator
app storage, export a TOTP seed, log in, unlock a vault, manage `BW_SESSION`,
or change a self-hosted Bitwarden server URL.

## Quick start

```sh
jumpotp config init
$EDITOR ~/.config/jumpotp/config.yaml
jumpotp config validate
jumpotp doctor production
jumpotp connect production/app-01
```

All public commands:

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

## Configuration

JumpOTP reads strict YAML version 1. Unknown fields, duplicate keys, invalid
references, YAML aliases, and unsupported values fail before any external
process starts.

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

Matcher presets are `jumpserver-koko`, `generic-totp`, and `custom`.
Custom patterns use bounded Go RE2 syntax. There is deliberately no broad
`generic-otp` matcher.

The default configuration path is
`${XDG_CONFIG_HOME:-~/.config}/jumpotp/config.yaml`. An explicit
`--config` selects one file; files are never merged or interpolated.

## Direct connection and visible fallback

`connect` runs `ssh <alias>` or `sshm <alias>` in a real PTY. ANSI output,
terminal resizing, signals, and child exit status pass through. Before an
automatic connection, JumpOTP makes a best-effort readiness check by running
exactly `bw status`. The check accepts only bounded valid JSON reporting an
unlocked vault, uses the same fixed 20-second deadline as TOTP retrieval, and
never includes an item reference or requests a code. A readiness failure emits
one redacted warning and continues so the configured `prompt` or `fail`
behavior remains authoritative. `--manual` skips both the check and retrieval.

When readiness or prompt-time retrieval fails, JumpOTP appends only a bounded,
low-resolution elapsed time to the redacted stage-specific reason. Durations
below one second appear as `<1s`; longer failures use the nearest whole second,
capped at the 20-second provider deadline. Successful operations remain silent,
and timing never includes item references, command output, session data, or
details from inside a `bw` wrapper.

JumpOTP requests a TOTP only after the configured complete prompt matches.

If retrieval fails and fallback is `prompt`, JumpOTP explains the failure,
temporarily restores normal terminal echo, and lets you type visible digits.
It never reads a fallback code from piped stdin. Use `fallback: fail` to exit
instead, or `--manual` to skip Bitwarden for that invocation.

## Workspace and tmux lifecycle

`workspace PROFILE` uses a dedicated tmux socket, one session per profile,
one wrapped SSH window per target, and an optional health window. It never
lists, modifies, or kills the default tmux server.

- Detach with normal tmux controls; the SSH windows remain.
- Run the same command to reattach.
- Closing a terminal tab does not stop the workspace.
- `jumpotp stop PROFILE` stops only that profile session.
- Stop does not issue `ssh -O exit` or remove ControlMaster sockets.

An ephemeral current-user Unix-socket broker groups targets that use the same
Bitwarden item. The broker exists only while a workspace command is active.
Target panes remain usable if it disappears and can accept visible manual
input or reconnect to a later broker. Starting a new automatic workspace runs
the same best-effort readiness check before creating targets. Reattaching to a
workspace with a validated active broker skips it; ambiguous broker state is
left to the existing fail-closed lifecycle checks.

Workspace provider failures carry the same elapsed-time bucket as an optional
bounded number in the existing broker protocol. Broker messages are not used
as diagnostics, and old version-one peers remain compatible when the field is
absent or ignored.

Health probes are disabled by default. When enabled they first require
`ssh -O check`, use `BatchMode=yes`, rotate in a separate health window,
and never inject commands into interactive panes.

## Threat model

JumpOTP assumes:

- the local user account and processes running as that user are trusted;
- OpenSSH host-key verification and alias configuration remain authoritative;
- the selected endpoint is authorized and expected to request TOTP.

Mitigations include narrow prompt matchers, explicit alias-to-item binding,
one automatic submission per connection, no TOTP seeds, no tmux paste buffers,
mode-0700 runtime directories, mode-0600 sockets and leases, bounded process
execution, and fail-closed tmux recovery.

JumpOTP cannot prevent the remote endpoint from visibly echoing a submitted
code. It collects no telemetry and performs no automatic update check.

## Development

Building from source requires Go 1.25 or newer and Node.js 22.14.0 or
newer for npm package verification.

```sh
go test ./...
go test -race ./...
./scripts/build.sh
node scripts/prepare-packages.mjs
node scripts/audit-packages.mjs
node scripts/test-packages.mjs
./scripts/scrub.sh
openspec validate build-jumpotp-cli --type change --strict --no-interactive
```

See [SECURITY.md](SECURITY.md), [CONTRIBUTING.md](CONTRIBUTING.md), and the
[Chinese guide](docs/zh-CN.md).

## Non-affiliation

JumpOTP is an independent project and is not affiliated with, endorsed by, or
sponsored by Bitwarden, JumpServer, SSHM, OpenSSH, or tmux.

## License

Apache-2.0.
