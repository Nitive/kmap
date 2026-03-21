package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
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
	closeCh         chan struct{}
}

func (m *mockInputDevice) Path() string { return m.path }
func (m *mockInputDevice) Close() error {
	m.closed = true
	if m.closeCh != nil {
		select {
		case m.closeCh <- struct{}{}:
		default:
		}
	}
	return nil
}
func (m *mockInputDevice) CapsLockEnabled() (bool, error) { return m.capsLockEnabled, nil }
func (m *mockInputDevice) Grab(enable bool) error         { m.grabbed = enable; return nil }

type mockOutputDevice struct {
	closed  bool
	closeCh chan struct{}
}

func (m *mockOutputDevice) EmitKey(code uint16, value int32) error        { return nil }
func (m *mockOutputDevice) TapKey(code uint16, delay time.Duration) error { return nil }
func (m *mockOutputDevice) Close() error {
	m.closed = true
	if m.closeCh != nil {
		select {
		case m.closeCh <- struct{}{}:
		default:
		}
	}
	return nil
}

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

	t.Run("keeps running when one device is unavailable at startup", func(t *testing.T) {
		useTempRuntimeDir(t)

		availablePath := "/dev/input/available"
		missingPath := "/dev/input/missing"
		inDev := &mockInputDevice{path: availablePath}
		outKB := &mockOutputDevice{}
		sigCh := make(chan os.Signal, 1)
		startedCh := make(chan string, 2)

		orc := &orchestrator{
			cfg:         config.Runtime{},
			devicePaths: []string{missingPath, availablePath},
			inputFactory: func(path string, grab bool) (inputDevice, <-chan event.KeyEvent, <-chan error, error) {
				_ = grab
				if path == missingPath {
					return nil, nil, nil, fmt.Errorf("open input device %s: %w", path, os.ErrNotExist)
				}
				startedCh <- path
				return inDev, make(chan event.KeyEvent), make(chan error), nil
			},
			outputFactory: func(name string, in <-chan event.KeyEvent) (outputDevice, <-chan error, error) {
				return outKB, make(chan error), nil
			},
			retrySource:  make(chan struct{}),
			signalSource: sigCh,
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- orc.run()
		}()

		select {
		case path := <-startedCh:
			if path != availablePath {
				t.Fatalf("unexpected started device: %s", path)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timed out waiting for available device to start")
		}

		select {
		case err := <-errCh:
			t.Fatalf("orc.run() returned early: %v", err)
		case <-time.After(20 * time.Millisecond):
		}

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

	t.Run("keeps running when one device is busy at startup", func(t *testing.T) {
		useTempRuntimeDir(t)

		busyPath := "/dev/input/busy"
		availablePath := "/dev/input/external"
		inDev := &mockInputDevice{path: availablePath}
		outKB := &mockOutputDevice{}
		sigCh := make(chan os.Signal, 1)
		startedCh := make(chan string, 2)

		orc := &orchestrator{
			cfg:         config.Runtime{},
			devicePaths: []string{busyPath, availablePath},
			inputFactory: func(path string, grab bool) (inputDevice, <-chan event.KeyEvent, <-chan error, error) {
				_ = grab
				if path == busyPath {
					return nil, nil, nil, fmt.Errorf("grab input device %s: %w", path, syscall.EBUSY)
				}
				startedCh <- path
				return inDev, make(chan event.KeyEvent), make(chan error), nil
			},
			outputFactory: func(name string, in <-chan event.KeyEvent) (outputDevice, <-chan error, error) {
				return outKB, make(chan error), nil
			},
			retrySource:  make(chan struct{}),
			signalSource: sigCh,
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- orc.run()
		}()

		select {
		case path := <-startedCh:
			if path != availablePath {
				t.Fatalf("unexpected started device: %s", path)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timed out waiting for available device to start")
		}

		select {
		case err := <-errCh:
			t.Fatalf("orc.run() returned early: %v", err)
		case <-time.After(20 * time.Millisecond):
		}

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

	t.Run("retries pending devices when they become available", func(t *testing.T) {
		useTempRuntimeDir(t)

		path := "/dev/input/hotplug"
		sigCh := make(chan os.Signal, 1)
		retryCh := make(chan struct{}, 1)
		startedCh := make(chan *mockInputDevice, 1)
		outKB := &mockOutputDevice{}
		var available atomic.Bool

		orc := &orchestrator{
			cfg:         config.Runtime{},
			devicePaths: []string{path},
			inputFactory: func(devicePath string, grab bool) (inputDevice, <-chan event.KeyEvent, <-chan error, error) {
				_ = grab
				if !available.Load() {
					return nil, nil, nil, fmt.Errorf("open input device %s: %w", devicePath, os.ErrNotExist)
				}
				dev := &mockInputDevice{path: devicePath}
				startedCh <- dev
				return dev, make(chan event.KeyEvent), make(chan error), nil
			},
			outputFactory: func(name string, in <-chan event.KeyEvent) (outputDevice, <-chan error, error) {
				return outKB, make(chan error), nil
			},
			retrySource:  retryCh,
			signalSource: sigCh,
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- orc.run()
		}()

		select {
		case err := <-errCh:
			t.Fatalf("orc.run() returned early: %v", err)
		case <-time.After(20 * time.Millisecond):
		}

		available.Store(true)
		retryCh <- struct{}{}

		var inDev *mockInputDevice
		select {
		case inDev = <-startedCh:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timed out waiting for hotplug device to start")
		}

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
			t.Fatal("reconnected input device was not closed")
		}
		if !outKB.closed {
			t.Fatal("output device was not closed")
		}
	})

	t.Run("reconnects a disconnected device", func(t *testing.T) {
		useTempRuntimeDir(t)

		path := "/dev/input/external"
		sigCh := make(chan os.Signal, 1)
		retryCh := make(chan struct{}, 1)
		startedCh := make(chan *mockInputDevice, 2)
		inErrChs := []chan error{
			make(chan error, 1),
			make(chan error, 1),
		}
		outFirst := &mockOutputDevice{}
		outSecond := &mockOutputDevice{}
		var starts atomic.Int32

		orc := &orchestrator{
			cfg:         config.Runtime{},
			devicePaths: []string{path},
			inputFactory: func(devicePath string, grab bool) (inputDevice, <-chan event.KeyEvent, <-chan error, error) {
				_ = grab
				idx := int(starts.Add(1)) - 1
				if idx >= len(inErrChs) {
					t.Fatalf("unexpected extra start for %s", devicePath)
				}
				dev := &mockInputDevice{path: devicePath, closeCh: make(chan struct{}, 1)}
				startedCh <- dev
				return dev, make(chan event.KeyEvent), inErrChs[idx], nil
			},
			outputFactory: func(name string, in <-chan event.KeyEvent) (outputDevice, <-chan error, error) {
				if name == "kmap-1" && starts.Load() == 1 {
					return outFirst, make(chan error), nil
				}
				return outSecond, make(chan error), nil
			},
			retrySource:  retryCh,
			signalSource: sigCh,
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- orc.run()
		}()

		var firstDev *mockInputDevice
		select {
		case firstDev = <-startedCh:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timed out waiting for first device start")
		}

		inErrChs[0] <- fmt.Errorf("device disconnected: %w", syscall.ENODEV)

		select {
		case <-firstDev.closeCh:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timed out waiting for disconnected device to close")
		}

		retryCh <- struct{}{}

		var secondDev *mockInputDevice
		select {
		case secondDev = <-startedCh:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timed out waiting for device reconnect")
		}

		sigCh <- syscall.SIGINT

		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("orc.run() returned error: %v", err)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timed out waiting for orc.run() to return")
		}

		if !firstDev.closed {
			t.Fatal("first input device was not closed on disconnect")
		}
		if !secondDev.closed {
			t.Fatal("reconnected input device was not closed on shutdown")
		}
		if !outFirst.closed {
			t.Fatal("first output device was not closed")
		}
		if !outSecond.closed {
			t.Fatal("second output device was not closed")
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

func TestSetPauseStateReleasesAndRestoresGrabs(t *testing.T) {
	inDev := &mockInputDevice{path: "/dev/input/test", grabbed: true}
	pauseCh := make(chan bool, 1)
	orc := &orchestrator{
		opts: StartOptions{Grab: true},
		pipelines: map[string]pipeline{
			"/dev/input/test": {
				path:  "/dev/input/test",
				inDev: inDev,
				pause: pauseCh,
			},
		},
	}

	if err := orc.setPauseState(true); err != nil {
		t.Fatalf("setPauseState(true): %v", err)
	}
	if !orc.paused {
		t.Fatalf("expected paused state to be true")
	}
	if inDev.grabbed {
		t.Fatalf("expected input grab to be released")
	}
	select {
	case paused := <-pauseCh:
		if !paused {
			t.Fatalf("expected pause broadcast to be true")
		}
	default:
		t.Fatalf("expected pause broadcast")
	}

	if err := orc.setPauseState(false); err != nil {
		t.Fatalf("setPauseState(false): %v", err)
	}
	if orc.paused {
		t.Fatalf("expected paused state to be false")
	}
	if !inDev.grabbed {
		t.Fatalf("expected input grab to be restored")
	}
	select {
	case paused := <-pauseCh:
		if paused {
			t.Fatalf("expected pause broadcast to be false")
		}
	default:
		t.Fatalf("expected resume broadcast")
	}
}

func TestWatchDirForPath(t *testing.T) {
	t.Run("uses direct parent when it exists", func(t *testing.T) {
		root := t.TempDir()
		parent := root + "/devices"
		if err := os.Mkdir(parent, 0o755); err != nil {
			t.Fatalf("mkdir parent: %v", err)
		}

		dir, ok := watchDirForPath(parent + "/keyboard0")
		if !ok {
			t.Fatalf("expected watch dir")
		}
		if dir != parent {
			t.Fatalf("unexpected watch dir: got=%q want=%q", dir, parent)
		}
	})

	t.Run("uses nearest existing ancestor when parent is missing", func(t *testing.T) {
		root := t.TempDir()
		existing := root + "/devices"
		if err := os.Mkdir(existing, 0o755); err != nil {
			t.Fatalf("mkdir existing: %v", err)
		}

		dir, ok := watchDirForPath(existing + "/by-id/keyboard0")
		if !ok {
			t.Fatalf("expected watch dir")
		}
		if dir != existing {
			t.Fatalf("unexpected watch dir: got=%q want=%q", dir, existing)
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

func TestOrchestratorKeepsRunningWhenInputModuleStopsCleanly(t *testing.T) {
	useTempRuntimeDir(t)

	inDev := &mockInputDevice{path: "/dev/input/test"}
	outKB := &mockOutputDevice{}
	sigCh := make(chan os.Signal, 1)

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
		retrySource:  make(chan struct{}),
		signalSource: sigCh,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- orc.run()
	}()

	select {
	case err := <-errCh:
		t.Fatalf("orc.run() returned early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	sigCh <- syscall.SIGINT

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("orc.run: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for orc.run() to return")
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
			shortcutMake: func(ctx context.Context, gotCfg config.Runtime, verbose bool) (shortcutSwitcher, shortcut.ValidationInfo, error) {
				_ = ctx
				_ = verbose
				if gotCfg.ShortcutLayout == nil || gotCfg.ShortcutLayout.Layout != "us" || gotCfg.ShortcutLayout.Variant != "dvorak" {
					t.Fatalf("unexpected config: %#v", gotCfg.ShortcutLayout)
				}
				return switcher, shortcut.ValidationInfo{
					Current:             shortcut.LayoutInfo{Layout: "ru", Description: "ru"},
					ShortcutTarget:      shortcut.LayoutInfo{Layout: "us", Variant: "dvorak", Description: "us(dvorak)"},
					ShortcutTargetIndex: 0,
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
			shortcutMake: func(ctx context.Context, gotCfg config.Runtime, verbose bool) (shortcutSwitcher, shortcut.ValidationInfo, error) {
				_ = ctx
				_ = verbose
				if gotCfg.ShortcutLayout == nil || gotCfg.ShortcutLayout.Layout != "us" {
					t.Fatalf("unexpected config: %#v", gotCfg.ShortcutLayout)
				}
				return nil, shortcut.ValidationInfo{}, errors.New("dbus failed")
			},
		}

		err := orc.loadShortcutSwitcher()
		if err == nil {
			t.Fatalf("expected loader error")
		}
		if err.Error() != "load layout switcher: dbus failed" {
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
