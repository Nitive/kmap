package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	defaultDevicePath = "/dev/input/by-path/platform-i8042-serio-0-event-kbd"
	uinputPath        = "/dev/uinput"
)

const (
	evSyn = 0x00
	evKey = 0x01
)

const (
	synReport = 0x00
)

const (
	keyEsc        = 1
	key1          = 2
	key2          = 3
	key3          = 4
	key4          = 5
	key5          = 6
	key6          = 7
	key7          = 8
	key8          = 9
	key9          = 10
	key0          = 11
	keyMinus      = 12
	keyEqual      = 13
	keyBackspace  = 14
	keyTab        = 15
	keyQ          = 16
	keyW          = 17
	keyE          = 18
	keyR          = 19
	keyT          = 20
	keyY          = 21
	keyU          = 22
	keyI          = 23
	keyO          = 24
	keyP          = 25
	keyLeftBrace  = 26
	keyRightBrace = 27
	keyEnter      = 28
	keyLeftCtrl   = 29
	keyA          = 30
	keyS          = 31
	keyD          = 32
	keyF          = 33
	keyG          = 34
	keyH          = 35
	keyJ          = 36
	keyK          = 37
	keyL          = 38
	keySemicolon  = 39
	keyApostrophe = 40
	keyGrv        = 41
	keyBackslash  = 43
	keyLeftShift  = 42
	keyZ          = 44
	keyX          = 45
	keyC          = 46
	keyV          = 47
	keyB          = 48
	keyN          = 49
	keyM          = 50
	keyComma      = 51
	keyDot        = 52
	keySlash      = 53
	keyRightShift = 54
	keyLeftAlt    = 56
	keySpace      = 57
	keyCapsLock   = 58
	keyScrollLock = 70
	keyRightCtrl  = 97
	keyRightAlt   = 100
	keyUp         = 103
	keyLeft       = 105
	keyRight      = 106
	keyDown       = 108
	keyLeftMeta   = 125
	keyRightMeta  = 126
)

const (
	busUSB = 0x03
)

// ioctl definitions from asm-generic/ioctl.h
const (
	iocNRBits   = 8
	iocTypeBits = 8
	iocSizeBits = 14
	iocDirBits  = 2

	iocNRShift   = 0
	iocTypeShift = iocNRShift + iocNRBits
	iocSizeShift = iocTypeShift + iocTypeBits
	iocDirShift  = iocSizeShift + iocSizeBits

	iocWrite = 1
	iocRead  = 2
)

func ioc(dir, typ, nr, size uintptr) uintptr {
	return (dir << iocDirShift) | (typ << iocTypeShift) | (nr << iocNRShift) | (size << iocSizeShift)
}

func iocNone(typ, nr uintptr) uintptr {
	return ioc(0, typ, nr, 0)
}

func iow(typ, nr, size uintptr) uintptr {
	return ioc(iocWrite, typ, nr, size)
}

func ior(typ, nr, size uintptr) uintptr {
	return ioc(iocRead, typ, nr, size)
}

var (
	eviocgrab    = iow(uintptr('E'), 0x90, 4)
	eviocgled    = ior(uintptr('E'), 0x19, 1)
	uiSetEvBit   = iow(uintptr('U'), 100, 4)
	uiSetKeyBit  = iow(uintptr('U'), 101, 4)
	uiDevCreate  = iocNone(uintptr('U'), 1)
	uiDevDestroy = iocNone(uintptr('U'), 2)
)

const (
	ledCapsLock = 1
)

type inputEvent struct {
	Sec   int64
	Usec  int64
	Type  uint16
	Code  uint16
	Value int32
}

type inputID struct {
	Bustype uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

type uinputUserDev struct {
	Name         [80]byte
	ID           inputID
	FFEffectsMax uint32
	AbsMax       [64]int32
	AbsMin       [64]int32
	AbsFuzz      [64]int32
	AbsFlat      [64]int32
}

type virtualKeyboard struct {
	f *os.File
}

type keyEmitter interface {
	emitKey(code uint16, value int32) error
	tapKey(code uint16, delay time.Duration) error
}

func createVirtualKeyboard(name string) (*virtualKeyboard, error) {
	f, err := os.OpenFile(uinputPath, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", uinputPath, err)
	}

	fd := int(f.Fd())
	if err := ioctl(fd, uiSetEvBit, uintptr(evKey)); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("UI_SET_EVBIT EV_KEY: %w", err)
	}
	if err := ioctl(fd, uiSetEvBit, uintptr(evSyn)); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("UI_SET_EVBIT EV_SYN: %w", err)
	}

	for code := 0; code <= 255; code++ {
		if err := ioctl(fd, uiSetKeyBit, uintptr(code)); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("UI_SET_KEYBIT %d: %w", code, err)
		}
	}

	var dev uinputUserDev
	copy(dev.Name[:], []byte(name))
	dev.ID = inputID{
		Bustype: busUSB,
		Vendor:  0x1,
		Product: 0x1,
		Version: 1,
	}

	if err := binary.Write(f, binary.LittleEndian, &dev); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write uinput device: %w", err)
	}

	if err := ioctl(fd, uiDevCreate, 0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("UI_DEV_CREATE: %w", err)
	}

	// Give the kernel/compositor a short moment to register the new keyboard.
	time.Sleep(150 * time.Millisecond)

	return &virtualKeyboard{f: f}, nil
}

