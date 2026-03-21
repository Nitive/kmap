package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon/input"
)

const defaultKeymapOutputPath = "keyboard-layout.json"

type capturedKey struct {
	Logical   string `json:"logical"`
	Code      uint16 `json:"code"`
	LinuxName string `json:"linux_name,omitempty"`
}

type layoutFile struct {
	SchemaVersion int           `json:"schema_version"`
	Keyboard      string        `json:"keyboard"`
	Device        string        `json:"device"`
	CapturedAt    string        `json:"captured_at"`
	Keys          []capturedKey `json:"keys"`
}

var defaultLogicalOrder = []string{
	"grv", "1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "-", "=", "bspc",
	"tab", "q", "w", "e", "r", "t", "y", "u", "i", "o", "p", "[", "]", "\\",
	"caps", "a", "s", "d", "f", "g", "h", "j", "k", "l", ";", "'", "ret",
	"lsft", "z", "x", "c", "v", "b", "n", "m", ",", ".", "/", "rsft",
	"lctl", "lmet", "lalt", "spc", "ralt", "rctl",
}

func runSetupKeymap(args []string) error {
	fs := flag.NewFlagSet("setup-keymap", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	devicePath := fs.String("device", config.DefaultDevicePath, "input keyboard device path")
	outputPath := fs.String("output", defaultKeymapOutputPath, "output JSON path")
	keyboardName := fs.String("name", "", "keyboard name for metadata (default: basename of --device)")
	keysCSV := fs.String("keys", "", "comma-separated logical keys to capture in order (default: built-in 59-key order)")
	grab := fs.Bool("grab", true, "grab input device while capturing to avoid duplicate input")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}

	order := parseLogicalOrder(*keysCSV)
	if len(order) == 0 {
		return errors.New("no keys to capture (empty --keys)")
	}

	name := strings.TrimSpace(*keyboardName)
	if name == "" {
		name = filepath.Base(*devicePath)
	}

	captured, err := captureLayout(*devicePath, order, *grab)
	if err != nil {
		return err
	}

	layout := buildLayout(name, *devicePath, order, captured, time.Now())
	if err := writeLayoutJSON(*outputPath, layout); err != nil {
		return err
	}

	fmt.Printf("\nSaved %d keys to %s\n", len(layout.Keys), *outputPath)
	return nil
}

func parseLogicalOrder(keysCSV string) []string {
	if strings.TrimSpace(keysCSV) == "" {
		out := make([]string, len(defaultLogicalOrder))
		copy(out, defaultLogicalOrder)
		return out
	}

	parts := strings.Split(keysCSV, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		v := strings.TrimSpace(part)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func buildLayout(name string, device string, order []string, captured map[string]uint16, now time.Time) layoutFile {
	keys := make([]capturedKey, 0, len(order))
	for _, logical := range order {
		code := captured[logical]
		keys = append(keys, capturedKey{
			Logical:   logical,
			Code:      code,
			LinuxName: config.KeyName(code),
		})
	}
	return layoutFile{
		SchemaVersion: 1,
		Keyboard:      name,
		Device:        device,
		CapturedAt:    now.UTC().Format(time.RFC3339),
		Keys:          keys,
	}
}

func captureLayout(devicePath string, logicalOrder []string, grab bool) (map[string]uint16, error) {
	in, err := os.Open(devicePath)
	if err != nil {
		return nil, fmt.Errorf("open input device %s: %w", devicePath, err)
	}
	defer in.Close()

	fd := int(in.Fd())
	if grab {
		if err := input.Grab(fd, true); err != nil {
			return nil, fmt.Errorf("grab input device %s: %w", devicePath, err)
		}
		defer func() {
			_ = input.Grab(fd, false)
		}()
	}

	var interrupted atomic.Bool
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh
		interrupted.Store(true)
		_ = in.Close()
	}()

	captured := make(map[string]uint16, len(logicalOrder))
	usedCodes := make(map[uint16]string, len(logicalOrder))

	fmt.Printf("Capturing from %s\n", devicePath)
	if grab {
		fmt.Println("Input device is grabbed during capture.")
	}
	fmt.Println("Press each requested key once. Ctrl+C aborts.")
	fmt.Println()

	for i, logical := range logicalOrder {
		fmt.Printf("[%2d/%2d] Press %-5s: ", i+1, len(logicalOrder), logical)
		code, err := input.WaitForNextKeyPress(in)
		if err != nil {
			if interrupted.Load() || errors.Is(err, os.ErrClosed) {
				return nil, errors.New("capture interrupted")
			}
			return nil, fmt.Errorf("read input event: %w", err)
		}

		if prev, exists := usedCodes[code]; exists {
			fmt.Printf("%d (%s) [duplicate of %s]\n", code, config.KeyName(code), prev)
		} else {
			fmt.Printf("%d (%s)\n", code, config.KeyName(code))
		}

		captured[logical] = code
		usedCodes[code] = logical
	}

	return captured, nil
}

func writeLayoutJSON(path string, layout layoutFile) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(layout); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}
