package xcompose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"keyboard/pkg/config"
)

func TestEntryForRuneUsesScopedDecimalSequence(t *testing.T) {
	entry, err := EntryForRune('«')
	if err != nil {
		t.Fatalf("EntryForRune: %v", err)
	}
	want := `<Multi_key> <3> <1> <7> <1> : "«" U00AB`
	if entry != want {
		t.Fatalf("entry mismatch:\n got: %q\nwant: %q", entry, want)
	}
}

func TestBuildContentUsesScopedHeader(t *testing.T) {
	content, err := BuildContent([]rune{'«', '→'})
	if err != nil {
		t.Fatalf("BuildContent: %v", err)
	}
	if strings.Contains(content, `include "%L"`) {
		t.Fatalf("content should not include include %%L line")
	}
	if !strings.Contains(content, `<Multi_key> <3> <1> <7> <1> : "«" U00AB`) {
		t.Fatalf("missing guillemet entry")
	}
	if !strings.Contains(content, `<Multi_key> <4> <8> <5> <9> <4> : "→" U2192`) {
		t.Fatalf("missing arrow entry")
	}
}

func TestDefaultOutputPathUsesHomeDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	got, err := DefaultOutputPath()
	if err != nil {
		t.Fatalf("DefaultOutputPath: %v", err)
	}
	if got != filepath.Join(homeDir, ".XCompose") {
		t.Fatalf("unexpected output path: got=%q want=%q", got, filepath.Join(homeDir, ".XCompose"))
	}
}

func TestGenerateXComposeFileWritesExpectedEntries(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "XCompose")
	configPath := filepath.Join("..", "..", "testdata", "example-kmap.yaml")
	if err := GenerateFile(configPath, outPath); err != nil {
		t.Fatalf("GenerateFile: %v", err)
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, `<Multi_key> <3> <1> <2> <4> : "|" U007C`) {
		t.Fatalf("missing pipe entry")
	}
	if !strings.Contains(content, `<Multi_key> <4> <8> <5> <9> <4> : "→" U2192`) {
		t.Fatalf("missing arrow entry")
	}
	if !strings.Contains(content, `<Multi_key> <3> <1> <6> <0> : " " U00A0`) {
		t.Fatalf("missing NBSP entry")
	}
}

func TestCollectMappedSymbolsSortsAndDeduplicates(t *testing.T) {
	cfg := config.Runtime{
		AltMappings: map[uint16]config.CompiledMapping{
			config.KeyA: {Kind: config.MappingSymbol, Symbol: 'z'},
			config.KeyB: {Kind: config.MappingSymbol, Symbol: 'a'},
		},
		CapsMappings: map[uint16]config.CompiledMapping{
			config.KeyC: {Kind: config.MappingSymbol, Symbol: 'z'},
		},
		ComboMappings: map[config.InputBinding]config.CompiledMapping{
			{Modifiers: config.ModifierAlt | config.ModifierShift, KeyCode: config.KeyD}: {Kind: config.MappingSymbol, Symbol: 'b'},
		},
	}

	got, err := collectMappedSymbols(cfg)
	if err != nil {
		t.Fatalf("collectMappedSymbols: %v", err)
	}
	if len(got) != 3 || got[0] != 'a' || got[1] != 'b' || got[2] != 'z' {
		t.Fatalf("unexpected symbols: %#v", got)
	}
}

func TestCollectMappedSymbolsRejectsEmptySymbol(t *testing.T) {
	cfg := config.Runtime{
		AltMappings: map[uint16]config.CompiledMapping{
			config.KeyA: {Kind: config.MappingSymbol},
		},
	}

	if _, err := collectMappedSymbols(cfg); err == nil {
		t.Fatalf("expected empty symbol error")
	}
}

func TestEntryForRuneRejectsInvalidRune(t *testing.T) {
	if _, err := EntryForRune(rune(0xD800)); err == nil {
		t.Fatalf("expected invalid rune error")
	}
}

func TestEscapeSymbolSpecialCases(t *testing.T) {
	tests := []struct {
		symbol rune
		want   string
	}{
		{symbol: '\\', want: `\\`},
		{symbol: '"', want: `\"`},
		{symbol: '\n', want: `\n`},
		{symbol: '\t', want: `\t`},
		{symbol: 'x', want: "x"},
	}

	for _, tc := range tests {
		if got := escapeSymbol(tc.symbol); got != tc.want {
			t.Fatalf("escapeSymbol(%q) mismatch: got=%q want=%q", tc.symbol, got, tc.want)
		}
	}
}

func TestGenerateFileCreatesParentDirectories(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "nested", "dir", "XCompose")
	configPath := filepath.Join("..", "..", "testdata", "example-kmap.yaml")

	if err := GenerateFile(configPath, outPath); err != nil {
		t.Fatalf("GenerateFile: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("Stat(%s): %v", outPath, err)
	}
}
