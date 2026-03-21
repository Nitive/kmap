package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type mappingKind int

const (
	mappingPassthrough mappingKind = iota
	mappingSymbol
	mappingRemap
	mappingChord
	mappingKeySeq
)

type compiledMapping struct {
	kind      mappingKind
	symbol    symbolAction
	remapCode uint16
	chordKey  uint16
	chordMods []uint16
	keySeq    []uint16
}

type runtimeConfig struct {
	suppressAlt  bool
	suppressCaps bool
	devices      []string

	altMappings  map[uint16]compiledMapping
	capsMappings map[uint16]compiledMapping
}

type rawConfig struct {
	suppressDefined bool
	suppressKeys    []string
	devices         []string
	mappings        map[string]rawAction
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
	SuppressKeydown *stringOrList         `yaml:"suppress_keydown"`
	Devices         *stringOrList         `yaml:"devices"`
	Mappings        map[string]yamlAction `yaml:"mappings"`
}

var keyCodeByName = map[string]uint16{
	// Digits
	"1": key1,
	"2": key2,
	"3": key3,
	"4": key4,
	"5": key5,
	"6": key6,
	"7": key7,
	"8": key8,
	"9": key9,
	"0": key0,

	// Letters
	"A": keyA,
	"B": keyB,
	"C": keyC,
	"D": keyD,
	"E": keyE,
	"F": keyF,
	"G": keyG,
	"H": keyH,
	"I": keyI,
	"J": keyJ,
	"K": keyK,
	"L": keyL,
	"M": keyM,
	"N": keyN,
	"O": keyO,
	"P": keyP,
	"Q": keyQ,
	"R": keyR,
	"S": keyS,
	"T": keyT,
	"U": keyU,
	"V": keyV,
	"W": keyW,
	"X": keyX,
	"Y": keyY,
	"Z": keyZ,

	// Modifiers and common keys
	"ALT":       keyLeftAlt,
	"LALT":      keyLeftAlt,
	"RALT":      keyRightAlt,
	"CAPS":      keyCapsLock,
	"CAPSLOCK":  keyCapsLock,
	"TAB":       keyTab,
	"ENTER":     keyEnter,
	"RET":       keyEnter,
	"BACKSPACE": keyBackspace,
	"BSPC":      keyBackspace,
	"ESC":       keyEsc,
	"SPACE":     keySpace,
	"SPC":       keySpace,
	"CTRL":      keyLeftCtrl,
	"CONTROL":   keyLeftCtrl,
	"SHIFT":     keyLeftShift,
	"META":      keyLeftMeta,
	"SUPER":     keyLeftMeta,
	"WIN":       keyLeftMeta,
	"UP":        keyUp,
	"DOWN":      keyDown,
	"LEFT":      keyLeft,
	"RIGHT":     keyRight,

	// Named punctuation/special keys
	"MINUS":      keyMinus,
	"EQUAL":      keyEqual,
	"GRAVE":      keyGrv,
	"SEMICOLON":  keySemicolon,
	"APOSTROPHE": keyApostrophe,
	"COMMA":      keyComma,
	"DOT":        keyDot,
	"PERIOD":     keyDot,
	"SLASH":      keySlash,
	"BACKSLASH":  keyBackslash,
	"LEFTBRACE":  keyLeftBrace,
	"RIGHTBRACE": keyRightBrace,

	// Symbol aliases
	";":  keySemicolon,
	"'":  keyApostrophe,
	",":  keyComma,
	".":  keyDot,
	"/":  keySlash,
	"\\": keyBackslash,
	"[":  keyLeftBrace,
	"]":  keyRightBrace,
	"=":  keyEqual,
	"`":  keyGrv,
}

func defaultRuntimeConfig() runtimeConfig {
	return runtimeConfig{
		suppressAlt:  true,
		suppressCaps: true,
		altMappings:  make(map[uint16]compiledMapping),
		capsMappings: make(map[uint16]compiledMapping),
	}
}

