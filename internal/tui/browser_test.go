package tui

import (
	"errors"
	"testing"
	"time"
)

func TestOpenRoomPageUsesPrivateWindowWhenAnonymous(t *testing.T) {
	for _, anonymous := range []bool{false, true} {
		m := newTestModel(t)
		m.cfg.RoomID = 42
		m.cfg.Anonymous = anonymous
		var gotURL string
		var gotPrivate bool
		m.OpenBrowser = func(url string, private bool) (func() ([]string, error), error) {
			gotURL, gotPrivate = url, private
			return nil, nil
		}
		cmd := m.handleKey(runes("o"))
		if cmd == nil {
			t.Fatal("o did nothing")
		}
		cmd()
		if gotURL != "https://live.bilibili.com/42" {
			t.Fatalf("url = %q", gotURL)
		}
		if gotPrivate != anonymous {
			t.Fatalf("anonymous=%v opened private=%v", anonymous, gotPrivate)
		}
	}
}

func TestOpenRoomPageMutesThisRunOnly(t *testing.T) {
	for _, fail := range []bool{false, true} {
		m := newTestModel(t)
		m.OpenBrowser = func(string, bool) (func() ([]string, error), error) {
			if fail {
				return nil, errors.New("no browser")
			}
			return nil, nil
		}
		m.update(m.handleKey(runes("o"))())
		if m.muted() == fail {
			t.Fatalf("browser failed=%v, muted=%v", fail, m.muted())
		}
		if m.cfg.Muted {
			t.Fatal("page mute was written to the config")
		}
	}
}

func TestMuteKeyUnmutesPageMute(t *testing.T) {
	m := newTestModel(t)
	m.pageMuted = true
	m.handleKey(runes("m"))
	if m.muted() {
		t.Fatal("m did not unmute")
	}
	m.handleKey(runes("m"))
	if !m.cfg.Muted {
		t.Fatal("m did not mute")
	}
}

// openWithTabs opens the room page with a browser whose tabs are read from
// *urls, and returns the model with the page muted.
func openWithTabs(t *testing.T, urls *[]string, err *error) *Model {
	t.Helper()
	m := newTestModel(t)
	m.cfg.RoomID = 42
	m.OpenBrowser = func(string, bool) (func() ([]string, error), error) {
		return func() ([]string, error) { return *urls, *err }, nil
	}
	m.update(m.handleKey(runes("o"))())
	if !m.pageMuted {
		t.Fatal("opening the page did not mute")
	}
	return m
}

// pollPage runs one tick of the page watch and reports whether it goes on.
func pollPage(m *Model) bool {
	cmd := m.update(pageTickMsg{gen: m.page.gen})
	if cmd == nil {
		return false
	}
	return m.update(cmd()) != nil
}

func TestClosingRoomPageRestoresSound(t *testing.T) {
	var urls []string
	var err error
	m := openWithTabs(t, &urls, &err)

	if !pollPage(m) || !m.pageMuted {
		t.Fatal("watch ended before the page loaded")
	}
	urls = []string{"https://example.com", "https://live.bilibili.com/42?spm_id_from=x"}
	if !pollPage(m) || !m.pageMuted {
		t.Fatal("open page lifted the mute")
	}
	urls = []string{"https://live.bilibili.com/420"}
	if pollPage(m) || m.pageMuted {
		t.Fatal("closed page kept the mute")
	}
}

func TestPageWatchStopsWithoutUnmuting(t *testing.T) {
	t.Run("list fails", func(t *testing.T) {
		urls, err := []string(nil), errors.New("not allowed")
		m := openWithTabs(t, &urls, &err)
		if pollPage(m) || !m.pageMuted {
			t.Fatal("failed tab list should stop the watch and keep the mute")
		}
	})
	t.Run("page never seen", func(t *testing.T) {
		var urls []string
		var err error
		m := openWithTabs(t, &urls, &err)
		m.page.opened = m.page.opened.Add(-pageAppearTimeout - time.Second)
		if pollPage(m) || !m.pageMuted {
			t.Fatal("unseen page should stop the watch and keep the mute")
		}
	})
	t.Run("unmuted by hand", func(t *testing.T) {
		var urls []string
		var err error
		m := openWithTabs(t, &urls, &err)
		m.handleKey(runes("m"))
		if m.update(pageTickMsg{gen: m.page.gen}) != nil {
			t.Fatal("watch kept polling after m")
		}
	})
}

func TestIsRoomURL(t *testing.T) {
	for url, want := range map[string]bool{
		"https://live.bilibili.com/42":          true,
		"https://live.bilibili.com/42/":         true,
		"https://live.bilibili.com/42?a=b":      true,
		"https://live.bilibili.com/42#x":        true,
		"https://m.live.bilibili.com/42":        true,
		"https://live.bilibili.com/421":         false,
		"https://live.bilibili.com/p/eden/area": false,
		"https://www.bilibili.com/42":           false,
	} {
		if got := isRoomURL(url, 42); got != want {
			t.Errorf("isRoomURL(%q) = %v", url, got)
		}
	}
}
