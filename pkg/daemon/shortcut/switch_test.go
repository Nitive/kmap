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

func makeSwitchConfig() config.Runtime {
	cfg := config.DefaultRuntime()
	cfg.ShortcutLayout = &config.ShortcutLayoutSpec{Layout: "us", Variant: "dvorak"}
	cfg.TapLayoutSwitches[config.KeyLeftAlt] = config.LayoutSwitchTapAction{
		Kind:    config.LayoutSwitchTapToLayout,
		Layout:  "us",
		Variant: "dvorak",
	}
	cfg.TapLayoutSwitches[config.KeyRightAlt] = config.LayoutSwitchTapAction{
		Kind:   config.LayoutSwitchTapToLayout,
		Layout: "ru",
	}
	cfg.TapLayoutSwitches[config.KeyCapsLock] = config.LayoutSwitchTapAction{
		Kind: config.LayoutSwitchTapToggleRecent,
	}
	return cfg
}

func TestNewSwitchManagerWithLoader(t *testing.T) {
	writeKDEConfig(t)

	runner := &switchRunner{current: 1}
	manager, info, err := NewSwitchManagerWithLoader(
		context.Background(),
		Loader{runner: runner},
		makeSwitchConfig(),
		false,
	)
	if err != nil {
		t.Fatalf("NewSwitchManagerWithLoader: %v", err)
	}
	if manager.shortcutIndex != 0 || manager.shortcutTarget.Layout != "us" || manager.shortcutTarget.Variant != "dvorak" {
		t.Fatalf("unexpected shortcut target: index=%d info=%#v", manager.shortcutIndex, manager.shortcutTarget)
	}
	if info.Current.Layout != "ru" || info.Current.Variant != "" {
		t.Fatalf("unexpected current layout: %#v", info.Current)
	}
	if info.ShortcutTarget.Layout != "us" || info.ShortcutTarget.Variant != "dvorak" || info.ShortcutTargetIndex != 0 {
		t.Fatalf("unexpected validation info: %#v", info)
	}
	if len(info.TapSwitches) != 3 {
		t.Fatalf("unexpected tap switch count: %#v", info.TapSwitches)
	}
}

func TestSwitchManagerSwitchesAndRestoresAroundShortcut(t *testing.T) {
	writeKDEConfig(t)

	runner := &switchRunner{current: 1}
	manager, _, err := NewSwitchManagerWithLoader(
		context.Background(),
		Loader{runner: runner},
		makeSwitchConfig(),
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
		{Kind: event.KindKey, Code: config.KeyLeftCtrl, Value: 1},
		{Kind: event.KindKey, Code: config.KeyLeftShift, Value: 1},
		{Kind: event.KindKey, Code: config.KeyDot, Value: 1},
		{Kind: event.KindKey, Code: config.KeyLeftCtrl, Value: 0},
		{Kind: event.KindKey, Code: config.KeyDot, Value: 0},
		{Kind: event.KindKey, Code: config.KeyLeftShift, Value: 0},
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
		makeSwitchConfig(),
		false,
	)
	if err != nil {
		t.Fatalf("NewSwitchManagerWithLoader: %v", err)
	}

	seq := []event.KeyEvent{
		{Kind: event.KindKey, Code: config.KeyLeftShift, Value: 1},
		{Kind: event.KindKey, Code: config.KeyDot, Value: 1},
		{Kind: event.KindKey, Code: config.KeyDot, Value: 0},
		{Kind: event.KindKey, Code: config.KeyLeftShift, Value: 0},
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
		makeSwitchConfig(),
		false,
	)
	if err != nil {
		t.Fatalf("NewSwitchManagerWithLoader: %v", err)
	}

	if err := manager.Process(context.Background(), event.KeyEvent{Kind: event.KindKey, Code: config.KeyLeftCtrl, Value: 1}, func(event.KeyEvent) error { return nil }); err != nil {
		t.Fatalf("ctrl down: %v", err)
	}
	if err := manager.Process(context.Background(), event.KeyEvent{Kind: event.KindKey, Code: config.KeyDot, Value: 1}, func(event.KeyEvent) error { return nil }); err != nil {
		t.Fatalf("dot down: %v", err)
	}
	if err := manager.Process(context.Background(), event.KeyEvent{Kind: event.KindKey, Code: config.KeyLeftCtrl, Value: 0}, func(event.KeyEvent) error { return nil }); err != nil {
		t.Fatalf("ctrl up: %v", err)
	}

	if len(runner.setCalls) != 1 || runner.setCalls[0] != 0 {
		t.Fatalf("unexpected setLayout calls after ctrl release: %#v", runner.setCalls)
	}

	if err := manager.Process(context.Background(), event.KeyEvent{Kind: event.KindKey, Code: config.KeyDot, Value: 0}, func(event.KeyEvent) error { return nil }); err != nil {
		t.Fatalf("dot up: %v", err)
	}

	if len(runner.setCalls) != 2 || runner.setCalls[1] != 1 {
		t.Fatalf("unexpected setLayout calls after dot release: %#v", runner.setCalls)
	}
}

