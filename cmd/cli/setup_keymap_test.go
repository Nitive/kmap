package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon"
)

type fakePromptReader struct {
	lines []string
	index int
}

func (f *fakePromptReader) ReadLine(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if f.index >= len(f.lines) {
		return "", errors.New("no more prompt input")
	}
	line := f.lines[f.index]
	f.index++
	return line, nil
}

type fakeSequenceReader struct {
	sequences [][]byte
	index     int
}

func (f *fakeSequenceReader) Next(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.index >= len(f.sequences) {
		return nil, errors.New("no more key sequences")
	}
	seq := append([]byte(nil), f.sequences[f.index]...)
	f.index++
	return seq, nil
}

func TestSetupKeymapParseLogicalOrderDefault(t *testing.T) {
	got := parseLogicalOrder("")
	if len(got) != len(defaultLogicalOrder) {
		t.Fatalf("default logical order length mismatch: got=%d want=%d", len(got), len(defaultLogicalOrder))
	}
	if got[0] != defaultLogicalOrder[0] || got[len(got)-1] != defaultLogicalOrder[len(defaultLogicalOrder)-1] {
		t.Fatalf("default logical order mismatch")
	}
}

func TestSetupKeymapParseLogicalOrderCustom(t *testing.T) {
	got := parseLogicalOrder("a, b,, c ,  ,d")
	want := []string{"a", "b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order[%d] mismatch: got=%q want=%q", i, got[i], want[i])
		}
	}
}

func TestPromptLabel(t *testing.T) {
	tests := []struct {
		logical string
		hint    string
		want    string
	}{
		{logical: "grv", hint: "`", want: "grave (`)"},
		{logical: "\\", hint: "\\", want: "backslash (\\)"},
		{logical: "ret", want: "enter"},
		{logical: "rctl", want: "right ctrl"},
		{logical: "q", hint: "q", want: "q"},
		{logical: "q", hint: "'", want: "q (')"},
		{logical: "custom-key", want: "custom-key"},
	}

	for _, tc := range tests {
		if got := promptLabel(tc.logical, tc.hint); got != tc.want {
			t.Fatalf("promptLabel(%q, %q) = %q, want %q", tc.logical, tc.hint, got, tc.want)
		}
	}
}

func TestParseSetupKeymapLayoutSpec(t *testing.T) {
	tests := []struct {
		raw     string
		want    config.ShortcutLayoutSpec
		wantErr string
	}{
		{raw: "ru", want: config.ShortcutLayoutSpec{Layout: "ru"}},
		{raw: "us(dvorak)", want: config.ShortcutLayoutSpec{Layout: "us", Variant: "dvorak"}},
		{raw: " us ( dvorak ) ", want: config.ShortcutLayoutSpec{Layout: "us", Variant: "dvorak"}},
		{raw: "us(", wantErr: "invalid layout"},
	}

	for _, tc := range tests {
		got, err := parseSetupKeymapLayoutSpec(tc.raw)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("parseSetupKeymapLayoutSpec(%q) error = %v, want %q", tc.raw, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseSetupKeymapLayoutSpec(%q): %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("parseSetupKeymapLayoutSpec(%q) = %#v, want %#v", tc.raw, got, tc.want)
		}
	}
}

