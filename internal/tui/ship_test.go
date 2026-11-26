package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/di404/HeitingTUI/internal/bili"
	"github.com/di404/HeitingTUI/internal/config"
)

func TestShipColumnAligned(t *testing.T) {
	cfg := &config.Config{ShowShipLevel: true, ShowMedalName: true}
	column := func(medal *bili.Medal, want string) int {
		line := ansi.Strip(renderDanmu(&bili.Danmu{Name: "名字", Content: "你好", Medal: medal}, cfg, 40))
		i := strings.Index(line, want)
		if i < 0 {
			t.Fatalf("%q missing from %q", want, line)
		}
		return ansi.StringWidth(line[:i])
	}
	ship := column(&bili.Medal{Name: "牌子", ShipLevel: 3}, "牌子")
	plain := column(&bili.Medal{Name: "牌子"}, "牌子")
	none := column(nil, "名字")
	if ship != shipWidth || plain != ship || none != ship {
		t.Fatalf("columns: ship %d, plain %d, none %d", ship, plain, none)
	}

	long := renderDanmu(&bili.Danmu{Name: strings.Repeat("a", 40), Content: "hi"}, cfg, 24)
	if first, _, _ := strings.Cut(ansi.Strip(long), "\n"); !strings.Contains(first, "a") {
		t.Fatalf("padding left an empty first line: %q", long)
	}

	cfg.ShowMedalName = false
	ship = column(&bili.Medal{ShipLevel: 3}, "名字")
	none = column(nil, "名字")
	if ship != shipWidth+1 || none != ship {
		t.Fatalf("badge only: ship %d, none %d", ship, none)
	}
	cfg.ShowMedalName = true

	cfg.ShowShipLevel = false
	if got := column(nil, "名字"); got != 0 {
		t.Fatalf("padded with ship badge off: %d", got)
	}
}
