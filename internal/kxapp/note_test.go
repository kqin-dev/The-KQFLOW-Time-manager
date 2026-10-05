package kxapp

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/plugin"
)

// typeRunes 逐字符输入（走真实按键路径）。
func typeRunes(m interface {
	Dispatch(plugin.Event) bool
}, s string) {
	for _, r := range []rune(s) {
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: string(r), Runes: []rune{r}})
	}
}

// TestNoteEditorSavesMultiline 验证随手记能写多行并保存。
//
// 这是检查表里**最后一个必补项**：多行输入是引擎此前完全缺失的能力
// （其它输入框都是单行）。
func TestNoteEditorSavesMultiline(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, services, _ := l.Build()
	m.Resize(120, 40)

	if !openMenuOptionOK(t, m, "随手记") {
		t.Fatalf("菜单里应有随手记，实际 %v", m.Options())
	}
	if !strings.Contains(m.View(), "随手记 · ") {
		t.Fatalf("应进入随手记编辑器：\n%s", m.View())
	}

	// 写两行：Enter 是换行，不是确认（这是关键取舍）。
	typeRunes(m, "第一行")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	typeRunes(m, "第二行")
	out := m.View()
	if !strings.Contains(out, "第一行") || !strings.Contains(out, "第二行") {
		t.Fatalf("两行都应当显示：\n%s", out)
	}
	// 这时还没保存。
	if !strings.Contains(out, "未保存") {
		t.Errorf("有改动时标题应标出「未保存」：\n%s", out)
	}
	if src.Day().Note != "" {
		t.Error("还没按 ctrl+s，数据不该被写")
	}

	// ctrl+s 保存。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "ctrl+s"})
	if got := src.Day().Note; got != "第一行\n第二行" {
		t.Errorf("保存的文本不对：%q", got)
	}
	if services.Saves == 0 {
		t.Error("保存应当落盘")
	}
	// 保存后应当关闭编辑器。
	if strings.Contains(m.View(), "ctrl+s 保存") {
		t.Errorf("保存后应关闭编辑器：\n%s", m.View())
	}
}

// TestNoteEditorEnterIsNewlineNotConfirm 验证 Enter 一定是换行。
//
// 2.1.0 的注释写得很清楚：按键刻意避开 Enter，
// 否则"写日记"和"确认"会互相打架。这条要钉死。
func TestNoteEditorEnterIsNewlineNotConfirm(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	openMenuOptionOK(t, m, "随手记")

	typeRunes(m, "甲")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	typeRunes(m, "乙")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	typeRunes(m, "丙")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "ctrl+s"})

	if got := src.Day().Note; got != "甲\n乙\n丙" {
		t.Errorf("三次输入应被两次换行分成三行，实际 %q", got)
	}
	if strings.Count(src.Day().Note, "\n") != 2 {
		t.Errorf("应当恰好有两个换行，实际 %q", src.Day().Note)
	}
}

// TestNoteEditorEscWarnsWhenDirty 验证有改动时 esc **先问一句**。
//
// 随手记是用户亲手写的东西，"按了 esc 就没了"是绝对不能接受的。
func TestNoteEditorEscWarnsWhenDirty(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	openMenuOptionOK(t, m, "随手记")

	typeRunes(m, "写了点东西")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})

	out := m.View()
	if !strings.Contains(out, "要丢弃吗") {
		t.Fatalf("有改动时 esc 应当先问一句：\n%s", out)
	}
	// 界面还开着。
	if !strings.Contains(out, "随手记") {
		t.Errorf("问话期间编辑器不该关闭：\n%s", out)
	}
	if src.Day().Note != "" {
		t.Error("问话期间不该写入任何东西")
	}

	// n = 继续写：回到编辑状态，内容还在。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "n"})
	if !strings.Contains(m.View(), "写了点东西") {
		t.Errorf("选择继续写之后内容应当还在：\n%s", m.View())
	}

	// 再 esc，然后 y = 丢弃并退出。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "y"})
	if src.Day().Note != "" {
		t.Errorf("选择丢弃后不该写入数据，实际 %q", src.Day().Note)
	}
	if strings.Contains(m.View(), "要丢弃吗") {
		t.Error("丢弃后应当关闭编辑器")
	}
}

// TestNoteEditorEscWithoutChangesClosesDirectly 验证没改动时 esc 直接关。
//
// 没改动还要问一句 = 白白多按一次键。
func TestNoteEditorEscWithoutChangesClosesDirectly(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	openMenuOptionOK(t, m, "随手记")

	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	out := m.View()
	if strings.Contains(out, "要丢弃吗") {
		t.Errorf("没改动时不该问：\n%s", out)
	}
	if strings.Contains(out, "ctrl+s 保存") {
		t.Errorf("没改动时 esc 应当直接关闭：\n%s", out)
	}
}

