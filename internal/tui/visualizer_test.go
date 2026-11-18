package tui

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestLineGlyphs(t *testing.T) {
	draw := func(halves int) string {
		var b strings.Builder
		for col := range 5 {
			if g := lineGlyph(col, 5, halves); g != "" {
				b.WriteString(g)
			} else {
				b.WriteByte('.')
			}
		}
		return b.String()
	}
	cases := map[int]string{
		1:  "....▗",
		2:  "....▄",
		3:  "...▗▄",
		10: "▄▄▄▄▄",
	}
	for halves, want := range cases {
		if got := draw(halves); got != want {
			t.Errorf("%d halves: %s, want %s", halves, got, want)
		}
	}
}

func TestDrawLineLeavesTextAlone(t *testing.T) {
	line := "\x1b[36m诺***:\x1b[0m 满昏!" + strings.Repeat(" ", 10)
	got := drawLine(line, 16, 12, "\x1b[34m")
	if ansi.StringWidth(got) != 16 {
		t.Fatalf("width %d: %q", ansi.StringWidth(got), got)
	}
	if plain := ansi.Strip(got); plain != "诺***: 满昏! ▄▄▄" {
		t.Fatalf("text not kept with a gap: %q", plain)
	}
}

func TestDrawLineOnEmptyRow(t *testing.T) {
	got := ansi.Strip(drawLine("", 6, 3, ""))
	if got != "    ▗▄" {
		t.Fatalf("%q", got)
	}
}

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
	if !strings.HasSuffix(got, "\x1b[48;5;1m  \x1b[49m") {
		t.Fatalf("bar not painted to the edge: %q", got)
	}
}

func TestOverlayBarRepaintsAfterReset(t *testing.T) {
	got := overlayBar("ab\x1b[31mc\x1b[0md", 4, 4, testBg)
	want := "\x1b[48;5;2mab\x1b[31m\x1b[48;5;1mc\x1b[0m\x1b[48;5;1md\x1b[49m"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestOverlayBarKeepsOwnBackground(t *testing.T) {
	got := overlayBar("\x1b[48;2;1;2;3mmedal\x1b[0m x", 8, 8, testBg)
	if strings.Index(got, "\x1b[48;5") < strings.Index(got, "\x1b[0m") {
		t.Fatalf("medal background overwritten: %q", got)
	}
}

func TestSGRBackground(t *testing.T) {
	cases := []struct {
		params     string
		set, reset bool
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

func TestFollowJumpsUpAndEasesDown(t *testing.T) {
	v := follow(0, 0.8, 33*time.Millisecond)
	if v != 0.8 {
		t.Fatalf("rise %.2f", v)
	}
	v = follow(v, 0, release)
	if math.Abs(v-0.8/math.E) > 0.001 {
		t.Fatalf("one time constant %.3f", v)
	}
	for range 30 {
		v = follow(v, 0, 33*time.Millisecond)
	}
	if v != 0 {
		t.Fatalf("not settled: %.3f", v)
	}
}

func TestVoiceReactsAtOnce(t *testing.T) {
	longest := func(levels []float64) float64 {
		m := 0.0
		for _, l := range levels {
			m = max(m, l)
		}
		return m
	}
	s := newVisualizerState(true)
	tick := 33 * time.Millisecond
	s.step(true, 13, tick) // silence
	if l := longest(s.levels); l != 0 {
		t.Fatalf("silence drew %.2f", l)
	}
	for i := range s.samples {
		s.samples[i] = float32(0.1 * math.Sin(2*math.Pi*1000*float64(i)/48000))
	}
	s.step(true, 13, tick)
	if l := longest(s.levels); l < 0.4 {
		t.Fatalf("first frame of sound drew only %.2f", l)
	}
	clear(s.samples)
	for range 10 {
		s.step(true, 13, tick)
	}
	if l := longest(s.levels); l > 0.05 {
		t.Fatalf("still %.2f after 330 ms of silence", l)
	}
}

func TestBlocksHaveNoFloor(t *testing.T) {
	s := newVisualizerState(true)
	for i := range s.samples {
		s.samples[i] = float32(0.1 * math.Sin(2*math.Pi*1000*float64(i)/48000))
	}
	low, high := 1.0, 0.0
	for range 300 {
		s.step(true, 13, 33*time.Millisecond)
		for _, l := range s.blocks {
			low, high = min(low, l), max(high, l)
		}
	}
	if low > 0.1 || high < 0.8 {
		t.Fatalf("blocks only span %.2f–%.2f during steady sound", low, high)
	}
}

func sine(dst []float32, amp, hz float64) {
	for i := range dst {
		dst[i] = float32(amp * math.Sin(2*math.Pi*hz*float64(i)/48000))
	}
}

func TestVoiceLoudnessIgnoresBass(t *testing.T) {
	x := make([]float32, envelopeWindow)
	sine(x, 1, 1000)
	if db := voiceLoudness(x); math.Abs(db) > 0.5 {
		t.Fatalf("full-scale 1 kHz read %.1f dB", db)
	}
	sine(x, 1, 80)
	if db := voiceLoudness(x); db > -30 {
		t.Fatalf("80 Hz bass read %.1f dB", db)
	}
}

func TestBackgroundNoiseStaysStill(t *testing.T) {
	s := newVisualizerState(true)
	tick := 33 * time.Millisecond
	longest := 0.0
	for i := range 150 { // 5 s of quiet noise
		for j := range s.samples {
			s.samples[j] = float32(0.01 * (rand.Float64()*2 - 1))
		}
		s.step(true, 13, tick)
		if i > 30 {
			for _, l := range s.levels {
				longest = max(longest, l)
			}
		}
	}
	if longest > 0.05 {
		t.Fatalf("background noise drew %.2f", longest)
	}
	sine(s.samples, 0.1, 1000)
	s.step(true, 13, tick)
	if s.envelope < 0.8 {
		t.Fatalf("voice over the noise only reached %.2f", s.envelope)
	}
}
