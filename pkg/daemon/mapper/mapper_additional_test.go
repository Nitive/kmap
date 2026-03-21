package mapper

import (
	"testing"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon/event"
)

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

func TestCleanupReleasesActiveRemappedKeys(t *testing.T) {
	out := &fakeEmitter{}
	cfg := config.DefaultRuntime()
	cfg.CapsMappings[config.KeyH] = config.CompiledMapping{
		Kind:      config.MappingRemap,
		RemapCode: config.KeyBackspace,
	}
	r := newRemapperWithConfig(out, 0, false, cfg)

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
	r := newRemapperWithConfig(out, 0, false, cfg)

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
