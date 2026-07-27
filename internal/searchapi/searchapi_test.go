package searchapi

import (
	"strings"
	"testing"
)

func TestTruncateRunes_KeepsShortInput(t *testing.T) {
	in := "hello world"
	out, was := truncateRunes(in, 100)
	if was {
		t.Fatalf("did not expect truncation, got was=true")
	}
	if out != in {
		t.Fatalf("expected unchanged input, got %q", out)
	}
}

func TestTruncateRunes_TruncatesLongInput(t *testing.T) {
	in := strings.Repeat("x", 50)
	out, was := truncateRunes(in, 10)
	if !was {
		t.Fatalf("expected truncation, got was=false")
	}
	if !strings.HasSuffix(out, "[truncated]") {
		t.Fatalf("expected [truncated] suffix, got %q", out)
	}
	if len([]rune(out)) <= 10 {
		t.Fatalf("expected output longer than max because of suffix, got %d runes", len([]rune(out)))
	}
}

func TestTruncateRunes_RuneSafe(t *testing.T) {
	in := strings.Repeat("ы", 50) // multibyte
	out, was := truncateRunes(in, 5)
	if !was {
		t.Fatalf("expected truncation")
	}
	prefix := []rune(out)[:5]
	if string(prefix) != strings.Repeat("ы", 5) {
		t.Fatalf("expected first 5 runes preserved, got %q", string(prefix))
	}
}

func TestTruncateRunes_ZeroMaxNoOp(t *testing.T) {
	in := "abc"
	out, was := truncateRunes(in, 0)
	if was {
		t.Fatalf("expected no truncation for max<=0")
	}
	if out != in {
		t.Fatalf("expected unchanged input")
	}
}
