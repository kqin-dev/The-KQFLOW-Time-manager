package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// appWithTodo 造一个带一条临时 TODO 的 App，并把焦点放在它上面。
func appWithTodo(t *testing.T, at time.Time, title string) (*App, *model.Todo) {
	t.Helper()
	app, s, _ := newTestApp(t, at)
	allowCarrySkip(t, app)
	todo := model.NewTodo(title, model.KindFloating, "2026-10-03", at)
	app.data.Floating = append(app.data.Floating, todo)
	app.focus = FocusFloating
	app.cursors.floating = 0
	_ = s
	return app, todo
}

// TestLabelViewOpensForSelectedItem 验证 l 键给当前选中的条目打开标签页。
func TestLabelViewOpensForSelectedItem(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	t.Run("TODO", func(t *testing.T) {
		app, todo := appWithTodo(t, at, "写论文")
		press(t, app, "l")
		if app.view != ViewLabels {
			t.Fatalf("应进入标签页，实际 view=%v", app.view)
		}
		if !app.labelView.active {
			t.Error("标签页应处于激活状态")
		}
		if app.labelView.target == nil {
			t.Fatal("应锁定一个条目")
		}
		if app.labelView.target.ItemTitle() != todo.Title {
			t.Errorf("应锁定选中的条目，实际 %q", app.labelView.target.ItemTitle())
		}
		// 预设标签必须出现在清单里。
		names := make([]string, 0, len(app.labelView.options))
		for _, o := range app.labelView.options {
			names = append(names, o.Name)
		}
		for _, want := range []string{"星星", "旗帜", "爱心", "紧急", "忽略"} {
			if !containsString(names, want) {
				t.Errorf("标签清单应包含预设 %q，实际 %v", want, names)
			}
		}
	})

	t.Run("GOAL", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		allowCarrySkip(t, app)
		goal := model.NewGoal("跑完半程马拉松", at)
		app.goals = append(app.goals, *goal)
		app.focus = FocusGoals
		app.cursors.goals = 0

		press(t, app, "l")
		if app.view != ViewLabels {
			t.Fatalf("GOAL 也应能打开标签页，实际 view=%v", app.view)
		}
		if app.labelView.target.ItemTitle() != goal.Title {
			t.Errorf("应锁定 GOAL，实际 %q", app.labelView.target.ItemTitle())
		}
	})

	t.Run("焦点在中间栏时拒绝并提示", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		allowCarrySkip(t, app)
		app.focus = FocusMenu
		press(t, app, "l")
		if app.view == ViewLabels {
			t.Error("没有选中条目时不该进入标签页")
		}
		if !strings.Contains(app.toast, "选中一个条目") {
			t.Errorf("应提示先选中条目，实际 toast=%q", app.toast)
		}
	})
}

// TestLabelTogglePersistsImmediately 验证勾选标签立刻落盘，不依赖退出时保存。
//
// 这一点很重要：标签页退出时会写配置（saveConfig 会 reload 数据），
// 如果标签只靠那时候才存，一次意外退出就会丢。
func TestLabelTogglePersistsImmediately(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, todo := appWithTodo(t, at, "写论文")
	press(t, app, "l")

	// 光标停在第一项（星星）上，回车打开。
	press(t, app, "enter")
	if !model.HasLabel(todo.Labels, "星星") {
		t.Fatalf("条目上应已有星星，实际 %v", todo.Labels)
	}

	// 直接查磁盘，确认没有等到退出才写。
	saved, err := app.store.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Floating) != 1 {
		t.Fatalf("落盘应有 1 条 TODO，实际 %d", len(saved.Floating))
	}
	if !model.HasLabel(saved.Floating[0].Labels, "星星") {
		t.Errorf("标签应已落盘，实际 %v", saved.Floating[0].Labels)
	}

	// 再按一次回车取下。
	press(t, app, "enter")
	if model.HasLabel(todo.Labels, "星星") {
		t.Errorf("再按一次应取下标签，实际 %v", todo.Labels)
	}
	saved, err = app.store.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if model.HasLabel(saved.Floating[0].Labels, "星星") {
		t.Errorf("取下后也应落盘，实际 %v", saved.Floating[0].Labels)
	}
}

// TestLabelToggleWithSpace 验证空格也能开关标签（与回车等价）。
func TestLabelToggleWithSpace(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, todo := appWithTodo(t, at, "写论文")
	press(t, app, "l")
	press(t, app, "space")
	if !model.HasLabel(todo.Labels, "星星") {
		t.Errorf("空格应能开关标签，实际 %v", todo.Labels)
	}
}

