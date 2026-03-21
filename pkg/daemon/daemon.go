package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
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
	id   uint64
	path string

	inDev inputDevice
	outKB outputDevice
	pause chan bool

	inErr       <-chan error
	mapErr      <-chan error
	shortcutErr <-chan error
	outErr      <-chan error
}

type moduleEvent struct {
	id     uint64
	path   string
	module string
	err    error
}

type controlEvent struct {
	id   uint64
	path string
	kind event.Kind
}

type inputFactory func(path string, grab bool) (inputDevice, <-chan event.KeyEvent, <-chan error, error)
type outputFactory func(name string, in <-chan event.KeyEvent) (outputDevice, <-chan error, error)

type shortcutSwitcher interface {
	Wrap(in <-chan event.KeyEvent) (<-chan event.KeyEvent, <-chan error)
	Close(ctx context.Context) error
}

type shortcutSwitchFactory func(ctx context.Context, cfg config.Runtime, verbose bool) (shortcutSwitcher, shortcut.ValidationInfo, error)

type orchestrator struct {
	cfg            config.Runtime
	devicePaths    []string
	opts           StartOptions
	inputFactory   inputFactory
	outputFactory  outputFactory
	signalSource   chan os.Signal
	pipelines      map[string]pipeline
	pendingPaths   map[string]struct{}
	moduleEventCh  chan moduleEvent
	controlEventCh chan controlEvent
	shortcutMake   shortcutSwitchFactory
	shortcutWrap   shortcutSwitcher
	retrySource    <-chan struct{}
	retryWatch     deviceWatcher
	nextPipelineID uint64
	grabEnabled    bool
	capsReleased   bool
	paused         bool
	stopCh         chan struct{}
	stopOnce       sync.Once
}

const deviceRetryInterval = time.Second

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
		shortcutMake: func(ctx context.Context, cfg config.Runtime, verbose bool) (shortcutSwitcher, shortcut.ValidationInfo, error) {
			return shortcut.NewSwitchManager(ctx, cfg, verbose)
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
	defer o.shutdown()

	if err := o.loadShortcutSwitcher(); err != nil {
		return err
	}

	o.ensureState()
	for _, path := range o.devicePaths {
		if err := o.startPipeline(path); err != nil {
			if isRetryableInputError(err) {
				o.markUnavailable(path, err)
				continue
			}
			return err
		}
	}

	o.logStartup()

	retryCh := o.retrySource
	var watchErrCh <-chan error
	ticker := (*time.Ticker)(nil)
	if retryCh == nil {
		if err := o.enableDeviceWatch(); err != nil {
			log.Printf("device watcher unavailable, falling back to polling: %v", err)
			ticker = time.NewTicker(deviceRetryInterval)
		} else {
			retryCh = o.retryWatch.Events()
			watchErrCh = o.retryWatch.Errors()
		}
	}
	if ticker != nil {
		defer ticker.Stop()
	}
	if len(o.pendingPaths) > 0 {
		if err := o.retryPendingDevices(); err != nil {
			return err
		}
	}

	for {
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

		case mErr := <-o.moduleEventCh:
			if err := o.handleModuleEvent(mErr); err != nil {
				return err
			}

		case ctrl := <-o.controlEventCh:
			if err := o.handleControlEvent(ctrl); err != nil {
				return err
			}

		case <-retryCh:
			if err := o.refreshDeviceWatch(); err != nil {
				log.Printf("device watcher failed, falling back to polling: %v", err)
				o.disableDeviceWatch()
				retryCh = nil
				watchErrCh = nil
				if ticker == nil {
					ticker = time.NewTicker(deviceRetryInterval)
				}
			}
			if err := o.retryPendingDevices(); err != nil {
				return err
			}

		case <-tickerC(ticker):
			if err := o.retryPendingDevices(); err != nil {
				return err
			}

		case err := <-watchErrCh:
			log.Printf("device watcher failed, falling back to polling: %v", err)
			o.disableDeviceWatch()
			retryCh = nil
			watchErrCh = nil
			if ticker == nil {
				ticker = time.NewTicker(deviceRetryInterval)
			}
		}
	}
}

