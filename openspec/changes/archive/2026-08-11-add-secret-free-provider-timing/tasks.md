## 1. Provider Timing And Rendering

- [x] 1.1 Add and run failing provider tests for deterministic runner timing, `<1s` and nearest-second rendering, deadline clamping, readiness/retrieval wording, successful silence, and unmeasured generic errors.
- [x] 1.2 Implement the provider clock seam, bounded measured-error representation, timing accessors, and centralized safe rendering; rerun focused provider tests to green.

## 2. Workspace Broker Transport

- [x] 2.1 Add and run failing broker tests for optional version-one `elapsed_seconds` transport, server-to-client preservation, absent-field compatibility, out-of-range rejection, and arbitrary-message isolation.
- [x] 2.2 Implement bounded numeric timing propagation without changing the protocol version, category validation, grouping, deadlines, or reconnect behavior; rerun focused broker tests to green.

## 3. User-Visible Failure Paths

- [x] 3.1 Add focused CLI and terminal integration tests proving timed readiness warnings, direct TOTP fallback timing, workspace-equivalent rendering, successful silence, and unchanged manual/fail behavior. The centralized provider rendering and broker red/green slices already supply the new behavior, so these caller tests are regression characterizations rather than a separate production slice.
- [x] 3.2 Complete orchestration integration through the existing safe-message surfaces and rerun focused provider, CLI, broker, and terminal suites to green.

## 4. Documentation And Verification

- [x] 4.1 Update README, Chinese guidance, changelog, and privacy assertions with failure-only low-resolution timing and unchanged secret/session boundaries.
- [x] 4.2 Run focused tests, strict OpenSpec validation, `make check`, and `go test -race ./...`; resolve any in-scope regressions.
- [x] 4.3 Run `./scripts/build.sh`, `node scripts/scrub.mjs`, and a final diff/completion audit without using live Bitwarden data, SSH/tmux sessions, versioning, publishing, or deployment.
