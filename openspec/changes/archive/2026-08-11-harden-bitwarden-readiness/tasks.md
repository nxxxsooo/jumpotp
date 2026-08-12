## 1. Bitwarden Provider Readiness

- [x] 1.1 Add and run failing provider tests for the twenty-second effective deadline, exact `bw status` arguments, unlocked-only parsing, bounded redaction, and readiness-specific error categories.
- [x] 1.2 Implement provider readiness, safe readiness messages, buffer cleanup, and the twenty-second default; rerun focused provider tests to green.

## 2. Workspace Broker State

- [x] 2.1 Add and run failing broker tests for validated live, absent, stale, and conflicting broker probes plus exact derived client and connection budgets.
- [x] 2.2 Implement the read-only validated broker-active probe and derived budget behavior; rerun focused broker tests to green.

## 3. Direct And Workspace Orchestration

- [x] 3.1 Add and run failing orchestration tests for readiness-before-launch, redacted continuation, manual bypass, new-workspace readiness, and active-broker reattachment bypass.
- [x] 3.2 Implement best-effort direct and workspace readiness wiring without changing prompt-time provider, PTY, fallback, or broker ownership semantics; rerun focused CLI and terminal tests to green.

## 4. Documentation And Verification

- [x] 4.1 Update README, Chinese guidance, and changelog with the bounded readiness behavior and unchanged security boundary.
- [x] 4.2 Run focused tests, `make check`, and `go test -race ./...`; fix any regressions within scope.
- [x] 4.3 Run strict OpenSpec validation, cross-platform builds, and the privacy scrub without versioning, publishing, restarting workspaces, or submitting live MFA.
