//go:build !darwin && !windows

package browser

import "os/exec"

func open(url string) (Tabs, error) {
	return nil, exec.Command("xdg-open", url).Start()
}

var privateCommands = [][]string{
	{"google-chrome", "--incognito"},
	{"chromium", "--incognito"},
	{"microsoft-edge", "--inprivate"},
	{"brave-browser", "--incognito"},
	{"firefox", "--private-window"},
}

func openPrivate(url string) (Tabs, error) {
	for _, c := range privateCommands {
		if path, err := exec.LookPath(c[0]); err == nil {
			return nil, exec.Command(path, c[1], url).Start()
		}
	}
	return nil, ErrNoPrivateBrowser
}
