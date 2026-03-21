# kmap

`kmap` is a low-level Linux keyboard remapper written in Go.

It reads events from one or more real keyboard devices, applies mapping rules from YAML, and emits remapped key events through a virtual keyboard on `/dev/uinput`.

## Quick Start

1. Build the binary:

```bash
make build
```

2. Install the active config at `/etc/kmap/kmap.yaml`:

```bash
sudo install -d /etc/kmap
sudo install -m 644 ./kmap.yaml /etc/kmap/kmap.yaml
```

3. Edit `/etc/kmap/kmap.yaml` for your machine:
   - set `devices:` if you want anything other than the built-in default device path
   - adjust `shortcut_layout`, `tap_layout_switches`, and `mappings`

4. Validate the config:

```bash
./bin/kmap validate-config
```

5. Start the daemon:

```bash
./bin/kmap start
```

`kmap start` automatically generates `~/.XCompose` before the daemon begins remapping, so `to_symbol` mappings stay aligned with the running config.

## Requirements

- Linux with access to `/dev/input/...` keyboard devices
- access to `/dev/uinput`
- if you use `shortcut_layout` or `tap_layout_switches`, KDE with `qdbus6`

If you run `kmap` as an unprivileged user, that user must have permission to read the input devices and open `/dev/uinput`.

## Config

The default config path is:

```text
/etc/kmap/kmap.yaml
```

You can override it with `--config <path>` on any command.

The repository root [`kmap.yaml`](./kmap.yaml) is an example config you can copy and adapt.

### Device Selection

`devices:` accepts one or more `/dev/input/...` paths. Good stable choices are usually under `/dev/input/by-id/` or `/dev/input/by-path/`.

If `devices:` is omitted, `kmap` falls back to the built-in default path:

```text
/dev/input/by-path/platform-i8042-serio-0-event-kbd
```

### Mapping Actions

Each mapping supports exactly one action:

- `passthrough: true`
- `pause: true`
- `to_symbol: …`
- `to_keys: [...]`
- `to_chord: Ctrl-Shift-X`

Bindings support exact modifier combinations, including combinations such as `Ctrl-Alt-Shift-Meta-K`.

### Pause Safety Switch

You can define a hotkey that pauses remapping and releases input grabs. While paused, physical keyboards pass through directly. Press the same hotkey again to resume.

Example:

```yaml
mappings:
  Caps-Backspace:
    pause: true
```

This is intended as a safety mechanism if a bad mapping leaves the daemon in an unusable state.

## Commands

```bash
kmap start [--config /etc/kmap/kmap.yaml] [--device /dev/input/...]
kmap generate-xcompose --output ~/.XCompose [--config /etc/kmap/kmap.yaml]
kmap validate-config [--config /etc/kmap/kmap.yaml]
```

Run help for details:

```bash
kmap <command> -h
```

## Systemd User Service

The repository includes a generic user service in [`services/kmap.service`](./services/kmap.service).

Install it with:

```bash
make install
```

This installs:

- `~/.local/bin/kmap`
- `~/.config/systemd/user/kmap.service`

and enables the service with `systemctl --user`.

Useful targets:

```bash
make restart
make uninstall
```

## How It Works

When you run `kmap start`, the app does this:

1. Load `/etc/kmap/kmap.yaml` by default.
2. Generate `~/.XCompose` from all configured `to_symbol` mappings.
3. Resolve input devices from `--device`, `devices:`, or the built-in default device path.
4. Start one independent input pipeline per device.
5. Watch unavailable devices and capture them when they appear.

Each pipeline uses:

- `pkg/daemon/input`: read Linux input events and manage EVIOCGRAB
- `pkg/daemon/mapper`: apply remapping, compose emission, and pause toggle detection
- `pkg/daemon/output`: send remapped events through a virtual keyboard
- `pkg/daemon/shortcut`: optional KDE shortcut layout switching

## Layout Switching

If `shortcut_layout` is configured, `kmap` validates that the target layout exists in KDE and temporarily switches to that layout while shortcut modifiers are held. This keeps shortcuts stable even when your typing layout is different.

`tap_layout_switches` can also assign tap-only actions to `LAlt`, `RAlt`, and `Caps`.

## Development

```bash
make build
make test
make lint
```
