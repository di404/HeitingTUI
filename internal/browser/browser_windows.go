package browser

import (
	"os/exec"
	"strings"
)

func open(url string) (Tabs, error) {
	return nil, exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func openPrivate(url string) (Tabs, error) {
	exe, flag := "msedge", "--inprivate" // Edge ships with Windows.
	progID := defaultProgID()
	switch {
	case strings.HasPrefix(progID, "ChromeHTML"):
		exe, flag = "chrome", "--incognito"
	case strings.HasPrefix(progID, "FirefoxURL"):
		exe, flag = "firefox", "-private-window"
	case strings.HasPrefix(progID, "BraveHTML"):
		exe, flag = "brave", "--incognito"
	}
	// start resolves browsers through the App Paths registry key.
	if exec.Command("cmd", "/c", "start", "", exe, flag, url).Run() != nil {
		return nil, ErrNoPrivateBrowser
	}
	return nil, nil
}

func defaultProgID() string {
	out, err := exec.Command("reg", "query",
		`HKCU\Software\Microsoft\Windows\Shell\Associations\UrlAssociations\https\UserChoice`,
		"/v", "ProgId").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}
