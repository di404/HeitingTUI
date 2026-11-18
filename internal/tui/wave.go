package tui

import (
	"math"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/di404/HeitingTUI/internal/audio"
	colorful "github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
)

// The wave is a column of horizontal bars along the right edge of the chat,
// one per row, drawn as a faint background tint so the chat stays readable on
// top of it. The newest level enters at the bottom and scrolls up with the chat.
const (
	waveInterval = 90 * time.Millisecond
	// waveFloor and waveCeil are the loudness, in dBFS, drawn as an empty and a
	// full bar.
	waveFloor = -48.0
	waveCeil  = -10.0
	// waveRelease is how much of the previous level a quieter one keeps, so
	// bars fall back more gently than they rise.
	waveRelease = 0.55
	// waveMinWidth is the narrowest chat that still gets a wave.
	waveMinWidth = 30
	// waveSteps is how many brightness levels a bar can have.
	waveSteps = 6
)

type waveTickMsg struct{}

type waveState struct {
	levels  []float64 // oldest first
	ticking bool
	bg      colorful.Color
	tint    colorful.Color
	minA    float64
	maxA    float64
	cache   map[[3]int]string
}

func newWaveState(dark bool) waveState {
	w := waveState{cache: map[[3]int]string{}}
	if dark {
		w.bg, _ = colorful.Hex("#1E1E1E")
		w.tint, _ = colorful.Hex("#73F59F")
		w.minA, w.maxA = 0.04, 0.22
	} else {
		w.bg, _ = colorful.Hex("#FFFFFF")
		w.tint, _ = colorful.Hex("#1E8E4E")
		w.minA, w.maxA = 0.03, 0.15
	}
	if c := terminalBackground(); c != nil {
		w.bg = *c
	}
	return w
}

// terminalBackground reads the colour the terminal reported at startup.
func terminalBackground() *colorful.Color {
	switch c := lipgloss.DefaultRenderer().Output().BackgroundColor().(type) {
	case nil, termenv.NoColor:
		return nil
	default:
		rgb := termenv.ConvertToRGB(c)
		return &rgb
	}
}

func waveTick() tea.Cmd {
	return tea.Tick(waveInterval, func(time.Time) tea.Msg { return waveTickMsg{} })
}

// startWave begins animating if the wave is shown and audio is playing.
func (m *Model) startWave() tea.Cmd {
	if m.wave.ticking || !m.cfg.ShowWave || m.audioStatus.State != audio.Playing {
		return nil
	}
	m.wave.ticking = true
	m.player.Level() // drop whatever built up while the wave was idle
	return waveTick()
}

// stepWave adds the latest level and keeps ticking until the wave has
// settled after playback stops.
func (m *Model) stepWave() tea.Cmd {
	raw := m.player.Level()
	if !m.cfg.ShowWave {
		m.wave.levels = nil
		m.wave.ticking = false
		return nil
	}
	playing := m.audioStatus.State == audio.Playing
	level := 0.0
	if playing && !m.muted() {
		level = loudness(raw)
	}
	if n := len(m.wave.levels); n > 0 {
		level = max(level, m.wave.levels[n-1]*waveRelease)
		if level < 0.02 {
			level = 0
		}
	}
	m.wave.levels = append(m.wave.levels, level)
	if excess := len(m.wave.levels) - m.viewport.Height; excess > 0 {
		m.wave.levels = append(m.wave.levels[:0], m.wave.levels[excess:]...)
	}
	if !playing && m.waveSilent() {
		m.wave.ticking = false
		return nil
	}
	return waveTick()
}

func (m *Model) waveSilent() bool {
	for _, level := range m.wave.levels {
		if level > 0 {
			return false
		}
	}
	return true
}

// loudness maps an RMS sample level to the 0–1 length of a bar.
func loudness(rms float64) float64 {
	if rms <= 0 {
		return 0
	}
	db := 20 * math.Log10(rms)
	return min(max((db-waveFloor)/(waveCeil-waveFloor), 0), 1)
}

