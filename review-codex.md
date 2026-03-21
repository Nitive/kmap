# Codex Review: kmap

## Scope

Reviewed current code in:

- `cmd/`
- `pkg/config`
- `pkg/daemon/{input,mapper,output}`
- `pkg/xcompose`
- service/bootstrap files (`kmap-start.sh`, `services/kmap.service`, `Makefile`)

Validation run:

- `GOCACHE=/tmp/go-build go test ./...`
- `GOCACHE=/tmp/go-build go test ./... -cover`
- `GOCACHE=/tmp/go-build go test ./... -covermode=count -coverpkg=./... -coverprofile /tmp/kmap.cover.out`
- `GOCACHE=/tmp/go-build go tool cover -func=/tmp/kmap.cover.out`

## Findings (Ordered by Severity)

### Medium

1. Polling input loop can cause unnecessary CPU wakeups

- Location: `pkg/daemon/input/input.go:82`, `pkg/daemon/input/input.go:148-150`
- Details: device is opened with `O_NONBLOCK`, then `EAGAIN` is handled by sleeping for `2ms` and retrying forever.
- Impact: periodic wakeups even when idle (power/CPU overhead), more complexity than needed.
- Suggestion: switch to blocking reads (remove `O_NONBLOCK` and `EAGAIN` loop) and rely on `Close()` to interrupt read on shutdown; or use `poll/epoll` if nonblocking is intentional.

2. Daemon orchestration is hard to verify because dependencies are concrete

- Location: `pkg/daemon/daemon.go:61-81`, `pkg/daemon/daemon.go:113-158`
- Details: `Start()` wires real input/output modules and real signal handling directly.
- Impact: the most failure-prone paths (shutdown, error fan-in, multi-device lifecycle) are minimally testable in unit tests, so regressions are easier to introduce.
- Suggestion: introduce injectable factories/interfaces for input, mapper, output, and signal source; keep current concrete behavior as default wiring.

### Low

3. `setup-keymap` signal goroutine can outlive function execution

- Location: `cmd/cli/setup_keymap.go:145-149`
- Details: goroutine blocks on `<-sigCh` and has no cancellation path when capture finishes normally.
- Impact: negligible for single-run CLI process, but still a goroutine lifecycle leak in-process.
- Suggestion: add `done` channel/context and `select` on `sigCh` or `done`.

4. Duplicate ioctl bit-macro implementations in multiple packages

- Location: `pkg/daemon/input/input.go:25-56`, `pkg/daemon/output/output.go:30-62`
- Details: same low-level ioctl helper logic is duplicated.
- Impact: maintainability cost and drift risk.
- Suggestion: centralize into one internal package or replace with `golang.org/x/sys/unix` helpers/constants where possible.

5. Small API/design simplifications available

- Location: `pkg/daemon/daemon.go:189`, `pkg/config/config.go:466-479`
- Details:
  - `resolveDevicePaths` returns `([]string, error)` but currently cannot return an error.
  - `ParseKeyName` has a redundant single-character branch before a generic map lookup.
- Impact: minor complexity.
- Suggestion: simplify signatures/branches.

## Simplification Opportunities

1. Replace repeated module watcher wiring with a small helper struct

- Current code repeats watch setup for each module (`input`, `mapper`, `output`).
- A compact `[]moduleWatcher` + loop would reduce boilerplate and improve readability.

2. Use `WorkingDirectory` + direct `ExecStart` in systemd to avoid wrapper script

- Current setup uses `kmap-start.sh` mainly for argument assembly.
- If env flexibility is not required, service can directly run `/home/nitive/Develop/keyboard/bin/kmap start --config ...`.

3. Reduce exported surface where not needed

- `mapper.ComposeRuneKey` appears used only in tests; can be unexported unless intended for package API.

## Library Reuse Assessment

### Recommended reuse

1. `golang.org/x/sys/unix`

- Use for ioctl/syscall helpers and Linux constants instead of hand-rolled ioctl bit macros.
- Benefit: less custom kernel ABI code, fewer mistakes, better portability handling.

### Optional (evaluate before adopting)

2. Evdev libraries for input side

- Candidates: `github.com/gvalkov/golang-evdev`, `github.com/holoplot/go-evdev`
- Potential benefit: less manual event parsing/open/grab boilerplate.
- Tradeoff: additional dependency + API constraints; current input code is small and understandable.

### Not strongly recommended now

3. Full uinput abstraction libraries

- Example candidate: `github.com/bendahl/uinput`
- Tradeoff: project-specific behavior here (compose delays, deterministic emission, exact lifecycle) is already explicit and controlled; replacing output path may not reduce complexity materially.

## Tests and Coverage Review

## Coverage snapshot

From `go test ./... -cover`:

- `cmd/cli`: **40.7%**
- `pkg/config`: **73.7%**
- `pkg/daemon`: **15.5%**
- `pkg/daemon/mapper`: **62.2%**
- `pkg/xcompose`: **81.2%**
- `pkg/daemon/input`: **0.0%**
- `pkg/daemon/output`: **0.0%**

From `-coverpkg=./...`: total **50.8%** statements.

## What is good

1. Mapper behavior has meaningful scenario tests (Alt/Caps behavior and all configured mappings).
2. Config parsing/compilation is well-covered and includes repository config checks.
3. XCompose generation has deterministic-content tests.

## Biggest test gaps and how to improve

1. `pkg/daemon/input` (0%)

- Add unit tests for non-IO parts:
  - `Grab` argument behavior via injected ioctl function.
  - `readLoop` error handling by injecting a fake `ReadRawEvent` function.
- Add integration tests (build tag `integration`) for real `/dev/input` behavior.

2. `pkg/daemon/output` (0%)

- Split writer/emitter from constructor:
  - test event encoding and sync sequence using `bytes.Buffer`/fake writer.
- Keep `/dev/uinput` tests as integration only.

3. `pkg/daemon.Start` control logic (mostly uncovered)

- Introduce dependency injection for factories and signal source.
- Unit-test:
  - startup failure paths
  - error propagation precedence
  - graceful shutdown and cleanup order
  - multi-device fan-in/fan-out behavior

4. `cmd/cli/setup_keymap.go` critical paths uncovered

- `runSetupKeymap`, `captureLayout`, `writeLayoutJSON` currently untested.
- Add seams for capture and write functions to test argument handling + file output without real devices.

## Where coverage is not required (or should not be mandatory)

These paths directly touch host input stack and can disrupt the system if run in normal unit tests:

1. `pkg/daemon/input`

- `Start`, `Grab`, `CapsLockEnabled`, low-level `ioctl` paths.

2. `pkg/daemon/output`

- `CreateVirtualKeyboard`, `EmitKey`, `TapKey`, `Close`, `Run` with real `/dev/uinput`.

3. End-to-end `daemon.Start` with real devices

- touches real keyboard devices, grabs input, emits synthetic keys globally.

### If still testing these is needed

Use opt-in integration tests:

- Build tag: `//go:build integration`
- Dedicated script/Make target (not part of default `go test ./...`)
- Run in isolated environment (VM/container) with explicit access to `/dev/input` and `/dev/uinput`
- Add clear preflight checks and fail-fast messaging when devices/permissions are unavailable

## Suggested Next Actions (Practical Order)

1. Refactor input loop to blocking reads (remove nonblocking polling).
2. Add dependency injection in `daemon.Start` for testability.
3. Add unit tests for daemon orchestration and setup-keymap non-device paths.
4. Decide whether to adopt `x/sys/unix` for ioctl cleanup before considering larger external libraries.
