package tui

import (
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/di404/HeitingTUI/internal/audio"
	"github.com/di404/HeitingTUI/internal/config"
	colorful "github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/termenv"
)

// The visualiser is a level meter along the right edge of the chat, one bar
// per row, that grows leftwards with the loudness of what is being heard and
// shrinks to its smallest size in silence. Each bar also wanders on its own
// so the column looks alive. It comes in two styles:
//
//   - lines: lower-half block characters, so each bar is half a row tall
//     and rows stay apart, like a voice-input indicator cut in half;
//   - gradient: background tint, strongest at the edge and fading out
//     towards the tip, so a bar can end part-way through a cell;
//   - blocks: background tint in whole cells, with a brighter cap that the
//     bar pushes up and that then falls back down.
const (
	visualizerInterval = time.Second / 30
	// envelopeWindow is how many samples the loudness is measured over.
	envelopeWindow = 960 // 20 ms
	// voiceLow and voiceHigh bound the band the loudness is measured in:
	// where speech carries its energy, leaving out the bass and drums and the
	// hiss above that most background music puts its weight in.
	voiceLow  = 250.0
	voiceHigh = 4000.0
	// loudnessRange is how many dB below the recent loudness a sound has to
	// be to draw the smallest bars, and loudnessHeadroom how far the recent
	// loudness sits below the longest bar, so stressed syllables still reach
	// past it.
	loudnessRange    = 24.0
	loudnessHeadroom = 2.0
	// gainRise is how quickly, as a time constant, the reference loudness
	// follows a louder stream, and gainFall how fast, in dB per second, it
	// follows a quieter one.
	gainRise = 300 * time.Millisecond
	gainFall = 3.0
	// The noise floor is the quietest the voice band has recently been: it
	// drops to any quieter frame at once and creeps up by floorRise dB per
	// second, so it settles on the background between words. Sound less than
	// noiseGate dB above it draws nothing, and the reference loudness never
	// comes closer to it than minSpan dB, so steady background noise or music
	// cannot be scaled up into a full-size display.
	floorRise = 2.0
	noiseGate = 6.0
	minSpan   = 18.0
	// contrast bends the loudness curve so that quiet sounds draw much less
	// than loud ones.
	contrast = 1.8
	// release is the time constant with which the bars shrink; they grow at
	// once.
	release = 80 * time.Millisecond
	// jitterEvery is how often each bar picks a new length of its own, and
	// jitterEase how quickly it moves there.
	jitterEvery = 90 * time.Millisecond
	jitterEase  = 40 * time.Millisecond
	// lineMax is the longest line in half cells, and blockMax the longest
	// block bar in cells. Blocks can only change by whole cells, so they get
	// as many cells as lines get half cells, to move in as many steps.
	lineMax  = 12
	blockMax = lineMax
	// capHold is how long a block cap stays where the bar pushed it before
	// falling, and capGravity how fast it then speeds up, in cells per second
	// squared.
	capHold    = 120 * time.Millisecond
	capGravity = 60.0
	// visualizerMinWidth is the narrowest chat that still gets a visualiser.
	visualizerMinWidth = 30
)

type visualizerTickMsg struct{}

type visualizerState struct {
	levels  []float64 // 0–1 length of each row's line, top first; nil when hidden
	blocks  []float64 // the same for blocks, which have no floor
	ticking bool
	last    time.Time

	ref      float64 // recent loudness, in dB
	floor    float64 // background loudness, in dB
	envelope float64 // 0–1 loudness right now
	jitter   []float64
	targets  []float64
	nextPick time.Duration
	samples  []float32

	top, bottom colorful.Color // bar colours at the top and bottom rows
	bg          colorful.Color
	edgeA       float64 // gradient opacity at the right edge, where it is strongest
	bodyA, capA float64 // block opacity of the bar and of its cap

	caps    []float64 // where each row's cap is, in cells
	capV    []float64 // how fast each cap is falling, in cells per second
	capWait []time.Duration
	colors  map[[4]int]string
}

