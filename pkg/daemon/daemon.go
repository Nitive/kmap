package daemon

import (
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
	"keyboard/pkg/daemon/input"
	"keyboard/pkg/daemon/mapper"
	"keyboard/pkg/daemon/output"
)

type StartOptions struct {
	DeviceOverride string
	ConfigPath     string
	ComposeDelay   time.Duration
	Grab           bool
	Verbose        bool
}

type pipeline struct {
	path string

	inDev *input.Device
	outKB *output.Keyboard

	inErr  <-chan error
	mapErr <-chan error
	outErr <-chan error
}

type moduleError struct {
	path   string
	module string
	err    error
}

func Start(opts StartOptions) error {
	cfg, err := config.LoadRuntime(opts.ConfigPath)
	if err != nil {
		return err
	}

	devicePaths, err := resolveDevicePaths(opts.DeviceOverride, cfg)
	if err != nil {
		return err
	}
	if len(devicePaths) == 0 {
		return errors.New("no input devices configured")
	}

	pipelines := make([]pipeline, 0, len(devicePaths))
	for i, path := range devicePaths {
		inDev, inCh, inErr, startErr := input.Start(input.Options{
			DevicePath: path,
			Grab:       opts.Grab,
		})
		if startErr != nil {
			closePipelines(pipelines)
			return startErr
		}

		mappedCh, mapErr := mapper.Start(cfg, inCh, mapper.Options{
			ComposeDelay: opts.ComposeDelay,
			Verbose:      opts.Verbose,
		})

		outKB, startErr := output.CreateVirtualKeyboard(fmt.Sprintf("kmap-%d", i+1))
		if startErr != nil {
			_ = inDev.Close()
			closePipelines(pipelines)
			return startErr
		}
		outErr := output.Run(outKB, mappedCh)

		pipelines = append(pipelines, pipeline{
			path:   path,
			inDev:  inDev,
			outKB:  outKB,
			inErr:  inErr,
			mapErr: mapErr,
			outErr: outErr,
		})
	}
	defer closePipelines(pipelines)

	capsEnabled, err := pipelines[0].inDev.CapsLockEnabled()
	if err != nil {
		log.Printf("could not query caps-lock state: %v", err)
	}
	if err := releaseCapsOnStart(pipelines[0].outKB, cfg, capsEnabled, opts.ComposeDelay); err != nil {
		return fmt.Errorf("release caps on start: %w", err)
	}

	log.Printf("kmap started on %d input device(s)", len(pipelines))
	for _, p := range pipelines {
		log.Printf("input device: %s", p.path)
	}
	if opts.Grab {
		log.Printf("input devices are grabbed")
	}
	if opts.ConfigPath != "" {
		log.Printf("config path: %s", opts.ConfigPath)
	}

	errCh := make(chan moduleError, len(pipelines)*3)
	doneCh := make(chan struct{}, len(pipelines)*3)
	watch := func(path string, module string, ch <-chan error) {
		go func() {
			for moduleErr := range ch {
				if moduleErr != nil {
					errCh <- moduleError{path: path, module: module, err: moduleErr}
					return
				}
			}
			doneCh <- struct{}{}
		}()
	}

	for _, p := range pipelines {
		watch(p.path, "input", p.inErr)
		watch(p.path, "mapper", p.mapErr)
		watch(p.path, "output", p.outErr)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var closeOnce sync.Once
	closeInputs := func() {
		closeOnce.Do(func() {
			for _, p := range pipelines {
				_ = p.inDev.Close()
			}
		})
	}

	expectedDone := len(pipelines) * 3
	doneCount := 0
	for doneCount < expectedDone {
		select {
		case sig := <-sigCh:
			log.Printf("received signal %s, shutting down", sig)
			closeInputs()
		case moduleErr := <-errCh:
			closeInputs()
			return fmt.Errorf("%s module %s: %w", moduleErr.module, moduleErr.path, moduleErr.err)
		case <-doneCh:
			doneCount++
		}
	}

	return nil
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

func resolveDevicePaths(deviceOverride string, cfg config.Runtime) ([]string, error) {
	trimmedOverride := strings.TrimSpace(deviceOverride)
	if trimmedOverride != "" {
		return []string{trimmedOverride}, nil
	}

	if len(cfg.Devices) > 0 {
		paths := make([]string, len(cfg.Devices))
		copy(paths, cfg.Devices)
		return paths, nil
	}

	return []string{config.DefaultDevicePath}, nil
}