func (k *virtualKeyboard) close() error {
	if k == nil || k.f == nil {
		return nil
	}

	fd := int(k.f.Fd())
	_ = ioctl(fd, uiDevDestroy, 0)
	return k.f.Close()
}

func (k *virtualKeyboard) emitEvent(evType uint16, code uint16, value int32) error {
	ev := inputEvent{
		Type:  evType,
		Code:  code,
		Value: value,
	}
	return binary.Write(k.f, binary.LittleEndian, &ev)
}

func (k *virtualKeyboard) sync() error {
	return k.emitEvent(evSyn, synReport, 0)
}

func (k *virtualKeyboard) emitKey(code uint16, value int32) error {
	if err := k.emitEvent(evKey, code, value); err != nil {
		return err
	}
	return k.sync()
}

func (k *virtualKeyboard) tapKey(code uint16, delay time.Duration) error {
	if err := k.emitKey(code, 1); err != nil {
		return err
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	if err := k.emitKey(code, 0); err != nil {
		return err
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	return nil
}

func ioctl(fd int, req uintptr, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, arg)
	if errno != 0 {
		return errno
	}
	return nil
}

func ioctlSetInt(fd int, req uintptr, value int32) error {
	return ioctl(fd, req, uintptr(unsafe.Pointer(&value)))
}

type symbolAction struct {
	symbol rune
}

var composeDigitKey = map[rune]uint16{
	'0': key0,
	'1': key1,
	'2': key2,
	'3': key3,
	'4': key4,
	'5': key5,
	'6': key6,
	'7': key7,
	'8': key8,
	'9': key9,
}

func composeSequenceForRune(r rune) []uint16 {
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
		keyCode, err := parseKeyName(string(ch))
		if err != nil {
			return 0, fmt.Errorf("compose code contains unsupported character %q", ch)
		}
		return keyCode, nil
	}

	return 0, fmt.Errorf("compose code contains unsupported character %q", ch)
}

type remapper struct {
	out          keyEmitter
	composeDelay time.Duration
	verbose      bool
	suppressAlt  bool
	suppressCaps bool
	altMappings  map[uint16]compiledMapping
	capsMappings map[uint16]compiledMapping

	altActive  bool
	altCode    uint16
	altEmitted bool
	capsActive bool

	activeRemapped map[uint16]uint16
	swallowed      map[uint16]bool
	modDown        map[uint16]bool
}

func newRemapper(out keyEmitter, composeDelay time.Duration, verbose bool) *remapper {
	return newRemapperWithConfig(out, composeDelay, verbose, defaultRuntimeConfig())
}

func newRemapperWithConfig(out keyEmitter, composeDelay time.Duration, verbose bool, cfg runtimeConfig) *remapper {
	altMappings := make(map[uint16]compiledMapping, len(cfg.altMappings))
	for code, mapping := range cfg.altMappings {
		altMappings[code] = cloneCompiledMapping(mapping)
	}
	capsMappings := make(map[uint16]compiledMapping, len(cfg.capsMappings))
	for code, mapping := range cfg.capsMappings {
		capsMappings[code] = cloneCompiledMapping(mapping)
	}

	return &remapper{
		out:            out,
		composeDelay:   composeDelay,
		verbose:        verbose,
		suppressAlt:    cfg.suppressAlt,
		suppressCaps:   cfg.suppressCaps,
		altMappings:    altMappings,
		capsMappings:   capsMappings,
		activeRemapped: make(map[uint16]uint16),
		swallowed:      make(map[uint16]bool),
		modDown:        make(map[uint16]bool),
	}
}

func cloneCompiledMapping(m compiledMapping) compiledMapping {
	if len(m.chordMods) > 0 {
		cloned := make([]uint16, len(m.chordMods))
		copy(cloned, m.chordMods)
		m.chordMods = cloned
	}
	if len(m.keySeq) > 0 {
		cloned := make([]uint16, len(m.keySeq))
		copy(cloned, m.keySeq)
		m.keySeq = cloned
	}
	return m
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
	return code == keyLeftAlt || code == keyRightAlt
}

func isCaps(code uint16) bool {
	return code == keyCapsLock
}

