package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/clock"
)

// startCountUpTimer 起一个不设终点的计时，并选中“自由专注”。
//
// 直接调 startTimer() 只会打开选择菜单，不会真的开始计时。
func startCountUpTimer(t *testing.T, app *App) {
	t.Helper()
	app.startTimer()
	if app.pick == nil {
		t.Fatal("应弹出计时方式菜单")
	}
	selectAction(t, app, "timer_countup")
	if app.pick == nil {
		t.Fatal("选择计时方式后应继续询问归属")
	}
	// 选最后一项（通常是“自由专注”，不归属任何 TODO）。
	app.pick.cursor = len(app.pick.items) - 1
	press(t, app, "enter")
}

// TestEditorClosePathsAreAllProtected 逐个走查“会关掉编辑器”的按键，
// 确认有未保存内容时没有任何一条路径会静默丢弃。
//
// 这张表就是这次的验收清单：早期只有 q 受保护，esc 会直接扔掉内容。
func TestEditorClosePathsAreAllProtected(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	cases := []struct {
		name string
		send func(app *App)
	}{
		{"esc", func(app *App) { app.Update(tea.KeyMsg{Type: tea.KeyEsc}) }},
		{"q", func(app *App) { app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}) }},
		{"ctrl+c", func(app *App) { app.Update(tea.KeyMsg{Type: tea.KeyCtrlC}) }},
		{"菜单退出", func(app *App) {
			app.focus = FocusMenu
			app.cursors.menu = len(menuItems) - 1
			app.activateMenuItem(app.cursors.menu)
		}},
	}

	for _, c := range cases {
		app, s, _ := newTestApp(t, at)
		app.openNote()
		app.insertEditorText("不能被丢掉的随手记")
		c.send(app)

		if !app.editor.active {
			t.Errorf("%s：有未保存内容时不应关闭编辑器", c.name)
			continue
		}
		if app.pick == nil {
			t.Errorf("%s：应弹出保存 / 丢弃的确认框", c.name)
			continue
		}
		if app.quitting {
			t.Errorf("%s：不应直接退出程序", c.name)
		}
		// 无论走哪条路，先确认一下“取消”都能回到编辑且内容还在。
		selectAction(t, app, "cancel")
		if !app.editor.active {
			t.Errorf("%s：取消后应回到编辑状态", c.name)
		}
		if got := string(app.editor.value); got != "不能被丢掉的随手记" {
			t.Errorf("%s：取消后内容应保留，实际 %q", c.name, got)
		}
		// 数据文件里也不该出现这段临时内容。
		saved, err := s.Day("2026-10-03")
		if err == nil && saved != nil && saved.Note == "不能被丢掉的随手记" {
			t.Errorf("%s：按取消却把内容写盘了", c.name)
		}
	}
}

// TestQuitArchivesRunningTimer 验证计时中退出会把已用时长归档。
//
// 早期退出只置 quitting 就 tea.Quit，正在计时的那一段直接丢失——
// 同样是“静默丢数据”，与随手记的问题同源。
func TestQuitArchivesRunningTimer(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)
	startCountUpTimer(t, app)
	if app.timer == nil {
		t.Fatal("应已开始计时")
	}
	// 把时钟推进 90 秒，让这段计时有真实时长。
	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)
	now = at.Add(90 * time.Second)

	// 从菜单退出并确认。
	app.focus = FocusMenu
	app.cursors.menu = len(menuItems) - 1
	app.activateMenuItem(app.cursors.menu)
	if app.pick == nil {
		t.Fatal("应弹出退出确认")
	}
	selectAction(t, app, "quit")
	if !app.quitting {
		t.Fatal("确认后应退出")
	}
	if app.timer != nil {
		t.Error("退出后计时应已停止")
	}

	saved, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Archive.Sessions) == 0 {
		t.Fatal("退出时正在计时的这一段应被归档，实际没有任何记录")
	}
	focus, _ := saved.FocusTotal()
	if focus <= 0 {
		t.Errorf("归档的专注时长应大于 0，实际 %s", focus)
	}
}
