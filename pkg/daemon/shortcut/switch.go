package shortcut

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"sync"
	"time"

	xkb "github.com/thegrumpylion/xkb-go"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon/event"
)

const (
	defaultConfirmTimeout = 100 * time.Millisecond
	confirmPollInterval   = 2 * time.Millisecond
)

type TapSwitchInfo struct {
	SourceCode  uint16
	Action      config.LayoutSwitchTapAction
	Target      LayoutInfo
	TargetIndex int
}

type ValidationInfo struct {
	Current             LayoutInfo
	ShortcutTarget      LayoutInfo
	ShortcutTargetIndex int
	TapSwitches         []TapSwitchInfo
}

type resolvedTapSwitch struct {
	action      config.LayoutSwitchTapAction
	target      LayoutInfo
	targetIndex int
}

type SwitchManager struct {
	loader         Loader
	layouts        []LayoutInfo
	shortcutTarget LayoutInfo
	shortcutIndex  int
	tapSwitches    map[uint16]resolvedTapSwitch
	confirmTimeout time.Duration
	verbose        bool

	mu             sync.Mutex
	modDown        map[uint16]int
	pressedNonMods map[uint16]int
	switched       bool
	restoreIndex   int
	recentIndex    int
}

func NewSwitchManager(ctx context.Context, cfg config.Runtime, verbose bool) (*SwitchManager, ValidationInfo, error) {
	return NewSwitchManagerWithLoader(ctx, NewLoader(), cfg, verbose)
}

func NewSwitchManagerWithLoader(ctx context.Context, loader Loader, cfg config.Runtime, verbose bool) (*SwitchManager, ValidationInfo, error) {
	layouts, err := readConfiguredKDELayouts()
	if err != nil {
		return nil, ValidationInfo{}, err
	}

	current, err := loader.currentKDELayoutFromList(ctx, layouts)
	if err != nil {
		return nil, ValidationInfo{}, err
	}

	info := ValidationInfo{
		Current:             current,
		ShortcutTargetIndex: -1,
	}
	manager := &SwitchManager{
		loader:         loader,
		layouts:        layouts,
		shortcutIndex:  -1,
		tapSwitches:    make(map[uint16]resolvedTapSwitch, len(cfg.TapLayoutSwitches)),
		confirmTimeout: defaultConfirmTimeout,
		verbose:        verbose,
		modDown:        make(map[uint16]int),
		pressedNonMods: make(map[uint16]int),
		restoreIndex:   -1,
		recentIndex:    -1,
	}

	if cfg.ShortcutLayout != nil {
		targetIndex, targetInfo, err := findTargetLayout(layouts, *cfg.ShortcutLayout)
		if err != nil {
			return nil, ValidationInfo{}, err
		}
		if err := validateXKBLayout(ctx, *cfg.ShortcutLayout); err != nil {
			return nil, ValidationInfo{}, err
		}
		manager.shortcutTarget = targetInfo
		manager.shortcutIndex = targetIndex
		info.ShortcutTarget = targetInfo
		info.ShortcutTargetIndex = targetIndex
	}

	for _, sourceCode := range sortedTapSwitchSourceCodes(cfg.TapLayoutSwitches) {
		resolved, tapInfo, err := resolveTapSwitch(ctx, layouts, sourceCode, cfg.TapLayoutSwitches[sourceCode])
		if err != nil {
			return nil, ValidationInfo{}, err
		}
		manager.tapSwitches[sourceCode] = resolved
		info.TapSwitches = append(info.TapSwitches, tapInfo)
	}

	return manager, info, nil
}

func ValidateSwitchConfig(ctx context.Context, cfg config.Runtime) (ValidationInfo, error) {
	_, info, err := NewSwitchManager(ctx, cfg, false)
	if err != nil {
		return ValidationInfo{}, err
	}
	return info, nil
}

func ValidateShortcutLayout(ctx context.Context, target config.ShortcutLayoutSpec) (ValidationInfo, error) {
	cfg := config.DefaultRuntime()
	cfg.ShortcutLayout = &config.ShortcutLayoutSpec{
		Layout:  target.Layout,
		Variant: target.Variant,
		Rules:   target.Rules,
		Model:   target.Model,
		Options: target.Options,
	}
	return ValidateSwitchConfig(ctx, cfg)
}

