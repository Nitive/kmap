package mapper

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon/event"
)

type emittedKey struct {
	code  uint16
	value int32
}

type emittedLayoutSwitch struct {
	sourceCode uint16
}

type fakeEmitter struct {
	events         []emittedKey
	layoutSwitches []emittedLayoutSwitch
	failAfter      int
	currentCall    int
}

func (f *fakeEmitter) emitKey(code uint16, value int32) error {
	f.currentCall++
	if f.failAfter > 0 && f.currentCall >= f.failAfter {
		return errors.New("forced emit error")
	}
	f.events = append(f.events, emittedKey{code: code, value: value})
	return nil
}

func (f *fakeEmitter) emitLayoutSwitch(action event.LayoutSwitchRequest) error {
	f.currentCall++
	if f.failAfter > 0 && f.currentCall >= f.failAfter {
		return errors.New("forced emit error")
	}
	f.layoutSwitches = append(f.layoutSwitches, emittedLayoutSwitch{sourceCode: action.SourceCode})
	return nil
}

func (f *fakeEmitter) tapKey(code uint16, _ time.Duration) error {
	if err := f.emitKey(code, 1); err != nil {
		return err
	}
	return f.emitKey(code, 0)
}

func evt(code uint16, value int32) emittedKey {
	return emittedKey{code: code, value: value}
}

func runSequence(t *testing.T, r *remapper, seq []emittedKey) {
	t.Helper()
	for _, ev := range seq {
		if err := r.handleKey(ev.code, ev.value); err != nil {
			t.Fatalf("handleKey(%d,%d): %v", ev.code, ev.value, err)
		}
	}
}

func assertEventsEqual(t *testing.T, got, want []emittedKey) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event count mismatch: got=%d want=%d\ngot=%v\nwant=%v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event[%d] mismatch: got=%v want=%v\nall got=%v\nall want=%v", i, got[i], want[i], got, want)
		}
	}
}

func composeExpectedEvents(t *testing.T, sequence []uint16) []emittedKey {
	t.Helper()
	events := []emittedKey{
		evt(config.KeyScrollLock, 1), evt(config.KeyScrollLock, 0),
	}
	for _, keyCode := range sequence {
		events = append(events, evt(keyCode, 1), evt(keyCode, 0))
	}
	return events
}

func chordExpectedEvents(mods []uint16, key uint16) []emittedKey {
	events := make([]emittedKey, 0, len(mods)*2+2)
	for _, m := range mods {
		events = append(events, evt(m, 1))
	}
	events = append(events, evt(key, 1), evt(key, 0))
	for i := len(mods) - 1; i >= 0; i-- {
		events = append(events, evt(mods[i], 0))
	}
	return events
}

func loadRepoRuntimeConfig(t *testing.T) config.Runtime {
	t.Helper()
	path := filepath.Join("..", "..", "..", "kmap.yaml")
	cfg, err := config.LoadRuntime(path)
	if err != nil {
		t.Fatalf("LoadRuntime(%s): %v", path, err)
	}
	return cfg
}

func newConfiguredRemapper(t *testing.T, out keyEmitter) *remapper {
	t.Helper()
	cfg := loadRepoRuntimeConfig(t)
	return newRemapperWithConfig(out, 0, false, nil, cfg)
}

