package daemon

import (
	"os"
	"testing"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon/event"
)

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

func TestOrchestratorReturnsWhenModulesFinishCleanly(t *testing.T) {
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