func TestCaptureLayoutCapturesAndSkips(t *testing.T) {
	prompts := &fakePromptReader{lines: []string{"\n", "\n", "skip\n"}}
	sequences := &fakeSequenceReader{sequences: [][]byte{
		[]byte("a"),
		[]byte{'\r'},
	}}
	out := &bytes.Buffer{}

	got, err := captureLayout(context.Background(), prompts, sequences, out, []string{"a", "ret", "b"}, nil)
	if err != nil {
		t.Fatalf("captureLayout: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("unexpected key count: %d", len(got))
	}
	if got[0].Logical != "a" || got[0].Text != "a" || got[0].Escaped != `"a"` {
		t.Fatalf("unexpected first key: %+v", got[0])
	}
	if got[1].Logical != "ret" || got[1].Skipped || got[1].Escaped != `"\r"` {
		t.Fatalf("unexpected return key: %+v", got[1])
	}
	if got[2].Logical != "b" || !got[2].Skipped {
		t.Fatalf("unexpected skipped key: %+v", got[2])
	}
}

func TestCaptureLayoutEnterSkipsNonEnterKey(t *testing.T) {
	prompts := &fakePromptReader{lines: []string{"\n"}}
	sequences := &fakeSequenceReader{sequences: [][]byte{
		[]byte{'\r'},
	}}

	got, err := captureLayout(context.Background(), prompts, sequences, ioDiscard{}, []string{"a"}, nil)
	if err != nil {
		t.Fatalf("captureLayout: %v", err)
	}
	if len(got) != 1 || !got[0].Skipped {
		t.Fatalf("expected skipped key, got %+v", got)
	}
}

func TestCaptureLayoutInterruptsOnCtrlCSequence(t *testing.T) {
	prompts := &fakePromptReader{lines: []string{"\n"}}
	sequences := &fakeSequenceReader{sequences: [][]byte{
		{0x03},
	}}

	_, err := captureLayout(context.Background(), prompts, sequences, ioDiscard{}, []string{"a"}, nil)
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetupKeymapBuildLayoutPreservesMetadata(t *testing.T) {
	keys := []capturedKey{
		{Logical: "a", Bytes: []byte("a"), Text: "a", Escaped: `"a"`},
		{Logical: "ret", Bytes: []byte{'\r'}, Escaped: `"\r"`},
	}
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	layout := buildLayout("kbd", "/dev/input/test", "us(dvorak)", keys, now)

	if layout.SchemaVersion != 2 {
		t.Fatalf("schema version mismatch: %d", layout.SchemaVersion)
	}
	if layout.Layout != "us(dvorak)" {
		t.Fatalf("layout mismatch: %q", layout.Layout)
	}
	if layout.Keyboard != "kbd" || layout.Device != "/dev/input/test" {
		t.Fatalf("metadata mismatch: %+v", layout)
	}
	if layout.CapturedAt != "2026-03-15T12:00:00Z" {
		t.Fatalf("captured_at mismatch: %q", layout.CapturedAt)
	}
}

func TestRunSetupKeymapWritesJSONToStdout(t *testing.T) {
	origInput := setupKeymapInput
	origStdout := setupKeymapStdout
	origStderr := setupKeymapStderr
	origNow := setupKeymapNowFn
	origPromptFactory := setupKeymapPromptReaderFactory
	origSequenceFactory := setupKeymapSequenceReaderFactory
	origPromptLabels := setupKeymapPromptLabelsFn
	origSignal := setupKeymapSignalGrabFn
	defer func() {
		setupKeymapInput = origInput
		setupKeymapStdout = origStdout
		setupKeymapStderr = origStderr
		setupKeymapNowFn = origNow
		setupKeymapPromptReaderFactory = origPromptFactory
		setupKeymapSequenceReaderFactory = origSequenceFactory
		setupKeymapPromptLabelsFn = origPromptLabels
		setupKeymapSignalGrabFn = origSignal
	}()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	setupKeymapStdout = stdout
	setupKeymapStderr = stderr
	setupKeymapNowFn = func() time.Time {
		return time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	}
	setupKeymapPromptReaderFactory = func(file *os.File) promptReader {
		return &fakePromptReader{lines: []string{"\n", "\n", "skip\n"}}
	}
	setupKeymapSequenceReaderFactory = func(file *os.File) sequenceReader {
		return &fakeSequenceReader{sequences: [][]byte{
			[]byte("a"),
			[]byte{'\r'},
		}}
	}
	setupKeymapPromptLabelsFn = func(ctx context.Context, logicalOrder []string, layoutName string) (map[string]string, error) {
		return map[string]string{}, nil
	}

	var signals []bool
	setupKeymapSignalGrabFn = func(release bool) error {
		signals = append(signals, release)
		return nil
	}

	if err := runSetupKeymap([]string{
		"--layout", "ru",
		"--device", "/dev/input/test",
		"--name", "kbd",
		"--keys", "a,ret,b",
		"--output", "-",
	}); err != nil {
		t.Fatalf("runSetupKeymap: %v", err)
	}

	var got layoutFile
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, stdout.String())
	}
	if got.Layout != "ru" || got.Keyboard != "kbd" || got.Device != "/dev/input/test" {
		t.Fatalf("metadata mismatch: %+v", got)
	}
	if len(got.Keys) != 3 {
		t.Fatalf("unexpected key count: %d", len(got.Keys))
	}
	if !got.Keys[2].Skipped {
		t.Fatalf("expected skipped third key: %+v", got.Keys[2])
	}
	if len(signals) != 2 || !signals[0] || signals[1] {
		t.Fatalf("unexpected daemon signal sequence: %#v", signals)
	}
}

func TestRunSetupKeymapAllowsMissingDaemon(t *testing.T) {
	origSignal := setupKeymapSignalGrabFn
	defer func() {
		setupKeymapSignalGrabFn = origSignal
	}()

	setupKeymapSignalGrabFn = func(release bool) error {
		return daemon.ErrDaemonNotRunning
	}

	resume, err := maybeReleaseDaemon(true)
	if err != nil {
		t.Fatalf("maybeReleaseDaemon: %v", err)
	}
	resume()
}

func TestRunSetupKeymapRequiresLayout(t *testing.T) {
	err := runSetupKeymap([]string{})
	if err == nil {
		t.Fatalf("expected missing layout error")
	}
	if !strings.Contains(err.Error(), "layout is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
