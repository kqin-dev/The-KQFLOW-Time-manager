package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/version"
)

// TestVersionVisibleOnBoard 验证版本号在看板上可见（见用户需求）。
//
// 版本号随每次发布更新，放在最底一行的右端，既不占中间栏空间又随时可见。
func TestVersionVisibleOnBoard(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	for _, size := range [][2]int{{160, 44}, {120, 36}, {100, 30}, {80, 24}} {
		app, _, _ := newTestApp(t, at)
		app.width, app.height = size[0], size[1]
		out := app.View()
		flat := stripStyles(out)
		if !strings.Contains(flat, version.String()) {
			t.Errorf("%dx%d 看板应显示版本号 %s", size[0], size[1], version.String())
		}
		// 每一行都不能超出终端宽度。
		for i, l := range strings.Split(out, "\n") {
			if w := len([]rune(stripStyles(l))); w > size[0] {
				t.Errorf("%dx%d 第 %d 行超宽（%d）", size[0], size[1], i, w)
			}
		}
	}
}

// TestVersionInHelpPage 验证帮助页也标出版本号，方便排查“用的是哪个版本”。
func TestVersionInHelpPage(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.width, app.height = 140, 44
	app.view = ViewHelp

	// 帮助页可能分屏，逐屏找版本号。
	var all []string
	app.pageScroll = 0
	for i := 0; i < 40; i++ {
		all = append(all, strings.Fields(stripStyles(app.View()))...)
		before := app.pageScroll
		press(t, app, "j")
		if app.pageScroll == before {
			break
		}
	}
	if !strings.Contains(strings.Join(all, ""), version.Version) {
		t.Errorf("帮助页应标出版本号 %s", version.Version)
	}
}

// quitPromptLabels 返回选择框里所有选项的文案。
func quitPromptLabels(a *App) []string {
	out := make([]string, 0, len(a.pick.items))
	for _, it := range a.pick.items {
		out = append(out, it.Label)
	}
	return out
}

// selectAction 把光标移到指定 action 上，再按回车执行。
func selectAction(t *testing.T, app *App, action string) {
	t.Helper()
	if app.pick == nil {
		t.Fatalf("没有选择框，无法选择 %s", action)
	}
	for i, it := range app.pick.items {
		if it.Action == action {
			app.pick.cursor = i
			press(t, app, "enter")
			return
		}
	}
	t.Fatalf("选择框里没有 %s，实际 %v", action, quitPromptLabels(app))
}

// assertProtectedPrompt 断言弹出的三选一必须包含“回去（取消）”“保存”“丢弃”三类选项，
// 且默认停在取消上——误触回车不能丢数据。
func assertProtectedPrompt(t *testing.T, app *App, what string) {
	t.Helper()
	if app.pick == nil {
		t.Fatalf("%s：应弹出确认框", what)
	}
	if len(app.pick.items) != 3 {
		t.Fatalf("%s：应有 3 个选项，实际 %v", what, quitPromptLabels(app))
	}
	joined := strings.Join(quitPromptLabels(app), " | ")
	// 三类的关键词：继续/回去/取消、保存、丢弃/不保存。
	for _, group := range [][]string{
		{"继续", "回去", "取消"},
		{"保存"},
		{"丢弃", "不保存"},
	} {
		hit := false
		for _, w := range group {
			if strings.Contains(joined, w) {
				hit = true
				break
			}
		}
		if !hit {
			t.Errorf("%s：选项里应包含 %v 之一，实际 %v", what, group, quitPromptLabels(app))
		}
	}
	if app.pick.cursor != 0 || app.pick.items[0].Action != "cancel" {
		t.Errorf("%s：默认应选中取消，实际 cursor=%d action=%s",
			what, app.pick.cursor, app.pick.items[0].Action)
	}
	if !app.pick.small {
		t.Errorf("%s：三选一应禁用 y/n 快捷键", what)
	}
}

// TestUnsavedNoteProtectedOnEsc 验证按 esc 不会静默丢掉随手记（用户报的问题）。
//
// 早期只有 q 有保护，esc 直接把内容扔掉——手的习惯偏偏就是按 esc。
func TestUnsavedNoteProtectedOnEsc(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)

	app.openNote()
	app.insertEditorText("写到一半的随手记")
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !app.editor.active {
		t.Fatal("有未保存内容时按 esc 不应直接关闭编辑器")
	}
	assertProtectedPrompt(t, app, "esc")

	// y/n 在三选一里必须失效，否则误按就把内容丢了。
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if app.pick == nil {
		t.Fatal("三选一里按 n 不应直接执行（会把内容丢掉）")
	}

	// 选“继续编辑”：回到编辑状态，内容还在。
	selectAction(t, app, "cancel")
	if app.pick != nil || !app.editor.active {
		t.Fatal("继续编辑后应回到编辑状态")
	}
	if got := string(app.editor.value); got != "写到一半的随手记" {
		t.Errorf("继续编辑后内容应保留，实际 %q", got)
	}
	if app.quitting {
		t.Error("继续编辑不应退出程序")
	}

	// 选“保存并关闭”：写盘、关闭编辑器，但**不退出程序**。
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	selectAction(t, app, "close_save")
	if app.editor.active {
		t.Error("保存并关闭后编辑器应关闭")
	}
	if app.quitting {
		t.Error("保存并关闭不应退出程序")
	}
	saved, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Note != "写到一半的随手记" {
		t.Errorf("保存并关闭应写盘，实际 %q", saved.Note)
	}
}

