package tui

import "testing"

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