// TestLabelAddCustomPersistsToConfig 验证新增自定义标签会同时写进配置，
// 下次启动不用重新输入。
func TestLabelAddCustomPersistsToConfig(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, todo := appWithTodo(t, at, "写论文")
	press(t, app, "l")

	press(t, app, "a")
	if !app.editor.active {
		t.Fatal("应打开新增标签输入框")
	}
	for _, r := range "开会" {
		press(t, app, string(r))
	}
	press(t, app, "enter")

	if !model.HasLabel(todo.Labels, "开会") {
		t.Fatalf("新标签应加在当前条目上，实际 %v", todo.Labels)
	}
	// 自定义标签要进入清单，退出后仍在（存进配置）。
	if !containsString(app.cfg.CustomLabelList(), "开会") {
		t.Errorf("自定义标签应记进配置，实际 %v", app.cfg.CustomLabelList())
	}

	press(t, app, "esc")
	if app.view != ViewDashboard {
		t.Errorf("esc 应回到看板，实际 view=%v", app.view)
	}
	if !containsString(app.cfg.CustomLabelList(), "开会") {
		t.Errorf("退出后配置里仍应有自定义标签，实际 %v", app.cfg.CustomLabelList())
	}

	// 重新打开标签页，自定义标签要出现在清单里。
	press(t, app, "l")
	names := make([]string, 0, len(app.labelView.options))
	for _, o := range app.labelView.options {
		names = append(names, o.Name)
	}
	if !containsString(names, "开会") {
		t.Errorf("重开后清单里应有自定义标签，实际 %v", names)
	}
}

// TestLabelAddCustomMultipleAtOnce 验证一次输入多个标签（空格/逗号分隔）。
func TestLabelAddCustomMultipleAtOnce(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, todo := appWithTodo(t, at, "写论文")
	press(t, app, "l")
	press(t, app, "a")
	app.editor.value = nil
	app.editor.cursor = 0
	for _, r := range "复习 预习,总结" {
		press(t, app, string(r))
	}
	press(t, app, "enter")

	for _, want := range []string{"复习", "预习", "总结"} {
		if !model.HasLabel(todo.Labels, want) {
			t.Errorf("应有标签 %q，实际 %v", want, todo.Labels)
		}
	}
}

// TestLabelAddCustomRejectsEmpty 验证空输入被拒且不留下垃圾标签。
func TestLabelAddCustomRejectsEmpty(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, todo := appWithTodo(t, at, "写论文")
	press(t, app, "l")
	press(t, app, "a")
	press(t, app, "enter")
	if len(todo.Labels) != 0 {
		t.Errorf("空输入不该加上标签，实际 %v", todo.Labels)
	}
	if !strings.Contains(app.toast, "不能为空") {
		t.Errorf("应提示标签不能为空，实际 toast=%q", app.toast)
	}
}

// TestLabelPresetsGetGlyphsAndCustomUsesHash 验证预设用记号、自定义用 #，
// 让「这条有标签」在看板上一眼可见。
func TestLabelPresetsGetGlyphsAndCustomUsesHash(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, todo := appWithTodo(t, at, "写论文")

	todo.SetItemLabels([]string{"星星", "开会"})
	out := stripStyles(app.View())
	for _, want := range []string{"★星星", "#开会"} {
		if !strings.Contains(out, want) {
			t.Errorf("看板应显示 %q，实际输出:\n%s", want, out)
		}
	}
}

// TestLabelRendersOnDashboardAndGoal 验证 TODO 与 GOAL 两栏都会显示标签。
func TestLabelRendersOnDashboardAndGoal(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	allowCarrySkip(t, app)

	todo := model.NewTodo("写论文", model.KindFloating, "2026-10-03", at)
	todo.SetItemLabels([]string{"紧急"})
	app.data.Floating = append(app.data.Floating, todo)

	goal := model.NewGoal("跑完半程马拉松", at)
	goal.SetItemLabels([]string{"星星"})
	app.goals = append(app.goals, *goal)

	out := stripStyles(app.View())
	if !strings.Contains(out, "!紧急") {
		t.Errorf("TODO 行应显示标签，实际输出:\n%s", out)
	}
	if !strings.Contains(out, "★星星") {
		t.Errorf("GOAL 行应显示标签，实际输出:\n%s", out)
	}
}

// TestLabelInlineShrinksOnNarrowPanel 验证面板窄时标签退化成只显示记号，
// 不会把标题整段挤掉。
func TestLabelInlineShrinksOnNarrowPanel(t *testing.T) {
	labels := []string{"星星", "紧急", "爱心", "进行中"}

	full := joinLabelTags(labels, false)
	if !strings.Contains(full, "星星") {
		t.Fatalf("完整形式应带名字，实际 %q", full)
	}
	glyphOnly := joinLabelTags(labels, true)
	if strings.Contains(glyphOnly, "星星") {
		t.Errorf("退化形式应只保留记号，实际 %q", glyphOnly)
	}
	// 退化形式必须更短，否则退化没有意义。
	if len([]rune(glyphOnly)) >= len([]rune(full)) {
		t.Errorf("退化形式应更短：%q vs %q", glyphOnly, full)
	}
	// 预算极小的时候必须走退化分支。
	if got := labelsInlineBudget(labels, 4); got != glyphOnly {
		t.Errorf("预算不足时应退化，实际 %q", got)
	}
	// 预算充足时保留名字。
	if got := labelsInlineBudget(labels, 100); got != full {
		t.Errorf("预算充足时应保留名字，实际 %q", got)
	}
}

