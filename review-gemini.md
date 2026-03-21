# Code Review: kmap

This document provides a comprehensive review of the `kmap` keyboard remapper project, highlighting opportunities for simplification, library reuse, and improvements in test coverage.

## Project Overview

`kmap` is a low-level Linux keyboard remapper written in Go. It operates by reading input events from `/dev/input/event*`, processing them through a state-driven mapper, and emitting remapped events via a virtual `/dev/uinput` keyboard.

## 1. Simplification Opportunities

### Keycode & Token Management (`pkg/config/config.go`, `pkg/config/keycodes.go`)

- **Manual Mapping**: `keyCodeByName` and `keyNameByCode` are manually maintained. This is error-prone and requires manual updates for new keys.
- **Normalization**: `normalizeToken` and `trimQuotes` are simple but could be consolidated or simplified using `strings.ToUpper(strings.Trim(s, " \t\r\n\"'"))`.
- **YAML Parsing**: `parseRawConfigYAML` manually checks for multiple documents. `yaml.v3`'s `Decode` could be used more idiomaticially by checking if any content remains in the decoder after the first document is decoded.

### Low-level I/O (`pkg/daemon/input/input.go`, `pkg/daemon/output/output.go`)

- **Redundant `ioctl` Definitions**: Both packages redefine `ioctl` and related constants (`iocNRBits`, etc.). This could be moved to a shared internal package or replaced by standard libraries.
- **Struct Packing**: The `RawEvent` and `inputEvent` structs rely on binary layout matching Linux's `input_event`. While correct for 64-bit systems, explicit padding or using a more robust library would improve reliability across different architectures.

### State Management (`pkg/daemon/mapper/mapper.go`)

- **Remapper State**: The `remapper` uses several maps (`activeRemapped`, `swallowed`, `modDown`). While functional, as more complex mapping logic is added (e.g., tap-hold, multi-layer), a formal state machine or a more structured "Active Actions" tracking system would be easier to maintain.

## 2. Library Replacement Suggestions

### Core Functionality

- **`github.com/holoplot/go-evdev`**:
  - **Benefit**: Replaces `pkg/daemon/input`. It provides a high-level API for reading input events, handling `EVIOCGRAB`, and accessing device information without manual `ioctl` calls.
  - **Impact**: Removes ~100 lines of low-level boilerplate and provides better error handling.
- **`github.com/marin-m/go-uinput`**:
  - **Benefit**: Replaces `pkg/daemon/output`. It simplifies the creation and management of virtual keyboards via `/dev/uinput`.
  - **Impact**: Provides a cleaner API for emitting keys and handles the `ioctl` sequence for device creation.
- **`golang.org/x/sys/unix`**:
  - **Benefit**: Provides standard Linux constants like `unix.EV_KEY`, `unix.KEY_A`, `unix.EVIOCGRAB`, etc.
  - **Impact**: Eliminates the need to manually define constants in `pkg/config/keycodes.go` and `pkg/daemon/input/input.go`.

## 3. Test Quality & Coverage Assessment

### Current State

- **High Quality**: Core logic in `pkg/daemon/mapper`, `pkg/config`, and `pkg/xcompose` is well-tested using fakes (`fakeEmitter`, `fakeTapper`). This allows for high-confidence testing of the mapping state machine without actual hardware.
- **CLI Testing**: The use of function pointers for CLI commands in `cmd/cli/cli.go` is an excellent pattern that allows testing command dispatch and flag parsing without executing the full daemon.

### Coverage Gaps

- **`pkg/daemon/input`**: Mostly untested.
- **`pkg/daemon/output`**: Mostly untested.
- **`pkg/daemon/daemon.go`**: The top-level orchestration (running multiple pipelines, handling signals) is not fully covered by automated tests.
- **`cmd/cli/setup_keymap.go`**: Interactive `captureLayout` logic is hard to test automatically.

## 4. Suggestions to Improve Tests and Coverage

### Testing Low-level I/O

- **Mocking Files**: Instead of testing against `/dev/input`, the `input` and `output` packages should accept an `io.Reader` or `io.Writer` (or a mockable file-like interface).
  - **Input**: Use a `bytes.Buffer` or `os.Pipe` to feed pre-encoded `input_event` structs into the `ReadRawEvent` loop.
  - **Output**: Capture bytes written to the "uinput file" and decode them to verify the correct sequence of `input_event` structs.
- **`ioctl` Abstraction**: Wrap `ioctl` calls in an interface:
  ```go
  type SystemCall interface {
      Ioctl(fd int, req uintptr, arg uintptr) error
  }
  ```
  This allows mocking `EVIOCGRAB` and other system-level commands to verify they are called with the correct parameters without requiring root or hardware.

### Testing Interactive CLI

- **`captureLayout`**: Refactor to accept an `io.Reader` for input and an `io.Writer` for prompts. This would allow simulating key presses in a test by writing encoded `input_event` structs to the reader.

## 5. Untested/Hard-to-Test Areas

### Areas affecting the system:

- **`ioctl(fd, EVIOCGRAB, 1)`**: This steals the input device from the OS.
- **`/dev/uinput` device creation**: Requires root and modifies the system's input devices.
- **Real-time `time.Sleep`**: Used in `TapKey` and `CreateVirtualKeyboard` for hardware/compositor synchronization.

### How to test anyway:

- **Logic vs. Effect**: Separate the _decision_ to call a system command from the _execution_ of the command. Test the logic using the `SystemCall` interface mentioned above.
- **Virtual Devices (CI)**: On Linux CI (like GitHub Actions), you can sometimes use `uinput` if the kernel module is loaded and permissions are set. However, mocking the file interface is generally more reliable and faster for unit tests.
- **Time Injection**: Instead of `time.Sleep`, use a `Clock` interface or pass a `delay` parameter that can be set to 0 in tests to speed them up.
