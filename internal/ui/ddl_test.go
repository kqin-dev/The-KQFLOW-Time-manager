package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// typeDDLValue 在输入框里敲入一段文本并提交。
func typeDDLValue(t *testing.T, app *App, value string) {
	t.Helper()
	app.editor.value = nil
	app.editor.cursor = 0
	for _, r := range value {
		press(t, app, string(r))
	}
	press(t, app, "enter")
}

// TestDdlViewOpensForSelectedItem 验证 D 键给当前选中条目打开 DDL 页，
// 且两类条目的粒度不同。
func TestDdlViewOpensForSelectedItem(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	t.Run("TODO 只到时分", func(t *testing.T) {
		app, todo := appWithTodo(t, at, "写论文")
		press(t, app, "D")
		if app.view != ViewDdl {
			t.Fatalf("应进入 DDL 页，实际 view=%v", app.view)
		}
		if !app.ddlView.active {
			t.Error("DDL 页应处于激活状态")
		}
		if app.ddlView.kind != "TODO" {
			t.Errorf("应识别为 TODO，实际 %q", app.ddlView.kind)
		}
		if app.ddlView.target.ItemTitle() != todo.Title {
			t.Errorf("应锁定选中条目，实际 %q", app.ddlView.target.ItemTitle())
		}
		// 提示文案要说明粒度。
		out := stripStyles(app.View())
		if !strings.Contains(out, "只到时分") {
			t.Errorf("应说明 TODO 的 DDL 只到时分，实际:\n%s", out)
		}
	})

	t.Run("GOAL 只到年月日", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		allowCarrySkip(t, app)
		goal := model.NewGoal("跑完半程马拉松", at)
		app.goals = append(app.goals, *goal)
		app.focus = FocusGoals
		app.cursors.goals = 0

		press(t, app, "D")
		if app.view != ViewDdl {
			t.Fatalf("GOAL 也应能打开 DDL 页，实际 view=%v", app.view)
		}
		if app.ddlView.kind != "GOAL" {
			t.Errorf("应识别为 GOAL，实际 %q", app.ddlView.kind)
		}
		if !strings.Contains(stripStyles(app.View()), "只到年月日") {
			t.Errorf("应说明 GOAL 的 DDL 只到年月日")
		}
	})

	t.Run("焦点在中间栏时拒绝并提示", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		allowCarrySkip(t, app)
		app.focus = FocusMenu
		press(t, app, "D")
		if app.view == ViewDdl {
			t.Error("没有选中条目时不该进入 DDL 页")
		}
		if !strings.Contains(app.toast, "选中一个条目") {
			t.Errorf("应提示先选中条目，实际 toast=%q", app.toast)
		}
	})
}

// TestDdlPresetOptionsMatchGranularity 验证两类条目的快捷选项分别是时分与年月日，
// 且应用后落盘。
func TestDdlPresetOptionsMatchGranularity(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	t.Run("TODO", func(t *testing.T) {
		app, todo := appWithTodo(t, at, "写论文")
		press(t, app, "D")
		opts := app.ddlOptions(app.ddlView.target)
		if len(opts) == 0 {
			t.Fatal("应有快捷选项")
		}
		for _, o := range opts {
			if o.Value == "" {
				continue // 清除项
			}
			if _, err := model.ParseDueTime(o.Value); err != nil {
				t.Errorf("TODO 的选项 %q 应是合法时分：%v", o.Value, err)
			}
		}

		// 应用第一项并确认落盘。
		press(t, app, "enter")
		if todo.Due == "" {
			t.Fatal("应用后条目应有 DDL")
		}
		saved, err := app.store.Day("2026-10-03")
		if err != nil {
			t.Fatal(err)
		}
		if len(saved.Floating) != 1 || saved.Floating[0].Due != todo.Due {
			t.Errorf("DDL 应立即落盘，实际 %+v", saved.Floating)
		}
	})

	t.Run("GOAL", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		allowCarrySkip(t, app)
		app.goals = append(app.goals, *model.NewGoal("跑完半程马拉松", at))
		app.focus = FocusGoals
		app.cursors.goals = 0

		press(t, app, "D")
		opts := app.ddlOptions(app.ddlView.target)
		for _, o := range opts {
			if o.Value == "" {
				continue
			}
			if _, err := model.ParseDueDate(o.Value); err != nil {
				t.Errorf("GOAL 的选项 %q 应是合法日期：%v", o.Value, err)
			}
		}
		press(t, app, "enter")
		if app.goals[0].Due == "" {
			t.Fatal("应用后目标应有 DDL")
		}
	})
}

