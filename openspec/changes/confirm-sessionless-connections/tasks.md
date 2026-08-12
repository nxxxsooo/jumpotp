# Tasks: Confirm Sessionless Connections

## 1. Establishment reporting

- [x] 1.1 Add failing supervisor tests: while an attempt runs, a bounded-interval master probe reports establishment exactly once per attempt on first confirmation; an attempt that never confirms prints no establishment line and still prints exit/backoff lines last; the probe stops when the attempt ends; a later attempt can report establishment again; the line names the target and its interactive alias and contains no code, item reference, or raw ssh arguments.
- [x] 1.2 Implement the in-attempt establishment probe in `internal/supervise` reusing the existing `MasterCheck` seam, and pass the alias needed for the message from `internal/cli/internal.go`; rerun focused tests to green.

## 2. Screen clearing at establishment

- [x] 2.1 Add failing supervisor tests: on first confirmed establishment the wrapper writes a clear-screen-and-scrollback sequence immediately before the establishment line; an attempt that never confirms writes no clear sequence; at most one clear per attempt; the write contains only the fixed control sequence and never a code value.
- [x] 2.2 Implement clearing in `internal/supervise` on the establishment path, ordered before the establishment line; keep `internal/terminal` free of clearing logic since a submission-time trigger fires before the endpoint's echo arrives; rerun focused tests to green.

## 3. Documentation and verification

- [x] 3.1 Update README, docs/zh-CN.md, and CHANGELOG (0.2.1) describing the establishment line and the cleared authentication exchange, including that the endpoint's echo is outside JumpOTP's control until clearing runs.
- [x] 3.2 Run `openspec validate --all --strict`, `make check`, `go test -race ./...`, `./scripts/build.sh`, and `node scripts/scrub.mjs`; resolve in-scope regressions.

## 4. Live verification and release (supervising agent)

- [ ] 4.1 Rebuild, install, restart the production workspace, and confirm in a real target window that authentication is followed by a cleared screen and an establishment line, and that `ssh <alias>` still reuses the master.
- [ ] 4.2 Merge, tag 0.2.1, and verify every distribution surface.
