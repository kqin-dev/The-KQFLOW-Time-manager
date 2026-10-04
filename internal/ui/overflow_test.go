package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// assertNoOverflow 检查渲染结果没有超出终端宽高。
//
// 浮层与整页都曾用固定尺寸，终端偏小时会撑破面板边框或顶出可视区域，
// 逼得用户必须全屏才能用。这里对每一行做显示宽度断言，并检查总行数。
// 终端小于 minWidth×minHeight 时看板会主动显示“窗口太小”，那同样是必须
// 装进终端的输出，因此不再跳过。
func assertNoOverflow(t *testing.T, name, out string, width, height int) {
	t.Helper()
	if width <= 0 || height <= 0 {
		return
	}
	lines := strings.Split(out, "\n")
	if len(lines) > height {
		t.Errorf("%s 行数 %d 超过终端高度 %d", name, len(lines), height)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("%s 第 %d 行宽度 %d 超过终端 %d: %q", name, i, w, width, line)
		}
	}
}

// TestVerySmallTerminalFits 验证极小终端下的提示文本本身也不溢出。
//
// 之前这段提示固定输出 5 行、每行 30 多列，在 10×3 的窗口里会糊掉整屏。
func TestVerySmallTerminalFits(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 30, 0, 0, time.Local)
	sizes := [][2]int{{1, 1}, {2, 1}, {5, 2}, {10, 3}, {20, 5}, {40, 10}, {59, 15}, {59, 40}, {100, 15}}
	for _, s := range sizes {
		app, _, _ := newTestApp(t, at)
		app.width, app.height = s[0], s[1]
		out := app.View()
		if out == "" {
			t.Errorf("%dx%d：渲染为空", s[0], s[1])
		}
		assertNoOverflow(t, fmt.Sprintf("小终端 %dx%d", s[0], s[1]), out, s[0], s[1])
	}
}