// TestDdlCustomInput 验证自己输入 DDL，并支持去掉首尾空白/补零。
func TestDdlCustomInput(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, todo := appWithTodo(t, at, "写论文")
	press(t, app, "D")
	press(t, app, "e")
	if !app.editor.active {
		t.Fatal("e 应打开输入框")
	}
	typeDDLValue(t, app, "8:05")
	if todo.Due != "08:05" {
		t.Errorf("应写入规范化的 08:05，实际 %q", todo.Due)
	}
	if !strings.Contains(app.toast, "08:05") {
		t.Errorf("应提示已设置，实际 toast=%q", app.toast)
	}
}

// TestDdlCustomInputRejectsInvalid 验证非法输入被拒、不写入，
// 并把输入框留给用户继续改（而不是丢弃他敲的内容）。
func TestDdlCustomInputRejectsInvalid(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, todo := appWithTodo(t, at, "写论文")
	press(t, app, "D")
	press(t, app, "e")
	typeDDLValue(t, app, "25:00")

	if todo.Due != "" {
		t.Errorf("非法 DDL 不该写入，实际 %q", todo.Due)
	}
	if !app.editor.active {
		t.Error("校验失败时应保留输入框让用户继续改")
	}
	if !strings.Contains(app.toast, "小时") {
		t.Errorf("应给出可读的错误原因，实际 toast=%q", app.toast)
	}

	// 改成合法值后应能提交成功。
	typeDDLValue(t, app, "18:30")
	if todo.Due != "18:30" {
		t.Errorf("改正后应写入 18:30，实际 %q", todo.Due)
	}
}

// TestDdlClear 验证 d 清除 DDL，以及本来就空时的提示。
func TestDdlClear(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, todo := appWithTodo(t, at, "写论文")

	// 先设一个。
	todo.SetDue("18:30")
	press(t, app, "D")
	press(t, app, "d")
	if todo.Due != "" {
		t.Errorf("d 应清除 DDL，实际 %q", todo.Due)
	}
	if !strings.Contains(app.toast, "已清除") {
		t.Errorf("应提示已清除，实际 toast=%q", app.toast)
	}

	// 再按一次应告知本来就没有。
	press(t, app, "d")
	if !strings.Contains(app.toast, "本来就没有") {
		t.Errorf("应提示本来就没有 DDL，实际 toast=%q", app.toast)
	}
}

// TestDdlPanelListsBothKinds 验证 DDL 栏位同时列出 TODO 与 GOAL，
// 超时的排在前面，已完成的目标不再出现。
func TestDdlPanelListsBothKinds(t *testing.T) {
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	allowCarrySkip(t, app)

	// 一条超时的 TODO（11:00 已过）与一条还早的 TODO。
	overdue := model.NewTodo("已经超时的活", model.KindFloating, "2026-10-03", at)
	overdue.SetDue("11:00")
	later := model.NewTodo("还早的活", model.KindFloating, "2026-10-03", at)
	later.SetDue("23:00")
	app.data.Floating = append(app.data.Floating, overdue, later)

	// 一个今天到期的 GOAL 与一个已完成（不该出现）的 GOAL。
	dueGoal := model.NewGoal("今天到期的目标", at)
	dueGoal.SetDue("2026-10-03")
	doneGoal := model.NewGoal("已完成的目标", at)
	doneGoal.SetDue("2026-10-03")
	doneGoal.Done = true
	app.goals = append(app.goals, *dueGoal, *doneGoal)

	entries := app.dueEntries()
	if len(entries) != 3 {
		t.Fatalf("应有 3 条 DDL（不含已完成目标），实际 %d: %+v", len(entries), entries)
	}
	// 超时的排最前。
	if entries[0].Title != overdue.Title {
		t.Errorf("超时应排最前，实际第一条是 %q", entries[0].Title)
	}
	if entries[0].State != model.DueOverdue {
		t.Errorf("第一条应为超时状态，实际 %v", entries[0].State)
	}
	// 两类都在列表里。
	kinds := map[string]bool{}
	for _, e := range entries {
		kinds[e.Kind] = true
	}
	if !kinds["TODO"] || !kinds["GOAL"] {
		t.Errorf("应同时包含 TODO 与 GOAL，实际 %v", kinds)
	}
	// 已完成的目标不该出现。
	for _, e := range entries {
		if e.Title == doneGoal.Title {
			t.Error("已完成的目标不该出现在 DDL 栏")
		}
	}

	// 右栏渲染应体现出来。
	out := stripStyles(app.View())
	if !strings.Contains(out, "DDL (3)") {
		t.Errorf("右栏应显示 DDL 栏位与数量，实际:\n%s", out)
	}
	if !strings.Contains(out, "11:00") {
		t.Errorf("DDL 栏应显示待办的时分，实际:\n%s", out)
	}
	if !strings.Contains(out, "10-03") {
		t.Errorf("DDL 栏应把目标显示成月-日，实际:\n%s", out)
	}
}

// TestDdlShownOnTodoRow 验证待办设了 DDL 后，左栏行上能直接看到。
func TestDdlShownOnTodoRow(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, todo := appWithTodo(t, at, "写论文")
	todo.SetDue("18:30")

	out := stripStyles(app.View())
	if !strings.Contains(out, "⌛18:30") {
		t.Errorf("左栏行上应显示 DDL，实际:\n%s", out)
	}
}

