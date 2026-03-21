package config

import (
	"context"
	"fmt"
	"unicode"

	xkb "github.com/thegrumpylion/xkb-go"
)

const xkbKeycodeOffset = 8

var layoutCandidateCodes = []uint16{
	KeyGrv,
	Key1, Key2, Key3, Key4, Key5, Key6, Key7, Key8, Key9, Key0,
	KeyMinus, KeyEqual,
	KeyQ, KeyW, KeyE, KeyR, KeyT, KeyY, KeyU, KeyI, KeyO, KeyP,
	KeyLeftBrace, KeyRightBrace,
	KeyA, KeyS, KeyD, KeyF, KeyG, KeyH, KeyJ, KeyK, KeyL, KeySemicolon, KeyApostrophe,
	KeyBackslash,
	KeyZ, KeyX, KeyC, KeyV, KeyB, KeyN, KeyM, KeyComma, KeyDot, KeySlash,
	KeySpace,
}

type keyNameParser struct {
	layoutTokens map[string]uint16
}

func newKeyNameParser(layout *ShortcutLayoutSpec) (keyNameParser, error) {
	if layout == nil {
		return keyNameParser{}, nil
	}

	xkbCtx := xkb.NewContext(context.Background(), xkb.ContextNoFlags)
	keymap, err := xkbCtx.NewKeymapFromNames(&xkb.RuleNames{
		Rules:   layout.Rules,
		Model:   layout.Model,
		Layout:  layout.Layout,
		Variant: layout.Variant,
		Options: layout.Options,
	})
	if err != nil {
		return keyNameParser{}, fmt.Errorf("compile shortcut_layout %s: %w", formatLayout(layout.Layout, layout.Variant), err)
	}

	return keyNameParser{layoutTokens: buildLayoutTokenMap(keymap)}, nil
}

func (p keyNameParser) Parse(name string) (uint16, error) {
	token := normalizeToken(name)
	if token == "" {
		return 0, fmt.Errorf("empty key name")
	}

	if code, ok := p.layoutTokens[token]; ok {
		return code, nil
	}

	code, ok := keyCodeByName[token]
	if !ok {
		return 0, fmt.Errorf("unsupported key name %q", name)
	}
	return code, nil
}

func buildLayoutTokenMap(keymap *xkb.Keymap) map[string]uint16 {
	baseState := keymap.NewState()
	shiftState := keymap.NewState()
	shiftState.UpdateMask(xkb.ModShift, 0, 0, 0, 0, 0)

	tokens := make(map[string]uint16)
	ambiguous := make(map[string]bool)
	for _, code := range layoutCandidateCodes {
		addLayoutTokens(tokens, ambiguous, code, baseState.KeyGetUTF32(evdevToXKB(code)))
		addLayoutTokens(tokens, ambiguous, code, shiftState.KeyGetUTF32(evdevToXKB(code)))
	}
	return tokens
}

func addLayoutTokens(tokens map[string]uint16, ambiguous map[string]bool, code uint16, ch rune) {
	if ch == 0 || (!unicode.IsPrint(ch) && ch != ' ') {
		return
	}

	for _, alias := range layoutKeyAliases(ch) {
		if alias == "" {
			continue
		}
		token := normalizeToken(alias)
		if token == "" || ambiguous[token] {
			continue
		}
		if existing, ok := tokens[token]; ok && existing != code {
			delete(tokens, token)
			ambiguous[token] = true
			continue
		}
		tokens[token] = code
	}
}

func layoutKeyAliases(ch rune) []string {
	switch ch {
	case ' ':
		return []string{"SPACE", "SPC"}
	case '-':
		return []string{"-", "MINUS"}
	case '=':
		return []string{"=", "EQUAL"}
	case '`':
		return []string{"`", "GRAVE"}
	case ';':
		return []string{";", "SEMICOLON"}
	case '\'':
		return []string{"'", "APOSTROPHE"}
	case ',':
		return []string{",", "COMMA"}
	case '.':
		return []string{".", "DOT", "PERIOD"}
	case '/':
		return []string{"/", "SLASH"}
	case '\\':
		return []string{"\\", "BACKSLASH"}
	case '[':
		return []string{"[", "LEFTBRACE"}
	case ']':
		return []string{"]", "RIGHTBRACE"}
	default:
		return []string{string(ch)}
	}
}

func evdevToXKB(code uint16) xkb.Keycode {
	return xkb.Keycode(uint32(code) + xkbKeycodeOffset)
}

func formatLayout(layout string, variant string) string {
	if trimASCIIWhitespace(variant) == "" {
		return layout
	}
	return fmt.Sprintf("%s(%s)", layout, variant)
}
