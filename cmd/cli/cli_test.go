package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon"
	"keyboard/pkg/daemon/shortcut"
)

func TestRunStartForwardsOptions(t *testing.T) {
	origStart := daemonStartFn
	origGenerate := generateFn
	origXComposePath := defaultXComposePathFn
	defer func() {
		daemonStartFn = origStart
		generateFn = origGenerate
		defaultXComposePathFn = origXComposePath
	}()

	var generated bool
	defaultXComposePathFn = func() (string, error) {
		return "/tmp/test.XCompose", nil
	}
	generateFn = func(configPath string, outputPath string) error {
		generated = true
		if configPath != "custom.yaml" {
			t.Fatalf("generateFn config mismatch: %q", configPath)
		}
		if outputPath != "/tmp/test.XCompose" {
			t.Fatalf("generateFn output mismatch: %q", outputPath)
		}
		return nil
	}

	var got daemon.StartOptions
	daemonStartFn = func(opts daemon.StartOptions) error {
		if !generated {
			t.Fatalf("expected XCompose to be generated before daemon start")
		}
		got = opts
		return nil
	}

	err := runStart("/dev/input/test-kbd", "custom.yaml", 2*time.Millisecond, false, true)
	if err != nil {
		t.Fatalf("runStart: %v", err)
	}

	if got.DeviceOverride != "/dev/input/test-kbd" {
		t.Fatalf("DeviceOverride mismatch: %q", got.DeviceOverride)
	}
	if got.ConfigPath != "custom.yaml" {
		t.Fatalf("ConfigPath mismatch: %q", got.ConfigPath)
	}
	if got.ComposeDelay != 2*time.Millisecond {
		t.Fatalf("ComposeDelay mismatch: %s", got.ComposeDelay)
	}
	if got.Grab {
		t.Fatalf("Grab should be false")
	}
	if !got.Verbose {
		t.Fatalf("Verbose should be true")
	}
}

