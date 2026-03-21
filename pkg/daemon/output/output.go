package output

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"time"

	"keyboard/pkg/daemon/event"
)

const (
	uinputPath = "/dev/uinput"
)

const (
	evSyn = 0x00
	evKey = 0x01
)

const (
	synReport = 0x00
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

type Keyboard struct {
	f *os.File
}

func CreateVirtualKeyboard(name string) (*Keyboard, error) {
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

	// Give kernel/compositor a short moment to register the new keyboard.
	time.Sleep(150 * time.Millisecond)

	return &Keyboard{f: f}, nil
}

func (k *Keyboard) Close() error {
	if k == nil || k.f == nil {
		return nil
	}

	fd := int(k.f.Fd())
	_ = ioctl(fd, uiDevDestroy, 0)
	return k.f.Close()
}

func (k *Keyboard) emitEvent(evType uint16, code uint16, value int32) error {
	ev := inputEvent{Type: evType, Code: code, Value: value}
	return binary.Write(k.f, binary.LittleEndian, &ev)
}

func (k *Keyboard) sync() error {
	return k.emitEvent(evSyn, synReport, 0)
}

func (k *Keyboard) EmitKey(code uint16, value int32) error {
	if err := k.emitEvent(evKey, code, value); err != nil {
		return err
	}
	return k.sync()
}

func (k *Keyboard) TapKey(code uint16, delay time.Duration) error {
	if err := k.EmitKey(code, 1); err != nil {
		return err
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	if err := k.EmitKey(code, 0); err != nil {
		return err
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	return nil
}

func Run(kb *Keyboard, in <-chan event.KeyEvent) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		for ev := range in {
			if err := kb.EmitKey(ev.Code, ev.Value); err != nil {
				errCh <- err
				return
			}
		}
	}()
	return errCh
}

func ioctl(fd int, req uintptr, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, arg)
	if errno != 0 {
		return errno
	}
	return nil
}
