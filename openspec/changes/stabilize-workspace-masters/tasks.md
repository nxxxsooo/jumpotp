# Tasks: Stabilize Workspace Masters

Execution notes: tasks 1.x and 8.x require live infrastructure or repository authority and are reserved for the supervising agent. Tasks 2.x–7.x are self-contained red/green slices suitable for a delegated implementation model; each slice must leave `go test ./...` green and must not touch files outside its listed scope plus its tests.

## 1. Foundations (supervising agent)

- [x] 1.1 Land the completed prerequisite work: commit the current working tree (Bitwarden readiness + secret-free timing implementation) to `codex/fix-terminal-input`, then archive `harden-bitwarden-readiness` and `add-secret-free-provider-timing` through the official OpenSpec archive workflow, then create branch `feat/stabilize-workspace-masters` from that commit.
- [x] 1.2 Start live experiment E1 (sessionless-master tolerance): establish `ssh -N` with a dedicated throwaway ControlPath against one production target (one TOTP consumed, operator authorized), record start time, and leave it unattended. E1 is evaluated in task 8.1 and gates release, not implementation.

## 2. Sessionless launcher arguments

- [x] 2.1 Add failing launcher tests: workspace master mode builds `<launcher> -N -o ServerAliveInterval=60 -o ServerAliveCountMax=3 -o ControlPersist=no <alias>` for both `ssh` and `sshm`, direct `connect` argument construction is unchanged, and the mode is explicit in the launcher API rather than inferred.
- [x] 2.2 Implement the master-mode argument builder in `internal/launcher` and thread the workspace call path (`internal/workspace/manager.go` target command → `__target` → launcher) to request it; rerun focused tests to green.

## 3. Supervised reconnection in the target wrapper

- [x] 3.1 Add failing supervisor tests (injectable clock and runner): child exit schedules backoff 5s→doubling→300s cap with bounded jitter; gate requires `ssh -O check` success or `broker.Active` truth before any dial; gate failure polls on a bounded interval with zero launcher invocations; SIGTERM/SIGHUP/interrupt exit without respawn; submission state resets per attempt; redacted status lines are emitted for connect, exit, backoff, and waiting states.
- [x] 3.2 Implement the supervision loop around the existing single-shot connect flow in the `__target` path (`internal/cli/internal.go` plus a new supervisor unit beside `internal/terminal` or `internal/workspace`), reusing `broker.Active` as the broker gate; rerun focused tests to green.

## 4. Broker rotation-boundary guard

- [x] 4.1 Add failing broker tests (injectable clock): a group flush starting with <8s remaining in the epoch-aligned 30s window delays retrieval until the next boundary; a flush with ≥8s runway proceeds immediately; aggregated waiters all receive the post-boundary code; existing grouping, deadlines, and error paths are unchanged.
- [x] 4.2 Implement the guard in `internal/broker` flush scheduling with named constants for the 30s period and 8s guard; rerun focused tests to green.

## 5. Bounded automatic resubmission

- [x] 5.1 Add failing terminal-proxy tests: first rejection triggers exactly one automatic retry that waits past the TOTP window boundary of the submitted code; the same code value is never written twice; a second rejection enters the existing manual/fail fallback; manual-mode and `--manual` behavior are unchanged; zeroization still covers every code buffer.
- [x] 5.2 Implement the bounded resubmission state machine in `internal/terminal/connect.go` (per-attempt counter, boundary wait, fresh broker request); rerun focused tests to green.

## 6. Status derivation

- [x] 6.1 Add failing status tests: wrapper window alive + ControlMaster unavailable reports `connecting`; alive + available reports `running`; absent reports `stopped`; failed rules unchanged; JSON schema field values updated accordingly.
- [x] 6.2 Implement the derivation in `internal/workspace/status.go`; rerun focused tests to green.

## 7. Integration, documentation, and full verification

- [x] 7.1 Extend workspace integration tests: a supervised wrapper whose child exits keeps its window and reconnects when the gate opens; `reconcileSession` remains fail-closed and never fights a live supervised pane; create/attach/stop flows unchanged.
- [x] 7.2 Update README, docs/zh-CN.md, and CHANGELOG (0.2.0 Unreleased) with the sessionless-window breaking change, reconnection behavior, boundary guard, and migration note ("workspace windows hold connections; use `ssh <alias>` for interactive work").
- [x] 7.3 Run strict OpenSpec validation, `make check`, `go test -race ./...`, `./scripts/build.sh`, and `node scripts/scrub.mjs`; resolve in-scope regressions without versioning, publishing, or live MFA.

## 8. Live validation and release preparation (supervising agent)

- [x] 8.1 Evaluate E1 after 40+ unattended minutes: require `ssh -O check` success and a mux exec success on the throwaway ControlPath, then tear the experiment master down. On failure, apply the documented pivot (drop `-N` from the launcher decision, keep supervision/guard/status) before proceeding.
- [ ] 8.2 Rebuild and install the local binary, restart the production workspace, and verify the Product Brief success scenarios: three targets `Master running` after 2+ unattended hours; a killed master self-heals with an active broker and waits gated without one; five consecutive cold starts authenticate all targets with zero manual TOTP entry; `status` shows `connecting` during reconnection.
- [ ] 8.3 Open the pull request from `feat/stabilize-workspace-masters` with the archived-prerequisite history and verification evidence. Version tagging and publishing remain a separately authorized release step.
