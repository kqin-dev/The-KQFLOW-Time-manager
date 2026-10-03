package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/model"
)

// assertNoOverflow 检查渲染结果没有超出终端宽度。
//
// 浮层与整页都曾用固定宽度，窄终端下会撑破后面的面板边框。
// 这里对每一行做显示宽度断言，作为防止回归的守门人。
// 终端小于 minWidth×minHeight 时看板会主动显示“窗口太小”，那是预期行为，跳过。
func assertNoOverflow(t *testing.T, name, out string, width, height int) {
	t.Helper()
	if width < minWidth || height < minHeight {
		return
	}
	if strings.Contains(out, "终端窗口太小") {
		return
	}
	for i, line := range strings.Split(out, "\n") {
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

			// 浮层：庆祝特效。
			app.celebrate = &celebrateState{started: time.Now()}
			assertNoOverflow(t, "庆祝特效", app.View(), w, h)
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