func loadRuntimeConfig(path string) (runtimeConfig, error) {
	cfg := defaultRuntimeConfig()
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

	// Reject multi-document YAML to keep semantics deterministic.
	var extra any
	if err := dec.Decode(&extra); err != nil && !errors.Is(err, io.EOF) {
		return rawConfig{}, err
	}
	if extra != nil {
		return rawConfig{}, errors.New("multiple YAML documents are not supported")
	}

	out := rawConfig{
		mappings: make(map[string]rawAction),
	}

	if decoded.SuppressKeydown != nil {
		out.suppressDefined = true
		out.suppressKeys = append(out.suppressKeys, []string(*decoded.SuppressKeydown)...)
	}
	if decoded.Devices != nil {
		out.devices = append(out.devices, []string(*decoded.Devices)...)
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

func applyRawConfig(cfg *runtimeConfig, raw rawConfig) error {
	if raw.suppressDefined {
		cfg.suppressAlt = false
		cfg.suppressCaps = false
		for _, key := range raw.suppressKeys {
			switch normalizeToken(key) {
			case "ALT":
				cfg.suppressAlt = true
			case "CAPS", "CAPSLOCK":
				cfg.suppressCaps = true
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
		cfg.devices = devices
	}

	for binding, action := range raw.mappings {
		layer, keyCode, err := parseBindingKey(binding)
		if err != nil {
			return err
		}
		compiled, err := compileAction(action)
		if err != nil {
			return fmt.Errorf("mapping %q: %w", binding, err)
		}

		switch layer {
		case "ALT":
			cfg.altMappings[keyCode] = compiled
		case "CAPS":
			cfg.capsMappings[keyCode] = compiled
		default:
			return fmt.Errorf("mapping %q: unsupported layer %q", binding, layer)
		}
	}

	return nil
}

func compileAction(action rawAction) (compiledMapping, error) {
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
		return compiledMapping{}, errors.New("no action specified (expected passthrough/to_symbol/to_keys/to_chord)")
	}
	if setCount > 1 {
		return compiledMapping{}, errors.New("only one action is allowed per mapping")
	}

	if action.passthrough != nil && *action.passthrough {
		return compiledMapping{kind: mappingPassthrough}, nil
	}

	if action.toSymbol != "" {
		symAction, err := symbolActionFromString(action.toSymbol)
		if err != nil {
			return compiledMapping{}, err
		}
		return compiledMapping{
			kind:   mappingSymbol,
			symbol: symAction,
		}, nil
	}

	if len(action.toKeys) > 0 {
		keys, err := parseKeyList(action.toKeys)
		if err != nil {
			return compiledMapping{}, err
		}
		if len(keys) == 1 {
			return compiledMapping{
				kind:      mappingRemap,
				remapCode: keys[0],
			}, nil
		}
		return compiledMapping{
			kind:   mappingKeySeq,
			keySeq: keys,
		}, nil
	}

	chordMods, chordKey, err := parseChordSpec(action.toChord)
	if err != nil {
		return compiledMapping{}, err
	}
	return compiledMapping{
		kind:      mappingChord,
		chordKey:  chordKey,
		chordMods: chordMods,
	}, nil
}

func symbolActionFromString(s string) (symbolAction, error) {
	if strings.EqualFold(s, "NBSP") {
		s = "\u00a0"
	}
	if utf8.RuneCountInString(s) != 1 {
		return symbolAction{}, fmt.Errorf("to_symbol must contain exactly 1 symbol, got %q", s)
	}
	r, _ := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return symbolAction{}, fmt.Errorf("to_symbol must contain a valid symbol, got %q", s)
	}
	return symbolAction{symbol: r}, nil
}

func parseBindingKey(binding string) (string, uint16, error) {
	parts := strings.Split(binding, "-")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("binding %q must be in <Layer>-<Key> form", binding)
	}

	layer := normalizeToken(parts[0])
	if layer != "ALT" && layer != "CAPS" {
		return "", 0, fmt.Errorf("binding %q uses unsupported layer %q", binding, parts[0])
	}

	keyCode, err := parseKeyName(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("binding %q: %w", binding, err)
	}
	return layer, keyCode, nil
}

func parseChordSpec(spec string) ([]uint16, uint16, error) {
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

	keyCode, err := parseKeyName(parts[len(parts)-1])
	if err != nil {
		return nil, 0, err
	}
	return mods, keyCode, nil
}

func parseModifierName(name string) (uint16, error) {
	switch normalizeToken(name) {
	case "CTRL", "CONTROL":
		return keyLeftCtrl, nil
	case "SHIFT":
		return keyLeftShift, nil
	case "ALT":
		return keyLeftAlt, nil
	case "META", "SUPER", "WIN":
		return keyLeftMeta, nil
	default:
		return 0, fmt.Errorf("unsupported modifier %q", name)
	}
}

func parseKeyList(keys []string) ([]uint16, error) {
	out := make([]uint16, 0, len(keys))
	for _, k := range keys {
		code, err := parseKeyName(k)
		if err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, nil
}

func parseKeyName(name string) (uint16, error) {
	token := normalizeToken(name)
	if token == "" {
		return 0, errors.New("empty key name")
	}

	// Single-character aliases (letters and digits) are accepted directly.
	if len(token) == 1 {
		ch := token[0]
		if ch >= 'A' && ch <= 'Z' {
			if code, ok := keyCodeByName[token]; ok {
				return code, nil
			}
		}
		if ch >= '0' && ch <= '9' {
			if code, ok := keyCodeByName[token]; ok {
				return code, nil
			}
		}
	}

	code, ok := keyCodeByName[token]
	if !ok {
		return 0, fmt.Errorf("unsupported key name %q", name)
	}
	return code, nil
}

func normalizeToken(s string) string {
	return strings.ToUpper(strings.TrimSpace(trimQuotes(s)))
}

func trimQuotes(s string) string {
	s = trimASCIIWhitespace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
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
		trimmed := trimASCIIWhitespace(path)
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
