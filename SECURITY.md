# Security Policy

## Supported versions

Security fixes are provided for the latest published minor release. Before the
first stable release, only the current source tree is supported.

## Reporting a vulnerability

Do not include credentials, TOTP seeds, current OTP values, private hostnames,
private SSH aliases, vault item names, or terminal transcripts in a public
issue. Use GitHub private vulnerability reporting when available.

Include a synthetic reproducer, affected version and platform, expected
security boundary, and observed impact. Reports concerning unauthorized access
or credential theft are not accepted as product use cases.

## Security boundaries

JumpOTP trusts the current operating-system user and does not defend against
malware already executing as that user. It relies on OpenSSH host-key
verification and the existing Bitwarden CLI session. It does not store TOTP
seeds, manage vault authentication, or guarantee that a remote terminal will
not echo a submitted code.

Automatic tmux recovery signals a process only after socket, owner, runtime
lease, PID, process start identity, and executable identity agree. Conflicting
evidence fails closed.