func newVisualizerState(dark bool) visualizerState {
	s := visualizerState{
		ref:     -30,
		floor:   -60,
		samples: make([]float32, envelopeWindow),
		colors:  map[[4]int]string{},
	}
	if dark {
		s.top, _ = colorful.Hex("#5CD8F6")
		s.bottom, _ = colorful.Hex("#9A8CFF")
		s.bg, _ = colorful.Hex("#1E1E1E")
		s.edgeA = 0.85
		s.bodyA, s.capA = 0.42, 0.9
	} else {
		s.top, _ = colorful.Hex("#0B9AD0")
		s.bottom, _ = colorful.Hex("#6150D8")
		s.bg, _ = colorful.Hex("#FFFFFF")
		s.edgeA = 0.55
		s.bodyA, s.capA = 0.22, 0.55
	}
	if c := terminalBackground(); c != nil {
		s.bg = *c
	}
	return s
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

func (m *Model) visualizerStyle() string {
	switch m.cfg.Visualizer {
	case config.VisualizerOff, config.VisualizerBlocks, config.VisualizerGradient:
		return m.cfg.Visualizer
	}
	return config.VisualizerLines
}

func visualizerTick() tea.Cmd {
	return tea.Tick(visualizerInterval, func(time.Time) tea.Msg { return visualizerTickMsg{} })
}

// startVisualizer begins animating if the visualiser is shown and audio is
// playing.
func (m *Model) startVisualizer() tea.Cmd {
	if m.visualizer.ticking || m.visualizerStyle() == config.VisualizerOff ||
		m.audioStatus.State != audio.Playing {
		return nil
	}
	m.visualizer.ticking = true
	m.visualizer.last = time.Now()
	return visualizerTick()
}

// stepVisualizer measures the audio being heard and moves the bars. It stops,
// and hides them, once playback stops.
func (m *Model) stepVisualizer() tea.Cmd {
	s := &m.visualizer
	style := m.visualizerStyle()
	if style == config.VisualizerOff || m.audioStatus.State != audio.Playing {
		s.levels, s.blocks, s.caps = nil, nil, nil
		s.ticking = false
		return nil
	}
	now := time.Now()
	dt := min(now.Sub(s.last), 200*time.Millisecond)
	s.last = now
	live := !m.muted() && m.player.Samples(s.samples)
	s.step(live, max(m.viewport.Height, 1), dt)
	return visualizerTick()
}

// step updates the envelope and works out each row's bar.
func (s *visualizerState) step(live bool, rows int, dt time.Duration) {
	envelope := 0.0
	if live {
		db := voiceLoudness(s.samples)
		up := 1 - math.Exp(-dt.Seconds()/gainRise.Seconds())
		if db < s.floor {
			s.floor = db
		} else {
			s.floor += floorRise * dt.Seconds()
		}
		s.floor = min(max(s.floor, -70), -minSpan)
		if db > s.ref {
			s.ref += (db - s.ref) * up
		} else {
			s.ref -= gainFall * dt.Seconds()
		}
		s.ref = min(max(s.ref, s.floor+minSpan, -50), 0)
		top := s.ref + loudnessHeadroom
		bottom := max(top-loudnessRange, s.floor+noiseGate)
		envelope = math.Pow(min(max((db-bottom)/(top-bottom), 0), 1), contrast)
	}
	s.envelope = follow(s.envelope, envelope, dt)

	if len(s.levels) != rows {
		s.levels = make([]float64, rows)
		s.blocks = make([]float64, rows)
		s.caps = make([]float64, rows)
		s.capV = make([]float64, rows)
		s.capWait = make([]time.Duration, rows)
		s.jitter = make([]float64, rows)
		s.targets = make([]float64, rows)
		for i := range s.jitter {
			s.jitter[i] = rand.Float64()
		}
		s.nextPick = 0
	}
	if s.nextPick -= dt; s.nextPick <= 0 {
		for i := range s.targets {
			s.targets[i] = rand.Float64()
		}
		s.nextPick = jitterEvery
	}
	ease := 1 - math.Exp(-dt.Seconds()/jitterEase.Seconds())
	half := float64(rows-1) / 2
	for i := range s.levels {
		s.jitter[i] += (s.targets[i] - s.jitter[i]) * ease
		u := 0.0 // 0 in the middle, 1 at the top and bottom rows
		if half > 0 {
			u = math.Abs(float64(i)-half) / half
		}
		// Bars in the middle reach a little further than those at the ends.
		shape := 0.6 + 0.4*math.Cos(u*math.Pi/2)
		s.levels[i] = s.envelope * shape * (0.35 + 0.65*s.jitter[i])
		// Blocks move a whole cell at a time and twice as far, so any floor
		// shows as a solid slab; they span the full range instead.
		s.blocks[i] = s.envelope * (0.75 + 0.25*math.Cos(u*math.Pi/2)) * s.jitter[i]
		s.moveCap(i, float64(blockCells(s.blocks[i])), dt)
	}
}

// blockCells is how many whole cells a block bar at level takes.
func blockCells(level float64) int {
	return 1 + int(math.Round(level*(blockMax-1)))
}

// moveCap lets the bar push row i's cap up to its length, and otherwise lets
// the cap fall back after a moment, speeding up as it goes.
func (s *visualizerState) moveCap(i int, length float64, dt time.Duration) {
	switch {
	case length >= s.caps[i]:
		s.caps[i], s.capV[i], s.capWait[i] = length, 0, capHold
	case s.capWait[i] > 0:
		s.capWait[i] -= dt
	default:
		s.capV[i] += capGravity * dt.Seconds()
		s.caps[i] = max(s.caps[i]-s.capV[i]*dt.Seconds(), length)
	}
}

// voiceWindow tapers the analysed samples so a frame's edges don't smear
// energy across the spectrum.
var voiceWindow = func() []float64 {
	w := make([]float64, envelopeWindow)
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(len(w)))
	}
	return w
}()

