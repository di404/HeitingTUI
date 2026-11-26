package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// privateFlags maps a browser's bundle ID (lower case) to the command-line
// flag that opens a private window. Safari has no such flag.
var privateFlags = []struct{ bundleID, flag string }{
	{"com.google.chrome", "--incognito"},
	{"com.microsoft.edgemac", "--inprivate"},
	{"org.mozilla.firefox", "--private-window"},
	{"com.brave.browser", "--incognito"},
	{"com.vivaldi.vivaldi", "--incognito"},
	{"org.chromium.chromium", "--incognito"},
	{"com.operasoftware.opera", "--private"},
}

// scriptable lists the browsers (lower-case bundle IDs) whose AppleScript
// dictionary gives every window's tabs a URL. Firefox and Opera have none.
var scriptable = map[string]bool{
	"com.apple.safari":           true,
	"com.google.chrome":          true,
	"com.microsoft.edgemac":      true,
	"com.brave.browser":          true,
	"com.vivaldi.vivaldi":        true,
	"org.chromium.chromium":      true,
	"company.thebrowser.browser": true, // Arc
}

func open(url string) (Tabs, error) {
	if err := exec.Command("open", url).Run(); err != nil {
		return nil, err
	}
	def := defaultBrowser()
	if def == "" {
		def = "com.apple.safari"
	}
	return tabsOf(def), nil
}

func openPrivate(url string) (Tabs, error) {
	order := make([]struct{ bundleID, flag string }, 0, len(privateFlags))
	def := defaultBrowser()
	for _, b := range privateFlags {
		if b.bundleID == def {
			order = append(order, b)
		}
	}
	for _, b := range privateFlags {
		if b.bundleID != def {
			order = append(order, b)
		}
	}
	for _, b := range order {
		// open fails when no app has this bundle ID, so the next one is tried.
		if exec.Command("open", "-n", "-b", b.bundleID, "--args", b.flag, url).Run() == nil {
			return tabsOf(b.bundleID), nil
		}
	}
	return nil, ErrNoPrivateBrowser
}

// tabsOf asks the browser for its tab URLs through AppleScript, or returns nil
// when the browser cannot be asked. The first call makes macOS ask the user to
// let this program control the browser; a refusal surfaces as an error.
func tabsOf(bundleID string) Tabs {
	if !scriptable[bundleID] {
		return nil
	}
	// The running check comes first: telling an app that has quit launches it.
	script := fmt.Sprintf(`if application id %[1]q is not running then return ""
set out to {}
tell application id %[1]q
	repeat with w in windows
		set out to out & (URL of tabs of w)
	end repeat
end tell
set AppleScript's text item delimiters to linefeed
return out as text`, bundleID)
	return func() ([]string, error) {
		out, err := exec.Command("osascript", "-e", script).Output()
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) && len(exit.Stderr) > 0 {
				return nil, fmt.Errorf("%s", strings.TrimSpace(string(exit.Stderr)))
			}
			return nil, err
		}
		var urls []string
		for _, line := range strings.Split(string(out), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				urls = append(urls, line)
			}
		}
		return urls, nil
	}
}

// defaultBrowser returns the lower-case bundle ID of the https handler, or ""
// when it cannot be read (the system default, Safari, is then assumed).
func defaultBrowser() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	plist := filepath.Join(home, "Library/Preferences/com.apple.LaunchServices/com.apple.launchservices.secure.plist")
	out, err := exec.Command("plutil", "-convert", "json", "-o", "-", plist).Output()
	if err != nil {
		return ""
	}
	var prefs struct {
		LSHandlers []struct {
			Scheme string `json:"LSHandlerURLScheme"`
			Role   string `json:"LSHandlerRoleAll"`
		}
	}
	if json.Unmarshal(out, &prefs) != nil {
		return ""
	}
	for _, h := range prefs.LSHandlers {
		if h.Scheme == "https" {
			return strings.ToLower(h.Role)
		}
	}
	return ""
}