func (o *orchestrator) setGrabState(enable bool) error {
	o.grabEnabled = enable
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
	if o.cfg.ShortcutLayout == nil && len(o.cfg.TapLayoutSwitches) == 0 {
		return nil
	}

	makeProvider := o.shortcutMake
	if makeProvider == nil {
		makeProvider = func(ctx context.Context, cfg config.Runtime, verbose bool) (shortcutSwitcher, shortcut.ValidationInfo, error) {
			return shortcut.NewSwitchManager(ctx, cfg, verbose)
		}
	}

	switcher, info, err := makeProvider(context.Background(), o.cfg, o.opts.Verbose)
	if err != nil {
		return fmt.Errorf("load layout switcher: %w", err)
	}

	o.shortcutWrap = switcher
	message := fmt.Sprintf("layout switching enabled: current=%s", formatLayoutForLog(info.Current.Layout, info.Current.Variant))
	if o.cfg.ShortcutLayout != nil {
		message += fmt.Sprintf(
			" shortcut_target=%s shortcut_target_index=%d",
			formatLayoutForLog(info.ShortcutTarget.Layout, info.ShortcutTarget.Variant),
			info.ShortcutTargetIndex,
		)
	}
	if len(info.TapSwitches) > 0 {
		message += fmt.Sprintf(" tap_switches=%d", len(info.TapSwitches))
	}
	log.Print(message)
	return nil
}

func (o *orchestrator) watch(id uint64, path string, module string, ch <-chan error) {
	if ch == nil {
		return
	}

	go func() {
		for moduleErr := range ch {
			if moduleErr != nil {
				o.sendModuleEvent(moduleEvent{id: id, path: path, module: module, err: moduleErr})
				return
			}
		}
		o.sendModuleEvent(moduleEvent{id: id, path: path, module: module})
	}()
}

