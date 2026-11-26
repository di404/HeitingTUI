// Package browser opens web pages in the user's browser, optionally in a
// private window so the browser's saved login is not used.
package browser

import "errors"

// ErrNoPrivateBrowser means no installed browser could be started in a
// private window. Callers must not fall back to a normal window.
var ErrNoPrivateBrowser = errors.New("没有找到支持无痕窗口的浏览器")

// Tabs lists the URLs of every tab open in the browser a page went to, across
// all of its windows. A browser that is no longer running has no tabs.
type Tabs func() ([]string, error)

// Open opens url in the default browser. tabs is nil when that browser's tabs
// cannot be listed.
func Open(url string) (tabs Tabs, err error) { return open(url) }

// OpenPrivate opens url in a private window. It prefers the default browser
// and otherwise uses another installed browser that supports private windows.
func OpenPrivate(url string) (tabs Tabs, err error) { return openPrivate(url) }
