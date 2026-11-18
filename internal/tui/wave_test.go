package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func testBg(d int) string {
	if d < 2 {
		return "\x1b[48;5;1m"
	}
	return "\x1b[48;5;2m"
}

func TestOverlayBarKeepsText(t *testing.T) {
	line := "\x1b[38;5;3m小明:\x1b[0m hello"
	got := overlayBar(line, 20, 6, testBg)
	if ansi.Strip(got) != ansi.Strip(line)+strings.Repeat(" ", 20-ansi.StringWidth(line)) {
		t.Fatalf("text changed: %q", ansi.Strip(got))
	}
	if ansi.StringWidth(got) != 20 {
		t.Fatalf("width %d", ansi.StringWidth(got))
	}
	if !strings.HasSuffix(got, "\x1b[48;5;1m  \x1b[49m") {
		t.Fatalf("bar not painted to the edge: %q", got)
	}
}

func TestOverlayBarRepaintsAfterReset(t *testing.T) {
	line := "ab\x1b[31mc\x1b[0md"
	got := overlayBar(line, 4, 4, testBg)
	// Codes in the line must not end the bar: it is restored after a reset.
	want := "\x1b[48;5;2mab\x1b[31m\x1b[48;5;1mc\x1b[0m\x1b[48;5;1md\x1b[49m"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestOverlayBarKeepsOwnBackground(t *testing.T) {
	line := "\x1b[48;2;1;2;3mmedal\x1b[0m x"
	got := overlayBar(line, 8, 8, testBg)
	if strings.Index(got, "\x1b[48;5") < strings.Index(got, "\x1b[0m") {
		t.Fatalf("medal background overwritten: %q", got)
	}
}

func TestSGRBackground(t *testing.T) {
	cases := []struct {
		params string
		set    bool
		reset  bool
	}{
		{"", false, true},
		{"0", false, true},
		{"38;2;48;0;0", false, false},
		{"1;48;5;200", true, false},
		{"48:2::1:2:3", true, false},
		{"41", true, false},
		{"49", false, true},
	}
	for _, c := range cases {
		set, reset := sgrBackground(c.params, false)
		if set != c.set || reset != c.reset {
			t.Errorf("%q: got %v %v", c.params, set, reset)
		}
	}
}

func TestLoudness(t *testing.T) {
	if loudness(0) != 0 || loudness(1) != 1 {
		t.Fatal("ends")
	}
	if l := loudness(0.05); l <= 0.2 || l >= 0.8 {
		t.Fatalf("speech level %.2f", l)
	}
}
