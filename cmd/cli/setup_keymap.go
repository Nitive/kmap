package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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

	in, err := os.Open(*devicePath)
	if err != nil {
		return fmt.Errorf("open input device %s: %w", *devicePath, err)
	}
	defer in.Close()

	grabFn := func(enable bool) error {
		if !*grab {
			return nil
		}
		return input.Grab(int(in.Fd()), enable)
	}

	captured, err := captureLayout(in, os.Stdout, order, grabFn)
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

func captureLayout(in io.Reader, out io.Writer, logicalOrder []string, grabFn func(bool) error) (map[string]uint16, error) {
	if err := grabFn(true); err != nil {
		return nil, fmt.Errorf("grab input device: %w", err)
	}
	defer func() {
		_ = grabFn(false)
	}()

	var interrupted atomic.Bool
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-sigCh:
			interrupted.Store(true)
			if closer, ok := in.(io.Closer); ok {
				_ = closer.Close()
			}
		case <-done:
		}
	}()

	captured := make(map[string]uint16, len(logicalOrder))
	usedCodes := make(map[uint16]string, len(logicalOrder))

	fmt.Fprintf(out, "Press each requested key once. Ctrl+C aborts.\n\n")

	for i, logical := range logicalOrder {
		fmt.Fprintf(out, "[%2d/%2d] Press %-5s: ", i+1, len(logicalOrder), logical)
		code, err := input.WaitForNextKeyPress(in)
		if err != nil {
			if interrupted.Load() {
				return nil, errors.New("capture interrupted")
			}
			return nil, fmt.Errorf("read input event: %w", err)
		}

		if prev, exists := usedCodes[code]; exists {
			fmt.Fprintf(out, "%d (%s) [duplicate of %s]\n", code, config.KeyName(code), prev)
		} else {
			fmt.Fprintf(out, "%d (%s)\n", code, config.KeyName(code))
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
