package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/di404/HeitingTUI/internal/bili"
	"github.com/di404/HeitingTUI/internal/config"
)

var (
	accentColor = lipgloss.Color("#F87299")
	dimColor    = lipgloss.AdaptiveColor{Light: "#8A8A8A", Dark: "#6C6C6C"}
	greenColor  = lipgloss.AdaptiveColor{Light: "#1E8E4E", Dark: "#73F59F"}
	redColor    = lipgloss.AdaptiveColor{Light: "#C62828", Dark: "#FF6B6B"}

	titleStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(dimColor)
	accentStyle   = lipgloss.NewStyle().Foreground(accentColor)
	selectedStyle = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	sectionStyle  = lipgloss.NewStyle().Foreground(dimColor).Bold(true)
	greenStyle    = lipgloss.NewStyle().Foreground(greenColor)
	redStyle      = lipgloss.NewStyle().Foreground(redColor)
)

var shipNames = map[int]string{1: "总", 2: "提", 3: "舰"}

// shipWidth is the terminal cells taken by one of shipNames.
const shipWidth = 2

// renderDanmu draws one message, wrapped to width.
func renderDanmu(d *bili.Danmu, cfg *config.Config, width int) string {
	var line strings.Builder
	if cfg.ShowShipLevel {
		line.WriteString(renderShip(d.Medal, cfg))
		// With no medal parts to follow, separate the column from the name.
		if !cfg.ShowMedalName && !cfg.ShowMedalLevel {
			line.WriteString("\u00a0")
		}
	}
	if d.Medal != nil {
		line.WriteString(renderMedal(d.Medal, cfg))
	}
	line.WriteString(colored(d.Name+":", d.NameColor, cfg))
	line.WriteByte(' ')
	line.WriteString(colored(d.Content, d.ContentColor, cfg))
	if width <= 0 {
		return line.String()
	}
	return ansi.Wrap(line.String(), width, "")
}

func colored(text, color string, cfg *config.Config) string {
	if color == "" || !cfg.ColorMode {
		return text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(text)
}

// renderShip draws the ship badge column. Senders without a ship get blank
// cells of the same width, so every line starts its medal or name in one column.
func renderShip(medal *bili.Medal, cfg *config.Config) string {
	if medal != nil {
		if ship := shipNames[medal.ShipLevel]; ship != "" {
			return colored(ship, "#F87299", cfg)
		}
	}
	// Non-breaking spaces: ordinary ones are a wrap point, which would leave
	// the first line empty before a long name.
	return strings.Repeat("\u00a0", shipWidth)
}

func renderMedal(medal *bili.Medal, cfg *config.Config) string {
	var out strings.Builder
	if cfg.ShowMedalName {
		style := lipgloss.NewStyle()
		if cfg.ColorMode {
			style = style.Foreground(lipgloss.Color("#FAFAFA")).Background(lipgloss.Color(medal.Color))
		}
		out.WriteString(style.Render(medal.Name))
	}
	if cfg.ShowMedalLevel {
		style := lipgloss.NewStyle()
		if cfg.ColorMode {
			style = style.Foreground(lipgloss.Color("#3C3C3C")).Background(lipgloss.Color("#FAFAFA"))
		}
		out.WriteString(style.Render(fmt.Sprintf("%2d", medal.Level)))
	}
	if out.Len() == 0 {
		return ""
	}
	return out.String() + " "
}

// truncate shortens text with ANSI styling to fit width.
func truncate(text string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(text, width, "…")
}

// fitLines pads or cuts lines to exactly height rows.
func fitLines(lines []string, height int) string {
	if len(lines) > height {
		lines = lines[:max(height, 0)]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// scrollWindow returns the slice of rows to display so the cursor stays visible.
func scrollWindow(cursor, total, height int) (int, int) {
	if total <= height {
		return 0, total
	}
	start := max(cursor-height/2, 0)
	if start+height > total {
		start = total - height
	}
	return start, start + height
}