func sortedSymbolKeys(cfg config.Runtime) []uint16 {
	keys := make([]uint16, 0, len(cfg.AltMappings))
	for k, mapping := range cfg.AltMappings {
		if mapping.Kind != config.MappingSymbol {
			continue
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func sortedCapsKeys(cfg config.Runtime) []uint16 {
	keys := make([]uint16, 0, len(cfg.CapsMappings))
	for k := range cfg.CapsMappings {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func collectMapperEvents(ch <-chan event.KeyEvent) []emittedKey {
	events := make([]emittedKey, 0)
	for ev := range ch {
		events = append(events, evt(ev.Code, ev.Value))
	}
	return events
}

func collectMapperErrors(ch <-chan error) []error {
	errs := make([]error, 0)
	for err := range ch {
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func TestComposeRuneKeySupportsHexLetters(t *testing.T) {
	tests := []struct {
		ch   rune
		want uint16
	}{
		{ch: '0', want: config.Key0},
		{ch: '9', want: config.Key9},
		{ch: 'a', want: config.KeyA},
		{ch: 'c', want: config.KeyC},
		{ch: 'f', want: config.KeyF},
		{ch: 'A', want: config.KeyA},
		{ch: 'F', want: config.KeyF},
	}

	for _, tc := range tests {
		got, err := composeRuneKey(tc.ch)
		if err != nil {
			t.Fatalf("composeRuneKey(%q): %v", tc.ch, err)
		}
		if got != tc.want {
			t.Fatalf("composeRuneKey(%q) mismatch: got=%d want=%d", tc.ch, got, tc.want)
		}
	}

	if _, err := composeRuneKey('-'); err == nil {
		t.Fatalf("composeRuneKey('-') expected error")
	}
}

func TestAltSymbolDoesNotEmitAlt(t *testing.T) {
	out := &fakeEmitter{}
	r := newConfiguredRemapper(t, out)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyL, 1),
		evt(config.KeyL, 0),
		evt(config.KeyLeftAlt, 0),
	})

	want := []emittedKey{}
	want = append(want, composeExpectedEvents(t, ComposeSequenceForRune('→'))...)
	assertEventsEqual(t, out.events, want)
}

func TestStartEmitsAltMappedKeySequence(t *testing.T) {
	cfg := config.DefaultRuntime()
	cfg.AltMappings[config.KeyJ] = config.CompiledMapping{
		Kind:   config.MappingKeySeq,
		KeySeq: []uint16{config.KeyTab, config.KeyEnter},
	}

	inCh := make(chan event.KeyEvent, 4)
	outCh, errCh := Start(cfg, inCh, Options{})

	inCh <- event.KeyEvent{Code: config.KeyLeftAlt, Value: 1}
	inCh <- event.KeyEvent{Code: config.KeyJ, Value: 1}
	inCh <- event.KeyEvent{Code: config.KeyJ, Value: 0}
	inCh <- event.KeyEvent{Code: config.KeyLeftAlt, Value: 0}
	close(inCh)

	gotEvents := collectMapperEvents(outCh)
	gotErrs := collectMapperErrors(errCh)
	if len(gotErrs) != 0 {
		t.Fatalf("unexpected errors: %v", gotErrs)
	}

	want := []emittedKey{
		evt(config.KeyTab, 1),
		evt(config.KeyTab, 0),
		evt(config.KeyEnter, 1),
		evt(config.KeyEnter, 0),
	}
	assertEventsEqual(t, gotEvents, want)
}

func TestAltTabHeldAcrossMultipleTabs(t *testing.T) {
	out := &fakeEmitter{}
	r := newConfiguredRemapper(t, out)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyTab, 1),
		evt(config.KeyTab, 0),
		evt(config.KeyTab, 1),
		evt(config.KeyTab, 0),
		evt(config.KeyLeftAlt, 0),
	})

	want := []emittedKey{
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyTab, 1),
		evt(config.KeyTab, 0),
		evt(config.KeyTab, 1),
		evt(config.KeyTab, 0),
		evt(config.KeyLeftAlt, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestAltSymbolWithShiftBecomesPassthrough(t *testing.T) {
	out := &fakeEmitter{}
	r := newConfiguredRemapper(t, out)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftShift, 1),
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyL, 1),
		evt(config.KeyL, 0),
		evt(config.KeyLeftAlt, 0),
		evt(config.KeyLeftShift, 0),
	})

	want := []emittedKey{
		evt(config.KeyLeftShift, 1),
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyL, 1),
		evt(config.KeyL, 0),
		evt(config.KeyLeftAlt, 0),
		evt(config.KeyLeftShift, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestExactAltShiftSymbolMappingMasksModifiers(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.ComboMappings[config.InputBinding{
		Modifiers: config.ModifierAlt | config.ModifierShift,
		KeyCode:   config.KeyE,
	}] = config.CompiledMapping{
		Kind:   config.MappingSymbol,
		Symbol: '€',
	}
	r := newRemapperWithConfig(out, 0, false, nil, cfg)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyLeftShift, 1),
		evt(config.KeyE, 1),
		evt(config.KeyE, 0),
		evt(config.KeyLeftShift, 0),
		evt(config.KeyLeftAlt, 0),
	})

	want := []emittedKey{
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyLeftShift, 1),
		evt(config.KeyLeftShift, 0),
		evt(config.KeyLeftAlt, 0),
	}
	want = append(want, composeExpectedEvents(t, ComposeSequenceForRune('€'))...)
	want = append(want,
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyLeftShift, 1),
		evt(config.KeyLeftShift, 0),
		evt(config.KeyLeftAlt, 0),
	)

	assertEventsEqual(t, out.events, want)
}

