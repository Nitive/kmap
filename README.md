# kmap

`kmap` is a low-level Linux keyboard remapper written in Go.

It reads events from one or more real keyboard input devices, applies mapping rules from YAML, and emits remapped key events through a virtual keyboard (`/dev/uinput`).

The project is designed to make symbol and layer mappings deterministic across keyboard layouts (QWERTY, Dvorak, Russian, etc.), including on Wayland.

## What This Project Is

`kmap` is a daemon + CLI toolkit with three main jobs:

1. Run a remapping daemon (`kmap start`)
2. Capture a keyboard layout map interactively (`kmap setup-keymap`)
3. Generate deterministic XCompose entries for mapped Unicode symbols (`kmap generate-xcompose`)
4. Validate config and shortcut layout loading (`kmap validate-config`)

## How It Works

### Runtime Architecture

When you run `kmap start`, the app does this:

1. Parse YAML config (`kmap.yaml` by default)
2. Resolve input devices (`--device` override or `devices:` from config)
3. For each device, run an independent pipeline:
   - `pkg/daemon/input`: read Linux input events
   - `pkg/daemon/mapper`: apply Alt/Caps layer mappings and compose logic
   - `pkg/daemon/output`: send remapped events through a virtual keyboard

This means multiple keyboards can be handled independently while sharing the same mapping rules.

### Modules

- `pkg/config`: YAML parsing and mapping compilation
- `pkg/daemon/input`: input device reader + optional EVIOCGRAB
- `pkg/daemon/mapper`: remap state machine (Alt/Caps layers, passthrough, chords, symbol compose)
- `pkg/daemon/output`: virtual keyboard emitter via `/dev/uinput`
- `pkg/xcompose`: deterministic XCompose generator
- `cmd/cli`: CLI command handlers
- `cmd/main.go`: single executable entrypoint

### Mapping Model

Config supports:

- `suppress_keydown`: layer keys whose down event is delayed (e.g. `Alt`, `Caps`)
- `devices`: one or more `/dev/input/...` device paths
- `shortcut_layout`: canonical layout used for modifier-based shortcuts
- `tap_layout_switches`: tap-only layout switch actions for `LAlt`, `RAlt`, and `Caps`
- `mappings` with per-layer bindings, for example:
  - `to_symbol: ←`
  - `to_keys: [Left]`
  - `to_chord: Meta-Ctrl-Alt-Shift-R`
  - `passthrough: true`

A full example is in [`kmap.yaml`](./kmap.yaml).

### Automatic Layout Switching

If `shortcut_layout` is configured, `kmap` validates that the target layout exists in KDE, then temporarily switches KDE to that layout while shortcut modifiers are held. This makes app shortcuts such as `Ctrl+Shift+V` behave consistently even when the typing layout is different (for example, Russian).

Example:

```yaml
shortcut_layout:
  layout: us
  variant: dvorak

tap_layout_switches:
  LAlt:
    layout: us
    variant: dvorak
  RAlt:
    layout: ru
  Caps:
    toggle_recent: true
```

With that enabled, `kmap` keeps normal typing on the active KDE layout, but switches to Dvorak for shortcuts and restores the previous layout when the shortcut finishes.

`tap_layout_switches` are persistent layout changes:

- tapping `LAlt` or `RAlt` can switch directly to a configured KDE layout
- tapping `Caps` can toggle back to the most recently used persistent layout
- only a clean tap triggers the switch; using the key as an Alt/Caps layer key does not

Current constraints:

- KDE-only: uses `org.kde.keyboard` via `qdbus6`
- active layout changes are detected at runtime, but restart `kmap` after changing KDE layout configuration
- shortcut handling is global across all keyboards because KDE layout state is global

### Unicode / Compose Strategy

For `to_symbol`, `kmap` emits compose key sequences using a deterministic decimal-encoded format. `kmap generate-xcompose` writes matching XCompose rules so Unicode output is layout-independent.

## Why This Project Exists

This project exists to provide a predictable remapping layer that is:

- Layout-agnostic: mappings are tied to physical keycodes, not text layout
- Wayland-friendly: works through Linux input/uinput instead of WM-specific shortcuts
- Deterministic: symbol output is explicit and reproducible via generated XCompose rules
- Device-aware: can run on multiple keyboards independently
- Config-driven: behavior is controlled in YAML, not hardcoded

In short: `kmap` gives you stable key behavior across layouts, apps, and compositors.

## CLI

```bash
kmap start [--config kmap.yaml] [--device /dev/input/...]
kmap setup-keymap --layout us(dvorak) > layouts/us-dvorak.json
kmap generate-xcompose --output ~/.XCompose [--config kmap.yaml]
kmap validate-config [--config kmap.yaml]
```

Run command help:

```bash
kmap <command> -h
```

### Layout Capture

`kmap setup-keymap` now captures the bytes your terminal receives for each logical key, which makes the output layout-dependent and suitable for comparing `us(dvorak)`, `ru`, and other KDE layouts.

- Prompts are written to stderr; JSON goes to stdout by default.
- Press Enter to arm a capture, then press the requested key.
- For non-Enter keys, pressing Enter after arming skips that key.
- `Ctrl+C` or `SIGTERM` aborts immediately.
- If the daemon is running, `setup-keymap` asks it to temporarily release grabbed devices and restores them on exit.

Example:

```bash
kmap setup-keymap --layout us(dvorak) > layouts/us-dvorak.json
kmap setup-keymap --layout ru > layouts/ru.json
```

## Build / Run

```bash
make build
./bin/kmap start --config ./kmap.yaml
```

The repository also includes:

- `kmap-start.sh`
- `services/kmap.service`

for systemd-based startup.
