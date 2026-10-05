// 命令 kxapp-demo 用 KXFLOW 引擎渲染**真实 KQFLOW 数据**。
//
// 它是 M3 的可运行验收：同一份 kqflow-data/，既可以被 v2.1.0 的原型界面打开，
// 也可以被这条命令用引擎渲染——证明适配层真的把业务数据接了上去，
// 而不是只跑通了测试夹具。
//
// 用法：
//
//	kxapp-demo                    用默认数据目录进入交互界面
//	kxapp-demo -data-dir <路径>   指定数据目录
//	kxapp-demo -render -w -h      离屏渲染一帧后退出（CI / 无终端环境用）
//
// 它**不修改任何数据**（除非你在界面里真的改了东西并触发保存）。
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/kxflow"
	"github.com/kqin-dev/kxflow/plugin"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/config"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/kxapp"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "kxapp-demo:", err)
		os.Exit(1)
	}
}

func run() error {
	render := flag.Bool("render", false, "离屏渲染一帧后退出（不进入交互）")
	width := flag.Int("w", 120, "离屏渲染的终端宽度")
	height := flag.Int("h", 40, "离屏渲染的终端高度")
	dataDir := flag.String("data-dir", "", "指定数据目录（覆盖配置）")
	seed := flag.Bool("seed", false, "向数据目录写入几条示例数据（仅在它为空时动手）")
	flag.Parse()

	// 与 cmd/kqf 一样的装配顺序：先解析路径，再读配置，最后打开数据层。
	// 顺序不能换：数据版本检查必须排在 Open 之前（Open 会建目录）。
	//
	// `-data-dir` 通过与 cmd/kqf 相同的 KQFLOW_HOME 通道生效，
	// 而不是在这里另写一套路径逻辑——否则"演示程序看到的数据目录"
	// 与"正式程序看到的"会是两套规则，排查问题时极易混淆。
	if *dataDir != "" {
		if err := os.Setenv("KQFLOW_HOME", *dataDir); err != nil {
			return err
		}
	}
	paths, err := config.Resolve(nil)
	if err != nil {
		return err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return err
	}

	if err := store.CheckDataVersion(paths.Root); err != nil {
		return err
	}
	st, err := store.Open(paths.Root)
	if err != nil {
		return err
	}

	now := func() time.Time { return clock.NewWith(time.Now, cfg.Location()).Now() }
	src, err := kxapp.NewStoreSource(st, cfg, now)
	if err != nil {
		return err
	}
	if *seed {
		if err := seedDemoData(src); err != nil {
			return err
		}
	}

	loader := kxapp.NewLoader(src, cfg)
	m, _, rep := loader.Build()

	if *render {
		m.Resize(*width, *height)
		fmt.Println(m.View())
		fmt.Fprintln(os.Stderr, "--- 装载报告 ---")
		fmt.Fprintln(os.Stderr, rep.Explain())
		// 画布诊断：正常路径下必须干净（越界与覆盖都是 0）。
		fmt.Fprintf(os.Stderr, "--- 画布诊断 --- %s\n", m.Diagnostics())
		return nil
	}

	p := tea.NewProgram(adapter{model: m}, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	fmt.Println("装载报告：")
	fmt.Println(rep.Explain())
	return nil
}

// adapter 把引擎接到 bubbletea（与引擎演示里的那一层相同，只做消息转换）。
type adapter struct{ model *kxflow.Model }

type tickMsg struct{}

func tick() tea.Cmd { return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} }) }

func (a adapter) Init() tea.Cmd { return tick() }

func (a adapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.model.Resize(m.Width, m.Height)
		return a, nil
	case tickMsg:
		return a, tick()
	case tea.KeyMsg:
		ev := plugin.Event{Kind: plugin.EventKey, Key: m.String()}
		if m.Type == tea.KeyRunes {
			ev.Runes = m.Runes
		}
		if m.Paste {
			ev.Kind = plugin.EventPaste
		}
		if quit := a.model.Dispatch(ev); quit {
			a.model.Dispose()
			return a, tea.Quit
		}
	}
	return a, nil
}

func (a adapter) View() string { return a.model.View() }

// seedDemoData 往数据目录里写几条示例数据。
//
// 只在**当天还没有任何条目**时动手：这条命令是拿来看渲染效果的，
// 绝不能把用户已有的数据改掉（"删除任何东西之前先确认它是程序产生的
// 还是用户的"——写也一样）。
func seedDemoData(src kxapp.Source) error {
	data := src.Day()
	if data == nil {
		return nil
	}
	if len(data.Fixed)+len(data.Floating) > 0 || len(src.Goals()) > 0 {
		return nil // 已经有数据，不动
	}
	now := src.Now()
	fixed := []string{"写架构设计文档", "回复邮件", "复盘昨天的专注记录"}
	for _, title := range fixed {
		data.Fixed = append(data.Fixed, model.NewTodo(title, model.KindFixed, data.Day, now))
	}
	data.Floating = append(data.Floating,
		model.NewTodo("临时：整理周报", model.KindFloating, data.Day, now))
	// 给一条设上截止时间，好让 DDL 面板与联动选项都有东西可显示。
	data.Fixed[0].SetDue("18:30")
	data.Note = "今天状态不错，上午专注了两个番茄。\n下午要继续把架构文档写完。"
	if err := src.Save(); err != nil {
		return err
	}
	goals := src.Goals()
	g := model.NewGoal("发布 KQFLOW v3.0.0", now)
	g.SetDue(now.AddDate(0, 1, 0).Format("2006-01-02"))
	goals = append(goals, *g)
	if setter, ok := src.(interface{ SetGoals([]model.Goal) }); ok {
		setter.SetGoals(goals)
	}
	return src.SaveGoals()
}
