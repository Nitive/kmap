package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon/event"
	"keyboard/pkg/daemon/input"
	"keyboard/pkg/daemon/mapper"
	"keyboard/pkg/daemon/output"
	"keyboard/pkg/daemon/shortcut"
)

type StartOptions struct {
	DeviceOverride string
	ConfigPath     string
	ComposeDelay   time.Duration
	Grab           bool
	Verbose        bool
}

type inputDevice interface {
	Path() string
	Close() error
	CapsLockEnabled() (bool, error)
	Grab(enable bool) error
}

type outputDevice interface {
	EmitKey(code uint16, value int32) error
	TapKey(code uint16, delay time.Duration) error
	Close() error
}

type pipeline struct {
	path string

	inDev inputDevice
	outKB outputDevice

	inErr       <-chan error
	mapErr      <-chan error
	shortcutErr <-chan error
	outErr      <-chan error
}

type moduleError struct {
	path   string
	module string
	err    error
}

type inputFactory func(path string, grab bool) (inputDevice, <-chan event.KeyEvent, <-chan error, error)
type outputFactory func(name string, in <-chan event.KeyEvent) (outputDevice, <-chan error, error)

type shortcutSwitcher interface {
	Wrap(in <-chan event.KeyEvent) (<-chan event.KeyEvent, <-chan error)
	Close(ctx context.Context) error
}

type shortcutSwitchFactory func(ctx context.Context, target config.ShortcutLayoutSpec, verbose bool) (shortcutSwitcher, shortcut.ValidationInfo, error)

type orchestrator struct {
	cfg           config.Runtime
	devicePaths   []string
	opts          StartOptions
	inputFactory  inputFactory
	outputFactory outputFactory
	signalSource  chan os.Signal
	pipelines     []pipeline
	moduleErrCh   chan moduleError
	moduleDoneCh  chan struct{}
	shortcutMake  shortcutSwitchFactory
	shortcutWrap  shortcutSwitcher
}

func Start(opts StartOptions) error {
	cfg, err := config.LoadRuntime(opts.ConfigPath)
	if err != nil {
		return err
	}

	devicePaths := resolveDevicePaths(opts.DeviceOverride, cfg)
	if len(devicePaths) == 0 {
		return errors.New("no input devices configured")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1, syscall.SIGUSR2)
	defer signal.Stop(sigCh)

	orc := &orchestrator{
		cfg:         cfg,
		devicePaths: devicePaths,
		opts:        opts,
		inputFactory: func(path string, grab bool) (inputDevice, <-chan event.KeyEvent, <-chan error, error) {
			return input.Start(input.Options{DevicePath: path, Grab: grab})
		},
		outputFactory: func(name string, in <-chan event.KeyEvent) (outputDevice, <-chan error, error) {
			kb, err := output.CreateVirtualKeyboard(name)
			if err != nil {
				return nil, nil, err
			}
			return kb, output.Run(kb, in), nil
		},
		shortcutMake: func(ctx context.Context, target config.ShortcutLayoutSpec, verbose bool) (shortcutSwitcher, shortcut.ValidationInfo, error) {
			return shortcut.NewSwitchManager(ctx, target, verbose)
		},
		signalSource: sigCh,
	}

	return orc.run()
}

func (o *orchestrator) run() error {
	cleanupPID, err := writePIDFile()
	if err != nil {
		return err
	}
	defer cleanupPID()

	if err := o.loadShortcutSwitcher(); err != nil {
		return err
	}

	for i, path := range o.devicePaths {
		inDev, inCh, inErr, startErr := o.inputFactory(path, o.opts.Grab)
		if startErr != nil {
			o.cleanup()
			return startErr
		}

		mappedCh, mapErr := mapper.Start(o.cfg, inCh, mapper.Options{
			ComposeDelay: o.opts.ComposeDelay,
			Verbose:      o.opts.Verbose,
		})

		outCh := (<-chan event.KeyEvent)(mappedCh)
		var shortcutErr <-chan error
		if o.shortcutWrap != nil {
			outCh, shortcutErr = o.shortcutWrap.Wrap(outCh)
		}

		outKB, outErr, startErr := o.outputFactory(fmt.Sprintf("kmap-%d", i+1), outCh)
		if startErr != nil {
			_ = inDev.Close()
			o.cleanup()
			return startErr
		}

		o.pipelines = append(o.pipelines, pipeline{
			path:        path,
			inDev:       inDev,
			outKB:       outKB,
			inErr:       inErr,
			mapErr:      mapErr,
			shortcutErr: shortcutErr,
			outErr:      outErr,
		})
	}
	defer o.cleanup()

	capsEnabled, err := o.pipelines[0].inDev.CapsLockEnabled()
	if err != nil {
		log.Printf("could not query caps-lock state: %v", err)
	}
	if err := releaseCapsOnStart(o.pipelines[0].outKB, o.cfg, capsEnabled, o.opts.ComposeDelay); err != nil {
		return fmt.Errorf("release caps on start: %w", err)
	}

	log.Printf("kmap started on %d input device(s)", len(o.pipelines))
	for _, p := range o.pipelines {
		log.Printf("input device: %s", p.path)
	}
	if o.opts.Grab {
		log.Printf("input devices are grabbed")
	}

	o.moduleErrCh = make(chan moduleError, len(o.pipelines)*4)
	o.moduleDoneCh = make(chan struct{}, len(o.pipelines)*4)
	expectedDone := 0
	for _, p := range o.pipelines {
		if o.watch(p.path, "input", p.inErr) {
			expectedDone++
		}
		if o.watch(p.path, "mapper", p.mapErr) {
			expectedDone++
		}
		if o.watch(p.path, "shortcut", p.shortcutErr) {
			expectedDone++
		}
		if o.watch(p.path, "output", p.outErr) {
			expectedDone++
		}
	}

	doneCount := 0
	for doneCount < expectedDone {
		select {
		case sig := <-o.signalSource:
			switch sig {
			case syscall.SIGUSR1:
				if err := o.setGrabState(false); err != nil {
					return err
				}
				log.Printf("released input grabs on request")
				continue
			case syscall.SIGUSR2:
				if err := o.setGrabState(true); err != nil {
					return err
				}
				log.Printf("restored input grabs on request")
				continue
			default:
				log.Printf("received signal %v, shutting down...", sig)
				return nil
			}

		case mErr := <-o.moduleErrCh:
			return fmt.Errorf("%s module %s: %w", mErr.module, mErr.path, mErr.err)

		case <-o.moduleDoneCh:
			doneCount++
		}
	}

	return nil
}

