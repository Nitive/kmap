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
	ComposeDelay     time.Duration
	Verbose          bool
	ShortcutMappings func() map[uint16]uint16
}

type keyEmitter interface {
	emitKey(code uint16, value int32) error
	emitLayoutSwitch(action event.LayoutSwitchRequest) error
	tapKey(code uint16, delay time.Duration) error
}

type channelEmitter struct {
	out chan<- event.KeyEvent
}

type activeRemap struct {
	code             uint16
	restoreModifiers []uint16
}

func (e *channelEmitter) emitKey(code uint16, value int32) error {
	e.out <- event.KeyEvent{Kind: event.KindKey, Code: code, Value: value}
	return nil
}

func (e *channelEmitter) emitLayoutSwitch(action event.LayoutSwitchRequest) error {
	e.out <- event.KeyEvent{
		Kind:         event.KindLayoutSwitch,
		LayoutSwitch: action,
	}
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
	out               keyEmitter
	composeDelay      time.Duration
	verbose           bool
	suppressAlt       bool
	suppressCaps      bool
	altMappings       map[uint16]config.CompiledMapping
	capsMappings      map[uint16]config.CompiledMapping
	comboMappings     map[config.InputBinding]config.CompiledMapping
	tapLayoutSwitches map[uint16]config.LayoutSwitchTapAction
	shortcutMaps      map[uint16]uint16
	shortcutFn        func() map[uint16]uint16

	altActive  bool
	altCode    uint16
	altEmitted bool
	altUsed    bool
	capsActive bool
	capsUsed   bool

	activeRemapped  map[uint16]activeRemap
	swallowed       map[uint16]bool
	maskedModifiers map[uint16]int
	modDown         map[uint16]bool
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
	r := newRemapperWithConfig(&channelEmitter{out: outCh}, opts.ComposeDelay, opts.Verbose, opts.ShortcutMappings, cfg)

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

func newRemapperWithConfig(out keyEmitter, composeDelay time.Duration, verbose bool, shortcutFn func() map[uint16]uint16, cfg config.Runtime) *remapper {
	altMappings := make(map[uint16]config.CompiledMapping, len(cfg.AltMappings))
	for code, mapping := range cfg.AltMappings {
		altMappings[code] = config.CloneCompiledMapping(mapping)
	}
	capsMappings := make(map[uint16]config.CompiledMapping, len(cfg.CapsMappings))
	for code, mapping := range cfg.CapsMappings {
		capsMappings[code] = config.CloneCompiledMapping(mapping)
	}
	comboMappings := make(map[config.InputBinding]config.CompiledMapping, len(cfg.ComboMappings))
	for binding, mapping := range cfg.ComboMappings {
		comboMappings[binding] = config.CloneCompiledMapping(mapping)
	}
	tapLayoutSwitches := make(map[uint16]config.LayoutSwitchTapAction, len(cfg.TapLayoutSwitches))
	for code, action := range cfg.TapLayoutSwitches {
		tapLayoutSwitches[code] = action
	}
	shortcutMaps := make(map[uint16]uint16, len(cfg.ShortcutMappings))
	for srcCode, dstCode := range cfg.ShortcutMappings {
		shortcutMaps[srcCode] = dstCode
	}

	return &remapper{
		out:               out,
		composeDelay:      composeDelay,
		verbose:           verbose,
		suppressAlt:       cfg.SuppressAlt,
		suppressCaps:      cfg.SuppressCaps,
		altMappings:       altMappings,
		capsMappings:      capsMappings,
		comboMappings:     comboMappings,
		tapLayoutSwitches: tapLayoutSwitches,
		shortcutMaps:      shortcutMaps,
		shortcutFn:        shortcutFn,
		activeRemapped:    make(map[uint16]activeRemap),
		swallowed:         make(map[uint16]bool),
		maskedModifiers:   make(map[uint16]int),
		modDown:           make(map[uint16]bool),
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

func composeRuneKey(ch rune) (uint16, error) {
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
	for srcCode, remap := range r.activeRemapped {
		if err := r.out.emitKey(remap.code, 0); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := r.unmaskModifierCodes(remap.restoreModifiers); err != nil && firstErr == nil {
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

func isShift(code uint16) bool {
	return code == config.KeyLeftShift || code == config.KeyRightShift
}

func isNonAltModifier(code uint16) bool {
	switch code {
	case config.KeyLeftShift, config.KeyRightShift, config.KeyLeftCtrl, config.KeyRightCtrl, config.KeyLeftMeta, config.KeyRightMeta:
		return true
	default:
		return false
	}
}

func isShortcutModifier(code uint16) bool {
	return isAlt(code) || isNonAltModifier(code)
}

func isBindingModifier(code uint16) bool {
	return config.ModifierMaskForKeyCode(code) != 0
}

func (r *remapper) anyNonAltModifierDown() bool {
	for code, down := range r.modDown {
		if isAlt(code) {
			continue
		}
		if down {
			return true
		}
	}
	return false
}

func (r *remapper) shouldApplyShortcutRemap() bool {
	hasNonShiftModifier := false
	for code, down := range r.modDown {
		if !down {
			continue
		}
		if isShortcutModifier(code) && !isShift(code) {
			hasNonShiftModifier = true
		}
	}
	return hasNonShiftModifier
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

func (r *remapper) emitTapLayoutSwitch(code uint16) error {
	if _, ok := r.tapLayoutSwitches[code]; !ok {
		return nil
	}
	r.logf("tap layout switch requested for key %d", code)
	return r.out.emitLayoutSwitch(event.LayoutSwitchRequest{SourceCode: code})
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
			r.altUsed = false
			r.logf("alt down pending (%d)", code)
			return nil
		}

		r.altUsed = true
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
			} else if !r.altUsed {
				if err := r.emitTapLayoutSwitch(code); err != nil {
					return err
				}
			}
			r.altActive = false
			r.altEmitted = false
			r.altUsed = false
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
			r.activeRemapped[code] = activeRemap{code: mapping.RemapCode}
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
	r.altUsed = true
	if value == 1 {
		if mapping, ok := r.altMappings[code]; ok && !r.anyNonAltModifierDown() {
			r.logf("alt key %d mapped via config kind=%d", code, mapping.Kind)
			return r.handleMappedAction(code, value, mapping, true)
		}
	}

	if err := r.emitAltDownIfNeeded(); err != nil {
		return err
	}
	return r.emitShortcutAwareKey(code, value)
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

func (r *remapper) currentBindingModifiers() config.ModifierMask {
	var modifiers config.ModifierMask
	for code, down := range r.modDown {
		if !down {
			continue
		}
		modifiers |= config.ModifierMaskForKeyCode(code)
	}
	if r.capsActive {
		modifiers |= config.ModifierCaps
	}
	return modifiers
}

func (r *remapper) comboMappingForKey(code uint16) (config.CompiledMapping, bool) {
	mapping, ok := r.comboMappings[config.InputBinding{
		Modifiers: r.currentBindingModifiers(),
		KeyCode:   code,
	}]
	return mapping, ok
}

func (r *remapper) modifierPhysicallyDown(code uint16) bool {
	if isCaps(code) {
		return r.capsActive
	}
	return r.modDown[code]
}

func (r *remapper) modifierOutputActive(code uint16) bool {
	switch {
	case isCaps(code):
		return !r.suppressCaps && r.capsActive
	case isAlt(code):
		return r.modDown[code] && (!r.suppressAlt || r.altEmitted)
	default:
		return r.modDown[code]
	}
}

func (r *remapper) currentEmittedModifierCodes() []uint16 {
	codes := []uint16{
		config.KeyLeftMeta,
		config.KeyRightMeta,
		config.KeyLeftCtrl,
		config.KeyRightCtrl,
		config.KeyLeftAlt,
		config.KeyRightAlt,
		config.KeyLeftShift,
		config.KeyRightShift,
		config.KeyCapsLock,
	}

	emitted := make([]uint16, 0, len(codes))
	for _, code := range codes {
		if r.modifierOutputActive(code) {
			emitted = append(emitted, code)
		}
	}
	return emitted
}

func (r *remapper) maskEmittedModifiers() ([]uint16, error) {
	codes := r.currentEmittedModifierCodes()
	for i := len(codes) - 1; i >= 0; i-- {
		code := codes[i]
		if r.maskedModifiers[code] == 0 {
			if err := r.out.emitKey(code, 0); err != nil {
				return nil, err
			}
		}
		r.maskedModifiers[code]++
	}
	return codes, nil
}

func (r *remapper) unmaskModifierCodes(codes []uint16) error {
	for _, code := range codes {
		count := r.maskedModifiers[code]
		if count == 0 {
			continue
		}
		if count == 1 {
			delete(r.maskedModifiers, code)
			if r.modifierPhysicallyDown(code) {
				if err := r.out.emitKey(code, 1); err != nil {
					return err
				}
			}
			continue
		}
		r.maskedModifiers[code] = count - 1
	}
	return nil
}

func (r *remapper) handleComboMapping(code uint16, value int32, mapping config.CompiledMapping) error {
	switch mapping.Kind {
	case config.MappingPassthrough:
		if r.altActive && !r.altEmitted {
			if err := r.emitAltDownIfNeeded(); err != nil {
				return err
			}
		}
		return r.out.emitKey(code, value)

	case config.MappingRemap:
		if value != 1 {
			return nil
		}
		restoreModifiers, err := r.maskEmittedModifiers()
		if err != nil {
			return err
		}
		if err := r.out.emitKey(mapping.RemapCode, 1); err != nil {
			_ = r.unmaskModifierCodes(restoreModifiers)
			return err
		}
		r.activeRemapped[code] = activeRemap{
			code:             mapping.RemapCode,
			restoreModifiers: restoreModifiers,
		}
		return nil

	case config.MappingSymbol, config.MappingChord, config.MappingKeySeq:
		if value != 1 {
			return nil
		}
		restoreModifiers, err := r.maskEmittedModifiers()
		if err != nil {
			return err
		}
		r.swallowed[code] = true
		actionErr := r.handleMappedAction(code, value, mapping, false)
		restoreErr := r.unmaskModifierCodes(restoreModifiers)
		if actionErr != nil {
			return actionErr
		}
		return restoreErr

	default:
		return fmt.Errorf("unsupported mapping kind %d", mapping.Kind)
	}
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
		r.capsUsed = false
		r.logf("caps layer active")
	case 0:
		if r.capsActive && !r.capsUsed {
			if err := r.emitTapLayoutSwitch(config.KeyCapsLock); err != nil {
				return err
			}
		}
		r.capsActive = false
		r.capsUsed = false
		r.logf("caps layer inactive")
	}
	return nil
}

func (r *remapper) handleCapsLayerKey(code uint16, value int32) error {
	r.capsUsed = true
	if value == 1 {
		if mapping, ok := r.capsMappings[code]; ok {
			r.logf("caps key %d mapped via config kind=%d", code, mapping.Kind)
			return r.handleMappedAction(code, value, mapping, false)
		}
	}

	return r.emitShortcutAwareKey(code, value)
}

func (r *remapper) emitShortcutAwareKey(code uint16, value int32) error {
	if value == 1 && r.shouldApplyShortcutRemap() {
		shortcutMaps := r.shortcutMaps
		if r.shortcutFn != nil {
			if dynamicMaps := r.shortcutFn(); dynamicMaps != nil {
				shortcutMaps = dynamicMaps
			}
		}
		if remapCode, ok := shortcutMaps[code]; ok {
			r.activeRemapped[code] = activeRemap{code: remapCode}
			return r.out.emitKey(remapCode, value)
		}
	}
	return r.out.emitKey(code, value)
}

func (r *remapper) handleKey(code uint16, value int32) error {
	if isShortcutModifier(code) {
		switch value {
		case 1:
			r.modDown[code] = true
		case 0:
			r.modDown[code] = false
		}
	}

	if r.maskedModifiers[code] > 0 && isBindingModifier(code) {
		if isAlt(code) && value == 0 {
			r.altActive = false
			r.altEmitted = false
			r.altUsed = false
		}
		if isCaps(code) && value == 0 {
			r.capsActive = false
			r.capsUsed = false
		}
		return nil
	}

	if remap, ok := r.activeRemapped[code]; ok {
		if value == 0 {
			if err := r.out.emitKey(remap.code, 0); err != nil {
				return err
			}
			delete(r.activeRemapped, code)
			return r.unmaskModifierCodes(remap.restoreModifiers)
		}
		return r.out.emitKey(remap.code, value)
	}

	if value == 1 {
		if mapping, ok := r.comboMappingForKey(code); ok {
			return r.handleComboMapping(code, value, mapping)
		}
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

	return r.emitShortcutAwareKey(code, value)
}
