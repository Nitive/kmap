package input

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"keyboard/pkg/daemon/event"
)

const (
	evKey = 0x01
)

const (
	ledCapsLock = 1
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

func iow(typ, nr, size uintptr) uintptr {
	return ioc(iocWrite, typ, nr, size)
}

func ior(typ, nr, size uintptr) uintptr {
	return ioc(iocRead, typ, nr, size)
}

var (
	eviocgrab = iow(uintptr('E'), 0x90, 4)
	eviocgled = ior(uintptr('E'), 0x19, 1)
)

// RawEvent mirrors Linux input_event.
type RawEvent struct {
	Sec   int64
	Usec  int64
	Type  uint16
	Code  uint16
	Value int32
}

type Options struct {
	DevicePath string
	Grab       bool
}

type ioctlCaller interface {
	Ioctl(fd int, req uintptr, arg uintptr) error
}

type realIoctl struct{}

func (realIoctl) Ioctl(fd int, req uintptr, arg uintptr) error {
	return ioctl(fd, req, arg)
}

type Device struct {
	path    string
	file    io.ReadCloser
	fd      int
	grabbed bool
	ioctl   ioctlCaller

	closeOnce sync.Once
}

func Start(opts Options) (*Device, <-chan event.KeyEvent, <-chan error, error) {
	in, err := os.OpenFile(opts.DevicePath, os.O_RDONLY, 0)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open input device %s: %w", opts.DevicePath, err)
	}

	return startWithFile(in, int(in.Fd()), opts, realIoctl{})
}

func startWithFile(file io.ReadCloser, fd int, opts Options, ic ioctlCaller) (*Device, <-chan event.KeyEvent, <-chan error, error) {
	device := &Device{
		path:  opts.DevicePath,
		file:  file,
		fd:    fd,
		ioctl: ic,
	}

	if opts.Grab {
		if err := device.Grab(true); err != nil {
			_ = file.Close()
			return nil, nil, nil, fmt.Errorf("grab input device %s: %w", opts.DevicePath, err)
		}
		device.grabbed = true
	}

	eventsCh := make(chan event.KeyEvent, 128)
	errCh := make(chan error, 1)
	go device.readLoop(eventsCh, errCh)

	return device, eventsCh, errCh, nil
}

func (d *Device) Path() string {
	if d == nil {
		return ""
	}
	return d.path
}

func (d *Device) Close() error {
	if d == nil || d.file == nil {
		return nil
	}

	var closeErr error
	d.closeOnce.Do(func() {
		if d.grabbed {
			_ = d.Grab(false)
		}
		closeErr = d.file.Close()
	})
	return closeErr
}

func (d *Device) CapsLockEnabled() (bool, error) {
	if d == nil {
		return false, errors.New("nil device")
	}
	var leds [1]byte
	if err := d.ioctl.Ioctl(d.fd, eviocgled, uintptr(unsafe.Pointer(&leds[0]))); err != nil {
		return false, err
	}
	return (leds[0] & (1 << ledCapsLock)) != 0, nil
}

func (d *Device) Grab(enable bool) error {
	value := int32(0)
	if enable {
		value = 1
	}
	if err := d.ioctl.Ioctl(d.fd, eviocgrab, uintptr(unsafe.Pointer(&value))); err != nil {
		return err
	}
	d.grabbed = enable
	return nil
}

func (d *Device) readLoop(eventsCh chan<- event.KeyEvent, errCh chan<- error) {
	defer close(eventsCh)
	defer close(errCh)

	for {
		ev, err := ReadRawEvent(d.file)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EBADF) {
				return
			}
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			errCh <- fmt.Errorf("read input event %s: %w", d.path, err)
			return
		}
		if ev.Type != evKey {
			continue
		}
		eventsCh <- event.KeyEvent{Kind: event.KindKey, Code: ev.Code, Value: ev.Value}
	}
}

func ReadRawEvent(r io.Reader) (RawEvent, error) {
	var ev RawEvent
	err := binary.Read(r, binary.LittleEndian, &ev)
	return ev, err
}

func WaitForNextKeyPress(r io.Reader) (uint16, error) {
	for {
		ev, err := ReadRawEvent(r)
		if err != nil {
			return 0, err
		}
		if ev.Type == evKey && ev.Value == 1 {
			return ev.Code, nil
		}
	}
}

func Grab(fd int, enable bool) error {
	value := int32(0)
	if enable {
		value = 1
	}
	return ioctlSetInt(fd, eviocgrab, value)
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
