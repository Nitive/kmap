package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseRawConfigYAML(t *testing.T) {
	raw, err := parseRawConfigYAML(`
devices:
  - /dev/input/by-path/platform-i8042-serio-0-event-kbd
  - /dev/input/by-id/usb-04d9_USB-HID_Keyboard-event-kbd
suppress_keydown:
  - Caps
  - Alt
tap_layout_switches:
  LAlt:
    layout: us
    variant: dvorak
  Caps:
    toggle_recent: true

mappings:
  Alt-Tab:
    passthrough: true
  Alt-J:
    to_symbol: ←
  Caps-H:
    to_keys: [Backspace]
  Caps-Backspace:
    pause: true
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
	if len(raw.tapLayoutSwitch) != 2 {
		t.Fatalf("unexpected tap_layout_switches size: %d", len(raw.tapLayoutSwitch))
	}
	if raw.tapLayoutSwitch[KeyLeftAlt].Kind != LayoutSwitchTapToLayout {
		t.Fatalf("unexpected LAlt tap switch: %#v", raw.tapLayoutSwitch[KeyLeftAlt])
	}
	if raw.tapLayoutSwitch[KeyCapsLock].Kind != LayoutSwitchTapToggleRecent {
		t.Fatalf("unexpected Caps tap switch: %#v", raw.tapLayoutSwitch[KeyCapsLock])
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
	if raw.mappings["Caps-Backspace"].pause == nil || !*raw.mappings["Caps-Backspace"].pause {
		t.Fatalf("expected Caps-Backspace pause=true, got %#v", raw.mappings["Caps-Backspace"].pause)
	}
}

func TestApplyRawConfigCompilesMappings(t *testing.T) {
	raw, err := parseRawConfigYAML(`
devices:
  - /dev/input/by-path/platform-i8042-serio-0-event-kbd
suppress_keydown: [Caps, Alt]
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
mappings:
  Alt-Tab:
    passthrough: true
  Alt-J:
    to_symbol: ←
  Alt-Shift-E:
    to_symbol: €
  Caps-H:
    to_keys: [Backspace]
  Caps-Backspace:
    pause: true
  Ctrl-Alt-Shift-Meta-K:
    to_chord: Ctrl-Shift-A
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
	if cfg.ShortcutLayout == nil {
		t.Fatalf("expected shortcut layout to be configured")
	}
	if cfg.ShortcutLayout.Layout != "us" || cfg.ShortcutLayout.Variant != "dvorak" {
		t.Fatalf("shortcut layout mismatch: %#v", cfg.ShortcutLayout)
	}
	if cfg.TapLayoutSwitches[KeyLeftAlt].Layout != "us" || cfg.TapLayoutSwitches[KeyLeftAlt].Variant != "dvorak" {
		t.Fatalf("unexpected left alt tap switch: %#v", cfg.TapLayoutSwitches[KeyLeftAlt])
	}
	if cfg.TapLayoutSwitches[KeyRightAlt].Layout != "ru" {
		t.Fatalf("unexpected right alt tap switch: %#v", cfg.TapLayoutSwitches[KeyRightAlt])
	}
	if cfg.TapLayoutSwitches[KeyCapsLock].Kind != LayoutSwitchTapToggleRecent {
		t.Fatalf("unexpected caps tap switch: %#v", cfg.TapLayoutSwitches[KeyCapsLock])
	}

	altTab := cfg.AltMappings[KeyTab]
	if altTab.Kind != MappingPassthrough {
		t.Fatalf("Alt-Tab kind: got=%d want=%d", altTab.Kind, MappingPassthrough)
	}

	altJ := cfg.AltMappings[KeyC]
	if altJ.Kind != MappingSymbol || altJ.Symbol != '←' {
		t.Fatalf("Alt-J mapping mismatch: %#v", altJ)
	}

	capsH := cfg.CapsMappings[KeyJ]
	if capsH.Kind != MappingRemap || capsH.RemapCode != KeyBackspace {
		t.Fatalf("Caps-H mapping mismatch: %#v", capsH)
	}
	if capsBackspace := cfg.CapsMappings[KeyBackspace]; capsBackspace.Kind != MappingPause {
		t.Fatalf("Caps-Backspace mapping mismatch: %#v", capsBackspace)
	}

	parser, err := newKeyNameParser(cfg.ShortcutLayout)
	if err != nil {
		t.Fatalf("newKeyNameParser: %v", err)
	}

	altShiftBinding, err := parseBindingKeyWithParser("Alt-Shift-E", parser)
	if err != nil {
		t.Fatalf("parseBindingKeyWithParser Alt-Shift-E: %v", err)
	}
	altShiftE, ok := cfg.ComboMappings[altShiftBinding]
	if !ok || altShiftE.Kind != MappingSymbol || altShiftE.Symbol != '€' {
		t.Fatalf("Alt-Shift-E mapping mismatch: %#v ok=%v", altShiftE, ok)
	}

	fullComboBinding, err := parseBindingKeyWithParser("Ctrl-Alt-Shift-Meta-K", parser)
	if err != nil {
		t.Fatalf("parseBindingKeyWithParser Ctrl-Alt-Shift-Meta-K: %v", err)
	}
	fullCombo, ok := cfg.ComboMappings[fullComboBinding]
	if !ok || fullCombo.Kind != MappingChord {
		t.Fatalf("Ctrl-Alt-Shift-Meta-K mapping mismatch: %#v ok=%v", fullCombo, ok)
	}
}

func TestShortcutLayoutKeyParserUsesLayoutLabels(t *testing.T) {
	parser, err := newKeyNameParser(&ShortcutLayoutSpec{
		Layout:  "us",
		Variant: "dvorak",
	})
	if err != nil {
		t.Fatalf("newKeyNameParser: %v", err)
	}

	tests := []struct {
		name string
		want uint16
	}{
		{name: "P", want: KeyR},
		{name: "Comma", want: KeyW},
		{name: "Slash", want: KeyLeftBrace},
		{name: "LeftBrace", want: KeyMinus},
		{name: "A", want: KeyA},
	}

	for _, tc := range tests {
		got, err := parser.Parse(tc.name)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("Parse(%q) mismatch: got=%d want=%d", tc.name, got, tc.want)
		}
	}
}

