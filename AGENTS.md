# Repository Guidelines

## Project Structure & Module Organization
`kmap` is a Go CLI and daemon for Linux keyboard remapping. Entry points live in `cmd/`: [`cmd/main.go`](/home/nitive/Develop/keyboard/cmd/main.go) boots the app, and [`cmd/cli`](/home/nitive/Develop/keyboard/cmd/cli) contains subcommands such as `start`, `setup-keymap`, and `generate-xcompose`. Core packages live under [`pkg`](/home/nitive/Develop/keyboard/pkg): `config` parses `kmap.yaml`, `daemon` handles input/output pipelines, and `xcompose` renders compose rules. Operational assets live in [`services/`](/home/nitive/Develop/keyboard/services), while generated binaries and coverage files go to ignored directories `bin/` and `tmp/`.

## Build, Test, and Development Commands
Use Go `1.26.1`, which is pinned in `go.mod` and `mise.toml`.

- `mise install` installs the pinned Go toolchain.
- `make build` compiles `./cmd` into `./bin/kmap`.
- `./bin/kmap start --config ./kmap.yaml` runs the remapping daemon locally.
- `make test` runs the full Go test suite with `go test ./...`.
- `make test-coverage` writes `tmp/coverage.out` and `tmp/coverage.html`.
- `make restart` rebuilds, regenerates `~/.XCompose`, and restarts the systemd service.

## Coding Style & Naming Conventions
Follow standard Go conventions: format with `gofmt`, keep imports grouped automatically, and use tabs as emitted by Go tooling. Exported identifiers use `CamelCase`; package-private names use `lowerCamelCase`. Keep packages focused by responsibility (`pkg/config`, `pkg/daemon`, `pkg/xcompose`). CLI commands are kebab-case (`generate-xcompose`), while YAML keys stay snake_case (`suppress_keydown`, `to_symbol`).

## Testing Guidelines
Tests sit next to the code they cover and use the standard `testing` package. Name files `*_test.go`; this repo also uses `*_additional_test.go` for extra edge-case coverage. Prefer table-driven tests for parsing and mapping logic, and inject fakes for device-facing code instead of relying on `/dev/input` or `/dev/uinput`. Run `make test` before opening a PR; use `make test-coverage` when changing config parsing, mapping behavior, or CLI dispatch.

## Commit & Pull Request Guidelines
Recent commits use short, imperative subjects such as `Fix build and test errors` and `Refactor CLI to use a command registry`. Keep commit messages concise, capitalized, and focused on one change. PRs should summarize user-visible behavior, list test commands run, and call out changes to `kmap.yaml`, `services/kmap.service`, or XCompose generation. Include terminal output or config snippets when a change affects setup or runtime behavior.
