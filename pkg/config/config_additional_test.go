package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStringOrListUnmarshalVariants(t *testing.T) {
	t.Run("null becomes nil", func(t *testing.T) {
		var got stringOrList
		if err := yaml.Unmarshal([]byte("null\n"), &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("sequence is preserved", func(t *testing.T) {
		var got stringOrList
		if err := yaml.Unmarshal([]byte("- Tab\n- Enter\n"), &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(got) != 2 || got[0] != "Tab" || got[1] != "Enter" {
			t.Fatalf("unexpected value: %#v", got)
		}
	})

	t.Run("mapping is rejected", func(t *testing.T) {
		var got stringOrList
		err := yaml.Unmarshal([]byte("field: value\n"), &got)
		if err == nil {
			t.Fatalf("expected kind error")
		}
		if !strings.Contains(err.Error(), "expected string or list") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLoadRuntimeBlankPathReturnsDefaults(t *testing.T) {
	got, err := LoadRuntime("  ")
	if err != nil {
		t.Fatalf("LoadRuntime: %v", err)
	}

	want := DefaultRuntime()
	if got.SuppressAlt != want.SuppressAlt || got.SuppressCaps != want.SuppressCaps {
		t.Fatalf("defaults mismatch: got=%+v want=%+v", got, want)
	}
	if len(got.Devices) != 0 {
		t.Fatalf("expected no devices, got %#v", got.Devices)
	}
}

func TestLoadRuntimeReadErrorIncludesPath(t *testing.T) {
	path := t.TempDir()
	_, err := LoadRuntime(path)
	if err == nil {
		t.Fatalf("expected read error")
	}
	if !strings.Contains(err.Error(), "read config "+path) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRuntimeCompileErrorIncludesPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(path, []byte("mappings:\n  Alt-A:\n    to_symbol: ab\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := LoadRuntime(path)
	if err == nil {
		t.Fatalf("expected compile error")
	}
	if !strings.Contains(err.Error(), "compile config "+path) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRawConfigYAMLRejectsMultipleDocuments(t *testing.T) {
	_, err := parseRawConfigYAML("mappings: {}\n---\nmappings: {}\n")
	if err == nil {
		t.Fatalf("expected multi-document error")
	}
	if !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCloneCompiledMappingDeepCopiesSlices(t *testing.T) {
	original := CompiledMapping{
		Kind:      MappingChord,
		ChordKey:  KeyA,
		ChordMods: []uint16{KeyLeftCtrl, KeyLeftShift},
		KeySeq:    []uint16{KeyA, KeyB},
	}

	cloned := CloneCompiledMapping(original)
	cloned.ChordMods[0] = KeyLeftAlt
	cloned.KeySeq[0] = KeyZ

	if original.ChordMods[0] != KeyLeftCtrl {
		t.Fatalf("ChordMods was not deep-copied: %#v", original.ChordMods)
	}
	if original.KeySeq[0] != KeyA {
		t.Fatalf("KeySeq was not deep-copied: %#v", original.KeySeq)
	}
}

func TestCompileActionSupportsChordAndKeySequence(t *testing.T) {
	keySeq, err := compileAction(rawAction{toKeys: []string{"Tab", "Enter"}})
	if err != nil {
		t.Fatalf("compileAction key sequence: %v", err)
	}
	if keySeq.Kind != MappingKeySeq {
		t.Fatalf("expected MappingKeySeq, got %d", keySeq.Kind)
	}
	if len(keySeq.KeySeq) != 2 || keySeq.KeySeq[0] != KeyTab || keySeq.KeySeq[1] != KeyEnter {
		t.Fatalf("unexpected KeySeq: %#v", keySeq.KeySeq)
	}

	chord, err := compileAction(rawAction{toChord: "Ctrl-Shift-A"})
	if err != nil {
		t.Fatalf("compileAction chord: %v", err)
	}
	if chord.Kind != MappingChord {
		t.Fatalf("expected MappingChord, got %d", chord.Kind)
	}
	if chord.ChordKey != KeyA {
		t.Fatalf("ChordKey mismatch: %d", chord.ChordKey)
	}
	if len(chord.ChordMods) != 2 || chord.ChordMods[0] != KeyLeftCtrl || chord.ChordMods[1] != KeyLeftShift {
		t.Fatalf("unexpected ChordMods: %#v", chord.ChordMods)
	}
}

func TestCompileActionRejectsMissingAction(t *testing.T) {
	_, err := compileAction(rawAction{})
	if err == nil {
		t.Fatalf("expected missing action error")
	}
	if !strings.Contains(err.Error(), "no action specified") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseAndFormatKeyNames(t *testing.T) {
	code, err := ParseKeyName(` "tab" `)
	if err != nil {
		t.Fatalf("ParseKeyName: %v", err)
	}
	if code != KeyTab {
		t.Fatalf("unexpected key code: %d", code)
	}

	if _, err := ParseKeyName(" "); err == nil {
		t.Fatalf("expected empty key name error")
	}

	if KeyName(KeyTab) != "KEY_TAB" {
		t.Fatalf("unexpected known key name: %q", KeyName(KeyTab))
	}
	if KeyName(255) != "KEY_255" {
		t.Fatalf("unexpected fallback key name: %q", KeyName(255))
	}
}

func TestApplyRawConfigRejectsUnsupportedSuppressKey(t *testing.T) {
	cfg := DefaultRuntime()
	err := applyRawConfig(&cfg, rawConfig{
		suppressDefined: true,
		suppressKeys:    []string{"meta"},
		mappings:        map[string]rawAction{},
	})
	if err == nil {
		t.Fatalf("expected suppress_keydown error")
	}
	if !strings.Contains(err.Error(), "unsupported suppress_keydown value") {
		t.Fatalf("unexpected error: %v", err)
	}
}
