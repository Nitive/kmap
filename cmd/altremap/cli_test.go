package main

import (
	"strings"
	"testing"
	"time"
)

func TestRunCLIDispatchesSetupKeymapSubcommand(t *testing.T) {
	origSetup := runSetupKeymapFn
	origRemap := runRemapFn
	defer func() {
		runSetupKeymapFn = origSetup
		runRemapFn = origRemap
	}()

	var called bool
	var gotArgs []string
	runSetupKeymapFn = func(args []string) error {
		called = true
		gotArgs = append([]string(nil), args...)
		return nil
	}
	runRemapFn = func(string, string, time.Duration, bool, bool) error {
		t.Fatalf("runRemapFn should not be called for setup-keymap subcommand")
		return nil
	}

	if err := runCLI([]string{"setup-keymap", "--output", "out.json"}); err != nil {
		t.Fatalf("runCLI: %v", err)
	}
	if !called {
		t.Fatalf("setup-keymap subcommand was not dispatched")
	}
	if len(gotArgs) != 2 || gotArgs[0] != "--output" || gotArgs[1] != "out.json" {
		t.Fatalf("unexpected setup-keymap args: %#v", gotArgs)
	}
}

func TestRunCLIDefaultsToRemapper(t *testing.T) {
	origSetup := runSetupKeymapFn
	origRemap := runRemapFn
	origGenerate := generateXComposeFn
	defer func() {
		runSetupKeymapFn = origSetup
		runRemapFn = origRemap
		generateXComposeFn = origGenerate
	}()

	runSetupKeymapFn = func(args []string) error {
		t.Fatalf("runSetupKeymapFn should not be called for default CLI mode")
		return nil
	}
	generateXComposeFn = func(configPath string, outputPath string) error {
		t.Fatalf("generateXComposeFn should not be called for default CLI mode")
		return nil
	}

	var (
		gotDevice  string
		gotConfig  string
		gotDelay   time.Duration
		gotGrab    bool
		gotVerbose bool
		called     bool
	)
	runRemapFn = func(device, config string, delay time.Duration, grab, verbose bool) error {
		called = true
		gotDevice = device
		gotConfig = config
		gotDelay = delay
		gotGrab = grab
		gotVerbose = verbose
		return nil
	}

	err := runCLI([]string{
		"--device", "/dev/input/test-kbd",
		"--config", "custom.yaml",
		"--compose-delay", "1ms",
		"--grab=false",
		"--verbose=true",
	})
	if err != nil {
		t.Fatalf("runCLI: %v", err)
	}
	if !called {
		t.Fatalf("runRemapFn was not called")
	}
	if gotDevice != "/dev/input/test-kbd" {
		t.Fatalf("device mismatch: %q", gotDevice)
	}
	if gotConfig != "custom.yaml" {
		t.Fatalf("config mismatch: %q", gotConfig)
	}
	if gotDelay != time.Millisecond {
		t.Fatalf("delay mismatch: %s", gotDelay)
	}
	if gotGrab {
		t.Fatalf("grab should be false")
	}
	if !gotVerbose {
		t.Fatalf("verbose should be true")
	}
}

func TestRunRemapCommandRejectsNegativeComposeDelay(t *testing.T) {
	err := runRemapCommand([]string{"--compose-delay", "-1ms"})
	if err == nil {
		t.Fatalf("expected error for negative compose delay")
	}
	if !strings.Contains(err.Error(), "compose-delay must be >= 0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCLIGeneratesXComposeAndExits(t *testing.T) {
	origSetup := runSetupKeymapFn
	origRemap := runRemapFn
	origGenerate := generateXComposeFn
	defer func() {
		runSetupKeymapFn = origSetup
		runRemapFn = origRemap
		generateXComposeFn = origGenerate
	}()

	runSetupKeymapFn = func(args []string) error {
		t.Fatalf("runSetupKeymapFn should not be called for generate-xcompose")
		return nil
	}
	runRemapFn = func(string, string, time.Duration, bool, bool) error {
		t.Fatalf("runRemapFn should not be called for generate-xcompose")
		return nil
	}

	var (
		called    bool
		gotConfig string
		gotOutput string
	)
	generateXComposeFn = func(configPath string, outputPath string) error {
		called = true
		gotConfig = configPath
		gotOutput = outputPath
		return nil
	}

	err := runCLI([]string{
		"--config", "altremap.yaml",
		"--generate-xcompose", "/tmp/generated.XCompose",
	})
	if err != nil {
		t.Fatalf("runCLI: %v", err)
	}
	if !called {
		t.Fatalf("generateXComposeFn was not called")
	}
	if gotConfig != "altremap.yaml" {
		t.Fatalf("config mismatch: %q", gotConfig)
	}
	if gotOutput != "/tmp/generated.XCompose" {
		t.Fatalf("output mismatch: %q", gotOutput)
	}
}
