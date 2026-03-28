package shortcut

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	xkb "github.com/thegrumpylion/xkb-go"

	"keyboard/pkg/config"
)

const xkbKeycodeOffset = 8

const (
	kdeKeyboardService   = "org.kde.keyboard"
	kdeKeyboardPath      = "/Layouts"
	kdeKeyboardInterface = "org.kde.KeyboardLayouts"
)

var shortcutCandidateCodes = []uint16{
	config.KeyGrv,
	config.Key1, config.Key2, config.Key3, config.Key4, config.Key5, config.Key6,
	config.Key7, config.Key8, config.Key9, config.Key0,
	config.KeyMinus, config.KeyEqual,
	config.KeyQ, config.KeyW, config.KeyE, config.KeyR, config.KeyT, config.KeyY,
	config.KeyU, config.KeyI, config.KeyO, config.KeyP,
	config.KeyLeftBrace, config.KeyRightBrace,
	config.KeyA, config.KeyS, config.KeyD, config.KeyF, config.KeyG, config.KeyH,
	config.KeyJ, config.KeyK, config.KeyL, config.KeySemicolon, config.KeyApostrophe,
	config.KeyBackslash,
	config.KeyZ, config.KeyX, config.KeyC, config.KeyV, config.KeyB, config.KeyN,
	config.KeyM, config.KeyComma, config.KeyDot, config.KeySlash,
	config.KeySpace,
}

type LayoutInfo struct {
	Layout      string
	Variant     string
	Description string
}

type runner interface {
	run(ctx context.Context, name string, args ...string) (string, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, msg)
	}
	return string(out), nil
}

type Loader struct {
	runner runner
}

type Provider struct {
	loader  Loader
	layouts []LayoutInfo
	remaps  map[string]map[uint16]uint16
}

func NewLoader() Loader {
	return Loader{runner: execRunner{}}
}

func NewLoaderWithRunner(r runner) Loader {
	if r == nil {
		r = execRunner{}
	}
	return Loader{runner: r}
}

func (l Loader) LoadCurrentRemap(ctx context.Context, target config.ShortcutLayoutSpec) (map[uint16]uint16, LayoutInfo, error) {
	active, err := l.currentKDELayout(ctx)
	if err != nil {
		return nil, LayoutInfo{}, err
	}

	source := config.ShortcutLayoutSpec{
		Layout:  active.Layout,
		Variant: active.Variant,
		Rules:   target.Rules,
		Model:   target.Model,
		Options: target.Options,
	}

	remap, err := BuildRemap(ctx, source, target)
	if err != nil {
		return nil, LayoutInfo{}, err
	}

	return remap, active, nil
}

func BuildRemap(ctx context.Context, source config.ShortcutLayoutSpec, target config.ShortcutLayoutSpec) (map[uint16]uint16, error) {
	xkbCtx := xkb.NewContext(ctx, xkb.ContextNoFlags)

	sourceKeymap, err := xkbCtx.NewKeymapFromNames(toRuleNames(source))
	if err != nil {
		return nil, fmt.Errorf("compile source layout %s: %w", formatLayout(source.Layout, source.Variant), err)
	}
	targetKeymap, err := xkbCtx.NewKeymapFromNames(toRuleNames(target))
	if err != nil {
		return nil, fmt.Errorf("compile target layout %s: %w", formatLayout(target.Layout, target.Variant), err)
	}

	return buildRemapFromKeymaps(sourceKeymap, targetKeymap), nil
}

func (l Loader) currentKDELayout(ctx context.Context) (LayoutInfo, error) {
	layouts, err := readConfiguredKDELayouts()
	if err != nil {
		return LayoutInfo{}, err
	}
	return l.currentKDELayoutFromList(ctx, layouts)
}

func (l Loader) currentKDELayoutFromList(ctx context.Context, layouts []LayoutInfo) (LayoutInfo, error) {
	indexOut, err := l.runKDELayoutMethod(ctx, "getLayout")
	if err != nil {
		return LayoutInfo{}, fmt.Errorf("query KDE active layout index: %w", err)
	}
	index, err := parseCurrentLayoutIndex(indexOut)
	if err != nil {
		return LayoutInfo{}, fmt.Errorf("parse KDE active layout index: %w", err)
	}
	if index < 0 || index >= len(layouts) {
		return LayoutInfo{}, fmt.Errorf("layout index %d out of range for %d KDE layouts", index, len(layouts))
	}

	return layouts[index], nil
}

func (l Loader) runKDELayoutMethod(ctx context.Context, method string, args ...string) (string, error) {
	busctlArgs := []string{
		"--user",
		"call",
		kdeKeyboardService,
		kdeKeyboardPath,
		kdeKeyboardInterface,
		method,
	}
	busctlArgs = append(busctlArgs, args...)

	out, err := l.runner.run(ctx, "busctl", busctlArgs...)
	if err == nil {
		return out, nil
	}
	if !errors.Is(err, exec.ErrNotFound) {
		return "", err
	}

	qdbusArgs := []string{
		kdeKeyboardService,
		kdeKeyboardPath,
		kdeKeyboardInterface + "." + method,
	}
	qdbusArgs = append(qdbusArgs, args...)
	return l.runner.run(ctx, "qdbus6", qdbusArgs...)
}