func TestRunStartReturnsGenerateXComposeError(t *testing.T) {
	origStart := daemonStartFn
	origGenerate := generateFn
	origXComposePath := defaultXComposePathFn
	defer func() {
		daemonStartFn = origStart
		generateFn = origGenerate
		defaultXComposePathFn = origXComposePath
	}()

	defaultXComposePathFn = func() (string, error) {
		return "/tmp/test.XCompose", nil
	}
	generateFn = func(configPath string, outputPath string) error {
		return errors.New("boom")
	}
	daemonStartFn = func(opts daemon.StartOptions) error {
		t.Fatalf("daemonStartFn should not be called when XCompose generation fails")
		return nil
	}

	err := runStart("", config.DefaultConfigPath, 0, true, false)
	if err == nil || !strings.Contains(err.Error(), "generate XCompose /tmp/test.XCompose: boom") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenerateXComposeCommandRequiresOutputPath(t *testing.T) {
	err := runGenerateXComposeCommand(nil)
	if err == nil {
		t.Fatalf("expected missing output error")
	}
	if !strings.Contains(err.Error(), "output path is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenerateXComposeCommandRejectsExtraPositionalArgs(t *testing.T) {
	err := runGenerateXComposeCommand([]string{"one", "two"})
	if err == nil {
		t.Fatalf("expected positional argument error")
	}
	if !strings.Contains(err.Error(), "unexpected positional arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCLIDispatchesStartSubcommand(t *testing.T) {
	origStart := runStartFn
	defer func() {
		runStartFn = origStart
	}()

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

func TestRunStartCommandUsesDefaultConfigPath(t *testing.T) {
	origStart := runStartFn
	defer func() {
		runStartFn = origStart
	}()

	var (
		gotDevice  string
		gotConfig  string
		gotDelay   time.Duration
		gotGrab    bool
		gotVerbose bool
	)
	runStartFn = func(device, config string, delay time.Duration, grab, verbose bool) error {
		gotDevice = device
		gotConfig = config
		gotDelay = delay
		gotGrab = grab
		gotVerbose = verbose
		return nil
	}

	if err := runStartCommand(nil); err != nil {
		t.Fatalf("runStartCommand: %v", err)
	}
	if gotDevice != "" {
		t.Fatalf("device mismatch: %q", gotDevice)
	}
	if gotConfig != config.DefaultConfigPath {
		t.Fatalf("default config mismatch: %q", gotConfig)
	}
	if gotDelay != 5*time.Millisecond {
		t.Fatalf("delay mismatch: %s", gotDelay)
	}
	if !gotGrab {
		t.Fatalf("grab should default to true")
	}
	if gotVerbose {
		t.Fatalf("verbose should default to false")
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
	origStart := runStartFn
	origGenerate := generateFn
	defer func() {
		runStartFn = origStart
		generateFn = origGenerate
	}()

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
		"--config", "custom.yaml",
		"--output", "/tmp/generated.XCompose",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !called {
		t.Fatalf("generateFn was not called")
	}
	if gotConfig != "custom.yaml" {
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
	if gotConfig != config.DefaultConfigPath {
		t.Fatalf("default config mismatch: %q", gotConfig)
	}
	if gotOutput != "/tmp/generated.XCompose" {
		t.Fatalf("output mismatch: %q", gotOutput)
	}
}

func TestRunRemovedSetupKeymapCommandFails(t *testing.T) {
	err := Run([]string{"setup-keymap"})
	if err == nil {
		t.Fatalf("expected unknown command error")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunValidateConfigWithoutShortcutLayout(t *testing.T) {
	origLoad := loadRuntimeFn
	origSwitch := switchValidateFn
	defer func() {
		loadRuntimeFn = origLoad
		switchValidateFn = origSwitch
	}()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("mappings: {}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadRuntimeFn = func(path string) (config.Runtime, error) {
		if path != configPath {
			t.Fatalf("unexpected config path: %q", path)
		}
		return config.DefaultRuntime(), nil
	}
	switchValidateFn = func(ctx context.Context, cfg config.Runtime) (shortcut.ValidationInfo, error) {
		t.Fatalf("switchValidateFn should not be called without layout switching")
		return shortcut.ValidationInfo{}, nil
	}

	var out bytes.Buffer
	if err := runValidateConfig(configPath, &out); err != nil {
		t.Fatalf("runValidateConfig: %v", err)
	}
	if got := out.String(); got != "config OK: "+configPath+"\n" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestRunValidateConfigWithShortcutLayout(t *testing.T) {
	origLoad := loadRuntimeFn
	origSwitch := switchValidateFn
	defer func() {
		loadRuntimeFn = origLoad
		switchValidateFn = origSwitch
	}()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("shortcut_layout:\n  layout: us\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadRuntimeFn = func(path string) (config.Runtime, error) {
		cfg := config.DefaultRuntime()
		cfg.ShortcutLayout = &config.ShortcutLayoutSpec{Layout: "us", Variant: "dvorak"}
		return cfg, nil
	}
	switchValidateFn = func(ctx context.Context, cfg config.Runtime) (shortcut.ValidationInfo, error) {
		if cfg.ShortcutLayout == nil || cfg.ShortcutLayout.Layout != "us" || cfg.ShortcutLayout.Variant != "dvorak" {
			t.Fatalf("unexpected config: %#v", cfg.ShortcutLayout)
		}
		return shortcut.ValidationInfo{
			Current:             shortcut.LayoutInfo{Layout: "us"},
			ShortcutTarget:      shortcut.LayoutInfo{Layout: "us", Variant: "dvorak"},
			ShortcutTargetIndex: 0,
		}, nil
	}

	var out bytes.Buffer
	if err := runValidateConfig(configPath, &out); err != nil {
		t.Fatalf("runValidateConfig: %v", err)
	}
	got := out.String()
	want := "config OK: " + configPath + " (shortcut current=us target=us(dvorak) target_index=0)\n"
	if got != want {
		t.Fatalf("unexpected output: got=%q want=%q", got, want)
	}
}

func TestRunValidateConfigWithTapLayoutSwitches(t *testing.T) {
	origLoad := loadRuntimeFn
	origSwitch := switchValidateFn
	defer func() {
		loadRuntimeFn = origLoad
		switchValidateFn = origSwitch
	}()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("tap_layout_switches:\n  Caps:\n    toggle_recent: true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadRuntimeFn = func(path string) (config.Runtime, error) {
		cfg := config.DefaultRuntime()
		cfg.TapLayoutSwitches[config.KeyCapsLock] = config.LayoutSwitchTapAction{Kind: config.LayoutSwitchTapToggleRecent}
		return cfg, nil
	}
	switchValidateFn = func(ctx context.Context, cfg config.Runtime) (shortcut.ValidationInfo, error) {
		if len(cfg.TapLayoutSwitches) != 1 {
			t.Fatalf("unexpected tap layout switches: %#v", cfg.TapLayoutSwitches)
		}
		return shortcut.ValidationInfo{
			Current: shortcut.LayoutInfo{Layout: "ru"},
			TapSwitches: []shortcut.TapSwitchInfo{
				{
					SourceCode: config.KeyCapsLock,
					Action:     config.LayoutSwitchTapAction{Kind: config.LayoutSwitchTapToggleRecent},
				},
			},
		}, nil
	}

	var out bytes.Buffer
	if err := runValidateConfig(configPath, &out); err != nil {
		t.Fatalf("runValidateConfig: %v", err)
	}
	want := "config OK: " + configPath + " (current=ru tap_switches=1)\n"
	if got := out.String(); got != want {
		t.Fatalf("unexpected output: got=%q want=%q", got, want)
	}
}

func TestRunValidateConfigWrapsSwitchErrors(t *testing.T) {
	origLoad := loadRuntimeFn
	origSwitch := switchValidateFn
	defer func() {
		loadRuntimeFn = origLoad
		switchValidateFn = origSwitch
	}()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("shortcut_layout:\n  layout: us\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadRuntimeFn = func(path string) (config.Runtime, error) {
		cfg := config.DefaultRuntime()
		cfg.ShortcutLayout = &config.ShortcutLayoutSpec{Layout: "us"}
		return cfg, nil
	}
	switchValidateFn = func(ctx context.Context, cfg config.Runtime) (shortcut.ValidationInfo, error) {
		return shortcut.ValidationInfo{}, errors.New("qdbus failed")
	}

	err := runValidateConfig(configPath, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "layout switch validation failed: qdbus failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunValidateConfigRejectsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")
	err := runValidateConfig(path, &bytes.Buffer{})
	if err == nil {
		t.Fatalf("expected missing file error")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunValidateConfigCommandRejectsExtraArgs(t *testing.T) {
	err := runValidateConfigCommand([]string{"one"})
	if err == nil {
		t.Fatalf("expected positional argument error")
	}
	if !strings.Contains(err.Error(), "unexpected positional arguments") {
		t.Fatalf("unexpected error: %v", err)
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
