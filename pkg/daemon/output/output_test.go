package output

import (
	"bytes"
	"encoding/binary"
	"testing"
)

type closeBuffer struct {
	bytes.Buffer
	closed bool
}

func (b *closeBuffer) Close() error {
	b.closed = true
	return nil
}

type recordingIoctl struct {
	calls []ioctlCall
}

type ioctlCall struct {
	req uintptr
	arg uintptr
}

func (i *recordingIoctl) Ioctl(_ int, req uintptr, arg uintptr) error {
	i.calls = append(i.calls, ioctlCall{req: req, arg: arg})
	return nil
}

func TestCreateVirtualKeyboardUsesInternalKeyboardBus(t *testing.T) {
	file := &closeBuffer{}
	ioctl := &recordingIoctl{}

	kb, err := createVirtualKeyboardWithFile(file, 42, "kmap-test", ioctl)
	if err != nil {
		t.Fatalf("createVirtualKeyboardWithFile failed: %v", err)
	}
	defer kb.Close()

	var dev uinputUserDev
	if err := binary.Read(bytes.NewReader(file.Bytes()), binary.LittleEndian, &dev); err != nil {
		t.Fatalf("read uinput user dev: %v", err)
	}

	if dev.ID.Bustype != busI8042 {
		t.Fatalf("bustype = %#x, want %#x", dev.ID.Bustype, busI8042)
	}
	if got := string(bytes.TrimRight(dev.Name[:], "\x00")); got != "kmap-test" {
		t.Fatalf("name = %q, want %q", got, "kmap-test")
	}
}
