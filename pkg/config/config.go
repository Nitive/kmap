package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type MappingKind int

const (
	MappingPassthrough MappingKind = iota
	MappingSymbol
	MappingRemap
	MappingChord
	MappingKeySeq
)

type CompiledMapping struct {
	Kind      MappingKind
	Symbol    rune
	RemapCode uint16
	ChordKey  uint16
	ChordMods []uint16
	KeySeq    []uint16
}

type ShortcutLayoutSpec struct {
	Layout  string
	Variant string
	Rules   string
	Model   string
	Options string
}

type LayoutSwitchTapKind int

const (
	LayoutSwitchTapToLayout LayoutSwitchTapKind = iota
	LayoutSwitchTapToggleRecent
)

type LayoutSwitchTapAction struct {
	Kind    LayoutSwitchTapKind
	Layout  string
	Variant string
}

type Runtime struct {
	SuppressAlt  bool
	SuppressCaps bool
	Devices      []string

	AltMappings       map[uint16]CompiledMapping
	CapsMappings      map[uint16]CompiledMapping
	ShortcutLayout    *ShortcutLayoutSpec
	TapLayoutSwitches map[uint16]LayoutSwitchTapAction
	ShortcutMappings  map[uint16]uint16
}

type rawConfig struct {
	suppressDefined bool
	suppressKeys    []string
	devices         []string
	mappings        map[string]rawAction
	shortcutLayout  *ShortcutLayoutSpec
	tapLayoutSwitch map[uint16]LayoutSwitchTapAction
}

type rawAction struct {
	passthrough *bool
	toSymbol    string
	toKeys      []string
	toChord     string
}

type stringOrList []string

func (s *stringOrList) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!null" {
		*s = nil
		return nil
	}

	switch node.Kind {
	case yaml.ScalarNode:
		var single string
		if err := node.Decode(&single); err != nil {
			return err
		}
		if strings.TrimSpace(single) == "" {
			*s = nil
			return nil
		}
		*s = []string{single}
		return nil

	case yaml.SequenceNode:
		var items []string
		if err := node.Decode(&items); err != nil {
			return err
		}
		*s = items
		return nil

	default:
		return fmt.Errorf("expected string or list, got YAML node kind %d", node.Kind)
	}
}

type yamlAction struct {
	Passthrough *bool        `yaml:"passthrough"`
	ToSymbol    string       `yaml:"to_symbol"`
	ToKeys      stringOrList `yaml:"to_keys"`
	ToChord     string       `yaml:"to_chord"`
}

type yamlConfig struct {
	SuppressKeydown *stringOrList                        `yaml:"suppress_keydown"`
	Devices         *stringOrList                        `yaml:"devices"`
	Mappings        map[string]yamlAction                `yaml:"mappings"`
	ShortcutLayout  *yamlShortcutLayout                  `yaml:"shortcut_layout"`
	TapLayoutSwitch map[string]yamlTapLayoutSwitchAction `yaml:"tap_layout_switches"`
}

type yamlShortcutLayout struct {
	Layout  string `yaml:"layout"`
	Variant string `yaml:"variant"`
	Rules   string `yaml:"rules"`
	Model   string `yaml:"model"`
	Options string `yaml:"options"`
}

type yamlTapLayoutSwitchAction struct {
	Layout       string `yaml:"layout"`
	Variant      string `yaml:"variant"`
	ToggleRecent bool   `yaml:"toggle_recent"`
}