// voiceLoudness is the power of samples between voiceLow and voiceHigh, in dB
// relative to a full-scale sine. Each frequency bin in the band is measured
// with the Goertzel algorithm, which for a few dozen bins is cheaper than a
// full FFT.
func voiceLoudness(samples []float32) float64 {
	n := len(samples)
	if n != len(voiceWindow) {
		return -120
	}
	x := make([]float64, n)
	windowPower := 0.0
	for i, v := range samples {
		x[i] = float64(v) * voiceWindow[i]
		windowPower += voiceWindow[i] * voiceWindow[i]
	}
	binHz := audio.SampleRate / float64(n)
	power := 0.0
	for k := int(math.Ceil(voiceLow / binHz)); float64(k)*binHz <= voiceHigh; k++ {
		coeff := 2 * math.Cos(2*math.Pi*float64(k)/float64(n))
		var s1, s2 float64
		for _, v := range x {
			s1, s2 = v+coeff*s1-s2, s1
		}
		power += s1*s1 + s2*s2 - coeff*s1*s2
	}
	// By Parseval, the bins on both sides of the spectrum add up to n times
	// the windowed signal's energy; this gives the mean square of the band.
	meanSquare := 2 * power / (float64(n) * windowPower)
	return 10 * math.Log10(max(2*meanSquare, 1e-12))
}

// follow moves value towards target: up at once, down smoothly.
func follow(value, target float64, dt time.Duration) float64 {
	if target >= value {
		return target
	}
	value = target + (value-target)*math.Exp(-dt.Seconds()/release.Seconds())
	if value < 0.005 {
		return 0
	}
	return value
}

// chatView is the chat viewport with the visualiser drawn on its right side.
func (m *Model) chatView() string {
	view := m.viewport.View()
	width := m.viewport.Width
	levels := m.visualizer.levels
	style := m.visualizerStyle()
	if style == config.VisualizerOff || width < visualizerMinWidth || len(levels) == 0 ||
		lipgloss.ColorProfile() == termenv.Ascii {
		return view
	}
	lines := strings.Split(view, "\n")
	for i := range lines {
		if i >= len(levels) {
			break
		}
		switch style {
		case config.VisualizerGradient:
			length := 1 + m.visualizer.blocks[i]*(blockMax-1)
			lines[i] = overlayBar(lines[i], width, int(math.Ceil(length)), func(d int) string {
				return m.gradientColor(i, len(levels), blockCover(d, length))
			})
			continue
		case config.VisualizerBlocks:
			n := blockCells(m.visualizer.blocks[i])
			top := max(int(math.Round(m.visualizer.caps[i])), n) - 1 // the cap's cell
			lines[i] = overlayBar(lines[i], width, top+1, func(d int) string {
				switch {
				case d == top:
					return m.capColor(i, len(levels))
				case d < n:
					return m.blockColor(i, len(levels), d, n)
				}
				return ""
			})
			continue
		}
		halves := 1 + int(math.Round(levels[i]*(lineMax-1)))
		lines[i] = drawLine(lines[i], width, halves, m.lineColor(i, len(levels)))
	}
	return strings.Join(lines, "\n")
}

// drawLine puts a line halves half-cells long against the right edge of a
// chat line, in its blank part. The chat text always wins: the line is cut
// back to leave a space after it.
func drawLine(line string, width, halves int, color string) string {
	text := strings.TrimRight(ansi.Strip(line), " ")
	used := ansi.StringWidth(text)
	if used > 0 {
		used++ // keep a space between the text and the line
	}
	prefix := ansi.Truncate(line, used, "")
	var out strings.Builder
	out.WriteString(prefix)
	out.WriteString("\x1b[0m")
	colored := false
	for col := ansi.StringWidth(prefix); col < width; col++ {
		glyph := ""
		if col >= used {
			glyph = lineGlyph(col, width, halves)
		}
		if glyph == "" {
			out.WriteByte(' ')
			continue
		}
		if !colored {
			out.WriteString(color)
			colored = true
		}
		out.WriteString(glyph)
	}
	if colored {
		out.WriteString("\x1b[39m")
	}
	return out.String()
}

