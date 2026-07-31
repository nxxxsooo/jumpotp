# Contributing

Contributions must use synthetic examples and must never contain private
infrastructure, vault references, credentials, realistic one-time codes, or
terminal transcripts.

The supported development toolchain is Go 1.25 or newer and Node.js 22.14.0
or newer.

Before opening a pull request:

```sh
gofmt -w cmd internal
go vet ./...
go test ./...
go test -race ./...
./scripts/build.sh
node scripts/prepare-packages.mjs
node scripts/audit-packages.mjs
node scripts/test-packages.mjs
./scripts/scrub.sh
```

Behavior changes require an OpenSpec change with testable scenarios. Keep
OpenSSH and Bitwarden authoritative; do not add arbitrary local command
execution, install scripts, telemetry, or automatic updates.
