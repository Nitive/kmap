package main

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

const (
	testKeyA         = 30
	testKeyBackspace = 14
	testKeyLeftShift = 42
)

func encodeEvents(t *testing.T, events []inputEvent) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	for _, ev := range events {
		if err := binary.Write(buf, binary.LittleEndian, ev); err != nil {
			t.Fatalf("binary.Write: %v", err)
		}
	}
	return buf
}

func TestParseLogicalOrderDefault(t *testing.T) {
	got := parseLogicalOrder("")
	if len(got) != len(defaultLogicalOrder) {
		t.Fatalf("default logical order length mismatch: got=%d want=%d", len(got), len(defaultLogicalOrder))
	}
	if got[0] != defaultLogicalOrder[0] || got[len(got)-1] != defaultLogicalOrder[len(defaultLogicalOrder)-1] {
		t.Fatalf("default logical order mismatch")
	}
}

func TestParseLogicalOrderCustom(t *testing.T) {
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

func TestWaitForNextKeyPressSkipsNonPressEvents(t *testing.T) {
	events := []inputEvent{
		{Type: 0, Code: 0, Value: 0},                    // non-key
		{Type: evKey, Code: testKeyA, Value: 0},         // key release
		{Type: evKey, Code: testKeyA, Value: 2},         // key repeat
		{Type: evKey, Code: testKeyLeftShift, Value: 1}, // first press should win
		{Type: evKey, Code: testKeyBackspace, Value: 1}, // would be next
	}
	buf := encodeEvents(t, events)

	code, err := waitForNextKeyPress(buf)
	if err != nil {
		t.Fatalf("waitForNextKeyPress: %v", err)
	}
	if code != testKeyLeftShift {
		t.Fatalf("unexpected code: got=%d want=%d", code, testKeyLeftShift)
	}
}

func TestBuildLayoutPreservesOrderAndNames(t *testing.T) {
	order := []string{"a", "bspc", "unknown"}
	captured := map[string]uint16{
		"a":       testKeyA,
		"bspc":    testKeyBackspace,
		"unknown": 255,
	}
	now := time.Date(2026, 3, 13, 12, 0, 0, 0, time.UTC)
	layout := buildLayout("kbd", "/dev/input/test", order, captured, now)

	if layout.SchemaVersion != 1 {
		t.Fatalf("schema version mismatch: %d", layout.SchemaVersion)
	}
	if layout.Keyboard != "kbd" {
		t.Fatalf("keyboard mismatch: %q", layout.Keyboard)
	}
	if layout.Device != "/dev/input/test" {
		t.Fatalf("device mismatch: %q", layout.Device)
	}
	if layout.CapturedAt != "2026-03-13T12:00:00Z" {
		t.Fatalf("captured_at mismatch: %q", layout.CapturedAt)
	}
	if len(layout.Keys) != len(order) {
		t.Fatalf("keys len mismatch: got=%d want=%d", len(layout.Keys), len(order))
	}

	if layout.Keys[0].Logical != "a" || layout.Keys[0].Code != testKeyA || layout.Keys[0].LinuxName != "KEY_A" {
		t.Fatalf("entry0 mismatch: %+v", layout.Keys[0])
	}
	if layout.Keys[1].Logical != "bspc" || layout.Keys[1].Code != testKeyBackspace || layout.Keys[1].LinuxName != "KEY_BACKSPACE" {
		t.Fatalf("entry1 mismatch: %+v", layout.Keys[1])
	}
	if layout.Keys[2].Logical != "unknown" || layout.Keys[2].Code != 255 || layout.Keys[2].LinuxName != "KEY_255" {
		t.Fatalf("entry2 mismatch: %+v", layout.Keys[2])
	}
}
