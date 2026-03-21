package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRawConfigYAML(t *testing.T) {
	raw, err := parseRawConfigYAML(`
devices:
  - /dev/input/by-path/platform-i8042-serio-0-event-kbd
  - /dev/input/by-id/usb-04d9_USB-HID_Keyboard-event-kbd
suppress_keydown:
  - Caps
  - Alt

mappings:
  Alt-Tab:
    passthrough: true
  Alt-J:
    to_symbol: ←
  Caps-H:
    to_keys: [Backspace]
`)
	if err != nil {
		t.Fatalf("parseRawConfigYAML: %v", err)
	}

	if !raw.suppressDefined {
		t.Fatalf("expected suppress_keydown to be defined")
	}
	if len(raw.suppressKeys) != 2 {
		t.Fatalf("unexpected suppress_keydown size: %d", len(raw.suppressKeys))
	}
	if len(raw.devices) != 2 {
		t.Fatalf("unexpected devices size: %d", len(raw.devices))
	}

	tab := raw.mappings["Alt-Tab"]
	if tab.passthrough == nil || !*tab.passthrough {
		t.Fatalf("expected Alt-Tab passthrough=true, got %#v", tab.passthrough)
	}
	if got := raw.mappings["Alt-J"].toSymbol; got != "←" {
		t.Fatalf("unexpected Alt-J to_symbol: %q", got)
	}
	if got := raw.mappings["Caps-H"].toKeys; len(got) != 1 || got[0] != "Backspace" {
		t.Fatalf("unexpected Caps-H to_keys: %#v", got)
	}
}

func TestApplyRawConfigCompilesMappings(t *testing.T) {
	raw, err := parseRawConfigYAML(`
devices:
  - /dev/input/by-path/platform-i8042-serio-0-event-kbd
suppress_keydown: [Caps, Alt]
mappings:
  Alt-Tab:
    passthrough: true
  Alt-J:
    to_symbol: ←
  Caps-H:
    to_keys: [Backspace]
`)
	if err != nil {
		t.Fatalf("parseRawConfigYAML: %v", err)
	}

	cfg := DefaultRuntime()
	if err := applyRawConfig(&cfg, raw); err != nil {
		t.Fatalf("applyRawConfig: %v", err)
	}

	if !cfg.SuppressAlt || !cfg.SuppressCaps {
		t.Fatalf("unexpected suppress flags: alt=%v caps=%v", cfg.SuppressAlt, cfg.SuppressCaps)
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0] != "/dev/input/by-path/platform-i8042-serio-0-event-kbd" {
		t.Fatalf("devices mismatch: %#v", cfg.Devices)
	}

	altTab := cfg.AltMappings[KeyTab]
	if altTab.Kind != MappingPassthrough {
		t.Fatalf("Alt-Tab kind: got=%d want=%d", altTab.Kind, MappingPassthrough)
	}

	altJ := cfg.AltMappings[KeyJ]
	if altJ.Kind != MappingSymbol || altJ.Symbol != '←' {
		t.Fatalf("Alt-J mapping mismatch: %#v", altJ)
	}

	capsH := cfg.CapsMappings[KeyH]
	if capsH.Kind != MappingRemap || capsH.RemapCode != KeyBackspace {
		t.Fatalf("Caps-H mapping mismatch: %#v", capsH)
	}
}

func TestParseSupportsScalarToKeys(t *testing.T) {
	raw, err := parseRawConfigYAML(`
mappings:
  Caps-H:
    to_keys: Backspace
`)
	if err != nil {
		t.Fatalf("parseRawConfigYAML: %v", err)
	}
	got := raw.mappings["Caps-H"].toKeys
	if len(got) != 1 || got[0] != "Backspace" {
		t.Fatalf("unexpected to_keys value: %#v", got)
	}
}