// TestNoViewOverflowsTerminal 在所有界面 × 多种终端宽度下检查不溢出。
func TestNoViewOverflowsTerminal(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 30, 0, 0, time.Local)
	widths := []int{60, 62, 70, 80, 100, 120, 160}
	heights := []int{20, 30, 44}

	for _, w := range widths {
		for _, h := range heights {
			// 每个尺寸都用全新的 App，避免状态互相影响。
			app, s, _ := newTestApp(t, at)
			app.width, app.height = w, h

			seedForViews(t, app, s, at)

			// 主看板。
			assertNoOverflow(t, "看板", app.View(), w, h)

			// 整页。
			app.view = ViewSettings
			assertNoOverflow(t, "设置页", app.View(), w, h)
			app.view = ViewHistory
			assertNoOverflow(t, "历史页", app.View(), w, h)
			app.view = ViewHelp
			assertNoOverflow(t, "帮助页", app.View(), w, h)
			app.view = ViewDashboard

			// 浮层：选择框。
			app.pick = &pickState{
				title: "选择计时方式",
				items: []pickItem{
					{Label: "番茄钟（专注 + 休息）", Action: "a"},
					{Label: "一段特别长的选项名称用来试探边框是否会被撑破", Action: "b"},
					{Label: "取消", Action: "cancel"},
				},
			}
			assertNoOverflow(t, "选择框", app.View(), w, h)
			app.pick = nil

			// 浮层：文本输入。
			app.editor.set("一个相当长的输入框标题用来试探边框", "一些已输入的旧内容")
			assertNoOverflow(t, "输入框", app.View(), w, h)
			app.editor = editorState{}

			// 浮层：自定义时段编辑器。
			app.openCustom()
			app.custom.plan.Segments = []model.Segment{
				{Name: "一个非常非常长的时段名字", Kind: "focus", Dur: 45 * time.Minute},
				{Name: "短暂休息", Kind: "break", Dur: 5 * time.Minute},
				{Name: "自定义状态", Kind: "other", Dur: 10 * time.Minute},
			}
			assertNoOverflow(t, "自定义时段", app.View(), w, h)
			app.custom = nil

			// 浮层：继承确认页。
			app.view = ViewCarry
			assertNoOverflow(t, "继承确认页", app.View(), w, h)
			app.view = ViewDashboard

			// 浮层：标签页（见需求 1）。一条带很多标签的长标题最容易撑破边框。
			app.openLabels()
			if app.labelView.active {
				assertNoOverflow(t, "标签页", app.View(), w, h)
				// 满载标签时也要装得下。
				app.labelView.target.SetItemLabels([]string{
					"一个相当长的自定义标签名字", "星星", "紧急", "爱心",
				})
				assertNoOverflow(t, "标签页（满载）", app.View(), w, h)
			}
			app.closeLabels()

			// 浮层：DDL 设置页（见需求 4）。
			if entries := app.dueEntries(); len(entries) == 0 {
				// 没有设 DDL 的条目时页面会被拒绝打开，先给一条设上。
				app.data.Floating = append(app.data.Floating, model.NewTodo(
					"一个名字相当长的待办用来试探 DDL 页宽度", model.KindFloating, "2026-10-03", at))
				app.data.Floating[len(app.data.Floating)-1].SetDue("23:59")
				app.focus = FocusFloating
			}
			app.openDdl()
			if app.ddlView.active {
				assertNoOverflow(t, "DDL 设置页", app.View(), w, h)
			}
			app.closeDdl()

			// 浮层：计时菜单（第二级菜单，含方向键选择）。
			app.startTimer()
			assertNoOverflow(t, "计时菜单", app.View(), w, h)
			app.pick = nil

			// 浮层：退出确认。
			app.askQuit()
			assertNoOverflow(t, "退出确认", app.View(), w, h)
			app.pick = nil

			// 浮层：计时结束确认（见已知 bug 1）。计时进行中的状态也要覆盖。
			app.beginTimer(model.Plan{Kind: model.TimerCountUp, Segments: []model.Segment{
				{Name: "自由专注", Kind: model.SegmentKindFocus},
			}}, "")
			app.askStopTimer()
			assertNoOverflow(t, "计时结束确认", app.View(), w, h)
			app.stopAsk = false

			// 浮层：计时中打开的计时菜单。
			app.openTimerMenu()
			assertNoOverflow(t, "计时中菜单", app.View(), w, h)
			app.pick = nil

			// 计时进行中的看板本身（进度条与提示都要装得下）。
			assertNoOverflow(t, "计时中的看板", app.View(), w, h)
			app.timer = nil

			// 浮层：庆祝特效。
			app.celebrate = &celebrateState{started: time.Now()}
			assertNoOverflow(t, "庆祝特效", app.View(), w, h)
		}
	}
}

// TestHelpPageFitsWithoutFullscreen 验证帮助页不需要全屏也能完整显示（见问题 2）。
func TestHelpPageFitsWithoutFullscreen(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 30, 0, 0, time.Local)
	// 常见终端尺寸，包含偏小的笔记本窗口。
	for _, size := range [][2]int{{100, 40}, {100, 30}, {120, 35}, {80, 24}, {90, 28}, {140, 45}} {
		app, _, _ := newTestApp(t, at)
		app.width, app.height = size[0], size[1]
		app.view = ViewHelp
		out := app.View()
		lines := strings.Split(out, "\n")
		if len(lines) > size[1] {
			t.Errorf("%dx%d：帮助页行数 %d 超出终端", size[0], size[1], len(lines))
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > size[0] {
				t.Errorf("%dx%d：帮助页第 %d 行宽 %d 超出终端", size[0], size[1], i, w)
			}
		}
		if !strings.Contains(out, "帮助") {
			t.Errorf("%dx%d：帮助页应包含标题", size[0], size[1])
		}
	}
}