// TestDdlOverdueSummary 验证右栏汇总行会提醒超时数量。
func TestDdlOverdueSummary(t *testing.T) {
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	allowCarrySkip(t, app)

	todo := model.NewTodo("超时的活", model.KindFloating, "2026-10-03", at)
	todo.SetDue("11:00")
	app.data.Floating = append(app.data.Floating, todo)

	if n := app.overdueCount(); n != 1 {
		t.Errorf("应有 1 项超时，实际 %d", n)
	}
	if !strings.Contains(stripStyles(app.View()), "1 项超时") {
		t.Errorf("汇总行应提醒超时数量，实际:\n%s", stripStyles(app.View()))
	}
}

// TestDdlFormatInvalidShownAsUnset 验证手改成非法格式的 DDL 不会让界面出错：
// 按「未设置」处理，但条目的原始值仍保留。
func TestDdlFormatInvalidShownAsUnset(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, todo := appWithTodo(t, at, "写论文")
	todo.SetDue("25:00")

	// 不该进 DDL 栏（算不出到期时间），但不该 panic。
	if n := len(app.dueEntries()); n != 0 {
		t.Errorf("非法 DDL 不该进 DDL 栏，实际 %d 条", n)
	}
	if out := app.View(); out == "" {
		t.Error("渲染不应为空")
	}
	if todo.Due != "25:00" {
		t.Errorf("原始值应保留供用户发现，实际 %q", todo.Due)
	}

	// 打开 DDL 页时应说明格式无法识别。
	press(t, app, "D")
	if !strings.Contains(stripStyles(app.View()), "格式无法识别") {
		t.Errorf("应提示格式无法识别，实际:\n%s", stripStyles(app.View()))
	}
}

// TestDdlKeyDoesNotCollideWithDelete 验证大写 D 设 DDL、小写 d 仍是删除，
// 两者不会互相触发。
func TestDdlKeyDoesNotCollideWithDelete(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _ := appWithTodo(t, at, "写论文")

	// 小写 d 走删除确认，不该进 DDL 页。
	press(t, app, "d")
	if app.view == ViewDdl {
		t.Error("小写 d 不该打开 DDL 页")
	}
	if app.pick == nil {
		t.Error("小写 d 应走删除确认流程")
	}
	selectAction(t, app, "cancel")

	// 大写 D 进 DDL 页，不该触发删除。
	press(t, app, "D")
	if app.view != ViewDdl {
		t.Fatalf("大写 D 应打开 DDL 页，实际 view=%v", app.view)
	}
	if len(app.data.Floating) != 1 {
		t.Errorf("大写 D 不该删除条目，实际剩 %d 条", len(app.data.Floating))
	}
}

// TestDdlEscReturnsToDashboard 验证 esc 返回看板。
func TestDdlEscReturnsToDashboard(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _ := appWithTodo(t, at, "写论文")
	press(t, app, "D")
	press(t, app, "esc")
	if app.view != ViewDashboard {
		t.Errorf("esc 应回到看板，实际 view=%v", app.view)
	}
	if app.ddlView.active {
		t.Error("退出后 DDL 页应关闭")
	}
}

// TestDdlUsesConfiguredDayCutoff 验证 DDL 的「今天」跟随用户设的日界线。
//
// 这是一个容易错的交互：日界线 04:00 时凌晨 2 点仍算前一天，
// 于是 DDL 18:30 应该已经超时，而不是「还有 16 小时」。
func TestDdlUsesConfiguredDayCutoff(t *testing.T) {
	at := time.Date(2026, 10, 3, 2, 0, 0, 0, time.Local)
	app, _, cfg := newTestApp(t, at)
	allowCarrySkip(t, app)
	cfg.DayCutoff = "04:00"
	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)

	todo := model.NewTodo("熬夜要做的事", model.KindFloating, "2026-10-02", at)
	todo.SetDue("18:30")
	app.data.Floating = append(app.data.Floating, todo)

	state, left := model.DueStatus(todo.Due, true, now, cfg.Cutoff(), cfg.Location())
	if state != model.DueOverdue {
		t.Errorf("日界线 04:00 时凌晨 2 点的 18:30 应已超时，实际 state=%v left=%v", state, left)
	}
}

// TestDdlEditorCommitKeepsView 验证提交后仍留在 DDL 页，用户可以接着调整。
func TestDdlEditorCommitKeepsView(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _ := appWithTodo(t, at, "写论文")
	press(t, app, "D")
	press(t, app, "e")

	var msg tea.KeyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("18:00")}
	app.Update(msg)
	press(t, app, "enter")

	if app.view != ViewDdl {
		t.Errorf("提交后应留在 DDL 页，实际 view=%v", app.view)
	}
}