// TestNoteEditorLoadsExisting 验证打开时载入已有内容，光标在末尾。
//
// 写随手记通常是"接着上次继续写"，因此光标默认在末尾。
func TestNoteEditorLoadsExisting(t *testing.T) {
	src := newMemSource(t, testNow())
	src.Day().Note = "以前写的第一行\n以前写的第二行"

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	openMenuOptionOK(t, m, "随手记")

	out := m.View()
	if !strings.Contains(out, "以前写的第一行") || !strings.Contains(out, "以前写的第二行") {
		t.Fatalf("应当载入已有内容：\n%s", out)
	}
	// 直接接着写：新内容应追加到末尾。
	typeRunes(m, "追加")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "ctrl+s"})
	want := "以前写的第一行\n以前写的第二行追加"
	if got := src.Day().Note; got != want {
		t.Errorf("光标应在末尾追加，期望 %q，实际 %q", want, got)
	}
}

// TestNoteEditorBackspaceAcrossLines 验证退格能跨行合并。
//
// 这是"分行存储"最容易写错的地方（行首退格要并到上一行）。
func TestNoteEditorBackspaceAcrossLines(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	openMenuOptionOK(t, m, "随手记")

	typeRunes(m, "甲")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	typeRunes(m, "乙")
	// 光标在第二行行首？不——先退一格到行首，再退一格应当并到第一行。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "backspace"}) // 删掉"乙"→ 第二行空
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "backspace"}) // 行首退格 → 并到第一行
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "ctrl+s"})

	if got := src.Day().Note; got != "甲" {
		t.Errorf("跨行退格后应为「甲」，实际 %q", got)
	}
}

// TestNoteEditorIgnoresInvisibleChars 验证控制字符进不来。
//
// 与 NewTodo/NewGoal 的 Sanitize 同一个理由：NUL 会以 \u0000
// 写进 JSON，看不见又让数据变脆。
func TestNoteEditorIgnoresInvisibleChars(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	openMenuOptionOK(t, m, "随手记")

	typeRunes(m, "正常")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "\x00", Runes: []rune{0}})
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "\x1b", Runes: []rune{0x1b}})
	typeRunes(m, "文字")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "ctrl+s"})

	if got := src.Day().Note; got != "正常文字" {
		t.Errorf("控制字符不该进入随手记，实际 %q", got)
	}
	if strings.ContainsRune(src.Day().Note, 0) {
		t.Error("随手记里出现了 NUL")
	}
}

// TestNoteEditorTrimsTrailingSpace 验证行尾空白与末尾空行被清掉。
//
// 行首缩进**保留**（用户可能刻意排版），行尾空白是"打多了"的痕迹。
func TestNoteEditorTrimsTrailingSpace(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	openMenuOptionOK(t, m, "随手记")

	typeRunes(m, "  保留行首缩进  ")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"}) // 末尾空行
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "ctrl+s"})

	if got := src.Day().Note; got != "  保留行首缩进" {
		t.Errorf("应保留行首缩进、清掉行尾空白与末尾空行，实际 %q", got)
	}
}

// TestNoteOptionAlwaysAvailable 验证空列表上也能写随手记。
//
// 它是看板选项而不是联动选项：恰恰是"今天什么都没记"的时候最想写点什么。
func TestNoteOptionAlwaysAvailable(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	if !openMenuOptionOK(t, m, "随手记") {
		t.Fatalf("空列表上也必须能写随手记，实际 %v", m.Options())
	}
	if !strings.Contains(m.View(), "随手记") {
		t.Errorf("应进入编辑器：\n%s", m.View())
	}
}

// TestNoteOptionLabelShowsLineCount 验证标签带上已有行数。
func TestNoteOptionLabelShowsLineCount(t *testing.T) {
	src := newMemSource(t, testNow())
	src.Day().Note = "一\n二\n三"
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	labels := make([]string, 0, len(m.Options()))
	for _, b := range m.Options() {
		labels = append(labels, b.Label)
	}
	joined := strings.Join(labels, " | ")
	if !strings.Contains(joined, "3 行") {
		t.Errorf("选项标签应含「3 行」，实际 %v", labels)
	}
}

// TestNoteEditorHints 验证编辑器的按键提示是它自己那一套。
func TestNoteEditorHints(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	openMenuOptionOK(t, m, "随手记")

	hints := m.Hints()
	if !hasHint(hints, "ctrl+s") {
		t.Errorf("编辑器应提示 ctrl+s，实际 %v", hints)
	}
	if hasHint(hints, "tab") {
		// tab 是引擎永远附加的，因此这里只该确认编辑器自己的提示在。
		t.Logf("提示含 tab（引擎附加的，正常）：%v", hints)
	}
	// 确认"丢弃吗"那一屏的提示不一样。
	typeRunes(m, "x")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if !hasHint(m.Hints(), "y") {
		t.Errorf("问话时应提示 y，实际 %v", m.Hints())
	}
}