func TestExactAllModifierMappingIsSupported(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.SuppressAlt = false
	cfg.ComboMappings[config.InputBinding{
		Modifiers: config.ModifierCtrl | config.ModifierAlt | config.ModifierShift | config.ModifierMeta,
		KeyCode:   config.KeyK,
	}] = config.CompiledMapping{
		Kind:   config.MappingSymbol,
		Symbol: '§',
	}
	r := newRemapperWithConfig(out, 0, false, nil, cfg)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftCtrl, 1),
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyLeftShift, 1),
		evt(config.KeyLeftMeta, 1),
		evt(config.KeyK, 1),
		evt(config.KeyK, 0),
		evt(config.KeyLeftMeta, 0),
		evt(config.KeyLeftShift, 0),
		evt(config.KeyLeftAlt, 0),
		evt(config.KeyLeftCtrl, 0),
	})

	want := []emittedKey{
		evt(config.KeyLeftCtrl, 1),
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyLeftShift, 1),
		evt(config.KeyLeftMeta, 1),
		evt(config.KeyLeftShift, 0),
		evt(config.KeyLeftAlt, 0),
		evt(config.KeyLeftCtrl, 0),
		evt(config.KeyLeftMeta, 0),
	}
	want = append(want, composeExpectedEvents(t, ComposeSequenceForRune('§'))...)
	want = append(want,
		evt(config.KeyLeftMeta, 1),
		evt(config.KeyLeftCtrl, 1),
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyLeftShift, 1),
		evt(config.KeyLeftMeta, 0),
		evt(config.KeyLeftShift, 0),
		evt(config.KeyLeftAlt, 0),
		evt(config.KeyLeftCtrl, 0),
	)

	assertEventsEqual(t, out.events, want)
}

