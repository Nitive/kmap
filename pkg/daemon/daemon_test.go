package daemon

import (
	"testing"
	"time"

	"keyboard/pkg/config"
)

type fakeTapper struct {
	events []struct {
		code  uint16
		delay time.Duration
	}
}

func (f *fakeTapper) TapKey(code uint16, delay time.Duration) error {
	f.events = append(f.events, struct {
		code  uint16
		delay time.Duration
	}{code: code, delay: delay})
	return nil
}

func TestResolveDevicePaths(t *testing.T) {
	cfg := config.Runtime{
		Devices: []string{"/dev/input/a", "/dev/input/b"},
	}

	t.Run("cli override wins", func(t *testing.T) {
		paths, err := resolveDevicePaths("/dev/input/override", cfg)
		if err != nil {
			t.Fatalf("resolveDevicePaths: %v", err)
		}
		if len(paths) != 1 || paths[0] != "/dev/input/override" {
			t.Fatalf("unexpected paths: %#v", paths)
		}
	})

	t.Run("config devices used when no override", func(t *testing.T) {
		paths, err := resolveDevicePaths("", cfg)
		if err != nil {
			t.Fatalf("resolveDevicePaths: %v", err)
		}
		if len(paths) != 2 || paths[0] != "/dev/input/a" || paths[1] != "/dev/input/b" {
			t.Fatalf("unexpected paths: %#v", paths)
		}
	})

	t.Run("default device fallback", func(t *testing.T) {
		paths, err := resolveDevicePaths("", config.Runtime{})
		if err != nil {
			t.Fatalf("resolveDevicePaths: %v", err)
		}
		if len(paths) != 1 || paths[0] != config.DefaultDevicePath {
			t.Fatalf("unexpected paths: %#v", paths)
		}
	})
}

func TestReleaseCapsOnStart(t *testing.T) {
	t.Run("toggles off when caps mappings exist and caps is enabled", func(t *testing.T) {
		out := &fakeTapper{}
		cfg := config.Runtime{
			CapsMappings: map[uint16]config.CompiledMapping{
				config.KeyH: {Kind: config.MappingRemap, RemapCode: config.KeyBackspace},
			},
		}

		if err := releaseCapsOnStart(out, cfg, true, time.Millisecond); err != nil {
			t.Fatalf("releaseCapsOnStart: %v", err)
		}
		if len(out.events) != 1 {
			t.Fatalf("unexpected events: %#v", out.events)
		}
		if out.events[0].code != config.KeyCapsLock {
			t.Fatalf("unexpected key: %#v", out.events[0])
		}
	})

	t.Run("noop without caps mappings", func(t *testing.T) {
		out := &fakeTapper{}
		cfg := config.Runtime{CapsMappings: map[uint16]config.CompiledMapping{}}

		if err := releaseCapsOnStart(out, cfg, true, 0); err != nil {
			t.Fatalf("releaseCapsOnStart: %v", err)
		}
		if len(out.events) != 0 {
			t.Fatalf("expected no events, got %#v", out.events)
		}
	})

	t.Run("noop when caps mappings exist but caps is already disabled", func(t *testing.T) {
		out := &fakeTapper{}
		cfg := config.Runtime{
			CapsMappings: map[uint16]config.CompiledMapping{
				config.KeyH: {Kind: config.MappingRemap, RemapCode: config.KeyBackspace},
			},
		}

		if err := releaseCapsOnStart(out, cfg, false, 0); err != nil {
			t.Fatalf("releaseCapsOnStart: %v", err)
		}
		if len(out.events) != 0 {
			t.Fatalf("expected no events, got %#v", out.events)
		}
	})
}
