package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/version"
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

// TestQuitWithUnsavedNoteAsksThreeWays 验证随手记没保存就退出时给出三个选择。
func TestQuitWithUnsavedNoteAsksThreeWays(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)

	app.openNote()
	app.insertEditorText("写到一半的随手记")

	// 编辑中按 q：不应直接退出，而应弹三选一。
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if app.quitting {
		t.Fatal("有未保存内容时按 q 不应直接退出")
	}
	if app.pick == nil {
		t.Fatal("应弹出退出确认")
	}
	if len(app.pick.items) != 3 {
		t.Fatalf("应有 3 个选项，实际 %d 个", len(app.pick.items))
	}
	labels := []string{
		app.pick.items[0].Label,
		app.pick.items[1].Label,
		app.pick.items[2].Label,
	}
	joined := strings.Join(labels, " | ")
	for _, want := range []string{"取消", "保存", "丢弃"} {
		if !strings.Contains(joined, want) {
			t.Errorf("选项里应包含“%s”，实际 %v", want, labels)
		}
	}
	// 默认选中“取消”，误触回车不会丢数据。
	if app.pick.cursor != 0 || app.pick.items[0].Action != "cancel" {
		t.Errorf("默认应选中取消，实际 cursor=%d action=%s", app.pick.cursor, app.pick.items[0].Action)
	}
	// y/n 在这个三选一里必须失效，否则会误触到首项或末项。
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if app.pick == nil {
		t.Fatal("三选一里按 n 不应直接执行末项（会把内容丢掉）")
	}
	if app.editor.active != true {
		t.Fatal("按 n 后仍应停留在编辑状态")
	}

	// 选“取消退出”：回到编辑状态，内容还在。
	app.Update(tea.KeyMsg{Type: tea.KeyEnter}) // cursor=0 → cancel
	if app.pick != nil {
		t.Fatal("取消后应关闭选择框")
	}
	if !app.editor.active {
		t.Fatal("取消后应回到编辑状态")
	}
	if got := string(app.editor.value); got != "写到一半的随手记" {
		t.Errorf("取消后内容应保留，实际 %q", got)
	}
	if app.quitting {
		t.Fatal("取消后不应退出")
	}

	// 选“保存并退出”：写盘并退出。
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	app.pick.cursor = 1
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !app.quitting {
		t.Fatal("选“保存并退出”后应退出")
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
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if app.pick == nil {
		t.Fatal("应弹出退出确认")
	}
	app.pick.cursor = 2
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