func TestCapsArrowAndBackspaceRemaps(t *testing.T) {
	out := &fakeEmitter{}
	r := newConfiguredRemapper(t, out)

	runSequence(t, r, []emittedKey{
		evt(config.KeyCapsLock, 1),
		evt(config.KeyH, 1), evt(config.KeyH, 0),
		evt(config.KeyJ, 1), evt(config.KeyJ, 0),
		evt(config.KeyK, 1), evt(config.KeyK, 0),
		evt(config.KeyL, 1), evt(config.KeyL, 0),
		evt(config.KeyCapsLock, 0),
	})

	want := []emittedKey{
		evt(config.KeyBackspace, 1), evt(config.KeyBackspace, 0),
		evt(config.KeyLeft, 1), evt(config.KeyLeft, 0),
		evt(config.KeyDown, 1), evt(config.KeyDown, 0),
		evt(config.KeyRight, 1), evt(config.KeyRight, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestCapsChordShortcut(t *testing.T) {
	out := &fakeEmitter{}
	r := newConfiguredRemapper(t, out)

	runSequence(t, r, []emittedKey{
		evt(config.KeyCapsLock, 1),
		evt(config.KeyA, 1),
		evt(config.KeyA, 0),
		evt(config.KeyCapsLock, 0),
	})

	want := []emittedKey{
		evt(config.KeyLeftMeta, 1),
		evt(config.KeyLeftCtrl, 1),
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyLeftShift, 1),
		evt(config.KeyA, 1),
		evt(config.KeyA, 0),
		evt(config.KeyLeftShift, 0),
		evt(config.KeyLeftAlt, 0),
		evt(config.KeyLeftCtrl, 0),
		evt(config.KeyLeftMeta, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestCleanupReleasesEmittedAlt(t *testing.T) {
	out := &fakeEmitter{}
	r := newConfiguredRemapper(t, out)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyTab, 1),
	})

	if err := r.cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	want := []emittedKey{
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyTab, 1),
		evt(config.KeyLeftAlt, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestCleanupNoAltWhenNeverEmitted(t *testing.T) {
	out := &fakeEmitter{}
	r := newConfiguredRemapper(t, out)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyL, 1),
		evt(config.KeyL, 0),
		evt(config.KeyLeftAlt, 0),
	})

	if err := r.cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	want := []emittedKey{}
	want = append(want, composeExpectedEvents(t, ComposeSequenceForRune('→'))...)
	assertEventsEqual(t, out.events, want)
}

func TestAltAllSymbolMappings(t *testing.T) {
	cfg := loadRepoRuntimeConfig(t)
	for _, k := range sortedSymbolKeys(cfg) {
		keyCode := k
		action := cfg.AltMappings[keyCode].Symbol
		t.Run(fmt.Sprintf("key_%d", keyCode), func(t *testing.T) {
			out := &fakeEmitter{}
			r := newConfiguredRemapper(t, out)

			runSequence(t, r, []emittedKey{
				evt(config.KeyLeftAlt, 1),
				evt(keyCode, 1),
				evt(keyCode, 0),
				evt(config.KeyLeftAlt, 0),
			})

			var want []emittedKey
			if action != 0 {
				want = composeExpectedEvents(t, ComposeSequenceForRune(action))
			}

			assertEventsEqual(t, out.events, want)
		})
	}
}

func TestCapsAllMappings(t *testing.T) {
	cfg := loadRepoRuntimeConfig(t)
	for _, k := range sortedCapsKeys(cfg) {
		keyCode := k
		action := cfg.CapsMappings[keyCode]
		t.Run(fmt.Sprintf("key_%d", keyCode), func(t *testing.T) {
			out := &fakeEmitter{}
			r := newConfiguredRemapper(t, out)

			runSequence(t, r, []emittedKey{
				evt(config.KeyCapsLock, 1),
				evt(keyCode, 1),
				evt(keyCode, 0),
				evt(config.KeyCapsLock, 0),
			})

			var want []emittedKey
			switch action.Kind {
			case config.MappingRemap:
				want = []emittedKey{
					evt(action.RemapCode, 1),
					evt(action.RemapCode, 0),
				}
			case config.MappingChord:
				want = chordExpectedEvents(action.ChordMods, action.ChordKey)
			default:
				t.Fatalf("unexpected caps mapping kind: %d", action.Kind)
			}

			assertEventsEqual(t, out.events, want)
		})
	}
}

func TestCleanupReleasesActiveRemappedKeys(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.CapsMappings[config.KeyH] = config.CompiledMapping{
		Kind:      config.MappingRemap,
		RemapCode: config.KeyBackspace,
	}
	r := newRemapperWithConfig(out, 0, false, nil, cfg)

	runSequence(t, r, []emittedKey{
		evt(config.KeyCapsLock, 1),
		evt(config.KeyH, 1),
	})

	if err := r.cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	want := []emittedKey{
		evt(config.KeyBackspace, 1),
		evt(config.KeyBackspace, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestAltPassthroughWhenSuppressionDisabled(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.SuppressAlt = false
	r := newRemapperWithConfig(out, 0, false, nil, cfg)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyJ, 1),
		evt(config.KeyJ, 0),
		evt(config.KeyLeftAlt, 0),
	})

	want := []emittedKey{
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyJ, 1),
		evt(config.KeyJ, 0),
		evt(config.KeyLeftAlt, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestShortcutRemapWithCtrl(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.ShortcutMappings[config.KeyDot] = config.KeyV
	r := newRemapperWithConfig(out, 0, false, nil, cfg)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftCtrl, 1),
		evt(config.KeyDot, 1),
		evt(config.KeyDot, 0),
		evt(config.KeyLeftCtrl, 0),
	})

	want := []emittedKey{
		evt(config.KeyLeftCtrl, 1),
		evt(config.KeyV, 1),
		evt(config.KeyV, 0),
		evt(config.KeyLeftCtrl, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestShortcutRemapWithCtrlAltPassthrough(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.ShortcutMappings[config.KeyDot] = config.KeyV
	r := newRemapperWithConfig(out, 0, false, nil, cfg)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftCtrl, 1),
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyDot, 1),
		evt(config.KeyDot, 0),
		evt(config.KeyLeftAlt, 0),
		evt(config.KeyLeftCtrl, 0),
	})

	want := []emittedKey{
		evt(config.KeyLeftCtrl, 1),
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyV, 1),
		evt(config.KeyV, 0),
		evt(config.KeyLeftAlt, 0),
		evt(config.KeyLeftCtrl, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestShortcutRemapDoesNotApplyWithShiftOnly(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.ShortcutMappings[config.KeyDot] = config.KeyV
	r := newRemapperWithConfig(out, 0, false, nil, cfg)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftShift, 1),
		evt(config.KeyDot, 1),
		evt(config.KeyDot, 0),
		evt(config.KeyLeftShift, 0),
	})

	want := []emittedKey{
		evt(config.KeyLeftShift, 1),
		evt(config.KeyDot, 1),
		evt(config.KeyDot, 0),
		evt(config.KeyLeftShift, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestShortcutRemapAppliesWithCtrlShift(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.ShortcutMappings[config.KeyDot] = config.KeyV
	r := newRemapperWithConfig(out, 0, false, nil, cfg)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftCtrl, 1),
		evt(config.KeyLeftShift, 1),
		evt(config.KeyDot, 1),
		evt(config.KeyDot, 0),
		evt(config.KeyLeftShift, 0),
		evt(config.KeyLeftCtrl, 0),
	})

	want := []emittedKey{
		evt(config.KeyLeftCtrl, 1),
		evt(config.KeyLeftShift, 1),
		evt(config.KeyV, 1),
		evt(config.KeyV, 0),
		evt(config.KeyLeftShift, 0),
		evt(config.KeyLeftCtrl, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestShortcutRemapUsesDynamicMappings(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.ShortcutMappings[config.KeyDot] = config.KeyV
	r := newRemapperWithConfig(out, 0, false, func() map[uint16]uint16 {
		return map[uint16]uint16{config.KeyDot: config.KeyC}
	}, cfg)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftCtrl, 1),
		evt(config.KeyDot, 1),
		evt(config.KeyDot, 0),
		evt(config.KeyLeftCtrl, 0),
	})

	want := []emittedKey{
		evt(config.KeyLeftCtrl, 1),
		evt(config.KeyC, 1),
		evt(config.KeyC, 0),
		evt(config.KeyLeftCtrl, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestAltTapEmitsLayoutSwitchRequest(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.TapLayoutSwitches[config.KeyLeftAlt] = config.LayoutSwitchTapAction{
		Kind:   config.LayoutSwitchTapToLayout,
		Layout: "us",
	}
	r := newRemapperWithConfig(out, 0, false, nil, cfg)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyLeftAlt, 0),
	})

	assertEventsEqual(t, out.events, nil)
	if len(out.layoutSwitches) != 1 || out.layoutSwitches[0].sourceCode != config.KeyLeftAlt {
		t.Fatalf("unexpected layout switch events: %#v", out.layoutSwitches)
	}
}