func (m *SwitchManager) Wrap(in <-chan event.KeyEvent) (<-chan event.KeyEvent, <-chan error) {
	outCh := make(chan event.KeyEvent, 128)
	errCh := make(chan error, 1)

	go func() {
		defer close(outCh)
		defer close(errCh)

		for ev := range in {
			if err := m.Process(context.Background(), ev, func(out event.KeyEvent) error {
				outCh <- out
				return nil
			}); err != nil {
				errCh <- err
				return
			}
		}
	}()

	return outCh, errCh
}

func (m *SwitchManager) Process(ctx context.Context, ev event.KeyEvent, emit func(event.KeyEvent) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch ev.Kind {
	case event.KindKey:
		if err := emit(ev); err != nil {
			return err
		}

		if isShortcutModifier(ev.Code) {
			updateKeyCount(m.modDown, ev.Code, ev.Value)
			if m.shortcutIndex >= 0 && isShortcutActivator(ev.Code) && ev.Value == 1 && m.activatorCountLocked() == 1 {
				if err := m.activateLocked(ctx); err != nil {
					return err
				}
			}
		} else if m.switched {
			updateKeyCount(m.pressedNonMods, ev.Code, ev.Value)
		}

		if m.switched && m.activatorCountLocked() == 0 && len(m.pressedNonMods) == 0 {
			if err := m.restoreLocked(ctx); err != nil {
				return err
			}
		}
		return nil

	case event.KindLayoutSwitch:
		return m.handleTapLayoutSwitchLocked(ctx, ev.LayoutSwitch)

	default:
		return fmt.Errorf("unsupported event kind %d", ev.Kind)
	}
}

func (m *SwitchManager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.modDown = make(map[uint16]int)
	m.pressedNonMods = make(map[uint16]int)
	if !m.switched {
		return nil
	}
	return m.restoreLocked(ctx)
}

func (m *SwitchManager) activatorCountLocked() int {
	count := 0
	for code, down := range m.modDown {
		if !isShortcutActivator(code) || down <= 0 {
			continue
		}
		count += down
	}
	return count
}

func (m *SwitchManager) handleTapLayoutSwitchLocked(ctx context.Context, req event.LayoutSwitchRequest) error {
	resolved, ok := m.tapSwitches[req.SourceCode]
	if !ok {
		return fmt.Errorf("unexpected tap layout switch source %s", config.KeyName(req.SourceCode))
	}
	if m.switched || m.activatorCountLocked() > 0 {
		m.logf("ignoring tap layout switch while shortcut layout is active")
		return nil
	}

	switch resolved.action.Kind {
	case config.LayoutSwitchTapToLayout:
		return m.switchPersistentLocked(ctx, resolved.targetIndex, "tap")

	case config.LayoutSwitchTapToggleRecent:
		return m.toggleRecentLocked(ctx)

	default:
		return fmt.Errorf("unsupported tap layout switch kind %d", resolved.action.Kind)
	}
}

func (m *SwitchManager) activateLocked(ctx context.Context) error {
	if m.switched || m.shortcutIndex < 0 {
		return nil
	}

	currentIndex, err := m.loader.currentKDELayoutIndex(ctx)
	if err != nil {
		return err
	}
	if currentIndex < 0 || currentIndex >= len(m.layouts) {
		return fmt.Errorf("active KDE layout index %d out of range for %d configured layouts", currentIndex, len(m.layouts))
	}
	if currentIndex == m.shortcutIndex {
		m.logf("shortcut layout already active: %s", m.shortcutTarget.Description)
		return nil
	}

	if err := m.loader.setKDELayoutIndex(ctx, m.shortcutIndex); err != nil {
		return err
	}
	if err := m.confirmLayoutIndex(ctx, m.shortcutIndex); err != nil {
		return err
	}

	m.restoreIndex = currentIndex
	m.switched = true
	m.logf("shortcut layout switched: %s -> %s", m.layouts[currentIndex].Description, m.shortcutTarget.Description)
	return nil
}