func isNonAltModifier(code uint16) bool {
	switch code {
	case keyLeftShift, keyRightShift, keyLeftCtrl, keyRightCtrl, keyLeftMeta, keyRightMeta:
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
	if err := r.out.tapKey(keyScrollLock, r.composeDelay); err != nil {
		return err
	}
	for _, keyCode := range keys {
		if err := r.out.tapKey(keyCode, r.composeDelay); err != nil {
			return err
		}
	}
	return nil
}

func (r *remapper) emitSymbol(action symbolAction) error {
	if action.symbol == 0 {
		return nil
	}
	return r.emitCompose(composeSequenceForRune(action.symbol))
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

		// Second Alt while one is already active: switch to passthrough and forward it.
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

func (r *remapper) handleMappedAction(code uint16, value int32, mapping compiledMapping, altLayer bool) error {
	switch mapping.kind {
	case mappingPassthrough:
		if altLayer {
			if err := r.emitAltDownIfNeeded(); err != nil {
				return err
			}
		}
		return r.out.emitKey(code, value)

	case mappingSymbol:
		if value != 1 {
			return nil
		}
		r.swallowed[code] = true
		return r.emitSymbol(mapping.symbol)

	case mappingRemap:
		if value == 1 {
			r.activeRemapped[code] = mapping.remapCode
		}
		if value == 0 {
			delete(r.activeRemapped, code)
		}
		return r.out.emitKey(mapping.remapCode, value)

	case mappingChord:
		if value != 1 {
			return nil
		}
		r.swallowed[code] = true
		return r.emitChord(mapping.chordMods, mapping.chordKey)

	case mappingKeySeq:
		if value != 1 {
			return nil
		}
		r.swallowed[code] = true
		return r.emitKeySequence(mapping.keySeq)

	default:
		return fmt.Errorf("unsupported mapping kind %d", mapping.kind)
	}
}

func (r *remapper) handlePendingAltKey(code uint16, value int32) error {
	if mapping, ok := r.altMappings[code]; ok && !r.anyNonAltModifierDown() {
		r.logf("alt key %d mapped via config kind=%d", code, mapping.kind)
		return r.handleMappedAction(code, value, mapping, true)
	}

	// Not an Alt mapping (or another modifier held): become real Alt passthrough.
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
		return r.out.emitKey(keyCapsLock, value)
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

	r.logf("caps key %d mapped via config kind=%d", code, mapping.kind)
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

func readInputEvent(r io.Reader) (inputEvent, error) {
	var ev inputEvent
	err := binary.Read(r, binary.LittleEndian, &ev)
	return ev, err
}

func isCapsLockEnabled(inFD int) (bool, error) {
	var leds [1]byte
	if err := ioctl(inFD, eviocgled, uintptr(unsafe.Pointer(&leds[0]))); err != nil {
		return false, err
	}
	return (leds[0] & (1 << ledCapsLock)) != 0, nil
}

func releaseCapsOnStart(out keyEmitter, cfg runtimeConfig, capsEnabled bool, delay time.Duration) error {
	if len(cfg.capsMappings) == 0 {
		return nil
	}
	if !capsEnabled {
		return nil
	}
	return out.tapKey(keyCapsLock, delay)
}

func run(devicePath string, configPath string, composeDelay time.Duration, grab bool, verbose bool) error {
	in, err := os.OpenFile(devicePath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("open input device %s: %w", devicePath, err)
	}
	defer in.Close()

	cfg, err := loadRuntimeConfig(configPath)
	if err != nil {
		return err
	}

	inFD := int(in.Fd())
	if grab {
		if err := ioctlSetInt(inFD, eviocgrab, 1); err != nil {
			return fmt.Errorf("grab input device %s: %w", devicePath, err)
		}
		defer func() {
			_ = ioctlSetInt(inFD, eviocgrab, 0)
		}()
	}

	out, err := createVirtualKeyboard("Go Alt Layer")
	if err != nil {
		return err
	}
	defer out.close()

	capsEnabled, err := isCapsLockEnabled(inFD)
	if err != nil {
		log.Printf("could not query caps-lock state: %v", err)
	}
	if err := releaseCapsOnStart(out, cfg, capsEnabled, composeDelay); err != nil {
		return fmt.Errorf("release caps on start: %w", err)
	}

	rem := newRemapperWithConfig(out, composeDelay, verbose, cfg)
	defer rem.cleanup()

	var stopping atomic.Bool
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		sig := <-sigCh
		log.Printf("received signal %s, shutting down", sig)
		stopping.Store(true)
		if err := in.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			log.Printf("close input device during shutdown: %v", err)
		}
	}()

	log.Printf("kmap started on %s", devicePath)
	if grab {
		log.Printf("input device is grabbed")
	}
	if configPath != "" {
		log.Printf("config path: %s", configPath)
	}

	for {
		if stopping.Load() {
			return nil
		}

		ev, err := readInputEvent(in)
		if err != nil {
			if stopping.Load() {
				return nil
			}
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				time.Sleep(2 * time.Millisecond)
				continue
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			if errors.Is(err, os.ErrClosed) {
				return nil
			}
			return fmt.Errorf("read input event: %w", err)
		}

		if ev.Type != evKey {
			continue
		}
		if err := rem.handleKey(ev.Code, ev.Value); err != nil {
			return fmt.Errorf("handle key code=%d value=%d: %w", ev.Code, ev.Value, err)
		}
	}
}

func main() {
	if err := runCLI(os.Args[1:]); err != nil {
		log.Fatalf("kmap failed: %v", err)
	}
}