var keyCodeByName = map[string]uint16{
	// Digits
	"1": Key1,
	"2": Key2,
	"3": Key3,
	"4": Key4,
	"5": Key5,
	"6": Key6,
	"7": Key7,
	"8": Key8,
	"9": Key9,
	"0": Key0,

	// Letters
	"A": KeyA,
	"B": KeyB,
	"C": KeyC,
	"D": KeyD,
	"E": KeyE,
	"F": KeyF,
	"G": KeyG,
	"H": KeyH,
	"I": KeyI,
	"J": KeyJ,
	"K": KeyK,
	"L": KeyL,
	"M": KeyM,
	"N": KeyN,
	"O": KeyO,
	"P": KeyP,
	"Q": KeyQ,
	"R": KeyR,
	"S": KeyS,
	"T": KeyT,
	"U": KeyU,
	"V": KeyV,
	"W": KeyW,
	"X": KeyX,
	"Y": KeyY,
	"Z": KeyZ,

	// Modifiers and common keys
	"ALT":       KeyLeftAlt,
	"LALT":      KeyLeftAlt,
	"RALT":      KeyRightAlt,
	"CAPS":      KeyCapsLock,
	"CAPSLOCK":  KeyCapsLock,
	"TAB":       KeyTab,
	"ENTER":     KeyEnter,
	"RET":       KeyEnter,
	"BACKSPACE": KeyBackspace,
	"BSPC":      KeyBackspace,
	"ESC":       KeyEsc,
	"SPACE":     KeySpace,
	"SPC":       KeySpace,
	"CTRL":      KeyLeftCtrl,
	"CONTROL":   KeyLeftCtrl,
	"SHIFT":     KeyLeftShift,
	"META":      KeyLeftMeta,
	"SUPER":     KeyLeftMeta,
	"WIN":       KeyLeftMeta,
	"UP":        KeyUp,
	"DOWN":      KeyDown,
	"LEFT":      KeyLeft,
	"RIGHT":     KeyRight,

	// Named punctuation/special keys
	"MINUS":      KeyMinus,
	"EQUAL":      KeyEqual,
	"GRAVE":      KeyGrv,
	"SEMICOLON":  KeySemicolon,
	"APOSTROPHE": KeyApostrophe,
	"COMMA":      KeyComma,
	"DOT":        KeyDot,
	"PERIOD":     KeyDot,
	"SLASH":      KeySlash,
	"BACKSLASH":  KeyBackslash,
	"LEFTBRACE":  KeyLeftBrace,
	"RIGHTBRACE": KeyRightBrace,

	// Symbol aliases
	";":  KeySemicolon,
	"'":  KeyApostrophe,
	",":  KeyComma,
	".":  KeyDot,
	"/":  KeySlash,
	"\\": KeyBackslash,
	"[":  KeyLeftBrace,
	"]":  KeyRightBrace,
	"=":  KeyEqual,
	"`":  KeyGrv,
}

func DefaultRuntime() Runtime {
	return Runtime{
		SuppressAlt:       true,
		SuppressCaps:      true,
		AltMappings:       make(map[uint16]CompiledMapping),
		CapsMappings:      make(map[uint16]CompiledMapping),
		TapLayoutSwitches: make(map[uint16]LayoutSwitchTapAction),
		ShortcutMappings:  make(map[uint16]uint16),
	}
}

func LoadRuntime(path string) (Runtime, error) {
	cfg := DefaultRuntime()
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}

	rawBytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}

	rawCfg, err := parseRawConfigYAML(string(rawBytes))
	if err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}

	if err := applyRawConfig(&cfg, rawCfg); err != nil {
		return cfg, fmt.Errorf("compile config %s: %w", path, err)
	}
	return cfg, nil
}

func CloneCompiledMapping(m CompiledMapping) CompiledMapping {
	if len(m.ChordMods) > 0 {
		cloned := make([]uint16, len(m.ChordMods))
		copy(cloned, m.ChordMods)
		m.ChordMods = cloned
	}
	if len(m.KeySeq) > 0 {
		cloned := make([]uint16, len(m.KeySeq))
		copy(cloned, m.KeySeq)
		m.KeySeq = cloned
	}
	return m
}

func parseRawConfigYAML(raw string) (rawConfig, error) {
	decoded := yamlConfig{}
	dec := yaml.NewDecoder(strings.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&decoded); err != nil {
		if errors.Is(err, io.EOF) {
			return rawConfig{mappings: make(map[string]rawAction)}, nil
		}
		return rawConfig{}, err
	}

	var extra any
	if err := dec.Decode(&extra); err != nil && !errors.Is(err, io.EOF) {
		return rawConfig{}, err
	}
	if extra != nil {
		return rawConfig{}, errors.New("multiple YAML documents are not supported")
	}

	out := rawConfig{
		mappings:        make(map[string]rawAction),
		tapLayoutSwitch: make(map[uint16]LayoutSwitchTapAction),
	}
	if decoded.SuppressKeydown != nil {
		out.suppressDefined = true
		out.suppressKeys = append(out.suppressKeys, []string(*decoded.SuppressKeydown)...)
	}
	if decoded.Devices != nil {
		out.devices = append(out.devices, []string(*decoded.Devices)...)
	}
	if decoded.ShortcutLayout != nil {
		out.shortcutLayout = &ShortcutLayoutSpec{
			Layout:  trimASCIIWhitespace(decoded.ShortcutLayout.Layout),
			Variant: trimASCIIWhitespace(decoded.ShortcutLayout.Variant),
			Rules:   trimASCIIWhitespace(decoded.ShortcutLayout.Rules),
			Model:   trimASCIIWhitespace(decoded.ShortcutLayout.Model),
			Options: trimASCIIWhitespace(decoded.ShortcutLayout.Options),
		}
	}
	for keyName, action := range decoded.TapLayoutSwitch {
		code, err := ParseKeyName(keyName)
		if err != nil {
			return rawConfig{}, fmt.Errorf("tap_layout_switches %q: %w", keyName, err)
		}
		compiled, err := compileTapLayoutSwitchAction(keyName, action)
		if err != nil {
			return rawConfig{}, err
		}
		out.tapLayoutSwitch[code] = compiled
	}

	for binding, action := range decoded.Mappings {
		out.mappings[binding] = rawAction{
			passthrough: action.Passthrough,
			toSymbol:    action.ToSymbol,
			toKeys:      []string(action.ToKeys),
			toChord:     trimASCIIWhitespace(action.ToChord),
		}
	}
	return out, nil
}

