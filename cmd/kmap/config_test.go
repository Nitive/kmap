package main

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

	cfg := defaultRuntimeConfig()
	if err := applyRawConfig(&cfg, raw); err != nil {
		t.Fatalf("applyRawConfig: %v", err)
	}

	if !cfg.suppressAlt || !cfg.suppressCaps {
		t.Fatalf("unexpected suppress flags: alt=%v caps=%v", cfg.suppressAlt, cfg.suppressCaps)
	}
	if len(cfg.devices) != 1 || cfg.devices[0] != "/dev/input/by-path/platform-i8042-serio-0-event-kbd" {
		t.Fatalf("devices mismatch: %#v", cfg.devices)
	}

	altTab := cfg.altMappings[keyTab]
	if altTab.kind != mappingPassthrough {
		t.Fatalf("Alt-Tab kind: got=%d want=%d", altTab.kind, mappingPassthrough)
	}

	altJ := cfg.altMappings[keyJ]
	if altJ.kind != mappingSymbol || altJ.symbol.symbol != '←' {
		t.Fatalf("Alt-J mapping mismatch: %#v", altJ)
	}

	capsH := cfg.capsMappings[keyH]
	if capsH.kind != mappingRemap || capsH.remapCode != keyBackspace {
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
	if compiled.kind != mappingSymbol || compiled.symbol.symbol != '\u00a0' {
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
	if compiled.kind != mappingSymbol {
		t.Fatalf("expected mappingSymbol, got %d", compiled.kind)
	}
	if compiled.symbol.symbol != '|' {
		t.Fatalf("expected symbol |, got %#v", compiled.symbol)
	}
}

func TestLoadRuntimeConfigMissingFileUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")
	cfg, err := loadRuntimeConfig(path)
	if err != nil {
		t.Fatalf("loadRuntimeConfig: %v", err)
	}

	def := defaultRuntimeConfig()
	if cfg.suppressAlt != def.suppressAlt || cfg.suppressCaps != def.suppressCaps {
		t.Fatalf("suppress flags mismatch with defaults")
	}
	if len(cfg.altMappings) != len(def.altMappings) {
		t.Fatalf("alt mappings size mismatch: got=%d want=%d", len(cfg.altMappings), len(def.altMappings))
	}
	if len(cfg.capsMappings) != len(def.capsMappings) {
		t.Fatalf("caps mappings size mismatch: got=%d want=%d", len(cfg.capsMappings), len(def.capsMappings))
	}
	if len(cfg.devices) != len(def.devices) {
		t.Fatalf("devices size mismatch: got=%d want=%d", len(cfg.devices), len(def.devices))
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

	cfg, err := loadRuntimeConfig(path)
	if err != nil {
		t.Fatalf("loadRuntimeConfig(%s): %v", path, err)
	}

	if !cfg.suppressAlt || !cfg.suppressCaps {
		t.Fatalf("expected suppressAlt/suppressCaps to be true")
	}
	if len(cfg.devices) == 0 {
		t.Fatalf("expected devices to be configured in kmap.yaml")
	}

	if _, ok := cfg.altMappings[keyTab]; ok {
		t.Fatalf("Alt-Tab should not be explicitly mapped in config")
	}
	if m := cfg.altMappings[keyM]; m.kind != mappingSymbol || m.symbol.symbol != '−' {
		t.Fatalf("Alt-M should map to symbol −, got %#v", m)
	}
	if m := cfg.altMappings[keyN]; m.kind != mappingSymbol || m.symbol.symbol != '\\' {
		t.Fatalf("Alt-N should map to symbol \\, got %#v", m)
	}
	if m := cfg.capsMappings[keyH]; m.kind != mappingRemap || m.remapCode != keyBackspace {
		t.Fatalf("Caps-H should map to Backspace, got %#v", m)
	}
}
