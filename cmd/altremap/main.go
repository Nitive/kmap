package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
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
	keyI          = 23
	keyO          = 24
	keyP          = 25
	keyLeftBrace  = 26
	keyEnter      = 28
	keyLeftCtrl   = 29
	keyA          = 30
	keyS          = 31
	keyF          = 33
	keyH          = 35
	keyJ          = 36
	keyK          = 37
	keyL          = 38
	keySemicolon  = 39
	keyApostrophe = 40
	keyBackslash  = 43
	keyLeftShift  = 42
	keyZ          = 44
	keyX          = 45
	keyV          = 47
	keyB          = 48
	keyN          = 49
	keyM          = 50
	keyComma      = 51
	keyDot        = 52
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

var (
	eviocgrab    = iow(uintptr('E'), 0x90, 4)
	uiSetEvBit   = iow(uintptr('U'), 100, 4)
	uiSetKeyBit  = iow(uintptr('U'), 101, 4)
	uiDevCreate  = iocNone(uintptr('U'), 1)
	uiDevDestroy = iocNone(uintptr('U'), 2)
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
	compose string
	pipe    bool
}

type capsAction struct {
	remapCode uint16
	chordKey  uint16
	chordMods []uint16
}

var symbolMap = map[uint16]symbolAction{
	// Top row
	key9:     {compose: "2205"}, // «
	key0:     {compose: "2206"}, // »
	keyMinus: {compose: "2207"}, // “
	keyEqual: {compose: "2208"}, // ”

	// Q row
	keyQ:         {compose: "2215"}, // `
	keyW:         {compose: "2216"}, // ,
	keyE:         {compose: "2217"}, // .
	keyR:         {compose: "2218"}, // +
	keyI:         {compose: "2203"}, // ↑
	keyP:         {compose: "2222"}, // —
	keyLeftBrace: {compose: "2209"}, // /

	// A row
	keyS:          {compose: "2214"}, // =
	keyF:          {compose: "2220"}, // ü
	keyH:          {compose: "2223"}, // -
	keyJ:          {compose: "2202"}, // ←
	keyK:          {compose: "2204"}, // ↓
	keyL:          {compose: "2201"}, // →
	keySemicolon:  {compose: "2219"}, // ;
	keyApostrophe: {compose: "2224"}, // \

	// Z row
	keyZ:   {compose: "2210"}, // :
	keyX:   {compose: "2211"}, // ?
	keyV:   {compose: "2225"}, // ~
	keyB:   {compose: "2213"}, // ×
	keyN:   {compose: "2224"}, // \
	keyM:   {compose: "2212"}, // −
	keyDot: {pipe: true},      // |

	// Space
	keySpace: {compose: "2221"}, // NBSP
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

var (
	modsHyper     = []uint16{keyLeftMeta, keyLeftCtrl, keyLeftAlt, keyLeftShift}
	modsCtrlShift = []uint16{keyLeftCtrl, keyLeftShift}
)

var capsActionMap = map[uint16]capsAction{
	// Row with `tab q w e r t y u i o p [ ] \`
	keyW: {chordKey: keyComma, chordMods: modsCtrlShift}, // C-S-, (close tab/window)
	keyR: {chordKey: keyR, chordMods: modsHyper},         // 1password
	keyI: {remapCode: keyUp},
	keyO: {chordKey: keyO, chordMods: modsHyper}, // kitty
	keyP: {chordKey: keyP, chordMods: modsHyper}, // neovide

	// Row with `caps a s d f g h j k l ; ' ret`
	keyA:          {chordKey: keyA, chordMods: modsHyper}, // spotify
	keyH:          {remapCode: keyBackspace},
	keyJ:          {remapCode: keyLeft},
	keyK:          {remapCode: keyDown},
	keyL:          {remapCode: keyRight},
	keySemicolon:  {chordKey: keySemicolon, chordMods: modsHyper},  // editor
	keyApostrophe: {chordKey: keyApostrophe, chordMods: modsHyper}, // sublime

	// Row with `lsft z x c v b n m , . / rsft`
	keyN:     {chordKey: keyN, chordMods: modsHyper}, // firefox
	keyM:     {remapCode: keyEsc},
	keyComma: {chordKey: keyComma, chordMods: modsHyper}, // telegram (< via Shift)
	keyDot:   {chordKey: keyDot, chordMods: modsHyper},   // mattermost (> via Shift)
}

type remapper struct {
	out          keyEmitter
	composeDelay time.Duration
	verbose      bool

	altActive  bool
	altCode    uint16
	altEmitted bool
	capsActive bool

	swallowed map[uint16]bool
	modDown   map[uint16]bool
}

func newRemapper(out keyEmitter, composeDelay time.Duration, verbose bool) *remapper {
	return &remapper{
		out:          out,
		composeDelay: composeDelay,
		verbose:      verbose,
		swallowed:    make(map[uint16]bool),
		modDown:      make(map[uint16]bool),
	}
}

func (r *remapper) logf(format string, args ...any) {
	if r.verbose {
		log.Printf(format, args...)
	}
}

func (r *remapper) cleanup() error {
	if r.altActive && r.altEmitted {
		return r.out.emitKey(r.altCode, 0)
	}
	return nil
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

func (r *remapper) emitCompose(code string) error {
	if err := r.out.tapKey(keyScrollLock, r.composeDelay); err != nil {
		return err
	}
	for _, ch := range code {
		keyCode, ok := composeDigitKey[ch]
		if !ok {
			return fmt.Errorf("compose code contains unsupported digit %q", ch)
		}
		if err := r.out.tapKey(keyCode, r.composeDelay); err != nil {
			return err
		}
	}
	return nil
}

func (r *remapper) emitPipe() error {
	if err := r.out.emitKey(keyLeftShift, 1); err != nil {
		return err
	}
	if err := r.out.emitKey(keyBackslash, 1); err != nil {
		return err
	}
	if err := r.out.emitKey(keyBackslash, 0); err != nil {
		return err
	}
	if err := r.out.emitKey(keyLeftShift, 0); err != nil {
		return err
	}
	return nil
}

func (r *remapper) emitSymbol(action symbolAction) error {
	if action.compose != "" {
		return r.emitCompose(action.compose)
	}
	if action.pipe {
		return r.emitPipe()
	}
	return nil
}

func (r *remapper) handleAltKey(code uint16, value int32) error {
	if value == 2 {
		return nil
	}

	switch value {
	case 1:
		if !r.altActive {
			r.altActive = true
			r.altCode = code
			r.altEmitted = false
			r.swallowed = make(map[uint16]bool)
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
			r.swallowed = make(map[uint16]bool)
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

func (r *remapper) handlePendingAltKey(code uint16, value int32) error {
	if value == 0 {
		if r.swallowed[code] {
			delete(r.swallowed, code)
			return nil
		}
		return r.out.emitKey(code, value)
	}

	if value == 2 {
		if r.swallowed[code] {
			return nil
		}
		return r.out.emitKey(code, value)
	}

	if value != 1 {
		return nil
	}

	if action, ok := symbolMap[code]; ok && !r.anyNonAltModifierDown() {
		r.logf("symbol key %d mapped", code)
		r.swallowed[code] = true
		return r.emitSymbol(action)
	}

	// Not a symbol mapping (or modifier held) -> become real Alt passthrough.
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
	action, ok := capsActionMap[code]
	if !ok {
		return r.out.emitKey(code, value)
	}

	if action.remapCode != 0 {
		return r.out.emitKey(action.remapCode, value)
	}

	// Chord actions fire once on press and swallow release/repeat.
	if value != 1 {
		return nil
	}
	r.logf("caps chord key %d mapped", code)
	return r.emitChord(action.chordMods, action.chordKey)
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

func readInputEvent(f *os.File) (inputEvent, error) {
	var ev inputEvent
	err := binary.Read(f, binary.LittleEndian, &ev)
	return ev, err
}

func run(devicePath string, composeDelay time.Duration, grab bool, verbose bool) error {
	in, err := os.Open(devicePath)
	if err != nil {
		return fmt.Errorf("open input device %s: %w", devicePath, err)
	}
	defer in.Close()

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

	rem := newRemapper(out, composeDelay, verbose)
	defer rem.cleanup()

	var stopping atomic.Bool
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		stopping.Store(true)
		_ = in.Close()
	}()

	log.Printf("altremap started on %s", devicePath)
	if grab {
		log.Printf("input device is grabbed")
	}

	for {
		ev, err := readInputEvent(in)
		if err != nil {
			if stopping.Load() {
				return nil
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
	devicePath := flag.String("device", defaultDevicePath, "input keyboard device path")
	composeDelay := flag.Duration("compose-delay", 5*time.Millisecond, "delay between compose key taps")
	grab := flag.Bool("grab", true, "grab input device so physical events are not duplicated")
	verbose := flag.Bool("verbose", false, "enable verbose logs")
	flag.Parse()

	if *composeDelay < 0 {
		log.Fatalf("compose-delay must be >= 0")
	}

	if err := run(*devicePath, *composeDelay, *grab, *verbose); err != nil {
		log.Fatalf("altremap failed: %v", err)
	}
}
