package xcompose

import (
	"os"
	"path/filepath"
	"testing"

	"keyboard/pkg/config"
)

func TestCollectMappedSymbolsSortsAndDeduplicates(t *testing.T) {
	cfg := config.Runtime{
		AltMappings: map[uint16]config.CompiledMapping{
			config.KeyA: {Kind: config.MappingSymbol, Symbol: 'z'},
			config.KeyB: {Kind: config.MappingSymbol, Symbol: 'a'},
		},
		CapsMappings: map[uint16]config.CompiledMapping{
			config.KeyC: {Kind: config.MappingSymbol, Symbol: 'z'},
		},
	}

	got, err := collectMappedSymbols(cfg)
	if err != nil {
		t.Fatalf("collectMappedSymbols: %v", err)
	}
	if len(got) != 2 || got[0] != 'a' || got[1] != 'z' {
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
	configPath := filepath.Join("..", "..", "kmap.yaml")

	if err := GenerateFile(configPath, outPath); err != nil {
		t.Fatalf("GenerateFile: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("Stat(%s): %v", outPath, err)
	}
}
