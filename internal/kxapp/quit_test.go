package kxapp

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/plugin"
)

// press 送一个按键并返回是否请求退出。
func press(m interface {
	Dispatch(plugin.Event) bool
}, key string) bool {
	return m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: key, Runes: []rune(key)})
}

// TestQuitAsksConfirmationFirst 验证 q **不直接退出**，而是先问一句。
//
// 这条是照着 2.1.0 的 askQuit 对齐的，而且它在引擎里以前**完全缺失**：
// 下栏一直写着"q:退出"，但引擎压根没有处理 q 的分支——按了毫无反应。
//
// 功能没做出来比做错了更隐蔽：没有任何报错，而提示还在骗人。
func TestQuitAsksConfirmationFirst(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	if press(m, "q") {
		t.Fatal("按 q 不该直接退出（要先确认）")
	}
	out := m.View()
	if !strings.Contains(out, "确定要退出") {
		t.Fatalf("按 q 应弹出退出确认：\n%s", out)
	}
	// 默认选项必须是"取消"：退出确认的目的是拦住误按，
	// 而不是让用户多按一次回车。
	if !strings.Contains(out, "▸ 取消") {
		t.Errorf("默认选项应当是「取消，继续使用」：\n%s", out)
	}
}

// TestQuitConfirmEscCancels 验证 esc 取消退出且回到原来位置。
func TestQuitConfirmEscCancels(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(m.Focus()) // 保持在某个磁贴上

	before := m.Focus()
	press(m, "q")
	if !strings.Contains(m.View(), "确定要退出") {
		t.Fatal("应弹出确认")
	}
	if press(m, "esc") {
		t.Fatal("esc 不该退出")
	}
	if m.Stage().Borrowing() {
		t.Error("esc 后确认层应当关闭")
	}
	if m.Focus() != before {
		t.Errorf("取消后焦点应回到 %v，实际 %v", before, m.Focus())
	}
	// 焦点回到磁贴后，普通按键应当重新可用。
	if press(m, "q") {
		t.Fatal("再次按 q 仍然不该直接退出")
	}
}

// TestQuitConfirmEnterOnCancelKeepsRunning 验证默认项回车 = 取消。
//
// 这是"误按 q 之后顺手按回车"的路径：必须什么都不发生。
func TestQuitConfirmEnterOnCancelKeepsRunning(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	press(m, "q")
	if press(m, "enter") {
		t.Fatal("光标在「取消」上按回车不该退出")
	}
	if m.Stage().Borrowing() {
		t.Error("取消后确认层应当关闭")
	}
}

// TestQuitConfirmSecondChoiceExits 验证选到"退出"才真的退出。
func TestQuitConfirmSecondChoiceExits(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	press(m, "q")
	press(m, "j") // 移到"退出 KQFLOW"
	out := m.View()
	if !strings.Contains(out, "▸ 退出 KQFLOW") {
		t.Fatalf("光标应当移到「退出 KQFLOW」：\n%s", out)
	}
	if !press(m, "enter") {
		t.Fatal("选中「退出」后回车应当请求退出")
	}
}

// TestQuitConfirmDoesNotStack 验证连按 q 不会叠出好几层确认框。
//
// 叠层会让 esc 一次只关一层，用户以为"取消没生效"。
func TestQuitConfirmDoesNotStack(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	press(m, "q")
	depth := m.Stage().Depth()
	for i := 0; i < 3; i++ {
		press(m, "q")
	}
	if got := m.Stage().Depth(); got != depth {
		t.Errorf("连按 q 不该叠层：深度 %d → %d", depth, got)
	}
	if press(m, "esc") {
		t.Fatal("esc 不该退出")
	}
	if m.Stage().Borrowing() {
		t.Error("一次 esc 就应当关掉确认层")
	}
}

// TestQuitConfirmTakesPriorityOverMenu 验证菜单开着时按 q 先问退出。
//
// 菜单也吃 q（它把 q 当"取消"），因此"q 归谁"必须明确：
// 退出是全局动作，优先级高于当前层的取消。
// 否则用户在菜单里按 q 会以为要退出，实际只是关了菜单。
func TestQuitConfirmTakesPriorityOverMenu(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	press(m, "l") // 打开菜单
	if !m.Stage().Borrowing() {
		t.Fatal("应打开菜单")
	}
	if press(m, "q") {
		t.Fatal("按 q 不该直接退出")
	}
	if !strings.Contains(m.View(), "确定要退出") {
		t.Errorf("菜单开着时按 q 也应当弹出退出确认：\n%s", m.View())
	}
}
