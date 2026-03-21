package shortcut

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"keyboard/pkg/config"
	"keyboard/pkg/daemon/event"
)

type switchRunner struct {
	current  int
	setCalls []int
}

func (r *switchRunner) run(ctx context.Context, name string, args ...string) (string, error) {
	_ = ctx
	if name != "qdbus6" {
		return "", fmt.Errorf("unexpected command %q", name)
	}
	if len(args) < 3 {
		return "", fmt.Errorf("unexpected args: %#v", args)
	}

	switch args[2] {
	case "org.kde.KeyboardLayouts.getLayout":
		return fmt.Sprintf("%d\n", r.current), nil

	case "org.kde.KeyboardLayouts.setLayout":
		if len(args) != 4 {
			return "", fmt.Errorf("unexpected setLayout args: %#v", args)
		}
		index, err := strconv.Atoi(args[3])
		if err != nil {
			return "", err
		}
		r.current = index
		r.setCalls = append(r.setCalls, index)
		return "true\n", nil

	default:
		return "", fmt.Errorf("unexpected method %q", args[2])
	}
}

func writeKDEConfig(t *testing.T) {
	t.Helper()
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	if err := os.WriteFile(filepath.Join(configDir, "kxkbrc"), []byte(`[Layout]
LayoutList=us,ru
VariantList=dvorak,
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestNewSwitchManagerWithLoader(t *testing.T) {
	writeKDEConfig(t)

	runner := &switchRunner{current: 1}
	manager, info, err := NewSwitchManagerWithLoader(
		context.Background(),
		Loader{runner: runner},
		config.ShortcutLayoutSpec{Layout: "us", Variant: "dvorak"},
		false,
	)
	if err != nil {
		t.Fatalf("NewSwitchManagerWithLoader: %v", err)
	}
	if manager.targetIndex != 0 || manager.target.Layout != "us" || manager.target.Variant != "dvorak" {
		t.Fatalf("unexpected target: index=%d info=%#v", manager.targetIndex, manager.target)
	}
	if info.Current.Layout != "ru" || info.Current.Variant != "" {
		t.Fatalf("unexpected current layout: %#v", info.Current)
	}
	if info.Target.Layout != "us" || info.Target.Variant != "dvorak" || info.TargetIndex != 0 {
		t.Fatalf("unexpected validation info: %#v", info)
	}
}

func TestSwitchManagerSwitchesAndRestoresAroundShortcut(t *testing.T) {
	writeKDEConfig(t)

	runner := &switchRunner{current: 1}
	manager, _, err := NewSwitchManagerWithLoader(
		context.Background(),
		Loader{runner: runner},
		config.ShortcutLayoutSpec{Layout: "us", Variant: "dvorak"},
		false,
	)
	if err != nil {
		t.Fatalf("NewSwitchManagerWithLoader: %v", err)
	}

	var emitted []event.KeyEvent
	emit := func(ev event.KeyEvent) error {
		emitted = append(emitted, ev)
		return nil
	}

	seq := []event.KeyEvent{
		{Code: config.KeyLeftCtrl, Value: 1},
		{Code: config.KeyLeftShift, Value: 1},
		{Code: config.KeyDot, Value: 1},
		{Code: config.KeyLeftCtrl, Value: 0},
		{Code: config.KeyDot, Value: 0},
		{Code: config.KeyLeftShift, Value: 0},
	}

	for _, ev := range seq {
		if err := manager.Process(context.Background(), ev, emit); err != nil {
			t.Fatalf("Process(%+v): %v", ev, err)
		}
	}

	if len(runner.setCalls) != 2 || runner.setCalls[0] != 0 || runner.setCalls[1] != 1 {
		t.Fatalf("unexpected setLayout calls: %#v", runner.setCalls)
	}
	if len(emitted) != len(seq) {
		t.Fatalf("unexpected emitted count: %d", len(emitted))
	}
	for i := range seq {
		if emitted[i] != seq[i] {
			t.Fatalf("emitted[%d] = %#v, want %#v", i, emitted[i], seq[i])
		}
	}
}

func TestSwitchManagerDoesNotSwitchForShiftOnly(t *testing.T) {
	writeKDEConfig(t)

	runner := &switchRunner{current: 1}
	manager, _, err := NewSwitchManagerWithLoader(
		context.Background(),
		Loader{runner: runner},
		config.ShortcutLayoutSpec{Layout: "us", Variant: "dvorak"},
		false,
	)
	if err != nil {
		t.Fatalf("NewSwitchManagerWithLoader: %v", err)
	}

	seq := []event.KeyEvent{
		{Code: config.KeyLeftShift, Value: 1},
		{Code: config.KeyDot, Value: 1},
		{Code: config.KeyDot, Value: 0},
		{Code: config.KeyLeftShift, Value: 0},
	}

	for _, ev := range seq {
		if err := manager.Process(context.Background(), ev, func(event.KeyEvent) error { return nil }); err != nil {
			t.Fatalf("Process(%+v): %v", ev, err)
		}
	}

	if len(runner.setCalls) != 0 {
		t.Fatalf("unexpected setLayout calls: %#v", runner.setCalls)
	}
}

func TestSwitchManagerDoesNotRestoreUntilPressedKeyIsReleased(t *testing.T) {
	writeKDEConfig(t)

	runner := &switchRunner{current: 1}
	manager, _, err := NewSwitchManagerWithLoader(
		context.Background(),
		Loader{runner: runner},
		config.ShortcutLayoutSpec{Layout: "us", Variant: "dvorak"},
		false,
	)
	if err != nil {
		t.Fatalf("NewSwitchManagerWithLoader: %v", err)
	}

	if err := manager.Process(context.Background(), event.KeyEvent{Code: config.KeyLeftCtrl, Value: 1}, func(event.KeyEvent) error { return nil }); err != nil {
		t.Fatalf("ctrl down: %v", err)
	}
	if err := manager.Process(context.Background(), event.KeyEvent{Code: config.KeyDot, Value: 1}, func(event.KeyEvent) error { return nil }); err != nil {
		t.Fatalf("dot down: %v", err)
	}
	if err := manager.Process(context.Background(), event.KeyEvent{Code: config.KeyLeftCtrl, Value: 0}, func(event.KeyEvent) error { return nil }); err != nil {
		t.Fatalf("ctrl up: %v", err)
	}

	if len(runner.setCalls) != 1 || runner.setCalls[0] != 0 {
		t.Fatalf("unexpected setLayout calls after ctrl release: %#v", runner.setCalls)
	}

	if err := manager.Process(context.Background(), event.KeyEvent{Code: config.KeyDot, Value: 0}, func(event.KeyEvent) error { return nil }); err != nil {
		t.Fatalf("dot up: %v", err)
	}

	if len(runner.setCalls) != 2 || runner.setCalls[1] != 1 {
		t.Fatalf("unexpected setLayout calls after dot release: %#v", runner.setCalls)
	}
}

func TestSwitchManagerCloseRestoresLayout(t *testing.T) {
	writeKDEConfig(t)

	runner := &switchRunner{current: 1}
	manager, _, err := NewSwitchManagerWithLoader(
		context.Background(),
		Loader{runner: runner},
		config.ShortcutLayoutSpec{Layout: "us", Variant: "dvorak"},
		false,
	)
	if err != nil {
		t.Fatalf("NewSwitchManagerWithLoader: %v", err)
	}

	if err := manager.Process(context.Background(), event.KeyEvent{Code: config.KeyLeftCtrl, Value: 1}, func(event.KeyEvent) error { return nil }); err != nil {
		t.Fatalf("ctrl down: %v", err)
	}

	if err := manager.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(runner.setCalls) != 2 || runner.setCalls[0] != 0 || runner.setCalls[1] != 1 {
		t.Fatalf("unexpected setLayout calls: %#v", runner.setCalls)
	}
}