// lineGlyph is what column col shows of a line halves half-cells long that
// ends at the right edge of a row width cells wide. The line fills the lower
// half of its cells; an odd length starts half way into a cell, drawn with a
// lower-right quadrant.
func lineGlyph(col, width, halves int) string {
	start := 2*width - halves // in half cells
	switch {
	case start <= 2*col:
		return "▄"
	case start == 2*col+1:
		return "▗"
	}
	return ""
}

// lineColor is the foreground for row i of n, blending from the top colour
// to the bottom one.
func (m *Model) lineColor(i, n int) string {
	key := [4]int{0, i, n, 0}
	if seq, ok := m.visualizer.colors[key]; ok {
		return seq
	}
	seq := colorSequence(m.visualizer.rowTint(i, n), false)
	m.visualizer.colors[key] = seq
	return seq
}

// blockFade is how sharply a block bar fades towards its tip: its opacity at
// a fraction x of the way from the edge to the tip is 1-x^blockFade, so the
// body stays strong and the last part thins out.
const blockFade = 1.5

// blockCover is how much of the cell d columns in from the right edge a bar
// length cells long covers, weighted by the fade: the integral of the
// opacity profile over the part of the cell inside the bar. As the bar grows
// by a fraction of a cell, its cells darken by a fraction, so it moves as
// smoothly as the level does instead of in whole cells.
func blockCover(d int, length float64) float64 {
	a, b := float64(d), min(float64(d+1), length)
	if b <= a {
		return 0
	}
	integral := func(x float64) float64 {
		return x - math.Pow(x/length, blockFade+1)*length/(blockFade+1)
	}
	return integral(b) - integral(a)
}

// gradientColor is the background for a cell on row i of n that a gradient
// bar covers by cover (0–1): the row's colour laid over the background with
// that much of the edge opacity. Cells too faint to tell from the background
// are left alone, because a cell barely off it reads as a smudge on a
// translucent terminal.
func (m *Model) gradientColor(i, n int, cover float64) string {
	const steps = 48
	q := int(math.Round(cover * steps))
	if q < 4 {
		return ""
	}
	s := &m.visualizer
	return s.cached([4]int{1, i, n, q}, func() colorful.Color {
		return s.bg.BlendRgb(s.rowTint(i, n), s.edgeA*float64(q)/steps)
	})
}

// blockColor is the background for the cell d columns in from the right edge
// of a block bar length cells long on row i of n, a little stronger towards
// the tip.
func (m *Model) blockColor(i, n, d, length int) string {
	s := &m.visualizer
	return s.cached([4]int{2, i, n, d<<8 | length}, func() colorful.Color {
		alpha := s.bodyA + (s.capA-s.bodyA)*0.5*float64(d)/float64(length)
		return s.bg.BlendRgb(s.rowTint(i, n), alpha)
	})
}

// capColor is the background of the cap on row i of n.
func (m *Model) capColor(i, n int) string {
	s := &m.visualizer
	return s.cached([4]int{3, i, n, 0}, func() colorful.Color {
		return s.bg.BlendRgb(s.rowTint(i, n), s.capA)
	})
}

// cached returns the background sequence for key, working it out with color
// the first time.
func (s *visualizerState) cached(key [4]int, color func() colorful.Color) string {
	if seq, ok := s.colors[key]; ok {
		return seq
	}
	seq := colorSequence(color(), true)
	if len(s.colors) > 8192 {
		clear(s.colors)
	}
	s.colors[key] = seq
	return seq
}

func (s *visualizerState) rowTint(i, n int) colorful.Color {
	return s.top.BlendHcl(s.bottom, float64(i)/float64(max(n-1, 1))).Clamped()
}

func colorSequence(c colorful.Color, background bool) string {
	code := lipgloss.ColorProfile().Color(c.Hex()).Sequence(background)
	if code == "" {
		return ""
	}
	return "\x1b[" + code + "m"
}

// overlayBar gives the last n columns of a line of the given width the
// background bg(d), where d counts columns in from the right edge; "" leaves
// a cell with the terminal's own. Cells that already have a background, such
// as fan medals, keep it.
func overlayBar(line string, width, n int, bg func(d int) string) string {
	start := width - n
	var out strings.Builder
	col := 0
	ownBg := false // the line has set a background of its own
	current := ""  // the bar background currently in effect
	setBg := func(seq string) {
		if seq == current {
			return
		}
		if seq == "" {
			out.WriteString("\x1b[49m")
		} else {
			out.WriteString(seq)
		}
		current = seq
	}
	put := func(seq string, w int) {
		if !ownBg && col >= start {
			setBg(bg(width - 1 - col))
		}
		out.WriteString(seq)
		col += w
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
		put(seq, w)
	}
	for col < width {
		put(" ", 1)
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