func TestAltTapDoesNotEmitLayoutSwitchAfterLayerUse(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.TapLayoutSwitches[config.KeyLeftAlt] = config.LayoutSwitchTapAction{
		Kind:   config.LayoutSwitchTapToLayout,
		Layout: "us",
	}
	cfg.AltMappings[config.KeyJ] = config.CompiledMapping{
		Kind:   config.MappingSymbol,
		Symbol: '←',
	}
	r := newRemapperWithConfig(out, 0, false, nil, cfg)

	runSequence(t, r, []emittedKey{
		evt(config.KeyLeftAlt, 1),
		evt(config.KeyJ, 1),
		evt(config.KeyJ, 0),
		evt(config.KeyLeftAlt, 0),
	})

	if len(out.layoutSwitches) != 0 {
		t.Fatalf("unexpected layout switch events: %#v", out.layoutSwitches)
	}
}

func TestCapsTapEmitsLayoutSwitchRequest(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.TapLayoutSwitches[config.KeyCapsLock] = config.LayoutSwitchTapAction{
		Kind: config.LayoutSwitchTapToggleRecent,
	}
	r := newRemapperWithConfig(out, 0, false, nil, cfg)

	runSequence(t, r, []emittedKey{
		evt(config.KeyCapsLock, 1),
		evt(config.KeyCapsLock, 0),
	})

	assertEventsEqual(t, out.events, nil)
	if len(out.layoutSwitches) != 1 || out.layoutSwitches[0].sourceCode != config.KeyCapsLock {
		t.Fatalf("unexpected layout switch events: %#v", out.layoutSwitches)
	}
}

