package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/model"
)

// assertNoOverflow 检查渲染结果没有超出终端宽高。
//
// 浮层与整页都曾用固定尺寸，终端偏小时会撑破面板边框或顶出可视区域，
// 逼得用户必须全屏才能用。这里对每一行做显示宽度断言，并检查总行数。
// 终端小于 minWidth×minHeight 时看板会主动显示“窗口太小”，那是预期行为，跳过。
func assertNoOverflow(t *testing.T, name, out string, width, height int) {
	t.Helper()
	if width < minWidth || height < minHeight {
		return
	}
	if strings.Contains(out, "终端窗口太小") {
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

			// 浮层：计时菜单（第二级菜单，含方向键选择）。
			app.startTimer()
			assertNoOverflow(t, "计时菜单", app.View(), w, h)
			app.pick = nil

			// 浮层：退出确认。
			app.askQuit()
			assertNoOverflow(t, "退出确认", app.View(), w, h)
			app.pick = nil

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
		out := app.renderHelp()
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
	app.helpScroll = 0
	first := app.View()

	press(t, app, "j", "j", "j")
	if app.helpScroll == 0 {
		t.Fatal("按 j 应向下滚动帮助内容")
	}
	if app.View() == first {
		t.Error("滚动后内容应有变化")
	}
	// 滚回顶部。
	press(t, app, "g")
	if app.helpScroll != 0 {
		t.Errorf("按 g 应回到顶部，实际 %d", app.helpScroll)
	}
	if app.View() != first {
		t.Error("回到顶部后内容应与初始一致")
	}
}

// TestSecondLevelMenuCentered 验证二级菜单居中而不是贴左边缘（见问题 6）。
func TestSecondLevelMenuCentered(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 30, 0, 0, time.Local)
	for _, w := range []int{80, 100, 120, 160} {
		app, _, _ := newTestApp(t, at)
		app.width, app.height = w, 30
		app.startTimer()
		out := app.View()
		// 找到弹窗上边框所在行，算出左右留白。
		var left, right int = -1, -1
		for _, line := range strings.Split(out, "\n") {
			if idx := strings.Index(line, "╔"); idx >= 0 {
				left = lipgloss.Width(line[:idx])
				if end := strings.Index(line, "╗"); end >= 0 {
					right = lipgloss.Width(line[:end])
				}
				break
			}
		}
		if left < 0 {
			t.Fatalf("宽度 %d：没找到弹窗边框", w)
		}
		if left < 1 {
			t.Errorf("宽度 %d：弹窗左侧没有留白，贴到了边缘", w)
		}
		if w-right-1 < 1 {
			t.Errorf("宽度 %d：弹窗右侧没有留白（右边剩 %d 列）", w, w-right-1)
		}
		// 左右留白应当接近，才算居中。
		if diff := (w - right - 1) - left; diff > 4 || diff < -4 {
			t.Errorf("宽度 %d：弹窗未居中，左留白 %d 右留白 %d", w, left, w-right-1)
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