func TestParseAndCompileNBSPToSymbol(t *testing.T) {
	raw, err := parseRawConfigYAML(`
mappings:
  Alt-Space:
    to_symbol: " "
`)
	if err != nil {
		t.Fatalf("parseRawConfigYAML: %v", err)
	}

	action, ok := raw.mappings["Alt-Space"]
	if !ok {
		t.Fatalf("missing Alt-Space mapping")
	}

	compiled, err := compileAction(action)
	if err != nil {
		t.Fatalf("compileAction: %v", err)
	}
	if compiled.Kind != MappingSymbol || compiled.Symbol != '\u00a0' {
		t.Fatalf("expected NBSP symbol, got %#v", compiled)
	}
}

func TestCompileActionToSymbolRejectsMultipleSymbols(t *testing.T) {
	_, err := compileAction(rawAction{toSymbol: "ab"})
	if err == nil {
		t.Fatalf("expected error for multi-symbol to_symbol")
	}
	if !strings.Contains(err.Error(), "exactly 1 symbol") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileActionPipeIsSymbol(t *testing.T) {
	compiled, err := compileAction(rawAction{toSymbol: "|"})
	if err != nil {
		t.Fatalf("compileAction: %v", err)
	}
	if compiled.Kind != MappingSymbol {
		t.Fatalf("expected MappingSymbol, got %d", compiled.Kind)
	}
	if compiled.Symbol != '|' {
		t.Fatalf("expected symbol |, got %q", compiled.Symbol)
	}
}

func TestLoadRuntimeConfigMissingFileUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")
	cfg, err := LoadRuntime(path)
	if err != nil {
		t.Fatalf("LoadRuntime: %v", err)
	}

	def := DefaultRuntime()
	if cfg.SuppressAlt != def.SuppressAlt || cfg.SuppressCaps != def.SuppressCaps {
		t.Fatalf("suppress flags mismatch with defaults")
	}
	if len(cfg.AltMappings) != len(def.AltMappings) {
		t.Fatalf("alt mappings size mismatch: got=%d want=%d", len(cfg.AltMappings), len(def.AltMappings))
	}
	if len(cfg.CapsMappings) != len(def.CapsMappings) {
		t.Fatalf("caps mappings size mismatch: got=%d want=%d", len(cfg.CapsMappings), len(def.CapsMappings))
	}
	if len(cfg.Devices) != len(def.Devices) {
		t.Fatalf("devices size mismatch: got=%d want=%d", len(cfg.Devices), len(def.Devices))
	}
}

func TestNormalizeDevicePaths(t *testing.T) {
	paths, err := normalizeDevicePaths([]string{
		" /dev/input/a ",
		"/dev/input/b",
		"/dev/input/a",
	})
	if err != nil {
		t.Fatalf("normalizeDevicePaths: %v", err)
	}
	if len(paths) != 2 || paths[0] != "/dev/input/a" || paths[1] != "/dev/input/b" {
		t.Fatalf("unexpected paths: %#v", paths)
	}

	if _, err := normalizeDevicePaths([]string{"", "/dev/input/a"}); err == nil {
		t.Fatalf("expected empty-device-path error")
	}
}

func TestRepositoryKMapConfigLoads(t *testing.T) {
	path := filepath.Join("..", "..", "kmap.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	cfg, err := LoadRuntime(path)
	if err != nil {
		t.Fatalf("LoadRuntime(%s): %v", path, err)
	}

	if !cfg.SuppressAlt || !cfg.SuppressCaps {
		t.Fatalf("expected SuppressAlt/SuppressCaps to be true")
	}
	if len(cfg.Devices) == 0 {
		t.Fatalf("expected devices to be configured in kmap.yaml")
	}

	if _, ok := cfg.AltMappings[KeyTab]; ok {
		t.Fatalf("Alt-Tab should not be explicitly mapped in config")
	}
	if m := cfg.AltMappings[KeyM]; m.Kind != MappingSymbol || m.Symbol != '−' {
		t.Fatalf("Alt-M should map to symbol −, got %#v", m)
	}
	if m := cfg.AltMappings[KeyN]; m.Kind != MappingSymbol || m.Symbol != '\\' {
		t.Fatalf("Alt-N should map to symbol \\, got %#v", m)
	}
	if m := cfg.CapsMappings[KeyH]; m.Kind != MappingRemap || m.RemapCode != KeyBackspace {
		t.Fatalf("Caps-H should map to Backspace, got %#v", m)
	}
}
