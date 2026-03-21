package cli

import (
	"strings"
	"testing"
	"time"
)

func TestRunCLIDispatchesSetupKeymapSubcommand(t *testing.T) {
	origSetup := runSetupKeymapFn
	origStart := runStartFn
	defer func() {
		runSetupKeymapFn = origSetup
		runStartFn = origStart
	}()

	var called bool
	var gotArgs []string
	runSetupKeymapFn = func(args []string) error {
		called = true
		gotArgs = append([]string(nil), args...)
		return nil
	}
	runStartFn = func(string, string, time.Duration, bool, bool) error {
		t.Fatalf("runStartFn should not be called for setup-keymap subcommand")
		return nil
	}

	if err := Run([]string{"setup-keymap", "--output", "out.json"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called {
		t.Fatalf("setup-keymap subcommand was not dispatched")
	}
	if len(gotArgs) != 2 || gotArgs[0] != "--output" || gotArgs[1] != "out.json" {
		t.Fatalf("unexpected setup-keymap args: %#v", gotArgs)
	}
}

func TestRunCLIDispatchesStartSubcommand(t *testing.T) {
	origSetup := runSetupKeymapFn
	origStart := runStartFn
	origGenerate := generateFn
	defer func() {
		runSetupKeymapFn = origSetup
		runStartFn = origStart
		generateFn = origGenerate
	}()

	runSetupKeymapFn = func(args []string) error {
		t.Fatalf("runSetupKeymapFn should not be called for start")
		return nil
	}
	generateFn = func(configPath string, outputPath string) error {
		t.Fatalf("generateFn should not be called for start")
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
	runStartFn = func(device, config string, delay time.Duration, grab, verbose bool) error {
		called = true
		gotDevice = device
		gotConfig = config
		gotDelay = delay
		gotGrab = grab
		gotVerbose = verbose
		return nil
	}

	err := Run([]string{
		"start",
		"--device", "/dev/input/test-kbd",
		"--config", "custom.yaml",
		"--compose-delay", "1ms",
		"--grab=false",
		"--verbose=true",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called {
		t.Fatalf("runStartFn was not called")
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

func TestRunStartCommandRejectsNegativeComposeDelay(t *testing.T) {
	err := runStartCommand([]string{"--compose-delay", "-1ms"})
	if err == nil {
		t.Fatalf("expected error for negative compose delay")
	}
	if !strings.Contains(err.Error(), "compose-delay must be >= 0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCLIGeneratesXComposeAndExits(t *testing.T) {
	origSetup := runSetupKeymapFn
	origStart := runStartFn
	origGenerate := generateFn
	defer func() {
		runSetupKeymapFn = origSetup
		runStartFn = origStart
		generateFn = origGenerate
	}()

	runSetupKeymapFn = func(args []string) error {
		t.Fatalf("runSetupKeymapFn should not be called for generate-xcompose")
		return nil
	}
	runStartFn = func(string, string, time.Duration, bool, bool) error {
		t.Fatalf("runStartFn should not be called for generate-xcompose")
		return nil
	}

	var (
		called    bool
		gotConfig string
		gotOutput string
	)
	generateFn = func(configPath string, outputPath string) error {
		called = true
		gotConfig = configPath
		gotOutput = outputPath
		return nil
	}

	err := Run([]string{
		"generate-xcompose",
		"--config", "kmap.yaml",
		"--output", "/tmp/generated.XCompose",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called {
		t.Fatalf("generateFn was not called")
	}
	if gotConfig != "kmap.yaml" {
		t.Fatalf("config mismatch: %q", gotConfig)
	}
	if gotOutput != "/tmp/generated.XCompose" {
		t.Fatalf("output mismatch: %q", gotOutput)
	}
}

func TestRunGenerateXComposeSupportsPositionalOutput(t *testing.T) {
	origGenerate := generateFn
	defer func() {
		generateFn = origGenerate
	}()

	var (
		called    bool
		gotConfig string
		gotOutput string
	)
	generateFn = func(configPath string, outputPath string) error {
		called = true
		gotConfig = configPath
		gotOutput = outputPath
		return nil
	}

	err := runGenerateXComposeCommand([]string{"/tmp/generated.XCompose"})
	if err != nil {
		t.Fatalf("runGenerateXComposeCommand: %v", err)
	}
	if !called {
		t.Fatalf("generateFn was not called")
	}
	if gotConfig != "kmap.yaml" {
		t.Fatalf("default config mismatch: %q", gotConfig)
	}
	if gotOutput != "/tmp/generated.XCompose" {
		t.Fatalf("output mismatch: %q", gotOutput)
	}
}

func TestRunCLIRequiresCommand(t *testing.T) {
	err := Run(nil)
	if err == nil {
		t.Fatalf("expected error for missing command")
	}
	if !strings.Contains(err.Error(), "missing command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCLIUnknownCommand(t *testing.T) {
	err := Run([]string{"unknown"})
	if err == nil {
		t.Fatalf("expected error for unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unexpected error: %v", err)
	}
}
