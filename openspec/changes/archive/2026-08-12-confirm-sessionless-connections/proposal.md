# Confirm Sessionless Connections

## Why

A sessionless target window produces no output once authentication succeeds: `ssh -N` opens no shell, so there is no prompt, banner, or motd. The wrapper prints `connecting` before dialing and prints nothing afterwards, so a healthy established master and a hung connection are visually identical. The first operator to use 0.2.0 reasonably read a working connection as stuck. The previous interactive-shell design carried this signal for free through the remote prompt; the sessionless redesign dropped it without replacing it.

The same window also retains the bastion's own echo of the submitted OTP. KoKo echoes the code it received back to the client, so the last line of an established target window is a plaintext six-digit code that stays on screen for the life of the workspace and inside tmux scrollback. JumpOTP never logs or prints codes itself, but leaving the endpoint's echo on a long-lived unattended window contradicts the same intent.

## What Changes

- Report a positive `master established` status line in the target window once the wrapper confirms a reusable ControlMaster for its alias after an attempt begins, naming the alias to use for interactive work. Confirmation comes from the existing `ssh -O check` probe, not from parsing remote output.
- Clear the target window's visible screen and scrollback at that same confirmed-establishment moment, replacing the authentication exchange (including the endpoint's echoed code) with the wrapper's own status line. Establishment is the only correct trigger: the endpoint's echo is a network round trip that arrives after the local submission write returns, so clearing at the write site would fire before the echo lands and leave the code on screen. Clearing is performed by writing terminal control sequences to the wrapper's own output stream; JumpOTP still never uses `capture-pane`, `send-keys`, or any tmux buffer operation to observe or manipulate an OTP.
- Preserve failure visibility: an attempt that never confirms a master never clears, so its exit, backoff, waiting, and manual-fallback lines remain the operator's evidence.

## Capabilities

### Modified Capabilities

- `workspace-management`: Target windows report established masters and present a cleared screen after authentication.
- `otp-assisted-ssh`: Confirmed establishment clears the authentication exchange, including the endpoint's echoed code, from the target window.

## Impact

- Affects the supervisor's status reporting and screen handling only, plus focused tests and user documentation. The terminal proxy is unchanged.
- No configuration, protocol, CLI surface, security boundary, or reconnection-policy change. Patch release (0.2.1).
- Does not change what `status` reports, how gating works, or any OTP handling other than clearing the screen after a submission completes.