func (m *SwitchManager) restoreLocked(ctx context.Context) error {
	if !m.switched || m.restoreIndex < 0 {
		m.switched = false
		m.restoreIndex = -1
		return nil
	}

	restoreIndex := m.restoreIndex
	if err := m.loader.setKDELayoutIndex(ctx, restoreIndex); err != nil {
		return err
	}
	if err := m.confirmLayoutIndex(ctx, restoreIndex); err != nil {
		return err
	}

	m.logf("shortcut layout restored: %s", m.layouts[restoreIndex].Description)
	m.switched = false
	m.restoreIndex = -1
	return nil
}

func (m *SwitchManager) switchPersistentLocked(ctx context.Context, targetIndex int, reason string) error {
	currentIndex, err := m.loader.currentKDELayoutIndex(ctx)
	if err != nil {
		return err
	}
	if currentIndex < 0 || currentIndex >= len(m.layouts) {
		return fmt.Errorf("active KDE layout index %d out of range for %d configured layouts", currentIndex, len(m.layouts))
	}
	if targetIndex < 0 || targetIndex >= len(m.layouts) {
		return fmt.Errorf("target KDE layout index %d out of range for %d configured layouts", targetIndex, len(m.layouts))
	}
	if currentIndex == targetIndex {
		m.logf("%s layout already active: %s", reason, m.layouts[targetIndex].Description)
		return nil
	}

	if err := m.loader.setKDELayoutIndex(ctx, targetIndex); err != nil {
		return err
	}
	if err := m.confirmLayoutIndex(ctx, targetIndex); err != nil {
		return err
	}

	m.recentIndex = currentIndex
	m.logf("%s layout switched: %s -> %s", reason, m.layouts[currentIndex].Description, m.layouts[targetIndex].Description)
	return nil
}

func (m *SwitchManager) toggleRecentLocked(ctx context.Context) error {
	if m.recentIndex < 0 {
		m.logf("tap recent layout ignored: no recent layout recorded")
		return nil
	}
	if m.recentIndex >= len(m.layouts) {
		return fmt.Errorf("recent KDE layout index %d out of range for %d configured layouts", m.recentIndex, len(m.layouts))
	}

	currentIndex, err := m.loader.currentKDELayoutIndex(ctx)
	if err != nil {
		return err
	}
	if currentIndex < 0 || currentIndex >= len(m.layouts) {
		return fmt.Errorf("active KDE layout index %d out of range for %d configured layouts", currentIndex, len(m.layouts))
	}
	if currentIndex == m.recentIndex {
		m.logf("tap recent layout ignored: current layout already matches recent layout")
		return nil
	}

	targetIndex := m.recentIndex
	if err := m.loader.setKDELayoutIndex(ctx, targetIndex); err != nil {
		return err
	}
	if err := m.confirmLayoutIndex(ctx, targetIndex); err != nil {
		return err
	}

	m.recentIndex = currentIndex
	m.logf("recent layout toggled: %s -> %s", m.layouts[currentIndex].Description, m.layouts[targetIndex].Description)
	return nil
}

func (m *SwitchManager) confirmLayoutIndex(ctx context.Context, want int) error {
	confirmCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		confirmCtx, cancel = context.WithTimeout(ctx, m.confirmTimeout)
		defer cancel()
	}

	for {
		current, err := m.loader.currentKDELayoutIndex(confirmCtx)
		if err != nil {
			return err
		}
		if current == want {
			return nil
		}

		select {
		case <-confirmCtx.Done():
			return fmt.Errorf("confirm layout index %d: %w", want, confirmCtx.Err())
		case <-time.After(confirmPollInterval):
		}
	}
}

func (m *SwitchManager) logf(format string, args ...any) {
	if m.verbose {
		log.Printf(format, args...)
	}
}

func (l Loader) currentKDELayoutIndex(ctx context.Context) (int, error) {
	indexOut, err := l.runKDELayoutMethod(ctx, "getLayout")
	if err != nil {
		return 0, fmt.Errorf("query KDE active layout index: %w", err)
	}
	index, err := parseCurrentLayoutIndex(indexOut)
	if err != nil {
		return 0, fmt.Errorf("parse KDE active layout index: %w", err)
	}
	return index, nil
}

