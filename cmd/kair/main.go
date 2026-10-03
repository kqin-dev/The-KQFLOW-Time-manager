// 命令 kair 是 Kairos 时间管理器的入口。
//
// Kairos 是一个无环境依赖、可直接分发的命令行时间管理工具：
// 每日 TODO、长期 GOAL、番茄钟与计时、历史统计，全部数据保存在本地。
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/clock"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/config"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/store"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/ui"
)

// version 会在编译时通过 -ldflags 覆盖。
var version = "0.1.0-dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "kair:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		showVersion = flag.Bool("version", false, "显示版本号后退出")
		showDataDir = flag.Bool("where", false, "显示数据目录后退出")
		dataDir     = flag.String("data-dir", "", "指定数据目录（覆盖配置）")
	)
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Printf("Kairos %s\n", version)
		return nil
	}

	// 先解析路径，再读配置，最后打开数据层。
	cfg := config.Default()
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	paths, err := config.Resolve(cfg)
	if err != nil {
		return err
	}
	if *showDataDir {
		fmt.Println(paths.Root)
		return nil
	}

	loaded, err := config.Load(paths)
	if err != nil {
		return err
	}
	// 命令行显式指定的数据目录优先级最高。
	if *dataDir != "" {
		loaded.DataDir = *dataDir
		if paths, err = config.Resolve(loaded); err != nil {
			return err
		}
	}
	cfg = loaded

	st, err := store.Open(paths.Root)
	if err != nil {
		return err
	}

	app, err := ui.NewApp(ui.Options{
		Store:  st,
		Config: cfg,
		Clock:  clock.NewWith(time.Now, cfg.Location()),
		Paths:  paths,
	})
	if err != nil {
		return err
	}

	// 鼠标支持默认开启，方便点击与滚轮；某些终端下会妨碍选中文本，因此可关。
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if !cfg.DisableMouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	prog := tea.NewProgram(app, opts...)
	if _, err := prog.Run(); err != nil {
		return err
	}

	printFarewell(st)
	return nil
}

// printFarewell 在退出后打印当天小结，让用户离开时也有反馈。
func printFarewell(st *store.Store) {
	cfgPaths, err := config.Resolve(config.Default())
	if err != nil {
		return
	}
	cfg, err := config.Load(cfgPaths)
	if err != nil {
		cfg = config.Default()
	}
	day := clock.LogicalDay(clock.New().Now(), cfg.Cutoff())
	data, err := st.Day(day)
	if err != nil || data == nil {
		return
	}
	done, total := data.Counts()
	focus, _ := data.FocusTotal()
	fmt.Printf("\nKairos · %s · 完成 %d/%d 项待办 · 今日专注 %s\n",
		day, done, total, clock.HumanDuration(focus))
}

func usage() {
	fmt.Fprintf(os.Stderr, `Kairos — 每天向前一点的时间管理器

用法:
  kair [选项]

选项:
  -data-dir <路径>   指定数据目录（默认放在可执行文件同级的 kairos-data）
  -where             只打印数据目录后退出
  -version           只打印版本号后退出
  -h, -help          显示本帮助

数据目录也可以用环境变量 KAIROS_HOME 指定。

看板操作:
  tab 切换栏位 · j/k 移动 · space 勾选 · a 添加 · t 加子任务 · r 继承昨日 · ? 帮助 · q 退出

计时:
  中间栏按 enter 选择番茄钟 / 倒计时 / 正计时 / 自定义，计时中 space 暂停、enter 归档、esc 中断
`)
}