func (o *orchestrator) cleanup() {
	o.disableDeviceWatch()
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

func (o *orchestrator) shutdown() {
	o.stopOnce.Do(func() {
		if o.stopCh != nil {
			close(o.stopCh)
		}
		o.cleanup()
	})
}

func (o *orchestrator) ensureState() {
	if o.pipelines == nil {
		o.pipelines = make(map[string]pipeline, len(o.devicePaths))
	}
	if o.pendingPaths == nil {
		o.pendingPaths = make(map[string]struct{}, len(o.devicePaths))
	}
	if o.moduleEventCh == nil {
		size := len(o.devicePaths)*8 + 8
		o.moduleEventCh = make(chan moduleEvent, size)
	}
	if o.controlEventCh == nil {
		o.controlEventCh = make(chan controlEvent, len(o.devicePaths)*4+4)
	}
	if o.stopCh == nil {
		o.stopCh = make(chan struct{})
	}
	o.grabEnabled = o.opts.Grab
}

func (o *orchestrator) sendModuleEvent(ev moduleEvent) {
	select {
	case o.moduleEventCh <- ev:
	case <-o.stopCh:
	}
}

func (o *orchestrator) sendControlEvent(ev controlEvent) {
	select {
	case o.controlEventCh <- ev:
	case <-o.stopCh:
	}
}

func (o *orchestrator) logStartup() {
	if len(o.pipelines) == 0 {
		log.Printf("kmap started with no active input devices; waiting for configured devices")
		return
	}

	log.Printf("kmap started on %d input device(s)", len(o.pipelines))
	for _, path := range o.devicePaths {
		if _, ok := o.pipelines[path]; ok {
			log.Printf("input device: %s", path)
		}
	}
	if o.opts.Grab {
		log.Printf("input devices are grabbed")
	}
	if len(o.pendingPaths) > 0 {
		log.Printf("waiting for %d unavailable input device(s)", len(o.pendingPaths))
	}
}

func (o *orchestrator) startPipeline(path string) error {
	if _, exists := o.pipelines[path]; exists {
		return nil
	}

	inDev, inCh, inErr, startErr := o.inputFactory(path, o.grabEnabled)
	if startErr != nil {
		return startErr
	}

	pipelineID := o.nextPipelineID + 1
	pauseCh := make(chan bool, 1)
	mappedCh, mapErr := mapper.Start(o.cfg, inCh, mapper.Options{
		ComposeDelay:  o.opts.ComposeDelay,
		Verbose:       o.opts.Verbose,
		PauseSource:   pauseCh,
		InitialPaused: o.paused,
	})

	outCh := o.wrapControlEvents(path, pipelineID, mappedCh)

	var shortcutErr <-chan error
	if o.shortcutWrap != nil {
		outCh, shortcutErr = o.shortcutWrap.Wrap(outCh)
	}

	outKB, outErr, startErr := o.outputFactory(o.outputName(path), outCh)
	if startErr != nil {
		_ = inDev.Close()
		return startErr
	}

	o.nextPipelineID = pipelineID
	p := pipeline{
		id:          pipelineID,
		path:        path,
		inDev:       inDev,
		outKB:       outKB,
		pause:       pauseCh,
		inErr:       inErr,
		mapErr:      mapErr,
		shortcutErr: shortcutErr,
		outErr:      outErr,
	}

	o.pipelines[path] = p
	delete(o.pendingPaths, path)
	o.watch(p.id, path, "input", p.inErr)
	o.watch(p.id, path, "mapper", p.mapErr)
	o.watch(p.id, path, "shortcut", p.shortcutErr)
	o.watch(p.id, path, "output", p.outErr)

	if err := o.releaseCapsIfNeeded(p); err != nil {
		o.removePipeline(path)
		return err
	}

	return nil
}

func (o *orchestrator) retryPendingDevices() error {
	for _, path := range o.devicePaths {
		if _, pending := o.pendingPaths[path]; !pending {
			continue
		}
		if err := o.startPipeline(path); err != nil {
			if isRetryableInputError(err) {
				continue
			}
			return err
		}
		log.Printf("captured input device: %s", path)
	}
	return o.refreshDeviceWatch()
}

func (o *orchestrator) handleModuleEvent(ev moduleEvent) error {
	p, ok := o.pipelines[ev.path]
	if !ok || p.id != ev.id {
		return nil
	}

	if ev.module != "input" {
		if ev.err != nil {
			return fmt.Errorf("%s module %s: %w", ev.module, ev.path, ev.err)
		}
		return nil
	}

	if ev.err != nil && !isRetryableInputError(ev.err) {
		return fmt.Errorf("%s module %s: %w", ev.module, ev.path, ev.err)
	}

	o.removePipeline(ev.path)
	o.markDisconnected(ev.path, ev.err)
	return o.retryPendingDevices()
}

func (o *orchestrator) removePipeline(path string) {
	p, ok := o.pipelines[path]
	if !ok {
		return
	}
	delete(o.pipelines, path)
	if p.inDev != nil {
		_ = p.inDev.Close()
	}
	if p.outKB != nil {
		_ = p.outKB.Close()
	}
}

func (o *orchestrator) wrapControlEvents(path string, id uint64, in <-chan event.KeyEvent) <-chan event.KeyEvent {
	out := make(chan event.KeyEvent, 128)

	go func() {
		defer close(out)
		for ev := range in {
			if ev.Kind == event.KindPauseToggle {
				o.sendControlEvent(controlEvent{path: path, id: id, kind: ev.Kind})
				continue
			}

			select {
			case out <- ev:
			case <-o.stopCh:
				return
			}
		}
	}()

	return out
}

func (o *orchestrator) markUnavailable(path string, err error) {
	if _, exists := o.pendingPaths[path]; exists {
		return
	}
	o.pendingPaths[path] = struct{}{}
	log.Printf("input device unavailable, waiting to capture %s: %v", path, err)
}

func (o *orchestrator) markDisconnected(path string, err error) {
	if _, exists := o.pendingPaths[path]; exists {
		return
	}
	o.pendingPaths[path] = struct{}{}
	if err != nil {
		log.Printf("input device disconnected, waiting to recapture %s: %v", path, err)
		return
	}
	log.Printf("input device disconnected, waiting to recapture: %s", path)
}

func (o *orchestrator) queuePauseState(p pipeline, paused bool) {
	if p.pause == nil {
		return
	}
	select {
	case p.pause <- paused:
	default:
		select {
		case <-p.pause:
		default:
		}
		p.pause <- paused
	}
}

func (o *orchestrator) setPauseState(paused bool) error {
	if o.paused == paused {
		return nil
	}

	if paused {
		for _, p := range o.pipelines {
			o.queuePauseState(p, true)
		}
		o.paused = true
		if err := o.setGrabState(false); err != nil {
			o.paused = false
			for _, p := range o.pipelines {
				o.queuePauseState(p, false)
			}
			return err
		}
		log.Printf("kmap paused: input grabs released and remapped output suspended")
		return nil
	}

	if err := o.setGrabState(o.opts.Grab); err != nil {
		return err
	}
	o.paused = false
	for _, p := range o.pipelines {
		o.queuePauseState(p, false)
	}
	log.Printf("kmap resumed: input grabs restored and remapping active")
	return nil
}

func (o *orchestrator) handleControlEvent(ev controlEvent) error {
	p, ok := o.pipelines[ev.path]
	if !ok || p.id != ev.id {
		return nil
	}
	if ev.kind != event.KindPauseToggle {
		return fmt.Errorf("unsupported control event kind %d", ev.kind)
	}
	return o.setPauseState(!o.paused)
}

func (o *orchestrator) releaseCapsIfNeeded(p pipeline) error {
	if o.capsReleased {
		return nil
	}

	capsEnabled, err := p.inDev.CapsLockEnabled()
	if err != nil {
		log.Printf("could not query caps-lock state: %v", err)
		o.capsReleased = true
		return nil
	}
	if err := releaseCapsOnStart(p.outKB, o.cfg, capsEnabled, o.opts.ComposeDelay); err != nil {
		return fmt.Errorf("release caps on start: %w", err)
	}
	o.capsReleased = true
	return nil
}

func (o *orchestrator) outputName(path string) string {
	for i, devicePath := range o.devicePaths {
		if devicePath == path {
			return fmt.Sprintf("kmap-%d", i+1)
		}
	}
	return "kmap"
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

func (o *orchestrator) enableDeviceWatch() error {
	if o.retryWatch != nil {
		return o.refreshDeviceWatch()
	}

	watcher, err := newDeviceWatcher()
	if err != nil {
		return err
	}
	o.retryWatch = watcher

	if err := o.refreshDeviceWatch(); err != nil {
		o.disableDeviceWatch()
		return err
	}

	return nil
}

func (o *orchestrator) disableDeviceWatch() {
	if o.retryWatch == nil {
		return
	}
	_ = o.retryWatch.Close()
	o.retryWatch = nil
}

func (o *orchestrator) refreshDeviceWatch() error {
	if o.retryWatch == nil {
		return nil
	}
	return o.retryWatch.Sync(o.pendingDevicePaths())
}

func (o *orchestrator) pendingDevicePaths() []string {
	if len(o.pendingPaths) == 0 {
		return nil
	}

	paths := make([]string, 0, len(o.pendingPaths))
	for _, path := range o.devicePaths {
		if _, pending := o.pendingPaths[path]; pending {
			paths = append(paths, path)
		}
	}
	return paths
}

func tickerC(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}

func isRetryableInputError(err error) bool {
	return errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ENODEV) ||
		errors.Is(err, syscall.ENXIO) ||
		errors.Is(err, syscall.EIO)
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
