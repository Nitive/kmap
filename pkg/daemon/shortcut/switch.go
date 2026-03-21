package shortcut

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
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

type ValidationInfo struct {
	Current     LayoutInfo
	Target      LayoutInfo
	TargetIndex int
}

type SwitchManager struct {
	loader         Loader
	layouts        []LayoutInfo
	target         LayoutInfo
	targetIndex    int
	confirmTimeout time.Duration
	verbose        bool

	mu             sync.Mutex
	modDown        map[uint16]int
	pressedNonMods map[uint16]int
	switched       bool
	restoreIndex   int
}

func NewSwitchManager(ctx context.Context, target config.ShortcutLayoutSpec, verbose bool) (*SwitchManager, ValidationInfo, error) {
	return NewSwitchManagerWithLoader(ctx, NewLoader(), target, verbose)
}

func NewSwitchManagerWithLoader(ctx context.Context, loader Loader, target config.ShortcutLayoutSpec, verbose bool) (*SwitchManager, ValidationInfo, error) {
	layouts, err := readConfiguredKDELayouts()
	if err != nil {
		return nil, ValidationInfo{}, err
	}

	current, err := loader.currentKDELayoutFromList(ctx, layouts)
	if err != nil {
		return nil, ValidationInfo{}, err
	}

	targetIndex, targetInfo, err := findTargetLayout(layouts, target)
	if err != nil {
		return nil, ValidationInfo{}, err
	}

	if err := validateXKBLayout(ctx, target); err != nil {
		return nil, ValidationInfo{}, err
	}

	manager := &SwitchManager{
		loader:         loader,
		layouts:        layouts,
		target:         targetInfo,
		targetIndex:    targetIndex,
		confirmTimeout: defaultConfirmTimeout,
		verbose:        verbose,
		modDown:        make(map[uint16]int),
		pressedNonMods: make(map[uint16]int),
		restoreIndex:   -1,
	}

	return manager, ValidationInfo{
		Current:     current,
		Target:      targetInfo,
		TargetIndex: targetIndex,
	}, nil
}

func ValidateShortcutLayout(ctx context.Context, target config.ShortcutLayoutSpec) (ValidationInfo, error) {
	_, info, err := NewSwitchManager(ctx, target, false)
	if err != nil {
		return ValidationInfo{}, err
	}
	return info, nil
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

	if err := emit(ev); err != nil {
		return err
	}

	if isShortcutModifier(ev.Code) {
		updateKeyCount(m.modDown, ev.Code, ev.Value)
		if isShortcutActivator(ev.Code) && ev.Value == 1 && m.activatorCountLocked() == 1 {
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

func (m *SwitchManager) activateLocked(ctx context.Context) error {
	if m.switched {
		return nil
	}

	currentIndex, err := m.loader.currentKDELayoutIndex(ctx)
	if err != nil {
		return err
	}
	if currentIndex < 0 || currentIndex >= len(m.layouts) {
		return fmt.Errorf("active KDE layout index %d out of range for %d configured layouts", currentIndex, len(m.layouts))
	}
	if currentIndex == m.targetIndex {
		m.logf("shortcut layout already active: %s", m.target.Description)
		return nil
	}

	if err := m.loader.setKDELayoutIndex(ctx, m.targetIndex); err != nil {
		return err
	}
	if err := m.confirmLayoutIndex(ctx, m.targetIndex); err != nil {
		return err
	}

	m.restoreIndex = currentIndex
	m.switched = true
	m.logf("shortcut layout switched: %s -> %s", m.layouts[currentIndex].Description, m.target.Description)
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
	indexOut, err := l.runner.run(ctx, "qdbus6", "org.kde.keyboard", "/Layouts", "org.kde.KeyboardLayouts.getLayout")
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
	out, err := l.runner.run(
		ctx,
		"qdbus6",
		"org.kde.keyboard",
		"/Layouts",
		"org.kde.KeyboardLayouts.setLayout",
		strconv.Itoa(index),
	)
	if err != nil {
		return fmt.Errorf("set KDE layout index %d: %w", index, err)
	}

	trimmed := strings.TrimSpace(out)
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
