package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon"
	"keyboard/pkg/daemon/input"
)

func TestRunStartForwardsOptions(t *testing.T) {
	orig := daemonStartFn
	defer func() {
		daemonStartFn = orig
	}()

	var got daemon.StartOptions
	daemonStartFn = func(opts daemon.StartOptions) error {
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

func TestRunSetupKeymapWritesLayoutJSON(t *testing.T) {
	devicePath := filepath.Join(t.TempDir(), "test-keyboard.events")
	outputPath := filepath.Join(t.TempDir(), "layout.json")

	events := []input.RawEvent{
		{Type: 0x01, Code: config.KeyA, Value: 1},
		{Type: 0x01, Code: config.KeyB, Value: 1},
	}
	buf := encodeSetupKeymapEvents(t, events)
	if err := os.WriteFile(devicePath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", devicePath, err)
	}

	err := runSetupKeymap([]string{
		"--device", devicePath,
		"--output", outputPath,
		"--keys", "a,b",
		"--grab=false",
	})
	if err != nil {
		t.Fatalf("runSetupKeymap: %v", err)
	}

	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", outputPath, err)
	}

	var got layoutFile
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Keyboard != filepath.Base(devicePath) {
		t.Fatalf("Keyboard mismatch: got=%q want=%q", got.Keyboard, filepath.Base(devicePath))
	}
	if got.Device != devicePath {
		t.Fatalf("Device mismatch: %q", got.Device)
	}
	if got.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion mismatch: %d", got.SchemaVersion)
	}
	if len(got.Keys) != 2 {
		t.Fatalf("keys len mismatch: %d", len(got.Keys))
	}
	if got.Keys[0] != (capturedKey{Logical: "a", Code: config.KeyA, LinuxName: "KEY_A"}) {
		t.Fatalf("key[0] mismatch: %+v", got.Keys[0])
	}
	if got.Keys[1] != (capturedKey{Logical: "b", Code: config.KeyB, LinuxName: "KEY_B"}) {
		t.Fatalf("key[1] mismatch: %+v", got.Keys[1])
	}
	if got.CapturedAt == "" {
		t.Fatalf("CapturedAt should not be empty")
	}
}

func TestRunSetupKeymapRejectsEmptyKeyList(t *testing.T) {
	err := runSetupKeymap([]string{"--keys", " , , "})
	if err == nil {
		t.Fatalf("expected empty key list error")
	}
	if !strings.Contains(err.Error(), "no keys to capture") {
		t.Fatalf("unexpected error: %v", err)
	}
}
