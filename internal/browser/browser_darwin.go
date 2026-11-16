package browser

import (
	"encoding/json"
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

func open(url string) error {
	return exec.Command("open", url).Run()
}

func openPrivate(url string) error {
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
			return nil
		}
	}
	return ErrNoPrivateBrowser
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
