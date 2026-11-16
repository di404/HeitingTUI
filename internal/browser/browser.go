// Package browser opens web pages in the user's browser, optionally in a
// private window so the browser's saved login is not used.
package browser

import "errors"

// ErrNoPrivateBrowser means no installed browser could be started in a
// private window. Callers must not fall back to a normal window.
var ErrNoPrivateBrowser = errors.New("没有找到支持无痕窗口的浏览器")

// Open opens url in the default browser.
func Open(url string) error { return open(url) }

// OpenPrivate opens url in a private window. It prefers the default browser
// and otherwise uses another installed browser that supports private windows.
func OpenPrivate(url string) error { return openPrivate(url) }