// chatView is the chat viewport with the wave drawn behind its right side.
func (m *Model) chatView() string {
	view := m.viewport.View()
	width := m.viewport.Width
	if !m.cfg.ShowWave || width < waveMinWidth || len(m.wave.levels) == 0 ||
		lipgloss.ColorProfile() == termenv.Ascii {
		return view
	}
	maxLen := min(width/3, 40)
	lines := strings.Split(view, "\n")
	offset := len(lines) - len(m.wave.levels)
	for i := range lines {
		if i < offset {
			continue
		}
		level := m.wave.levels[i-offset]
		n := int(math.Round(level * float64(maxLen)))
		if n == 0 {
			continue
		}
		step := int(math.Round(level * (waveSteps - 1)))
		lines[i] = overlayBar(lines[i], width, n, func(d int) string {
			return m.waveColor(d, maxLen, step)
		})
	}
	return strings.Join(lines, "\n")
}

// waveColor is the background for the cell d columns in from the right edge.
// Bars are strongest at the edge and fade towards their tips, and louder bars
// are a little stronger overall.
func (m *Model) waveColor(d, maxLen, step int) string {
	key := [3]int{d, maxLen, step}
	if seq, ok := m.wave.cache[key]; ok {
		return seq
	}
	if len(m.wave.cache) > 4096 {
		clear(m.wave.cache)
	}
	fade := 1 - math.Pow(float64(d)/float64(maxLen), 0.8)
	strength := 0.65 + 0.35*float64(step)/(waveSteps-1)
	alpha := (m.wave.minA + (m.wave.maxA-m.wave.minA)*fade) * strength
	color := m.wave.bg.BlendRgb(m.wave.tint, alpha).Clamped().Hex()
	seq := ""
	if s := lipgloss.ColorProfile().Color(color).Sequence(true); s != "" {
		seq = "\x1b[" + s + "m"
	}
	m.wave.cache[key] = seq
	return seq
}

// overlayBar gives the last n columns of a line of the given width the
// background bg(d), where d counts columns in from the right edge. Cells that
// already have a background, such as fan medals, keep it.
func overlayBar(line string, width, n int, bg func(d int) string) string {
	start := width - n
	var out strings.Builder
	col := 0
	ownBg := false // the line has set a background of its own
	current := ""  // the wave background currently in effect
	paint := func() {
		if ownBg || col < start {
			return
		}
		if seq := bg(width - 1 - col); seq != current {
			out.WriteString(seq)
			current = seq
		}
	}
	state := byte(0)
	for len(line) > 0 {
		seq, w, size, next := ansi.DecodeSequence(line, state, nil)
		state = next
		line = line[size:]
		if w == 0 {
			out.WriteString(seq)
			if params, ok := sgrParams(seq); ok {
				var reset bool
				ownBg, reset = sgrBackground(params, ownBg)
				if reset || ownBg {
					current = ""
				}
			}
			continue
		}
		paint()
		out.WriteString(seq)
		col += w
	}
	for ; col < width; col++ {
		paint()
		out.WriteByte(' ')
	}
	if current != "" {
		out.WriteString("\x1b[49m")
	}
	return out.String()
}

func sgrParams(seq string) (string, bool) {
	if !strings.HasPrefix(seq, "\x1b[") || !strings.HasSuffix(seq, "m") {
		return "", false
	}
	return seq[2 : len(seq)-1], true
}

// sgrBackground reports whether a background is set after the SGR parameters
// are applied, and whether they cleared the background in effect.
func sgrBackground(params string, set bool) (bool, bool) {
	if params == "" {
		return false, true
	}
	reset := false
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		head, _, sub := strings.Cut(parts[i], ":")
		code, err := strconv.Atoi(head)
		if head == "" {
			code, err = 0, nil
		}
		if err != nil {
			continue
		}
		switch {
		case code == 0 || code == 49:
			set, reset = false, true
		case code == 38 || code == 48 || code == 58:
			if !sub && i+1 < len(parts) {
				switch parts[i+1] {
				case "5":
					i += 2
				case "2":
					i += 4
				}
			}
			if code == 48 {
				set = true
			}
		case code >= 40 && code <= 47, code >= 100 && code <= 107:
			set = true
		}
	}
	return set, reset
}