func TestStuckKeyBug(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.ShortcutMappings[config.KeyA] = config.KeyB
	r := newRemapperWithConfig(out, 0, false, nil, cfg)

	t.Run("release_with_modifier_should_not_remap_if_press_was_not_remapped", func(t *testing.T) {
		out.events = nil
		runSequence(t, r, []emittedKey{
			evt(config.KeyA, 1),
			evt(config.KeyLeftCtrl, 1),
			evt(config.KeyA, 0),
			evt(config.KeyLeftCtrl, 0),
		})

		want := []emittedKey{
			evt(config.KeyA, 1),
			evt(config.KeyLeftCtrl, 1),
			evt(config.KeyA, 0),
			evt(config.KeyLeftCtrl, 0),
		}
		assertEventsEqual(t, out.events, want)
	})

	t.Run("press_with_modifier_should_remain_remapped_after_modifier_release", func(t *testing.T) {
		out.events = nil
		runSequence(t, r, []emittedKey{
			evt(config.KeyLeftCtrl, 1),
			evt(config.KeyA, 1),
			evt(config.KeyLeftCtrl, 0),
			evt(config.KeyA, 0),
		})

		want := []emittedKey{
			evt(config.KeyLeftCtrl, 1),
			evt(config.KeyB, 1),
			evt(config.KeyLeftCtrl, 0),
			evt(config.KeyB, 0),
		}
		assertEventsEqual(t, out.events, want)
	})

	t.Run("repeat_with_modifier_should_not_remap_if_press_was_not_remapped", func(t *testing.T) {
		out.events = nil
		runSequence(t, r, []emittedKey{
			evt(config.KeyA, 1),
			evt(config.KeyLeftCtrl, 1),
			evt(config.KeyA, 2),
			evt(config.KeyA, 0),
			evt(config.KeyLeftCtrl, 0),
		})

		want := []emittedKey{
			evt(config.KeyA, 1),
			evt(config.KeyLeftCtrl, 1),
			evt(config.KeyA, 2),
			evt(config.KeyA, 0),
			evt(config.KeyLeftCtrl, 0),
		}
		assertEventsEqual(t, out.events, want)
	})
}

func TestStuckKeyLayerBug(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.CapsMappings[config.KeyA] = config.CompiledMapping{
		Kind:   config.MappingSymbol,
		Symbol: '→',
	}
	r := newRemapperWithConfig(out, 0, false, nil, cfg)

	t.Run("release_with_layer_should_not_swallow_if_press_was_not_swallowed", func(t *testing.T) {
		out.events = nil
		runSequence(t, r, []emittedKey{
			evt(config.KeyA, 1),        // Press A (no layer)
			evt(config.KeyCapsLock, 1), // Press CapsLock (activates layer)
			evt(config.KeyA, 0),        // Release A (Layer is active) -> should NOT be swallowed
			evt(config.KeyCapsLock, 0), // Release CapsLock
		})

		want := []emittedKey{
			evt(config.KeyA, 1),
			evt(config.KeyA, 0),
		}
		assertEventsEqual(t, out.events, want)
	})
}
