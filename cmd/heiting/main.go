package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/di404/HeitingTUI/internal/audio"
	"github.com/di404/HeitingTUI/internal/bili"
	"github.com/di404/HeitingTUI/internal/browser"
	"github.com/di404/HeitingTUI/internal/config"
	"github.com/di404/HeitingTUI/internal/ime"
	"github.com/di404/HeitingTUI/internal/tui"
)

const maxLogSize = 5 << 20

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var initialRoom uint64
	if len(os.Args) > 2 {
		return fmt.Errorf("用法: %s [房间号或直播间链接]", filepath.Base(os.Args[0]))
	}
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			fmt.Print(usage(filepath.Base(os.Args[0])))
			return nil
		}
		id, ok := bili.ParseRoomID(os.Args[1])
		if !ok {
			return fmt.Errorf("无效的房间号或直播间链接: %s", os.Args[1])
		}
		initialRoom = id
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if closeLog := setupLog(); closeLog != nil {
		defer closeLog()
	}

	cookies := ""
	if !cfg.Anonymous {
		cookies = config.LoadCookie()
	}
	client := bili.New(cfg.UserAgent, cookies)
	player := audio.NewPlayer(client.ResolveAudio, tui.AudioOptions(cfg))
	defer player.Close()

	model := tui.New(cfg, client, player, initialRoom)
	defer model.Close()

	model.IMEControl = ime.SetEnabled
	model.OpenBrowser = func(url string, private bool) (func() ([]string, error), error) {
		open := browser.Open
		if private {
			open = browser.OpenPrivate
		}
		return open(url)
	}
	defer ime.SetEnabled(true)

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithReportFocus())

	// Closing the terminal window sends SIGHUP, which Bubble Tea does not handle.
	// Quitting normally is what puts the input method back.
	hangup := make(chan os.Signal, 1)
	signal.Notify(hangup, syscall.SIGHUP)
	go func() {
		<-hangup
		program.Quit()
	}()

	_, err = program.Run()
	return err
}

func usage(name string) string {
	return fmt.Sprintf(`用法:
  %[1]s                                 打开房间列表
  %[1]s <房间号>                        按房间号打开
  %[1]s <https://live.bilibili.com/..>  按链接打开
  %[1]s -h, --help                      显示帮助

按键:
  tab         切换弹幕输入 / 返回
  m           静音
  + -         调节音量
  o           在浏览器打开直播间
  r           房间列表
  s           设置
  ↑ ↓ j k     滚动弹幕
  q           退出
`, name)
}

// setupLog sends logs to a file; anything on stderr would corrupt the screen.
func setupLog() func() {
	dir, err := config.CacheDir()
	if err != nil {
		return nil
	}
	path := filepath.Join(dir, "heiting.log")
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogSize {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return nil
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(file, nil)))
	return func() { file.Close() }
}
