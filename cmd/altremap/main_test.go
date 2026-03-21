package main

import (
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"
)

type emittedKey struct {
	code  uint16
	value int32
}

type fakeEmitter struct {
	events      []emittedKey
	failAfter   int
	currentCall int
}

func (f *fakeEmitter) emitKey(code uint16, value int32) error {
	f.currentCall++
	if f.failAfter > 0 && f.currentCall >= f.failAfter {
		return errors.New("forced emit error")
	}
	f.events = append(f.events, emittedKey{code: code, value: value})
	return nil
}

func (f *fakeEmitter) tapKey(code uint16, _ time.Duration) error {
	if err := f.emitKey(code, 1); err != nil {
		return err
	}
	return f.emitKey(code, 0)
}

func event(code uint16, value int32) emittedKey {
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

func composeExpectedEvents(t *testing.T, code string) []emittedKey {
	t.Helper()
	events := []emittedKey{
		event(keyScrollLock, 1), event(keyScrollLock, 0),
	}
	for _, ch := range code {
		keyCode, ok := composeDigitKey[ch]
		if !ok {
			t.Fatalf("unsupported compose digit in test expectation: %q", ch)
		}
		events = append(events, event(keyCode, 1), event(keyCode, 0))
	}
	return events
}

func chordExpectedEvents(mods []uint16, key uint16) []emittedKey {
	events := make([]emittedKey, 0, len(mods)*2+2)
	for _, m := range mods {
		events = append(events, event(m, 1))
	}
	events = append(events, event(key, 1), event(key, 0))
	for i := len(mods) - 1; i >= 0; i-- {
		events = append(events, event(mods[i], 0))
	}
	return events
}

func sortedSymbolKeys() []uint16 {
	keys := make([]uint16, 0, len(symbolMap))
	for k := range symbolMap {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func sortedCapsKeys() []uint16 {
	keys := make([]uint16, 0, len(capsActionMap))
	for k := range capsActionMap {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func TestAltSymbolDoesNotEmitAlt(t *testing.T) {
	out := &fakeEmitter{}
	r := newRemapper(out, 0, false)

	runSequence(t, r, []emittedKey{
		event(keyLeftAlt, 1),
		event(keyL, 1),
		event(keyL, 0),
		event(keyLeftAlt, 0),
	})

	want := []emittedKey{
		event(keyScrollLock, 1), event(keyScrollLock, 0),
		event(key2, 1), event(key2, 0),
		event(key2, 1), event(key2, 0),
		event(key0, 1), event(key0, 0),
		event(key1, 1), event(key1, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestAltTabHeldAcrossMultipleTabs(t *testing.T) {
	out := &fakeEmitter{}
	r := newRemapper(out, 0, false)

	runSequence(t, r, []emittedKey{
		event(keyLeftAlt, 1),
		event(keyTab, 1),
		event(keyTab, 0),
		event(keyTab, 1),
		event(keyTab, 0),
		event(keyLeftAlt, 0),
	})

	want := []emittedKey{
		event(keyLeftAlt, 1),
		event(keyTab, 1),
		event(keyTab, 0),
		event(keyTab, 1),
		event(keyTab, 0),
		event(keyLeftAlt, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestAltSymbolWithShiftBecomesPassthrough(t *testing.T) {
	out := &fakeEmitter{}
	r := newRemapper(out, 0, false)

	runSequence(t, r, []emittedKey{
		event(keyLeftShift, 1),
		event(keyLeftAlt, 1),
		event(keyL, 1),
		event(keyL, 0),
		event(keyLeftAlt, 0),
		event(keyLeftShift, 0),
	})

	want := []emittedKey{
		event(keyLeftShift, 1),
		event(keyLeftAlt, 1),
		event(keyL, 1),
		event(keyL, 0),
		event(keyLeftAlt, 0),
		event(keyLeftShift, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestCapsArrowAndBackspaceRemaps(t *testing.T) {
	out := &fakeEmitter{}
	r := newRemapper(out, 0, false)

	runSequence(t, r, []emittedKey{
		event(keyCapsLock, 1),
		event(keyH, 1), event(keyH, 0),
		event(keyJ, 1), event(keyJ, 0),
		event(keyK, 1), event(keyK, 0),
		event(keyL, 1), event(keyL, 0),
		event(keyCapsLock, 0),
	})

	want := []emittedKey{
		event(keyBackspace, 1), event(keyBackspace, 0),
		event(keyLeft, 1), event(keyLeft, 0),
		event(keyDown, 1), event(keyDown, 0),
		event(keyRight, 1), event(keyRight, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestCapsChordShortcut(t *testing.T) {
	out := &fakeEmitter{}
	r := newRemapper(out, 0, false)

	runSequence(t, r, []emittedKey{
		event(keyCapsLock, 1),
		event(keyA, 1),
		event(keyA, 0), // swallowed in caps chord mode
		event(keyCapsLock, 0),
	})

	want := []emittedKey{
		event(keyLeftMeta, 1),
		event(keyLeftCtrl, 1),
		event(keyLeftAlt, 1),
		event(keyLeftShift, 1),
		event(keyA, 1),
		event(keyA, 0),
		event(keyLeftShift, 0),
		event(keyLeftAlt, 0),
		event(keyLeftCtrl, 0),
		event(keyLeftMeta, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestCleanupReleasesEmittedAlt(t *testing.T) {
	out := &fakeEmitter{}
	r := newRemapper(out, 0, false)

	runSequence(t, r, []emittedKey{
		event(keyLeftAlt, 1),
		event(keyTab, 1),
	})

	if err := r.cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	want := []emittedKey{
		event(keyLeftAlt, 1),
		event(keyTab, 1),
		event(keyLeftAlt, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestCleanupNoAltWhenNeverEmitted(t *testing.T) {
	out := &fakeEmitter{}
	r := newRemapper(out, 0, false)

	runSequence(t, r, []emittedKey{
		event(keyLeftAlt, 1),
		event(keyL, 1),
		event(keyL, 0),
		event(keyLeftAlt, 0),
	})

	if err := r.cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	want := []emittedKey{
		event(keyScrollLock, 1), event(keyScrollLock, 0),
		event(key2, 1), event(key2, 0),
		event(key2, 1), event(key2, 0),
		event(key0, 1), event(key0, 0),
		event(key1, 1), event(key1, 0),
	}
	assertEventsEqual(t, out.events, want)
}

func TestAltAllSymbolMappings(t *testing.T) {
	for _, k := range sortedSymbolKeys() {
		keyCode := k
		action := symbolMap[keyCode]
		t.Run(fmt.Sprintf("key_%d", keyCode), func(t *testing.T) {
			out := &fakeEmitter{}
			r := newRemapper(out, 0, false)

			runSequence(t, r, []emittedKey{
				event(keyLeftAlt, 1),
				event(keyCode, 1),
				event(keyCode, 0),
				event(keyLeftAlt, 0),
			})

			var want []emittedKey
			switch {
			case action.compose != "":
				want = composeExpectedEvents(t, action.compose)
			case action.pipe:
				want = []emittedKey{
					event(keyLeftShift, 1),
					event(keyBackslash, 1),
					event(keyBackslash, 0),
					event(keyLeftShift, 0),
				}
			default:
				want = nil
			}

			assertEventsEqual(t, out.events, want)
		})
	}
}

func TestCapsAllMappings(t *testing.T) {
	for _, k := range sortedCapsKeys() {
		keyCode := k
		action := capsActionMap[keyCode]
		t.Run(fmt.Sprintf("key_%d", keyCode), func(t *testing.T) {
			out := &fakeEmitter{}
			r := newRemapper(out, 0, false)

			runSequence(t, r, []emittedKey{
				event(keyCapsLock, 1),
				event(keyCode, 1),
				event(keyCode, 0),
				event(keyCapsLock, 0),
			})

			var want []emittedKey
			if action.remapCode != 0 {
				want = []emittedKey{
					event(action.remapCode, 1),
					event(action.remapCode, 0),
				}
			} else {
				want = chordExpectedEvents(action.chordMods, action.chordKey)
			}

			assertEventsEqual(t, out.events, want)
		})
	}
}
