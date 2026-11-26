package tui

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	// pagePollInterval is how often the browser's tabs are listed.
	pagePollInterval = 2 * time.Second
	// pageAppearTimeout bounds the wait for the opened page to show up among
	// the tabs. A page never seen cannot be seen closing, so the mute stays.
	pageAppearTimeout = 20 * time.Second
)

// pageWatch follows the room page opened in the browser, so the page mute
// lifts once no tab shows the room any more.
type pageWatch struct {
	gen    int // bumps on every open, so ticks of an older watch are dropped
	tabs   func() ([]string, error)
	rooms  []uint64
	opened time.Time
	seen   bool
}

type (
	pageTickMsg struct{ gen int }
	pageTabsMsg struct {
		gen  int
		urls []string
		err  error
	}
)

func (m *Model) watchPage(tabs func() ([]string, error), rooms []uint64) tea.Cmd {
	m.page = pageWatch{gen: m.page.gen + 1, tabs: tabs, rooms: rooms, opened: time.Now()}
	if tabs == nil {
		return nil
	}
	return m.pageTick()
}

func (m *Model) pageTick() tea.Cmd {
	gen := m.page.gen
	return tea.Tick(pagePollInterval, func(time.Time) tea.Msg { return pageTickMsg{gen: gen} })
}

func (m *Model) updatePage(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case pageTickMsg:
		// Unmuting by hand ends the watch.
		if msg.gen != m.page.gen || !m.pageMuted {
			return nil
		}
		gen, tabs := m.page.gen, m.page.tabs
		return func() tea.Msg {
			urls, err := tabs()
			return pageTabsMsg{gen: gen, urls: urls, err: err}
		}
	case pageTabsMsg:
		if msg.gen != m.page.gen || !m.pageMuted {
			return nil
		}
		if msg.err != nil {
			// Usually the user declined to let this program control the browser.
			slog.Warn("list browser tabs", "err", msg.err)
			return nil
		}
		switch open := m.page.showsRoom(msg.urls); {
		case open:
			m.page.seen = true
		case m.page.seen:
			m.pageMuted = false
			m.player.Configure(m.audioOptions())
			return nil
		case time.Since(m.page.opened) > pageAppearTimeout:
			return nil
		}
		return m.pageTick()
	}
	return nil
}

// showsRoom reports whether any URL is a page of the watched room.
func (p *pageWatch) showsRoom(urls []string) bool {
	for _, u := range urls {
		for _, room := range p.rooms {
			if isRoomURL(u, room) {
				return true
			}
		}
	}
	return false
}

// isRoomURL matches live.bilibili.com/<room> under any scheme or subdomain and
// with any path, query or fragment after it, but not a longer number that starts with the same digits.
func isRoomURL(url string, room uint64) bool {
	const host = "live.bilibili.com/"
	i := strings.Index(url, host)
	if i < 0 {
		return false
	}
	rest := url[i+len(host):]
	id := strconv.FormatUint(room, 10)
	if !strings.HasPrefix(rest, id) {
		return false
	}
	rest = rest[len(id):]
	return rest == "" || strings.IndexByte("/?#", rest[0]) >= 0
}
