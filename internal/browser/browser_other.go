//go:build !darwin && !windows

package browser

import "os/exec"

func open(url string) error {
	return exec.Command("xdg-open", url).Start()
}

var privateCommands = [][]string{
	{"google-chrome", "--incognito"},
	{"chromium", "--incognito"},
	{"microsoft-edge", "--inprivate"},
	{"brave-browser", "--incognito"},
	{"firefox", "--private-window"},
}

func openPrivate(url string) error {
	for _, c := range privateCommands {
		if path, err := exec.LookPath(c[0]); err == nil {
			return exec.Command(path, c[1], url).Start()
		}
	}
	return ErrNoPrivateBrowser
}