func applyRawConfig(cfg *Runtime, raw rawConfig) error {
	if raw.suppressDefined {
		cfg.SuppressAlt = false
		cfg.SuppressCaps = false
		for _, key := range raw.suppressKeys {
			switch normalizeToken(key) {
			case "ALT":
				cfg.SuppressAlt = true
			case "CAPS", "CAPSLOCK":
				cfg.SuppressCaps = true
			default:
				return fmt.Errorf("unsupported suppress_keydown value %q", key)
			}
		}
	}

	if len(raw.devices) > 0 {
		devices, err := normalizeDevicePaths(raw.devices)
		if err != nil {
			return err
		}
		cfg.Devices = devices
	}
	if raw.shortcutLayout != nil {
		if raw.shortcutLayout.Layout == "" {
			return errors.New("shortcut_layout.layout must not be empty")
		}
		cfg.ShortcutLayout = &ShortcutLayoutSpec{
			Layout:  raw.shortcutLayout.Layout,
			Variant: raw.shortcutLayout.Variant,
			Rules:   raw.shortcutLayout.Rules,
			Model:   raw.shortcutLayout.Model,
			Options: raw.shortcutLayout.Options,
		}
	}
	keyParser, err := newKeyNameParser(cfg.ShortcutLayout)
	if err != nil {
		return err
	}
	for code, action := range raw.tapLayoutSwitch {
		switch code {
		case KeyLeftAlt, KeyRightAlt:
			if !cfg.SuppressAlt {
				return fmt.Errorf("tap_layout_switches %q requires suppress_keydown to include Alt", KeyName(code))
			}
		case KeyCapsLock:
			if !cfg.SuppressCaps {
				return fmt.Errorf("tap_layout_switches %q requires suppress_keydown to include Caps", KeyName(code))
			}
		default:
			return fmt.Errorf("tap_layout_switches %q is unsupported (only LAlt, RAlt, and Caps are supported)", KeyName(code))
		}
		cfg.TapLayoutSwitches[code] = action
	}

	for binding, action := range raw.mappings {
		layer, keyCode, err := parseBindingKeyWithParser(binding, keyParser)
		if err != nil {
			return err
		}
		compiled, err := compileActionWithParser(action, keyParser)
		if err != nil {
			return fmt.Errorf("mapping %q: %w", binding, err)
		}

		switch layer {
		case "ALT":
			cfg.AltMappings[keyCode] = compiled
		case "CAPS":
			cfg.CapsMappings[keyCode] = compiled
		default:
			return fmt.Errorf("mapping %q: unsupported layer %q", binding, layer)
		}
	}

	return nil
}

func compileTapLayoutSwitchAction(keyName string, action yamlTapLayoutSwitchAction) (LayoutSwitchTapAction, error) {
	layout := trimASCIIWhitespace(action.Layout)
	variant := trimASCIIWhitespace(action.Variant)
	setCount := 0
	if layout != "" || variant != "" {
		setCount++
	}
	if action.ToggleRecent {
		setCount++
	}
	if setCount == 0 {
		return LayoutSwitchTapAction{}, fmt.Errorf("tap_layout_switches %q: no action specified", keyName)
	}
	if setCount > 1 {
		return LayoutSwitchTapAction{}, fmt.Errorf("tap_layout_switches %q: only one action is allowed", keyName)
	}
	if action.ToggleRecent {
		return LayoutSwitchTapAction{Kind: LayoutSwitchTapToggleRecent}, nil
	}
	if layout == "" {
		return LayoutSwitchTapAction{}, fmt.Errorf("tap_layout_switches %q: layout must not be empty", keyName)
	}
	return LayoutSwitchTapAction{
		Kind:    LayoutSwitchTapToLayout,
		Layout:  layout,
		Variant: variant,
	}, nil
}

func compileAction(action rawAction) (CompiledMapping, error) {
	return compileActionWithParser(action, keyNameParser{})
}