func (l Loader) setKDELayoutIndex(ctx context.Context, index int) error {
	out, err := l.runKDELayoutMethod(ctx, "setLayout", "u", strconv.Itoa(index))
	if err != nil {
		return fmt.Errorf("set KDE layout index %d: %w", index, err)
	}

	trimmed := trimKDEScalarOutput(out)
	if trimmed == "" {
		return nil
	}

	ok, err := strconv.ParseBool(trimmed)
	if err != nil {
		return fmt.Errorf("parse KDE setLayout result %q: %w", trimmed, err)
	}
	if !ok {
		return fmt.Errorf("KDE rejected layout index %d", index)
	}
	return nil
}

func resolveTapSwitch(ctx context.Context, layouts []LayoutInfo, sourceCode uint16, action config.LayoutSwitchTapAction) (resolvedTapSwitch, TapSwitchInfo, error) {
	info := TapSwitchInfo{
		SourceCode:  sourceCode,
		Action:      action,
		TargetIndex: -1,
	}
	resolved := resolvedTapSwitch{
		action:      action,
		targetIndex: -1,
	}

	if action.Kind == config.LayoutSwitchTapToggleRecent {
		return resolved, info, nil
	}

	targetSpec := config.ShortcutLayoutSpec{
		Layout:  action.Layout,
		Variant: action.Variant,
	}
	targetIndex, targetInfo, err := findTargetLayout(layouts, targetSpec)
	if err != nil {
		return resolvedTapSwitch{}, TapSwitchInfo{}, fmt.Errorf("tap_layout_switches %s: %w", config.KeyName(sourceCode), err)
	}
	if err := validateXKBLayout(ctx, targetSpec); err != nil {
		return resolvedTapSwitch{}, TapSwitchInfo{}, fmt.Errorf("tap_layout_switches %s: %w", config.KeyName(sourceCode), err)
	}

	resolved.target = targetInfo
	resolved.targetIndex = targetIndex
	info.Target = targetInfo
	info.TargetIndex = targetIndex
	return resolved, info, nil
}

func sortedTapSwitchSourceCodes(actions map[uint16]config.LayoutSwitchTapAction) []uint16 {
	sourceCodes := make([]uint16, 0, len(actions))
	for sourceCode := range actions {
		sourceCodes = append(sourceCodes, sourceCode)
	}
	sort.Slice(sourceCodes, func(i, j int) bool { return sourceCodes[i] < sourceCodes[j] })
	return sourceCodes
}

func findTargetLayout(layouts []LayoutInfo, target config.ShortcutLayoutSpec) (int, LayoutInfo, error) {
	for i, layout := range layouts {
		if layout.Layout == target.Layout && layout.Variant == target.Variant {
			return i, layout, nil
		}
	}
	return 0, LayoutInfo{}, fmt.Errorf(
		"shortcut layout %s is not configured in KDE layout list",
		formatLayout(target.Layout, target.Variant),
	)
}

func validateXKBLayout(ctx context.Context, spec config.ShortcutLayoutSpec) error {
	xkbCtx := xkb.NewContext(ctx, xkb.ContextNoFlags)
	if _, err := xkbCtx.NewKeymapFromNames(toRuleNames(spec)); err != nil {
		return fmt.Errorf("compile target layout %s: %w", formatLayout(spec.Layout, spec.Variant), err)
	}
	return nil
}

func isShortcutModifier(code uint16) bool {
	switch code {
	case config.KeyLeftShift, config.KeyRightShift,
		config.KeyLeftCtrl, config.KeyRightCtrl,
		config.KeyLeftAlt, config.KeyRightAlt,
		config.KeyLeftMeta, config.KeyRightMeta:
		return true
	default:
		return false
	}
}

func isShortcutActivator(code uint16) bool {
	switch code {
	case config.KeyLeftCtrl, config.KeyRightCtrl,
		config.KeyLeftAlt, config.KeyRightAlt,
		config.KeyLeftMeta, config.KeyRightMeta:
		return true
	default:
		return false
	}
}

func updateKeyCount(counts map[uint16]int, code uint16, value int32) {
	switch value {
	case 1:
		counts[code]++
	case 0:
		if counts[code] <= 1 {
			delete(counts, code)
			return
		}
		counts[code]--
	}
}
