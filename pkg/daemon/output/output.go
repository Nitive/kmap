package output

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"keyboard/pkg/daemon/event"
)

const (
	uinputPath = "/dev/uinput"
)

var (
	uiSetEvBit   = unix.UI_SET_EVBIT
	uiSetKeyBit  = unix.UI_SET_KEYBIT
	uiDevCreate  = unix.UI_DEV_CREATE
	uiDevDestroy = unix.UI_DEV_DESTROY
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
	Name         [unix.UINPUT_MAX_NAME_SIZE]byte
	ID           inputID
	FFEffectsMax uint32
	AbsMax       [unix.ABS_CNT]int32
	AbsMin       [unix.ABS_CNT]int32
	AbsFuzz      [unix.ABS_CNT]int32
	AbsFlat      [unix.ABS_CNT]int32
}

type ioctlCaller interface {
	Ioctl(fd int, req uintptr, arg uintptr) error
}

type realIoctl struct{}

func (realIoctl) Ioctl(fd int, req uintptr, arg uintptr) error {
	return unix.IoctlSetInt(fd, uint(req), int(arg))
}

type Keyboard struct {
	f     io.WriteCloser
	fd    int
	ioctl ioctlCaller
}

func CreateVirtualKeyboard(name string) (*Keyboard, error) {
	f, err := os.OpenFile(uinputPath, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", uinputPath, err)
	}

	return createVirtualKeyboardWithFile(f, int(f.Fd()), name, realIoctl{})
}

func createVirtualKeyboardWithFile(file io.WriteCloser, fd int, name string, ic ioctlCaller) (*Keyboard, error) {
	if err := ic.Ioctl(fd, uiSetEvBit, uintptr(unix.EV_KEY)); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("UI_SET_EVBIT EV_KEY: %w", err)
	}
	if err := ic.Ioctl(fd, uiSetEvBit, uintptr(unix.EV_SYN)); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("UI_SET_EVBIT EV_SYN: %w", err)
	}

	for code := 0; code <= 255; code++ {
		if err := ic.Ioctl(fd, uiSetKeyBit, uintptr(code)); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("UI_SET_KEYBIT %d: %w", code, err)
		}
	}

	var dev uinputUserDev
	copy(dev.Name[:], []byte(name))
	dev.ID = inputID{
		Bustype: unix.BUS_USB,
		Vendor:  0x1,
		Product: 0x1,
		Version: 1,
	}

	if err := binary.Write(file, binary.LittleEndian, &dev); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write uinput device: %w", err)
	}

	if err := unix.IoctlSetInt(fd, uint(uiDevCreate), 0); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("UI_DEV_CREATE: %w", err)
	}

	// Give kernel/compositor a short moment to register the new keyboard.
	time.Sleep(150 * time.Millisecond)

	return &Keyboard{f: file, fd: fd, ioctl: ic}, nil
}

func (k *Keyboard) Close() error {
	if k == nil || k.f == nil {
		return nil
	}

	_ = unix.IoctlSetInt(k.fd, uint(uiDevDestroy), 0)
	return k.f.Close()
}

func (k *Keyboard) emitEvent(evType uint16, code uint16, value int32) error {
	ev := inputEvent{Type: evType, Code: code, Value: value}
	return binary.Write(k.f, binary.LittleEndian, &ev)
}

func (k *Keyboard) sync() error {
	return k.emitEvent(unix.EV_SYN, unix.SYN_REPORT, 0)
}

func (k *Keyboard) EmitKey(code uint16, value int32) error {
	if err := k.emitEvent(unix.EV_KEY, code, value); err != nil {
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
