package tui

import (
	"strings"
	"testing"
)

func TestRenderGlamour_BulletList(t *testing.T) {
	md := "- First item\n- Second item\n"
	result := renderGlamour(md, 60)
	if result == "" {
		t.Fatal("expected non-empty output")
	}
	plain := stripANSI(result)
	if !strings.Contains(plain, "First item") {
		t.Errorf("expected 'First item' in output, got: %s", plain)
	}
	if !strings.Contains(plain, "Second item") {
		t.Errorf("expected 'Second item' in output, got: %s", plain)
	}
}

func TestRenderGlamour_Bold(t *testing.T) {
	md := "- **Learnings for future iterations:**\n"
	result := renderGlamour(md, 60)
	if result == "" {
		t.Fatal("expected non-empty output")
	}
	plain := stripANSI(result)
	if strings.Contains(plain, "**") {
		t.Error("expected ** markers to be rendered as bold")
	}
	if !strings.Contains(plain, "Learnings") {
		t.Errorf("expected bold text content to be present, got: %s", plain)
	}
}

func TestRenderGlamour_InlineCode(t *testing.T) {
	md := "- Created `Parser` at `lib/Parser.php`\n"
	result := renderGlamour(md, 60)
	if result == "" {
		t.Fatal("expected non-empty output")
	}
	plain := stripANSI(result)
	if !strings.Contains(plain, "Parser") {
		t.Errorf("expected code text to be present, got: %s", plain)
	}
}

func TestRenderGlamour_NestedList(t *testing.T) {
	md := "- Top level\n  - Sub item\n  - Another sub\n"
	result := renderGlamour(md, 60)
	if result == "" {
		t.Fatal("expected non-empty output")
	}
	plain := stripANSI(result)
	if !strings.Contains(plain, "Top level") {
		t.Errorf("expected top level text, got: %s", plain)
	}
	if !strings.Contains(plain, "Sub item") {
		t.Errorf("expected sub item text, got: %s", plain)
	}
}

func TestRenderGlamour_EmptyInput(t *testing.T) {
	result := renderGlamour("", 60)
	if result != "" {
		t.Errorf("expected empty output for empty input, got %q", result)
	}
}

func TestRenderGlamour_WhitespaceOnly(t *testing.T) {
	result := renderGlamour("   \n  \n", 60)
	if result != "" {
		t.Errorf("expected empty output for whitespace-only input, got %q", result)
	}
}

func TestRenderGlamour_ZeroWidth(t *testing.T) {
	result := renderGlamour("- test\n", 0)
	if result != "" {
		t.Errorf("expected empty output for zero width, got %q", result)
	}
}

func TestProgressStyleTamesInlineCode(t *testing.T) {
	cfg := buildProgressStyle()

	if cfg.Code.Color == nil {
		t.Fatal("expected inline code to have an explicit color")
	}
	if *cfg.Code.Color == "203" {
		t.Error("inline code still uses glamour's default red (ANSI 203)")
	}
	if cfg.Code.BackgroundColor != nil {
		t.Errorf("expected inline code background box to be removed, got %q", *cfg.Code.BackgroundColor)
	}
}

func TestStripANSI(t *testing.T) {
	input := "\x1b[38;5;252mhello\x1b[0m \x1b[1mworld\x1b[0m"
	got := stripANSI(input)
	want := "hello world"
	if got != want {
		t.Errorf("stripANSI() = %q, want %q", got, want)
	}
}