// TestUnsavedNoteDiscardOnEsc 验证选“不保存，关闭”确实不写盘且不退出。
func TestUnsavedNoteDiscardOnEsc(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)

	app.openNote()
	app.insertEditorText("原来的内容")
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	app.openNote()
	app.editor.value = []rune("改成了新内容")
	app.editor.cursor = len(app.editor.value)
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	selectAction(t, app, "close_discard")

	if app.editor.active {
		t.Error("不保存关闭后编辑器应关闭")
	}
	if app.quitting {
		t.Error("不保存关闭不应退出程序")
	}
	saved, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Note != "原来的内容" {
		t.Errorf("不保存关闭不应写盘，期望 %q，实际 %q", "原来的内容", saved.Note)
	}
}

// TestEscClosesEditorWhenUnchanged 验证没改动时 esc 照旧直接关闭，不多问。
func TestEscClosesEditorWhenUnchanged(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.openNote()
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.pick != nil {
		t.Error("没改动时 esc 不应弹确认框")
	}
	if app.editor.active {
		t.Error("没改动时 esc 应直接关闭编辑器")
	}
	if app.quitting {
		t.Error("esc 关编辑器不应退出程序")
	}
}

// TestUnsavedNoteProtectedOnQuit 验证退出流程（菜单）同样受保护。
func TestUnsavedNoteProtectedOnQuit(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)

	app.openNote()
	app.insertEditorText("写到一半的随手记")
	// 从菜单触发退出：中间栏最后一项是退出。
	app.focus = FocusMenu
	app.cursors.menu = len(menuItems) - 1
	app.activateMenuItem(app.cursors.menu)

	assertProtectedPrompt(t, app, "菜单退出")
	selectAction(t, app, "quit_save")
	if !app.quitting {
		t.Fatal("保存并退出后应退出")
	}
	saved, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Note != "写到一半的随手记" {
		t.Errorf("“保存并退出”应把随手记写盘，实际 %q", saved.Note)
	}
}

// TestQuitDiscardingUnsavedNote 验证“直接退出”确实不写盘。
func TestQuitDiscardingUnsavedNote(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)

	// 先存一版，再改成别的内容后选择丢弃。
	app.openNote()
	app.insertEditorText("原来的内容")
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	app.openNote()
	app.editor.value = []rune("改成了新内容")
	app.editor.cursor = len(app.editor.value)
	app.focus = FocusMenu
	app.cursors.menu = len(menuItems) - 1
	app.activateMenuItem(app.cursors.menu)

	selectAction(t, app, "quit_discard")
	if !app.quitting {
		t.Fatal("选“直接退出”后应退出")
	}
	saved, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Note != "原来的内容" {
		t.Errorf("“直接退出”不应写盘，期望仍为 %q，实际 %q", "原来的内容", saved.Note)
	}
}

// TestQuitWithoutChangesDoesNotPrompt 验证没改动时按 q 不弹额外提示。
//
// 打开随手记看一眼又原样退出，不该被多问一次。
func TestQuitWithoutChangesDoesNotPrompt(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.openNote()
	// 原样不动（甚至输入再删掉）。
	app.insertEditorText("临时")
	app.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	app.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if app.editor.Dirty() {
		t.Fatal("改回原样后不应算作有改动")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !app.quitting {
		t.Error("没有未保存改动时应直接退出")
	}
	if app.pick != nil {
		t.Error("没有未保存改动时不应弹选择框")
	}
}

// TestQuitPromptNotShownForSingleLineEditor 验证单行输入框（添加 TODO）
// 按 q 仍然是“输入 q”还是“退出提示”。
//
// 单行输入框用于标题/设置项，按 q 应当正常输入字母 q，
// 不能因为 Dirt 判断把 q 吞掉——否则中文标题里打不出 q。
func TestSingleLineEditorStillTypesQ(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.focus = FocusFixed
	app.startAdd()
	if app.editor.multiline {
		t.Fatal("添加 TODO 应是单行输入")
	}
	app.insertEditorText("qu")
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if app.pick != nil {
		t.Error("单行输入框里按 q 不应弹退出确认")
	}
	if got := string(app.editor.value); got != "quq" {
		t.Errorf("单行输入框里 q 应作为字符输入，实际 %q", got)
	}
}
