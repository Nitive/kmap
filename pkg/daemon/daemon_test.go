package daemon

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon/event"
	"keyboard/pkg/daemon/shortcut"
)

type mockInputDevice struct {
	path            string
	closed          bool
	capsLockEnabled bool
	grabbed         bool
}

func (m *mockInputDevice) Path() string                   { return m.path }
func (m *mockInputDevice) Close() error                   { m.closed = true; return nil }
func (m *mockInputDevice) CapsLockEnabled() (bool, error) { return m.capsLockEnabled, nil }
func (m *mockInputDevice) Grab(enable bool) error         { m.grabbed = enable; return nil }

type mockOutputDevice struct {
	closed bool
}

func (m *mockOutputDevice) EmitKey(code uint16, value int32) error        { return nil }
func (m *mockOutputDevice) TapKey(code uint16, delay time.Duration) error { return nil }
func (m *mockOutputDevice) Close() error                                  { m.closed = true; return nil }

type fakeShortcutSwitcher struct {
	wrapped bool
	closed  bool
}

func (f *fakeShortcutSwitcher) Wrap(in <-chan event.KeyEvent) (<-chan event.KeyEvent, <-chan error) {
	f.wrapped = true
	return in, closedErrChan()
}

func (f *fakeShortcutSwitcher) Close(ctx context.Context) error {
	_ = ctx
	f.closed = true
	return nil
}

func useTempRuntimeDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
}

func TestOrchestrator(t *testing.T) {
	t.Run("successful startup and shutdown on signal", func(t *testing.T) {
		useTempRuntimeDir(t)

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
		useTempRuntimeDir(t)

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
		useTempRuntimeDir(t)

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
			if err == nil || err.Error() != "input module /dev/input/test: module error" {
				t.Fatalf("unexpected error: %v", err)
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

func closedEventChan() <-chan event.KeyEvent {
	ch := make(chan event.KeyEvent)
	close(ch)
	return ch
}

func closedErrChan() <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
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

func TestOrchestratorReturnsWhenModulesFinishCleanly(t *testing.T) {
	useTempRuntimeDir(t)

	inDev := &mockInputDevice{path: "/dev/input/test"}
	outKB := &mockOutputDevice{}

	orc := &orchestrator{
		cfg:         config.Runtime{},
		devicePaths: []string{"/dev/input/test"},
		opts:        StartOptions{},
		inputFactory: func(path string, grab bool) (inputDevice, <-chan event.KeyEvent, <-chan error, error) {
			return inDev, closedEventChan(), closedErrChan(), nil
		},
		outputFactory: func(name string, in <-chan event.KeyEvent) (outputDevice, <-chan error, error) {
			return outKB, closedErrChan(), nil
		},
		signalSource: make(chan os.Signal, 1),
	}

	if err := orc.run(); err != nil {
		t.Fatalf("orc.run: %v", err)
	}
	if !inDev.closed {
		t.Fatalf("input device was not closed")
	}
	if !outKB.closed {
		t.Fatalf("output device was not closed")
	}
}

func TestOrchestratorClosesInputWhenOutputFactoryFails(t *testing.T) {
	useTempRuntimeDir(t)

	inDev := &mockInputDevice{path: "/dev/input/test"}

	orc := &orchestrator{
		cfg:         config.Runtime{},
		devicePaths: []string{"/dev/input/test"},
		opts:        StartOptions{},
		inputFactory: func(path string, grab bool) (inputDevice, <-chan event.KeyEvent, <-chan error, error) {
			return inDev, closedEventChan(), closedErrChan(), nil
		},
		outputFactory: func(name string, in <-chan event.KeyEvent) (outputDevice, <-chan error, error) {
			return nil, nil, os.ErrPermission
		},
		signalSource: make(chan os.Signal, 1),
	}

	err := orc.run()
	if err == nil {
		t.Fatalf("expected output factory error")
	}
	if !inDev.closed {
		t.Fatalf("input device was not closed")
	}
}

func TestClosePipelinesClosesAllDevices(t *testing.T) {
	firstIn := &mockInputDevice{}
	secondIn := &mockInputDevice{}
	firstOut := &mockOutputDevice{}
	secondOut := &mockOutputDevice{}

	closePipelines([]pipeline{
		{inDev: firstIn, outKB: firstOut},
		{inDev: secondIn, outKB: secondOut},
	})

	if !firstIn.closed || !secondIn.closed {
		t.Fatalf("not all input devices were closed")
	}
	if !firstOut.closed || !secondOut.closed {
		t.Fatalf("not all output devices were closed")
	}
}

func TestLoadShortcutSwitcher(t *testing.T) {
	t.Run("noop without shortcut layout", func(t *testing.T) {
		orc := &orchestrator{cfg: config.DefaultRuntime()}
		if err := orc.loadShortcutSwitcher(); err != nil {
			t.Fatalf("loadShortcutSwitcher: %v", err)
		}
		if orc.shortcutWrap != nil {
			t.Fatalf("expected no shortcut switcher")
		}
	})

	t.Run("loads shortcut switcher from factory", func(t *testing.T) {
		cfg := config.DefaultRuntime()
		cfg.ShortcutLayout = &config.ShortcutLayoutSpec{Layout: "us", Variant: "dvorak"}
		switcher := &fakeShortcutSwitcher{}

		orc := &orchestrator{
			cfg: cfg,
			shortcutMake: func(ctx context.Context, target config.ShortcutLayoutSpec, verbose bool) (shortcutSwitcher, shortcut.ValidationInfo, error) {
				_ = ctx
				_ = verbose
				if target.Layout != "us" || target.Variant != "dvorak" {
					t.Fatalf("unexpected target layout: %#v", target)
				}
				return switcher, shortcut.ValidationInfo{
					Current:     shortcut.LayoutInfo{Layout: "ru", Description: "ru"},
					Target:      shortcut.LayoutInfo{Layout: "us", Variant: "dvorak", Description: "us(dvorak)"},
					TargetIndex: 0,
				}, nil
			},
		}

		if err := orc.loadShortcutSwitcher(); err != nil {
			t.Fatalf("loadShortcutSwitcher: %v", err)
		}
		if orc.shortcutWrap != switcher {
			t.Fatalf("unexpected shortcut switcher: %#v", orc.shortcutWrap)
		}
	})

	t.Run("surfaces loader errors", func(t *testing.T) {
		cfg := config.DefaultRuntime()
		cfg.ShortcutLayout = &config.ShortcutLayoutSpec{Layout: "us"}

		orc := &orchestrator{
			cfg: cfg,
			shortcutMake: func(ctx context.Context, target config.ShortcutLayoutSpec, verbose bool) (shortcutSwitcher, shortcut.ValidationInfo, error) {
				_ = ctx
				_ = verbose
				return nil, shortcut.ValidationInfo{}, errors.New("dbus failed")
			},
		}

		err := orc.loadShortcutSwitcher()
		if err == nil {
			t.Fatalf("expected loader error")
		}
		if err.Error() != "load shortcut layout switch: dbus failed" {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestCleanupClosesShortcutSwitcher(t *testing.T) {
	switcher := &fakeShortcutSwitcher{}
	orc := &orchestrator{shortcutWrap: switcher}
	orc.cleanup()
	if !switcher.closed {
		t.Fatalf("expected shortcut switcher to be closed")
	}
}