// TestHelpPageScrolls 验证帮助页内容超出时可以用 j/k 滚动查看。
func TestHelpPageScrolls(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 30, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	// 故意用很矮的终端，逼出滚动。
	app.width, app.height = 100, minHeight
	app.view = ViewHelp
	app.pageScroll = 0
	first := app.View()

	press(t, app, "j", "j", "j")
	if app.pageScroll == 0 {
		t.Fatal("按 j 应向下滚动帮助内容")
	}
	if app.View() == first {
		t.Error("滚动后内容应有变化")
	}
	// 滚回顶部。
	press(t, app, "g")
	if app.pageScroll != 0 {
		t.Errorf("按 g 应回到顶部，实际 %d", app.pageScroll)
	}
	if app.View() != first {
		t.Error("回到顶部后内容应与初始一致")
	}
}

// TestSecondLevelMenuInCenterColumn 验证二级菜单只占用中间栏（见用户反馈）。
//
// 之前弹窗是拼接在整个看板上的，会盖住左右面板的边框，把界面画花。
// 现在它只落在中间栏里：左右两侧的 TODO / GOAL 面板必须完整可见。
func TestSecondLevelMenuInCenterColumn(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 30, 0, 0, time.Local)
	for _, w := range []int{80, 100, 120, 160} {
		app, _, _ := newTestApp(t, at)
		app.width, app.height = w, 30
		app.data.Fixed = []*model.Todo{model.NewTodo("固定项", model.KindFixed, app.day, at)}
		app.data.Floating = []*model.Todo{model.NewTodo("临时项", model.KindFloating, app.day, at)}
		app.startTimer()
		out := app.View()
		lines := strings.Split(out, "\n")

		if !strings.Contains(out, "选择计时方式") {
			t.Fatalf("宽度 %d：没找到计时菜单", w)
		}
		// 左右面板的内容必须依然可见——菜单不该把它们盖掉。
		// 窄终端下标题会被截短（例如 "TODAY · …"），所以只断言条目内容。
		flat := strings.Join(strings.Fields(out), "")
		for _, want := range []string{"固定项", "临时项", "GOAL"} {
			if !strings.Contains(flat, want) {
				t.Errorf("宽度 %d：菜单盖住了 %q，左右面板应保持可见", w, want)
			}
		}
		// 菜单文字必须落在中间栏以内：它左边应当紧邻左侧面板的右边框。
		// 做法：找到含菜单标题的行，确认该行同时含有左侧面板的 │ 边界。
		found := false
		for _, l := range lines {
			if !strings.Contains(l, "选择计时方式") {
				continue
			}
			found = true
			if !strings.Contains(l, "│") {
				t.Errorf("宽度 %d：菜单行看不到面板边界", w)
			}
		}
		if !found {
			t.Errorf("宽度 %d：菜单标题不在任何一行上", w)
		}
		// 宽度必须严格等于终端宽度，否则说明拼接出了问题。
		for i, l := range lines {
			if lw := lipgloss.Width(l); lw != w {
				t.Errorf("宽度 %d：第 %d 行宽 %d，应等于终端宽度", w, i, lw)
			}
		}
	}
}

// seedForViews 准备一些长文本，让各界面都有内容可渲染。
func seedForViews(t *testing.T, app *App, s interface {
	SaveDay(*model.DayData) error
	SaveGoals([]model.Goal) error
}, at time.Time) {
	t.Helper()
	data := app.data
	data.Fixed = []*model.Todo{
		func() *model.Todo {
			item := model.NewTodo("每天坚持阅读三十分钟并记录笔记", model.KindFixed, app.day, at)
			item.Tasks = append(item.Tasks, model.NewTask("读《设计数据密集型应用》第四章"))
			return item
		}(),
	}
	data.Floating = []*model.Todo{
		model.NewTodo("写一份很长的周报并抄送给所有相关同事", model.KindFloating, app.day, at),
	}
	data.Archive.Goals = []model.Goal{*model.NewGoal("一个名字相当长的长期目标用于测试右栏换行", at)}
	if err := s.SaveDay(data); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveGoals([]model.Goal{*model.NewGoal("跑完半程马拉松并在两小时内完成", at)}); err != nil {
		t.Fatal(err)
	}
	if err := app.reload(); err != nil {
		t.Fatal(err)
	}
}
