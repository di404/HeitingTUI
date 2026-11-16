package tui

import (
	"errors"
	"testing"
)

func TestOpenRoomPageUsesPrivateWindowWhenAnonymous(t *testing.T) {
	for _, anonymous := range []bool{false, true} {
		m := newTestModel(t)
		m.cfg.RoomID = 42
		m.cfg.Anonymous = anonymous
		var gotURL string
		var gotPrivate bool
		m.OpenBrowser = func(url string, private bool) error {
			gotURL, gotPrivate = url, private
			return nil
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
		m.OpenBrowser = func(string, bool) error {
			if fail {
				return errors.New("no browser")
			}
			return nil
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