func TestSwitchManagerTapDirectSwitchRecordsRecent(t *testing.T) {
	writeKDEConfig(t)

	runner := &switchRunner{current: 1}
	manager, _, err := NewSwitchManagerWithLoader(
		context.Background(),
		Loader{runner: runner},
		makeSwitchConfig(),
		false,
	)
	if err != nil {
		t.Fatalf("NewSwitchManagerWithLoader: %v", err)
	}

	err = manager.Process(
		context.Background(),
		event.KeyEvent{Kind: event.KindLayoutSwitch, LayoutSwitch: event.LayoutSwitchRequest{SourceCode: config.KeyLeftAlt}},
		func(event.KeyEvent) error { t.Fatalf("tap layout switch should not emit key events"); return nil },
	)
	if err != nil {
		t.Fatalf("Process(tap direct): %v", err)
	}

	if len(runner.setCalls) != 1 || runner.setCalls[0] != 0 {
		t.Fatalf("unexpected setLayout calls: %#v", runner.setCalls)
	}
	if runner.current != 0 {
		t.Fatalf("unexpected current layout index: %d", runner.current)
	}
	if manager.recentIndex != 1 {
		t.Fatalf("unexpected recent index: %d", manager.recentIndex)
	}
}

func TestSwitchManagerTapToggleRecentSwapsLayouts(t *testing.T) {
	writeKDEConfig(t)

	runner := &switchRunner{current: 1}
	manager, _, err := NewSwitchManagerWithLoader(
		context.Background(),
		Loader{runner: runner},
		makeSwitchConfig(),
		false,
	)
	if err != nil {
		t.Fatalf("NewSwitchManagerWithLoader: %v", err)
	}

	if err := manager.Process(context.Background(), event.KeyEvent{
		Kind:         event.KindLayoutSwitch,
		LayoutSwitch: event.LayoutSwitchRequest{SourceCode: config.KeyLeftAlt},
	}, func(event.KeyEvent) error { return nil }); err != nil {
		t.Fatalf("direct switch: %v", err)
	}
	if err := manager.Process(context.Background(), event.KeyEvent{
		Kind:         event.KindLayoutSwitch,
		LayoutSwitch: event.LayoutSwitchRequest{SourceCode: config.KeyCapsLock},
	}, func(event.KeyEvent) error { return nil }); err != nil {
		t.Fatalf("toggle recent: %v", err)
	}

	if len(runner.setCalls) != 2 || runner.setCalls[0] != 0 || runner.setCalls[1] != 1 {
		t.Fatalf("unexpected setLayout calls: %#v", runner.setCalls)
	}
	if runner.current != 1 {
		t.Fatalf("unexpected current layout index: %d", runner.current)
	}
	if manager.recentIndex != 0 {
		t.Fatalf("unexpected recent index after toggle: %d", manager.recentIndex)
	}
}

func TestSwitchManagerTapToggleRecentWithoutHistoryIsNoop(t *testing.T) {
	writeKDEConfig(t)

	runner := &switchRunner{current: 1}
	manager, _, err := NewSwitchManagerWithLoader(
		context.Background(),
		Loader{runner: runner},
		makeSwitchConfig(),
		false,
	)
	if err != nil {
		t.Fatalf("NewSwitchManagerWithLoader: %v", err)
	}

	if err := manager.Process(context.Background(), event.KeyEvent{
		Kind:         event.KindLayoutSwitch,
		LayoutSwitch: event.LayoutSwitchRequest{SourceCode: config.KeyCapsLock},
	}, func(event.KeyEvent) error { return nil }); err != nil {
		t.Fatalf("toggle recent: %v", err)
	}

	if len(runner.setCalls) != 0 {
		t.Fatalf("unexpected setLayout calls: %#v", runner.setCalls)
	}
}

func TestSwitchManagerCloseRestoresLayout(t *testing.T) {
	writeKDEConfig(t)

	runner := &switchRunner{current: 1}
	manager, _, err := NewSwitchManagerWithLoader(
		context.Background(),
		Loader{runner: runner},
		makeSwitchConfig(),
		false,
	)
	if err != nil {
		t.Fatalf("NewSwitchManagerWithLoader: %v", err)
	}

	if err := manager.Process(context.Background(), event.KeyEvent{Kind: event.KindKey, Code: config.KeyLeftCtrl, Value: 1}, func(event.KeyEvent) error { return nil }); err != nil {
		t.Fatalf("ctrl down: %v", err)
	}

	if err := manager.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(runner.setCalls) != 2 || runner.setCalls[0] != 0 || runner.setCalls[1] != 1 {
		t.Fatalf("unexpected setLayout calls: %#v", runner.setCalls)
	}
}
