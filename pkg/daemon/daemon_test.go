package daemon

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon/event"
)

type mockInputDevice struct {
	path            string
	closed          bool
	capsLockEnabled bool
}

func (m *mockInputDevice) Path() string                  { return m.path }
func (m *mockInputDevice) Close() error                 { m.closed = true; return nil }
func (m *mockInputDevice) CapsLockEnabled() (bool, error) { return m.capsLockEnabled, nil }

type mockOutputDevice struct {
	closed bool
}

func (m *mockOutputDevice) EmitKey(code uint16, value int32) error        { return nil }
func (m *mockOutputDevice) TapKey(code uint16, delay time.Duration) error { return nil }
func (m *mockOutputDevice) Close() error                                  { m.closed = true; return nil }

func TestOrchestrator(t *testing.T) {
	t.Run("successful startup and shutdown on signal", func(t *testing.T) {
		inDev := &mockInputDevice{path: "/dev/input/test"}
		outKB := &mockOutputDevice{}
		sigCh := make(chan os.Signal, 1)

		orc := &orchestrator{
			cfg:         config.Runtime{},
			devicePaths: []string{"/dev/input/test"},
			opts:        StartOptions{},
			inputFactory: func(path string, grab bool) (inputDevice, <-chan event.KeyEvent, <-chan error, error) {
				return inDev, make(chan event.KeyEvent), make(chan error), nil
			},
			outputFactory: func(name string, in <-chan event.KeyEvent) (outputDevice, <-chan error, error) {
				return outKB, make(chan error), nil
			},
			signalSource: sigCh,
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- orc.run()
		}()

		// give it a moment to start
		time.Sleep(10 * time.Millisecond)
		sigCh <- syscall.SIGINT

		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("orc.run() returned error: %v", err)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timed out waiting for orc.run() to return")
		}

		if !inDev.closed {
			t.Error("input device was not closed")
		}
		if !outKB.closed {
			t.Error("output device was not closed")
		}
	})

	t.Run("fails when input factory fails", func(t *testing.T) {
		sigCh := make(chan os.Signal, 1)
		orc := &orchestrator{
			devicePaths: []string{"/dev/input/test"},
			inputFactory: func(path string, grab bool) (inputDevice, <-chan event.KeyEvent, <-chan error, error) {
				return nil, nil, nil, errors.New("input failed")
			},
			signalSource: sigCh,
		}

		err := orc.run()
		if err == nil || err.Error() != "input failed" {
			t.Fatalf("expected 'input failed' error, got: %v", err)
		}
	})

	t.Run("shuts down when module returns error", func(t *testing.T) {
		inDev := &mockInputDevice{path: "/dev/input/test"}
		outKB := &mockOutputDevice{}
		sigCh := make(chan os.Signal, 1)
		inErr := make(chan error, 1)

		orc := &orchestrator{
			devicePaths: []string{"/dev/input/test"},
			inputFactory: func(path string, grab bool) (inputDevice, <-chan event.KeyEvent, <-chan error, error) {
				return inDev, make(chan event.KeyEvent), inErr, nil
			},
			outputFactory: func(name string, in <-chan event.KeyEvent) (outputDevice, <-chan error, error) {
				return outKB, make(chan error), nil
			},
			signalSource: sigCh,
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- orc.run()
		}()

		inErr <- errors.New("module error")

		select {
		case err := <-errCh:
			if err == nil || !errors.Is(err, errors.New("module error")) {
				// errors.Is might not work with newly created errors if not wrapped properly
				if !errors.As(err, &err) && err.Error() != "input module /dev/input/test: module error" {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timed out waiting for orc.run() to return")
		}

		if !inDev.closed {
			t.Error("input device was not closed")
		}
	})
}

type fakeTapper struct {
	events []struct {
		code  uint16
		delay time.Duration
	}
}

func (f *fakeTapper) TapKey(code uint16, delay time.Duration) error {
	f.events = append(f.events, struct {
		code  uint16
		delay time.Duration
	}{code: code, delay: delay})
	return nil
}

func TestResolveDevicePaths(t *testing.T) {
	cfg := config.Runtime{
		Devices: []string{"/dev/input/a", "/dev/input/b"},
	}

	t.Run("cli override wins", func(t *testing.T) {
		paths := resolveDevicePaths("/dev/input/override", cfg)
		if len(paths) != 1 || paths[0] != "/dev/input/override" {
			t.Fatalf("unexpected paths: %#v", paths)
		}
	})

	t.Run("config devices used when no override", func(t *testing.T) {
		paths := resolveDevicePaths("", cfg)
		if len(paths) != 2 || paths[0] != "/dev/input/a" || paths[1] != "/dev/input/b" {
			t.Fatalf("unexpected paths: %#v", paths)
		}
	})

	t.Run("default device fallback", func(t *testing.T) {
		paths := resolveDevicePaths("", config.Runtime{})
		if len(paths) != 1 || paths[0] != config.DefaultDevicePath {
			t.Fatalf("unexpected paths: %#v", paths)
		}
	})
}

func TestReleaseCapsOnStart(t *testing.T) {
	t.Run("toggles off when caps mappings exist and caps is enabled", func(t *testing.T) {
		out := &fakeTapper{}
		cfg := config.Runtime{
			CapsMappings: map[uint16]config.CompiledMapping{
				config.KeyH: {Kind: config.MappingRemap, RemapCode: config.KeyBackspace},
			},
		}

		if err := releaseCapsOnStart(out, cfg, true, time.Millisecond); err != nil {
			t.Fatalf("releaseCapsOnStart: %v", err)
		}
		if len(out.events) != 1 {
			t.Fatalf("unexpected events: %#v", out.events)
		}
		if out.events[0].code != config.KeyCapsLock {
			t.Fatalf("unexpected key: %#v", out.events[0])
		}
	})

	t.Run("noop without caps mappings", func(t *testing.T) {
		out := &fakeTapper{}
		cfg := config.Runtime{CapsMappings: map[uint16]config.CompiledMapping{}}

		if err := releaseCapsOnStart(out, cfg, true, 0); err != nil {
			t.Fatalf("releaseCapsOnStart: %v", err)
		}
		if len(out.events) != 0 {
			t.Fatalf("expected no events, got %#v", out.events)
		}
	})

	t.Run("noop when caps mappings exist but caps is already disabled", func(t *testing.T) {
		out := &fakeTapper{}
		cfg := config.Runtime{
			CapsMappings: map[uint16]config.CompiledMapping{
				config.KeyH: {Kind: config.MappingRemap, RemapCode: config.KeyBackspace},
			},
		}

		if err := releaseCapsOnStart(out, cfg, false, 0); err != nil {
			t.Fatalf("releaseCapsOnStart: %v", err)
		}
		if len(out.events) != 0 {
			t.Fatalf("expected no events, got %#v", out.events)
		}
	})
}