// TestLabelViewSavesConfigOnlyWhenNeeded 验证只有新增过自定义标签才写配置。
//
// saveConfig() 会 reload 数据（日界线可能随之改变），把 a.data 换成新对象；
// 没有必要时不该触发它。
func TestLabelViewSavesConfigOnlyWhenNeeded(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	// 只勾预设：退出时不需要写配置。
	app, _ := appWithTodo(t, at, "写论文")
	press(t, app, "l")
	press(t, app, "enter")
	press(t, app, "esc")
	if app.labelView.customDirty {
		t.Error("只勾预设不该标记自定义标签变更")
	}

	// 新增自定义：退出时应写配置。
	app2, _ := appWithTodo(t, at, "写论文")
	press(t, app2, "l")
	press(t, app2, "a")
	app2.editor.value = nil
	app2.editor.cursor = 0
	for _, r := range "自造标签" {
		press(t, app2, string(r))
	}
	press(t, app2, "enter")
	if !app2.labelView.customDirty {
		t.Error("新增自定义标签后应标记变更")
	}
	press(t, app2, "esc")
	if !containsString(app2.cfg.CustomLabelList(), "自造标签") {
		t.Errorf("配置里应保留自定义标签，实际 %v", app2.cfg.CustomLabelList())
	}
}

// TestLabelDKeyRemovesFromItemOnly 验证 d 只把标签从当前条目取下，
// 不会从清单或别的条目上删掉。
func TestLabelDKeyRemovesFromItemOnly(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, todo := appWithTodo(t, at, "写论文")

	// 另一条条目也在用同一个标签。
	other := model.NewTodo("整理桌面", model.KindFloating, "2026-10-03", at)
	other.SetItemLabels([]string{"星星"})
	app.data.Floating = append(app.data.Floating, other)

	todo.SetItemLabels([]string{"星星"})
	press(t, app, "l")
	// 光标停在「星星」上（该条目已有标签，打开时会定位过去）。
	if app.labelView.cursor >= len(app.labelView.options) ||
		app.labelView.options[app.labelView.cursor].Name != "星星" {
		t.Fatalf("光标应停在星星上，实际 cursor=%d", app.labelView.cursor)
	}
	press(t, app, "d")

	if model.HasLabel(todo.Labels, "星星") {
		t.Errorf("当前条目上的标签应被取下，实际 %v", todo.Labels)
	}
	if !model.HasLabel(other.Labels, "星星") {
		t.Errorf("别的条目上的同名标签不该被动，实际 %v", other.Labels)
	}
	names := make([]string, 0, len(app.labelView.options))
	for _, o := range app.labelView.options {
		names = append(names, o.Name)
	}
	if !containsString(names, "星星") {
		t.Error("标签仍应留在清单里（别的条目还在用/它是预设）")
	}
}

// TestLabelCapShowsToast 验证到达上限时给出提示，而不是静默无反应。
func TestLabelCapShowsToast(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, todo := appWithTodo(t, at, "写论文")

	full := make([]string, 0, model.MaxLabelsPerItem)
	for i := 0; i < model.MaxLabelsPerItem; i++ {
		full = append(full, "标签"+string(rune('A'+i)))
	}
	todo.SetItemLabels(full)

	press(t, app, "l")
	press(t, app, "a")
	app.editor.value = nil
	app.editor.cursor = 0
	for _, r := range "再来一个" {
		press(t, app, string(r))
	}
	press(t, app, "enter")

	if len(todo.Labels) != model.MaxLabelsPerItem {
		t.Errorf("超过上限不该再加，实际 %d 个", len(todo.Labels))
	}
	if !strings.Contains(app.toast, "最多") {
		t.Errorf("应提示达到上限，实际 toast=%q", app.toast)
	}
}

// TestLabelOldDataWithoutFieldLoads 验证没有 labels 字段的老数据照常可用。
func TestLabelOldDataWithoutFieldLoads(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	allowCarrySkip(t, app)

	// 老数据：JSON 里根本没有 labels 字段。
	legacy := model.NewTodo("老条目", model.KindFloating, "2026-10-03", at)
	legacy.Labels = nil
	app.data.Floating = append(app.data.Floating, legacy)
	app.focus = FocusFloating

	// 照常渲染、照常能打标签。
	_ = app.View()
	press(t, app, "l")
	press(t, app, "enter")
	if !model.HasLabel(legacy.Labels, "星星") {
		t.Errorf("老数据应能正常加标签，实际 %v", legacy.Labels)
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
