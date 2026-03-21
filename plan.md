# kmap Improvement Plan

This plan outlines the steps to modernize `kmap`, improve its testability, and simplify its architecture.

## Phase 1: Core Reliability & Standard Standards
- [x] **Blocking Input Reads**: Update `pkg/daemon/input/input.go` to use blocking reads (removing `O_NONBLOCK` and the `EAGAIN` retry loop).
- [x] **Fix Goroutine Leaks**: Update `cmd/cli/setup_keymap.go` to ensure the signal-handling goroutine terminates when key capture is complete.
- [ ] **Adopt `x/sys/unix`**: (Note: skipped `go get` for now due to environment constraints)
    - [ ] Replace manual `ioctl` bit-macro definitions with `unix` package helpers.
    - [ ] Replace manual keycode constants in `pkg/config/keycodes.go` with `unix.KEY_*` constants.
    - [ ] Verify that all existing mappings match `unix` constants (Manual check: ESC=1, 1=2, A=30 confirmed).

## Phase 2: Simplification & Deployment
- [x] **Remove Shell Wrapper**: Delete `kmap-start.sh`.
- [x] **Update Systemd Service**:
    - [x] Modify `services/kmap.service` to call the `kmap` binary directly with flags.
    - [x] Remove reliance on environment variables (`KEYBOARD_DEVICE`, `KMAP_CONFIG`) for basic configuration.
- [x] **Simplify Config Utilities**:
    - [x] Consolidate `normalizeToken` and `trimQuotes` in `pkg/config/config.go`.
    - [x] Clean up `ParseKeyName` logic to remove redundant branches.
    - [x] Removed unused error return from `resolveDevicePaths`.
- [ ] **Refine CLI Commands**: (Note: skipped for now due to environment constraints)
    - [ ] Evaluate using a library like `cobra` or `urfave/cli` to simplify subcommand handling and help generation, given the project's openness to dependencies.

## Phase 3: Architecture & Testability (Dependency Injection)
- [x] **Define Core Interfaces**: Create interfaces for `InputDevice`, `Mapper`, and `OutputDevice` to decouple the daemon from hardware.
- [x] **Refactor `daemon.Start`**:
    - [x] Update the entry point to accept injected dependencies/factories.
    - [x] Allow mocking of system signals for lifecycle testing.
- [x] **Implement I/O Mocks**:
    - [x] Refactor `input` package to work with `io.Reader`/`io.Writer` or a custom file interface.
    - [x] Create a mockable `SystemCall` interface for `ioctl` operations (Grab, LED checks).
    - [x] Refactor `output` package to work with `io.Writer`.
- [x] **Expand Test Coverage**:
    - [x] Add unit tests for the daemon orchestration (startup/shutdown sequencing) in `pkg/daemon/daemon_test.go`.
    - [x] Add unit tests for `setup-keymap` capture logic in `cmd/cli/setup_keymap_test.go`.

## Phase 4: Library Evaluation & Maintenance
- [ ] **High-Level Input/Output Libraries**: (Note: blocked by lack of `go` binary to manage dependencies)
    - [ ] Evaluate `github.com/holoplot/go-evdev` and `github.com/marin-m/go-uinput`.
    - [ ] Adopt them if they provide better error handling or cleaner abstractions than the `unix` package alone.
- [ ] **Integration Suite**: Create a separate integration test suite (via build tags) for validating behavior against real `/dev/input` and `/dev/uinput` devices in controlled environments.
- [x] **Visibility Cleanup**: Unexport internal implementation details (e.g., `mapper.ComposeRuneKey`) that are not part of the public API.
