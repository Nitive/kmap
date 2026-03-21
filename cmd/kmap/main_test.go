package main

import (
	"errors"
	"fmt"
	"path/filepath"
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

func composeExpectedEvents(t *testing.T, sequence []uint16) []emittedKey {
	t.Helper()
	events := []emittedKey{
		event(keyScrollLock, 1), event(keyScrollLock, 0),
	}
	for _, keyCode := range sequence {
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

func loadRepoRuntimeConfig(t *testing.T) runtimeConfig {
	t.Helper()
	path := filepath.Join("..", "..", "kmap.yaml")
	cfg, err := loadRuntimeConfig(path)
	if err != nil {
		t.Fatalf("loadRuntimeConfig(%s): %v", path, err)
	}
	return cfg
}

func newConfiguredRemapper(t *testing.T, out keyEmitter) *remapper {
	t.Helper()
	cfg := loadRepoRuntimeConfig(t)
	return newRemapperWithConfig(out, 0, false, cfg)
}

func sortedSymbolKeys(cfg runtimeConfig) []uint16 {
	keys := make([]uint16, 0, len(cfg.altMappings))
	for k, mapping := range cfg.altMappings {
		if mapping.kind != mappingSymbol {
			continue
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func sortedCapsKeys(cfg runtimeConfig) []uint16 {
	keys := make([]uint16, 0, len(cfg.capsMappings))
	for k := range cfg.capsMappings {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func TestComposeRuneKeySupportsHexLetters(t *testing.T) {
	tests := []struct {
		ch   rune
		want uint16
	}{
		{ch: '0', want: key0},
		{ch: '9', want: key9},
		{ch: 'a', want: keyA},
		{ch: 'c', want: keyC},
		{ch: 'f', want: keyF},
		{ch: 'A', want: keyA},
		{ch: 'F', want: keyF},
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

func TestReleaseCapsOnStart(t *testing.T) {
	t.Run("toggles off when caps mappings exist and caps is enabled", func(t *testing.T) {
		out := &fakeEmitter{}
		cfg := runtimeConfig{
			capsMappings: map[uint16]compiledMapping{
				keyH: {kind: mappingRemap, remapCode: keyBackspace},
			},
		}

		if err := releaseCapsOnStart(out, cfg, true, 0); err != nil {
			t.Fatalf("releaseCapsOnStart: %v", err)
		}

		want := []emittedKey{event(keyCapsLock, 1), event(keyCapsLock, 0)}
		assertEventsEqual(t, out.events, want)
	})

	t.Run("noop without caps mappings", func(t *testing.T) {
		out := &fakeEmitter{}
		cfg := runtimeConfig{capsMappings: map[uint16]compiledMapping{}}

		if err := releaseCapsOnStart(out, cfg, true, 0); err != nil {
			t.Fatalf("releaseCapsOnStart: %v", err)
		}

		assertEventsEqual(t, out.events, nil)
	})

	t.Run("noop when caps mappings exist but caps is already disabled", func(t *testing.T) {
		out := &fakeEmitter{}
		cfg := runtimeConfig{
			capsMappings: map[uint16]compiledMapping{
				keyH: {kind: mappingRemap, remapCode: keyBackspace},
			},
		}

		if err := releaseCapsOnStart(out, cfg, false, 0); err != nil {
			t.Fatalf("releaseCapsOnStart: %v", err)
		}

		assertEventsEqual(t, out.events, nil)
	})
}

func TestResolveDevicePaths(t *testing.T) {
	cfg := runtimeConfig{
		devices: []string{"/dev/input/a", "/dev/input/b"},
	}

	t.Run("cli override wins", func(t *testing.T) {
		paths, err := resolveDevicePaths("/dev/input/override", cfg)
		if err != nil {
			t.Fatalf("resolveDevicePaths: %v", err)
		}
		if len(paths) != 1 || paths[0] != "/dev/input/override" {
			t.Fatalf("unexpected paths: %#v", paths)
		}
	})

	t.Run("config devices used when no override", func(t *testing.T) {
		paths, err := resolveDevicePaths("", cfg)
		if err != nil {
			t.Fatalf("resolveDevicePaths: %v", err)
		}
		if len(paths) != 2 || paths[0] != "/dev/input/a" || paths[1] != "/dev/input/b" {
			t.Fatalf("unexpected paths: %#v", paths)
		}
	})

	t.Run("default device fallback", func(t *testing.T) {
		paths, err := resolveDevicePaths("", runtimeConfig{})
		if err != nil {
			t.Fatalf("resolveDevicePaths: %v", err)
		}
		if len(paths) != 1 || paths[0] != defaultDevicePath {
			t.Fatalf("unexpected paths: %#v", paths)
		}
	})
}

func TestAltSymbolDoesNotEmitAlt(t *testing.T) {
	out := &fakeEmitter{}
	r := newConfiguredRemapper(t, out)

	runSequence(t, r, []emittedKey{
		event(keyLeftAlt, 1),
		event(keyL, 1),
		event(keyL, 0),
		event(keyLeftAlt, 0),
	})

	want := []emittedKey{}
	want = append(want, composeExpectedEvents(t, composeSequenceForRune('→'))...)
	assertEventsEqual(t, out.events, want)
}

func TestAltTabHeldAcrossMultipleTabs(t *testing.T) {
	out := &fakeEmitter{}
	r := newConfiguredRemapper(t, out)

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
	r := newConfiguredRemapper(t, out)

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
	r := newConfiguredRemapper(t, out)

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
	r := newConfiguredRemapper(t, out)

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
	r := newConfiguredRemapper(t, out)

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
	r := newConfiguredRemapper(t, out)

	runSequence(t, r, []emittedKey{
		event(keyLeftAlt, 1),
		event(keyL, 1),
		event(keyL, 0),
		event(keyLeftAlt, 0),
	})

	if err := r.cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	want := []emittedKey{}
	want = append(want, composeExpectedEvents(t, composeSequenceForRune('→'))...)
	assertEventsEqual(t, out.events, want)
}

func TestAltAllSymbolMappings(t *testing.T) {
	cfg := loadRepoRuntimeConfig(t)
	for _, k := range sortedSymbolKeys(cfg) {
		keyCode := k
		action := cfg.altMappings[keyCode].symbol
		t.Run(fmt.Sprintf("key_%d", keyCode), func(t *testing.T) {
			out := &fakeEmitter{}
			r := newConfiguredRemapper(t, out)

			runSequence(t, r, []emittedKey{
				event(keyLeftAlt, 1),
				event(keyCode, 1),
				event(keyCode, 0),
				event(keyLeftAlt, 0),
			})

			var want []emittedKey
			if action.symbol != 0 {
				want = composeExpectedEvents(t, composeSequenceForRune(action.symbol))
			}

			assertEventsEqual(t, out.events, want)
		})
	}
}

func TestCapsAllMappings(t *testing.T) {
	cfg := loadRepoRuntimeConfig(t)
	for _, k := range sortedCapsKeys(cfg) {
		keyCode := k
		action := cfg.capsMappings[keyCode]
		t.Run(fmt.Sprintf("key_%d", keyCode), func(t *testing.T) {
			out := &fakeEmitter{}
			r := newConfiguredRemapper(t, out)

			runSequence(t, r, []emittedKey{
				event(keyCapsLock, 1),
				event(keyCode, 1),
				event(keyCode, 0),
				event(keyCapsLock, 0),
			})

			var want []emittedKey
			if action.kind == mappingRemap {
				want = []emittedKey{
					event(action.remapCode, 1),
					event(action.remapCode, 0),
				}
			} else if action.kind == mappingChord {
				want = chordExpectedEvents(action.chordMods, action.chordKey)
			} else {
				t.Fatalf("unexpected caps mapping kind: %d", action.kind)
			}

			assertEventsEqual(t, out.events, want)
		})
	}
}
