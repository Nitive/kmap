# kmap

`kmap` is a low-level Linux keyboard remapper written in Go.

It reads events from one or more real keyboard input devices, applies mapping rules from YAML, and emits remapped key events through a virtual keyboard (`/dev/uinput`).

The project is designed to make symbol and layer mappings deterministic across keyboard layouts (QWERTY, Dvorak, Russian, etc.), including on Wayland.

## What This Project Is

`kmap` is a daemon + CLI toolkit with three main jobs:

1. Run a remapping daemon (`kmap start`)
2. Capture a keyboard layout map interactively (`kmap setup-keymap`)
3. Generate deterministic XCompose entries for mapped Unicode symbols (`kmap generate-xcompose`)

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
- `mappings` with per-layer bindings, for example:
  - `to_symbol: ←`
  - `to_keys: [Left]`
  - `to_chord: Meta-Ctrl-Alt-Shift-R`
  - `passthrough: true`

A full example is in [`kmap.yaml`](./kmap.yaml).

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
kmap setup-keymap [--device /dev/input/...] [--output keyboard-layout.json]
kmap generate-xcompose --output ~/.XCompose [--config kmap.yaml]
```

Run command help:

```bash
kmap <command> -h
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