func NewProvider(ctx context.Context, target config.ShortcutLayoutSpec) (*Provider, map[uint16]uint16, LayoutInfo, error) {
	loader := NewLoader()
	layouts, err := readConfiguredKDELayouts()
	if err != nil {
		return nil, nil, LayoutInfo{}, err
	}

	remaps := make(map[string]map[uint16]uint16, len(layouts))
	for _, layout := range layouts {
		source := config.ShortcutLayoutSpec{
			Layout:  layout.Layout,
			Variant: layout.Variant,
			Rules:   target.Rules,
			Model:   target.Model,
			Options: target.Options,
		}
		remap, err := BuildRemap(ctx, source, target)
		if err != nil {
			return nil, nil, LayoutInfo{}, err
		}
		remaps[formatLayout(layout.Layout, layout.Variant)] = remap
	}

	provider := &Provider{
		loader:  loader,
		layouts: layouts,
		remaps:  remaps,
	}
	remap, active, err := provider.CurrentRemap(ctx)
	if err != nil {
		return nil, nil, LayoutInfo{}, err
	}
	return provider, remap, active, nil
}

func (p *Provider) CurrentRemap(ctx context.Context) (map[uint16]uint16, LayoutInfo, error) {
	active, err := p.loader.currentKDELayoutFromList(ctx, p.layouts)
	if err != nil {
		return nil, LayoutInfo{}, err
	}

	remap, ok := p.remaps[formatLayout(active.Layout, active.Variant)]
	if !ok {
		return nil, LayoutInfo{}, fmt.Errorf("no shortcut remap for active layout %s", formatLayout(active.Layout, active.Variant))
	}
	return remap, active, nil
}

func buildRemapFromKeymaps(sourceKeymap *xkb.Keymap, targetKeymap *xkb.Keymap) map[uint16]uint16 {
	sourceIndex := buildSymbolIndex(sourceKeymap)
	targetState := targetKeymap.NewState()
	remap := make(map[uint16]uint16)

	for _, code := range shortcutCandidateCodes {
		targetRune := targetState.KeyGetUTF32(evdevToXKB(code))
		if !isRemappableRune(targetRune) {
			continue
		}

		sourceCode, ok := sourceIndex[targetRune]
		if !ok || sourceCode == code {
			continue
		}
		remap[code] = sourceCode
	}

	return remap
}

func buildSymbolIndex(keymap *xkb.Keymap) map[rune]uint16 {
	state := keymap.NewState()
	index := make(map[rune]uint16)
	ambiguous := make(map[rune]bool)

	for _, code := range shortcutCandidateCodes {
		ch := state.KeyGetUTF32(evdevToXKB(code))
		if !isRemappableRune(ch) {
			continue
		}
		if ambiguous[ch] {
			continue
		}
		if existing, ok := index[ch]; ok && existing != code {
			delete(index, ch)
			ambiguous[ch] = true
			continue
		}
		index[ch] = code
	}

	return index
}

func toRuleNames(spec config.ShortcutLayoutSpec) *xkb.RuleNames {
	return &xkb.RuleNames{
		Rules:   spec.Rules,
		Model:   spec.Model,
		Layout:  spec.Layout,
		Variant: spec.Variant,
		Options: spec.Options,
	}
}

func evdevToXKB(code uint16) xkb.Keycode {
	return xkb.Keycode(uint32(code) + xkbKeycodeOffset)
}

func isRemappableRune(ch rune) bool {
	if ch == 0 {
		return false
	}
	if ch == ' ' {
		return true
	}
	return unicode.IsPrint(ch)
}

func parseCurrentLayoutIndex(raw string) (int, error) {
	trimmed := trimKDEScalarOutput(raw)
	if trimmed == "" {
		return 0, fmt.Errorf("empty KDE layout output")
	}

	idx, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid layout index %q", trimmed)
	}
	return idx, nil
}

func trimKDEScalarOutput(raw string) string {
	trimmed := strings.TrimSpace(raw)
	fields := strings.Fields(trimmed)
	if len(fields) == 2 && isBusctlScalarType(fields[0]) {
		return fields[1]
	}
	return trimmed
}

func isBusctlScalarType(token string) bool {
	switch token {
	case "b", "n", "q", "i", "u", "x", "t", "y":
		return true
	default:
		return false
	}
}

func readConfiguredKDELayouts() ([]LayoutInfo, error) {
	configPath, err := kdeKeyboardConfigPath()
	if err != nil {
		return nil, fmt.Errorf("resolve KDE keyboard config path: %w", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read KDE keyboard config %s: %w", configPath, err)
	}

	values := parseINISection(string(data), "Layout")
	layoutNames := splitCommaValues(values["LayoutList"])
	if len(layoutNames) == 0 {
		return nil, fmt.Errorf("KDE keyboard config %s does not define LayoutList", configPath)
	}

	variantNames := splitCommaValues(values["VariantList"])
	layouts := make([]LayoutInfo, 0, len(layoutNames))
	for i, layoutName := range layoutNames {
		variant := ""
		if i < len(variantNames) {
			variant = variantNames[i]
		}
		layouts = append(layouts, LayoutInfo{
			Layout:      layoutName,
			Variant:     variant,
			Description: formatLayout(layoutName, variant),
		})
	}
	return layouts, nil
}

func kdeKeyboardConfigPath() (string, error) {
	if xdgConfigHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "kxkbrc"), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".config", "kxkbrc"), nil
}

func parseINISection(raw string, sectionName string) map[string]string {
	section := ""
	values := make(map[string]string)

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if section != sectionName {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	return values
}

func splitCommaValues(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	parts := strings.Split(trimmed, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func formatLayout(layout string, variant string) string {
	if strings.TrimSpace(variant) == "" {
		return layout
	}
	return fmt.Sprintf("%s(%s)", layout, variant)
}
