package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRawConfigYAML(t *testing.T) {
	raw, err := parseRawConfigYAML(`
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

	altTab := cfg.altMappings[keyTab]
	if altTab.kind != mappingPassthrough {
		t.Fatalf("Alt-Tab kind: got=%d want=%d", altTab.kind, mappingPassthrough)
	}

	altJ := cfg.altMappings[keyJ]
	if altJ.kind != mappingSymbol || altJ.symbol.compose != "2202" {
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
}

func TestRepositoryAltremapConfigLoads(t *testing.T) {
	path := filepath.Join("..", "..", "altremap.yaml")
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

	if m := cfg.altMappings[keyTab]; m.kind != mappingPassthrough {
		t.Fatalf("Alt-Tab should be passthrough, got kind=%d", m.kind)
	}
	if m := cfg.altMappings[keyM]; m.kind != mappingSymbol || m.symbol.compose != "2212" {
		t.Fatalf("Alt-M should map to symbol − (2212), got %#v", m)
	}
	if m := cfg.altMappings[keyN]; m.kind != mappingSymbol || m.symbol.compose != "2224" {
		t.Fatalf("Alt-N should map to symbol \\ (2224), got %#v", m)
	}
	if m := cfg.capsMappings[keyH]; m.kind != mappingRemap || m.remapCode != keyBackspace {
		t.Fatalf("Caps-H should map to Backspace, got %#v", m)
	}
}
