package shortcut

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"keyboard/pkg/config"
)

type fakeRunner struct {
	outputs map[string]string
	errs    map[string]error
}

func (f fakeRunner) run(ctx context.Context, name string, args ...string) (string, error) {
	_ = ctx
	key := fmt.Sprintf("%s|%v", name, args)
	if err := f.errs[key]; err != nil {
		return "", err
	}
	return f.outputs[key], nil
}

func TestParseCurrentLayoutIndex(t *testing.T) {
	got, err := parseCurrentLayoutIndex(" 1 \n")
	if err != nil {
		t.Fatalf("parseCurrentLayoutIndex: %v", err)
	}
	if got != 1 {
		t.Fatalf("unexpected index: got=%d want=1", got)
	}
}

func TestLoaderCurrentKDELayout(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	if err := os.WriteFile(filepath.Join(configDir, "kxkbrc"), []byte(`[Layout]
LayoutList=us,ru
VariantList=dvorak,
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loader := NewLoaderWithRunner(fakeRunner{
		outputs: map[string]string{
			"qdbus6|[org.kde.keyboard /Layouts org.kde.KeyboardLayouts.getLayout]": "1\n",
		},
	})

	got, err := loader.currentKDELayout(context.Background())
	if err != nil {
		t.Fatalf("currentKDELayout: %v", err)
	}
	if got.Layout != "ru" || got.Variant != "" || got.Description != "ru" {
		t.Fatalf("unexpected layout info: %#v", got)
	}
}

func TestReadConfiguredKDELayouts(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	if err := os.WriteFile(filepath.Join(configDir, "kxkbrc"), []byte(`[Layout]
LayoutList=us,ru
VariantList=dvorak,
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := readConfiguredKDELayouts()
	if err != nil {
		t.Fatalf("readConfiguredKDELayouts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("unexpected layout count: %d", len(got))
	}
	if got[0].Layout != "us" || got[0].Variant != "dvorak" || got[0].Description != "us(dvorak)" {
		t.Fatalf("unexpected first layout: %#v", got[0])
	}
	if got[1].Layout != "ru" || got[1].Variant != "" || got[1].Description != "ru" {
		t.Fatalf("unexpected second layout: %#v", got[1])
	}
}

func TestProviderCurrentRemapUsesActiveLayout(t *testing.T) {
	provider := &Provider{
		loader: NewLoaderWithRunner(fakeRunner{
			outputs: map[string]string{
				"qdbus6|[org.kde.keyboard /Layouts org.kde.KeyboardLayouts.getLayout]": "1\n",
			},
		}),
		layouts: []LayoutInfo{
			{Layout: "us", Variant: "dvorak"},
			{Layout: "ru", Variant: ""},
		},
		remaps: map[string]map[uint16]uint16{
			"us(dvorak)": {config.KeyDot: config.KeyV},
			"ru":         {config.KeyDot: config.KeySlash},
		},
	}

	remap, active, err := provider.CurrentRemap(context.Background())
	if err != nil {
		t.Fatalf("CurrentRemap: %v", err)
	}
	if active.Layout != "ru" || active.Variant != "" {
		t.Fatalf("unexpected active layout: %#v", active)
	}
	if got := remap[config.KeyDot]; got != config.KeySlash {
		t.Fatalf("unexpected remap: got=%d want=%d", got, config.KeySlash)
	}
}

func TestBuildRemapUsToDvorak(t *testing.T) {
	remap, err := BuildRemap(context.Background(),
		config.ShortcutLayoutSpec{Layout: "us"},
		config.ShortcutLayoutSpec{Layout: "us", Variant: "dvorak"},
	)
	if err != nil {
		t.Fatalf("BuildRemap: %v", err)
	}

	if got := remap[config.KeyDot]; got != config.KeyV {
		t.Fatalf("expected KeyDot -> KeyV, got %d", got)
	}
	if got := remap[config.KeyV]; got != config.KeyK {
		t.Fatalf("expected KeyV -> KeyK, got %d", got)
	}
}

func TestBuildRemapSameLayoutIsIdentity(t *testing.T) {
	remap, err := BuildRemap(context.Background(),
		config.ShortcutLayoutSpec{Layout: "us", Variant: "dvorak"},
		config.ShortcutLayoutSpec{Layout: "us", Variant: "dvorak"},
	)
	if err != nil {
		t.Fatalf("BuildRemap: %v", err)
	}
	if len(remap) != 0 {
		t.Fatalf("expected empty remap for identical layouts, got %#v", remap)
	}
}