func TestCompileActionWithShortcutLayoutParserUsesLayoutLabels(t *testing.T) {
	parser, err := newKeyNameParser(&ShortcutLayoutSpec{
		Layout:  "us",
		Variant: "dvorak",
	})
	if err != nil {
		t.Fatalf("newKeyNameParser: %v", err)
	}

	compiled, err := compileActionWithParser(rawAction{toChord: "Ctrl-P"}, parser)
	if err != nil {
		t.Fatalf("compileActionWithParser chord: %v", err)
	}
	if compiled.Kind != MappingChord || compiled.ChordKey != KeyR {
		t.Fatalf("unexpected chord mapping: %#v", compiled)
	}

	keySeq, err := compileActionWithParser(rawAction{toKeys: []string{"Comma", "Enter"}}, parser)
	if err != nil {
		t.Fatalf("compileActionWithParser key sequence: %v", err)
	}
	if keySeq.Kind != MappingKeySeq {
		t.Fatalf("expected MappingKeySeq, got %d", keySeq.Kind)
	}
	if len(keySeq.KeySeq) != 2 || keySeq.KeySeq[0] != KeyW || keySeq.KeySeq[1] != KeyEnter {
		t.Fatalf("unexpected key sequence: %#v", keySeq.KeySeq)
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

func TestParseRawConfigYAMLShortcutLayout(t *testing.T) {
	raw, err := parseRawConfigYAML(`
shortcut_layout:
  layout: us
  variant: dvorak
  rules: evdev
  model: pc105
  options: compose:sclk
`)
	if err != nil {
		t.Fatalf("parseRawConfigYAML: %v", err)
	}
	if raw.shortcutLayout == nil {
		t.Fatalf("expected shortcut layout")
	}
	if raw.shortcutLayout.Layout != "us" || raw.shortcutLayout.Variant != "dvorak" {
		t.Fatalf("unexpected shortcut layout: %#v", raw.shortcutLayout)
	}
	if raw.shortcutLayout.Options != "compose:sclk" {
		t.Fatalf("unexpected shortcut options: %#v", raw.shortcutLayout)
	}
}

func TestApplyRawConfigRejectsBlankShortcutLayout(t *testing.T) {
	raw, err := parseRawConfigYAML("shortcut_layout:\n  variant: dvorak\n")
	if err != nil {
		t.Fatalf("parseRawConfigYAML: %v", err)
	}

	cfg := DefaultRuntime()
	err = applyRawConfig(&cfg, raw)
	if err == nil {
		t.Fatalf("expected shortcut layout validation error")
	}
	if !strings.Contains(err.Error(), "shortcut_layout.layout") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyRawConfigRejectsUnsupportedTapLayoutSwitchKey(t *testing.T) {
	raw, err := parseRawConfigYAML("tap_layout_switches:\n  Q:\n    layout: us\n")
	if err != nil {
		t.Fatalf("parseRawConfigYAML: %v", err)
	}

	cfg := DefaultRuntime()
	err = applyRawConfig(&cfg, raw)
	if err == nil {
		t.Fatalf("expected unsupported tap switch key error")
	}
	if !strings.Contains(err.Error(), "tap_layout_switches") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyRawConfigRejectsTapLayoutSwitchWithoutSuppression(t *testing.T) {
	raw, err := parseRawConfigYAML(`
suppress_keydown: [Caps]
tap_layout_switches:
  LAlt:
    layout: us
`)
	if err != nil {
		t.Fatalf("parseRawConfigYAML: %v", err)
	}

	cfg := DefaultRuntime()
	err = applyRawConfig(&cfg, raw)
	if err == nil {
		t.Fatalf("expected suppression validation error")
	}
	if !strings.Contains(err.Error(), "requires suppress_keydown") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileTapLayoutSwitchActionRejectsMixedActions(t *testing.T) {
	_, err := compileTapLayoutSwitchAction("Caps", yamlTapLayoutSwitchAction{
		Layout:       "us",
		ToggleRecent: true,
	})
	if err == nil {
		t.Fatalf("expected mixed action error")
	}
	if !strings.Contains(err.Error(), "only one action") {
		t.Fatalf("unexpected error: %v", err)
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
	path := filepath.Join("..", "..", "testdata", "example-kmap.yaml")
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
	if len(cfg.Devices) != 0 {
		t.Fatalf("expected repository example config to omit machine-specific devices, got %#v", cfg.Devices)
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
	parser, err := newKeyNameParser(cfg.ShortcutLayout)
	if err != nil {
		t.Fatalf("newKeyNameParser: %v", err)
	}
	binding, err := parseBindingKeyWithParser("Alt-Shift-Semicolon", parser)
	if err != nil {
		t.Fatalf("parseBindingKeyWithParser Alt-Shift-Semicolon: %v", err)
	}
	if m := cfg.ComboMappings[binding]; m.Kind != MappingSymbol || m.Symbol != ';' {
		t.Fatalf("Alt-Shift-Semicolon should map to symbol ;, got %#v", m)
	}
	if m := cfg.CapsMappings[KeyBackspace]; m.Kind != MappingPause {
		t.Fatalf("Caps-Backspace should pause remapping, got %#v", m)
	}
}

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

func TestParseBindingKeyWithMultipleModifiers(t *testing.T) {
	parser := keyNameParser{}

	binding, err := parseBindingKeyWithParser("Ctrl-Alt-Shift-Meta-K", parser)
	if err != nil {
		t.Fatalf("parseBindingKeyWithParser: %v", err)
	}
	if binding.Modifiers != ModifierCtrl|ModifierAlt|ModifierShift|ModifierMeta {
		t.Fatalf("unexpected modifiers: %#v", binding)
	}
	if binding.KeyCode != KeyK {
		t.Fatalf("unexpected key code: %#v", binding)
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