func (o *orchestrator) setGrabState(enable bool) error {
	if !o.opts.Grab {
		return nil
	}

	for _, p := range o.pipelines {
		if p.inDev == nil {
			continue
		}
		if err := p.inDev.Grab(enable); err != nil {
			action := "release"
			if enable {
				action = "restore"
			}
			return fmt.Errorf("%s input grab %s: %w", action, p.path, err)
		}
	}
	return nil
}

func (o *orchestrator) loadShortcutSwitcher() error {
	if o.cfg.ShortcutLayout == nil {
		return nil
	}

	makeProvider := o.shortcutMake
	if makeProvider == nil {
		makeProvider = func(ctx context.Context, target config.ShortcutLayoutSpec, verbose bool) (shortcutSwitcher, shortcut.ValidationInfo, error) {
			return shortcut.NewSwitchManager(ctx, target, verbose)
		}
	}

	switcher, info, err := makeProvider(context.Background(), *o.cfg.ShortcutLayout, o.opts.Verbose)
	if err != nil {
		return fmt.Errorf("load shortcut layout switch: %w", err)
	}

	o.shortcutWrap = switcher
	log.Printf(
		"shortcut layout switch enabled: current=%s target=%s target_index=%d",
		formatLayoutForLog(info.Current.Layout, info.Current.Variant),
		formatLayoutForLog(info.Target.Layout, info.Target.Variant),
		info.TargetIndex,
	)
	return nil
}

func (o *orchestrator) watch(path string, module string, ch <-chan error) bool {
	if ch == nil {
		return false
	}

	go func() {
		for moduleErr := range ch {
			if moduleErr != nil {
				o.moduleErrCh <- moduleError{path: path, module: module, err: moduleErr}
				return
			}
		}
		o.moduleDoneCh <- struct{}{}
	}()
	return true
}

func (o *orchestrator) cleanup() {
	for _, p := range o.pipelines {
		if p.inDev != nil {
			_ = p.inDev.Close()
		}
		if p.outKB != nil {
			_ = p.outKB.Close()
		}
	}
	if o.shortcutWrap != nil {
		_ = o.shortcutWrap.Close(context.Background())
	}
}

func closePipelines(pipelines []pipeline) {
	for _, p := range pipelines {
		if p.inDev != nil {
			_ = p.inDev.Close()
		}
		if p.outKB != nil {
			_ = p.outKB.Close()
		}
	}
}

type keyTapper interface {
	TapKey(code uint16, delay time.Duration) error
}

func releaseCapsOnStart(out keyTapper, cfg config.Runtime, capsEnabled bool, delay time.Duration) error {
	if len(cfg.CapsMappings) == 0 {
		return nil
	}
	if !capsEnabled {
		return nil
	}
	return out.TapKey(config.KeyCapsLock, delay)
}

func resolveDevicePaths(deviceOverride string, cfg config.Runtime) []string {
	trimmedOverride := strings.TrimSpace(deviceOverride)
	if trimmedOverride != "" {
		return []string{trimmedOverride}
	}

	if len(cfg.Devices) > 0 {
		paths := make([]string, len(cfg.Devices))
		copy(paths, cfg.Devices)
		return paths
	}

	return []string{config.DefaultDevicePath}
}

func formatLayoutForLog(layout string, variant string) string {
	if strings.TrimSpace(variant) == "" {
		return layout
	}
	return fmt.Sprintf("%s(%s)", layout, variant)
}
