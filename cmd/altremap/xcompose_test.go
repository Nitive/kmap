package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestXComposeEntryForRuneUsesScopedDecimalSequence(t *testing.T) {
	entry, err := xcomposeEntryForRune('«')
	if err != nil {
		t.Fatalf("xcomposeEntryForRune: %v", err)
	}
	want := `<Multi_key> <3> <1> <7> <1> : "«" U00AB`
	if entry != want {
		t.Fatalf("entry mismatch:\n got: %q\nwant: %q", entry, want)
	}
}

func TestBuildXComposeContentUsesScopedHeader(t *testing.T) {
	content, err := buildXComposeContent([]rune{'«', '→'})
	if err != nil {
		t.Fatalf("buildXComposeContent: %v", err)
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

func TestGenerateXComposeFileWritesExpectedEntries(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "XCompose")
	if err := generateXComposeFile("", outPath); err != nil {
		t.Fatalf("generateXComposeFile: %v", err)
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
