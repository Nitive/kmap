package mapper

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon/event"
)

type Options struct {
	ComposeDelay time.Duration
	Verbose      bool
}

type keyEmitter interface {
	emitKey(code uint16, value int32) error
	tapKey(code uint16, delay time.Duration) error
}

type channelEmitter struct {
	out chan<- event.KeyEvent
}

func (e *channelEmitter) emitKey(code uint16, value int32) error {
	e.out <- event.KeyEvent{Code: code, Value: value}
	return nil
}

func (e *channelEmitter) tapKey(code uint16, delay time.Duration) error {
	if err := e.emitKey(code, 1); err != nil {
		return err
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	if err := e.emitKey(code, 0); err != nil {
		return err
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	return nil
}

type remapper struct {
	out          keyEmitter
	composeDelay time.Duration
	verbose      bool
	suppressAlt  bool
	suppressCaps bool
	altMappings  map[uint16]config.CompiledMapping
	capsMappings map[uint16]config.CompiledMapping

	altActive  bool
	altCode    uint16
	altEmitted bool
	capsActive bool

	activeRemapped map[uint16]uint16
	swallowed      map[uint16]bool
	modDown        map[uint16]bool
}

var composeDigitKey = map[rune]uint16{
	'0': config.Key0,
	'1': config.Key1,
	'2': config.Key2,
	'3': config.Key3,
	'4': config.Key4,
	'5': config.Key5,
	'6': config.Key6,
	'7': config.Key7,
	'8': config.Key8,
	'9': config.Key9,
}

func Start(cfg config.Runtime, in <-chan event.KeyEvent, opts Options) (<-chan event.KeyEvent, <-chan error) {
	outCh := make(chan event.KeyEvent, 128)
	errCh := make(chan error, 1)
	r := newRemapperWithConfig(&channelEmitter{out: outCh}, opts.ComposeDelay, opts.Verbose, cfg)

	go func() {
		defer close(outCh)
		defer close(errCh)

		for ev := range in {
			if err := r.handleKey(ev.Code, ev.Value); err != nil {
				errCh <- err
				return
			}
		}

		if err := r.cleanup(); err != nil {
			errCh <- err
		}
	}()

	return outCh, errCh
}

func newRemapperWithConfig(out keyEmitter, composeDelay time.Duration, verbose bool, cfg config.Runtime) *remapper {
	altMappings := make(map[uint16]config.CompiledMapping, len(cfg.AltMappings))
	for code, mapping := range cfg.AltMappings {
		altMappings[code] = config.CloneCompiledMapping(mapping)
	}
	capsMappings := make(map[uint16]config.CompiledMapping, len(cfg.CapsMappings))
	for code, mapping := range cfg.CapsMappings {
		capsMappings[code] = config.CloneCompiledMapping(mapping)
	}

	return &remapper{
		out:            out,
		composeDelay:   composeDelay,
		verbose:        verbose,
		suppressAlt:    cfg.SuppressAlt,
		suppressCaps:   cfg.SuppressCaps,
		altMappings:    altMappings,
		capsMappings:   capsMappings,
		activeRemapped: make(map[uint16]uint16),
		swallowed:      make(map[uint16]bool),
		modDown:        make(map[uint16]bool),
	}
}

func ComposeSequenceForRune(r rune) []uint16 {
	decimal := strconv.Itoa(int(r))
	sequence := make([]uint16, 0, len(decimal)+1)
	sequence = append(sequence, composeDigitKey[rune('0'+len(decimal))])
	for _, ch := range decimal {
		sequence = append(sequence, composeDigitKey[ch])
	}
	return sequence
}

func ComposeRuneKey(ch rune) (uint16, error) {
	if ch >= '0' && ch <= '9' {
		keyCode, ok := composeDigitKey[ch]
		if !ok {
			return 0, fmt.Errorf("compose code contains unsupported character %q", ch)
		}
		return keyCode, nil
	}

	if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
		keyCode, err := config.ParseKeyName(string(ch))
		if err != nil {
			return 0, fmt.Errorf("compose code contains unsupported character %q", ch)
		}
		return keyCode, nil
	}

	return 0, fmt.Errorf("compose code contains unsupported character %q", ch)
}

func (r *remapper) logf(format string, args ...any) {
	if r.verbose {
		log.Printf(format, args...)
	}
}

func (r *remapper) cleanup() error {
	var firstErr error
	if r.altActive && r.altEmitted {
		if err := r.out.emitKey(r.altCode, 0); err != nil {
			firstErr = err
		}
	}
	for srcCode, remapCode := range r.activeRemapped {
		if err := r.out.emitKey(remapCode, 0); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(r.activeRemapped, srcCode)
	}
	return firstErr
}

func isAlt(code uint16) bool {
	return code == config.KeyLeftAlt || code == config.KeyRightAlt
}

func isCaps(code uint16) bool {
	return code == config.KeyCapsLock
}

func isNonAltModifier(code uint16) bool {
	switch code {
	case config.KeyLeftShift, config.KeyRightShift, config.KeyLeftCtrl, config.KeyRightCtrl, config.KeyLeftMeta, config.KeyRightMeta:
		return true
	default:
		return false
	}
}

func (r *remapper) anyNonAltModifierDown() bool {
	for _, down := range r.modDown {
		if down {
			return true
		}
	}
	return false
}

func (r *remapper) emitAltDownIfNeeded() error {
	if r.altEmitted {
		return nil
	}
	if err := r.out.emitKey(r.altCode, 1); err != nil {
		return err
	}
	r.altEmitted = true
	r.logf("alt passthrough enabled (%d)", r.altCode)
	return nil
}

func (r *remapper) emitCompose(keys []uint16) error {
	if err := r.out.tapKey(config.KeyScrollLock, r.composeDelay); err != nil {
		return err
	}
	for _, keyCode := range keys {
		if err := r.out.tapKey(keyCode, r.composeDelay); err != nil {
			return err
		}
	}
	return nil
}

func (r *remapper) emitSymbol(symbol rune) error {
	if symbol == 0 {
		return nil
	}
	return r.emitCompose(ComposeSequenceForRune(symbol))
}

func (r *remapper) handleAltKey(code uint16, value int32) error {
	if !r.suppressAlt {
		r.altActive = false
		r.altEmitted = false
		return r.out.emitKey(code, value)
	}
	if value == 2 {
		return nil
	}

	switch value {
	case 1:
		if !r.altActive {
			r.altActive = true
			r.altCode = code
			r.altEmitted = false
			r.logf("alt down pending (%d)", code)
			return nil
		}

		if err := r.emitAltDownIfNeeded(); err != nil {
			return err
		}
		return r.out.emitKey(code, value)

	case 0:
		if r.altActive && code == r.altCode {
			if r.altEmitted {
				if err := r.out.emitKey(code, 0); err != nil {
					return err
				}
			}
			r.altActive = false
			r.altEmitted = false
			r.logf("alt released (%d)", code)
			return nil
		}
		if r.altEmitted {
			return r.out.emitKey(code, value)
		}
		return nil

	default:
		return nil
	}
}

func (r *remapper) emitKeySequence(keys []uint16) error {
	for _, keyCode := range keys {
		if err := r.out.tapKey(keyCode, r.composeDelay); err != nil {
			return err
		}
	}
	return nil
}

func (r *remapper) handleMappedAction(code uint16, value int32, mapping config.CompiledMapping, altLayer bool) error {
	switch mapping.Kind {
	case config.MappingPassthrough:
		if altLayer {
			if err := r.emitAltDownIfNeeded(); err != nil {
				return err
			}
		}
		return r.out.emitKey(code, value)

	case config.MappingSymbol:
		if value != 1 {
			return nil
		}
		r.swallowed[code] = true
		return r.emitSymbol(mapping.Symbol)

	case config.MappingRemap:
		if value == 1 {
			r.activeRemapped[code] = mapping.RemapCode
		}
		if value == 0 {
			delete(r.activeRemapped, code)
		}
		return r.out.emitKey(mapping.RemapCode, value)

	case config.MappingChord:
		if value != 1 {
			return nil
		}
		r.swallowed[code] = true
		return r.emitChord(mapping.ChordMods, mapping.ChordKey)

	case config.MappingKeySeq:
		if value != 1 {
			return nil
		}
		r.swallowed[code] = true
		return r.emitKeySequence(mapping.KeySeq)

	default:
		return fmt.Errorf("unsupported mapping kind %d", mapping.Kind)
	}
}

func (r *remapper) handlePendingAltKey(code uint16, value int32) error {
	if mapping, ok := r.altMappings[code]; ok && !r.anyNonAltModifierDown() {
		r.logf("alt key %d mapped via config kind=%d", code, mapping.Kind)
		return r.handleMappedAction(code, value, mapping, true)
	}

	if err := r.emitAltDownIfNeeded(); err != nil {
		return err
	}
	return r.out.emitKey(code, value)
}

func (r *remapper) emitChord(mods []uint16, targetCode uint16) error {
	for _, mod := range mods {
		if err := r.out.emitKey(mod, 1); err != nil {
			return err
		}
	}
	if err := r.out.emitKey(targetCode, 1); err != nil {
		return err
	}
	if err := r.out.emitKey(targetCode, 0); err != nil {
		return err
	}
	for i := len(mods) - 1; i >= 0; i-- {
		if err := r.out.emitKey(mods[i], 0); err != nil {
			return err
		}
	}
	return nil
}

func (r *remapper) handleCapsKey(value int32) error {
	if !r.suppressCaps {
		r.capsActive = false
		return r.out.emitKey(config.KeyCapsLock, value)
	}
	if value == 2 {
		return nil
	}
	switch value {
	case 1:
		r.capsActive = true
		r.logf("caps layer active")
	case 0:
		r.capsActive = false
		r.logf("caps layer inactive")
	}
	return nil
}

func (r *remapper) handleCapsLayerKey(code uint16, value int32) error {
	mapping, ok := r.capsMappings[code]
	if !ok {
		return r.out.emitKey(code, value)
	}

	r.logf("caps key %d mapped via config kind=%d", code, mapping.Kind)
	return r.handleMappedAction(code, value, mapping, false)
}

func (r *remapper) handleKey(code uint16, value int32) error {
	if isNonAltModifier(code) {
		switch value {
		case 1:
			r.modDown[code] = true
		case 0:
			r.modDown[code] = false
		}
	}

	if remapCode, ok := r.activeRemapped[code]; ok {
		if value == 0 {
			delete(r.activeRemapped, code)
		}
		return r.out.emitKey(remapCode, value)
	}

	if r.swallowed[code] {
		if value == 0 {
			delete(r.swallowed, code)
		}
		return nil
	}

	if isAlt(code) {
		return r.handleAltKey(code, value)
	}

	if isCaps(code) {
		return r.handleCapsKey(value)
	}

	if r.capsActive {
		return r.handleCapsLayerKey(code, value)
	}

	if r.altActive && !r.altEmitted {
		return r.handlePendingAltKey(code, value)
	}

	return r.out.emitKey(code, value)
}