func compileActionWithParser(action rawAction, parser keyNameParser) (CompiledMapping, error) {
	setCount := 0
	if action.passthrough != nil && *action.passthrough {
		setCount++
	}
	if action.toSymbol != "" {
		setCount++
	}
	if len(action.toKeys) > 0 {
		setCount++
	}
	if action.toChord != "" {
		setCount++
	}
	if setCount == 0 {
		return CompiledMapping{}, errors.New("no action specified (expected passthrough/to_symbol/to_keys/to_chord)")
	}
	if setCount > 1 {
		return CompiledMapping{}, errors.New("only one action is allowed per mapping")
	}

	if action.passthrough != nil && *action.passthrough {
		return CompiledMapping{Kind: MappingPassthrough}, nil
	}

	if action.toSymbol != "" {
		sym, err := symbolFromString(action.toSymbol)
		if err != nil {
			return CompiledMapping{}, err
		}
		return CompiledMapping{Kind: MappingSymbol, Symbol: sym}, nil
	}

	if len(action.toKeys) > 0 {
		keys, err := parseKeyListWithParser(action.toKeys, parser)
		if err != nil {
			return CompiledMapping{}, err
		}
		if len(keys) == 1 {
			return CompiledMapping{Kind: MappingRemap, RemapCode: keys[0]}, nil
		}
		return CompiledMapping{Kind: MappingKeySeq, KeySeq: keys}, nil
	}

	chordMods, chordKey, err := parseChordSpecWithParser(action.toChord, parser)
	if err != nil {
		return CompiledMapping{}, err
	}
	return CompiledMapping{
		Kind:      MappingChord,
		ChordKey:  chordKey,
		ChordMods: chordMods,
	}, nil
}

func symbolFromString(s string) (rune, error) {
	if strings.EqualFold(s, "NBSP") {
		s = "\u00a0"
	}
	if utf8.RuneCountInString(s) != 1 {
		return 0, fmt.Errorf("to_symbol must contain exactly 1 symbol, got %q", s)
	}
	r, _ := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return 0, fmt.Errorf("to_symbol must contain a valid symbol, got %q", s)
	}
	return r, nil
}

func parseBindingKeyWithParser(binding string, parser keyNameParser) (string, uint16, error) {
	parts := strings.Split(binding, "-")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("binding %q must be in <Layer>-<Key> form", binding)
	}

	layer := normalizeToken(parts[0])
	if layer != "ALT" && layer != "CAPS" {
		return "", 0, fmt.Errorf("binding %q uses unsupported layer %q", binding, parts[0])
	}

	keyCode, err := parser.Parse(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("binding %q: %w", binding, err)
	}
	return layer, keyCode, nil
}

func parseChordSpecWithParser(spec string, parser keyNameParser) ([]uint16, uint16, error) {
	parts := strings.Split(spec, "-")
	if len(parts) < 2 {
		return nil, 0, fmt.Errorf("to_chord %q must be <Mod>-...-<Key>", spec)
	}

	mods := make([]uint16, 0, len(parts)-1)
	for _, p := range parts[:len(parts)-1] {
		modCode, err := parseModifierName(p)
		if err != nil {
			return nil, 0, err
		}
		mods = append(mods, modCode)
	}

	keyCode, err := parser.Parse(parts[len(parts)-1])
	if err != nil {
		return nil, 0, err
	}
	return mods, keyCode, nil
}

func parseModifierName(name string) (uint16, error) {
	switch normalizeToken(name) {
	case "CTRL", "CONTROL":
		return KeyLeftCtrl, nil
	case "SHIFT":
		return KeyLeftShift, nil
	case "ALT":
		return KeyLeftAlt, nil
	case "META", "SUPER", "WIN":
		return KeyLeftMeta, nil
	default:
		return 0, fmt.Errorf("unsupported modifier %q", name)
	}
}

func parseKeyListWithParser(keys []string, parser keyNameParser) ([]uint16, error) {
	out := make([]uint16, 0, len(keys))
	for _, k := range keys {
		code, err := parser.Parse(k)
		if err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, nil
}

func ParseKeyName(name string) (uint16, error) {
	token := normalizeToken(name)
	if token == "" {
		return 0, errors.New("empty key name")
	}

	code, ok := keyCodeByName[token]
	if !ok {
		return 0, fmt.Errorf("unsupported key name %q", name)
	}
	return code, nil
}

func normalizeToken(s string) string {
	s = strings.Trim(s, " \t\r\n")
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			s = s[1 : len(s)-1]
		}
	}
	return strings.ToUpper(strings.Trim(s, " \t\r\n"))
}

func trimASCIIWhitespace(s string) string {
	return strings.Trim(s, " \t\r\n")
}

func normalizeDevicePaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.Trim(path, " \t\r\n")
		if trimmed == "" {
			return nil, errors.New("devices must not contain empty paths")
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out, nil
}
