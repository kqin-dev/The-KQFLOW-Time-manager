package kxapp

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// TestJumpToFirstAndLast 验证 g/G 跳到首/末（2.1.0 的 jumpCursor）。
//
// 列表长起来之后，这是唯一"一次到末尾"的方式。
func TestJumpToFirstAndLast(t *testing.T) {
	src := newMemSource(t, testNow())
	for _, title := range []string{"甲", "乙", "丙", "丁"} {
		src.addTodo(title, model.KindFixed)
	}

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftTop)

	// G 到末项。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "G"})
	if got := m.Selection().Title; got != "丁" {
		t.Errorf("G 应跳到末项「丁」，实际 %q", got)
	}

	// g 回首项。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "g"})
	if got := m.Selection().Title; got != "甲" {
		t.Errorf("g 应跳到首项「甲」，实际 %q", got)
	}

	// Home/End 等价（终端上更顺手）。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "end"})
	if got := m.Selection().Title; got != "丁" {
		t.Errorf("end 应跳到末项，实际 %q", got)
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "home"})
	if got := m.Selection().Title; got != "甲" {
		t.Errorf("home 应跳到首项，实际 %q", got)
	}
}

// TestJumpInGoalList 验证目标栏也能跳首/末。
func TestJumpInGoalList(t *testing.T) {
	src := newMemSource(t, testNow())
	src.addGoal("目标一")
	src.addGoal("目标二")
	src.addGoal("目标三")

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorRightTop)

	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "G"})
	if got := m.Selection().Title; got != "目标三" {
		t.Errorf("目标栏 G 应跳到末项，实际 %q", got)
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "g"})
	if got := m.Selection().Title; got != "目标一" {
		t.Errorf("目标栏 g 应跳到首项，实际 %q", got)
	}
}

// TestJumpOnEmptyListDoesNotPanic 验证空列表上跳转不炸。
//
// 空列表是常态（新用户第一次打开），越界 panic 会直接崩掉程序。
func TestJumpOnEmptyListDoesNotPanic(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftTop)

	for _, k := range []string{"g", "G", "home", "end"} {
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: k})
	}
	// 没有条目时选中应当是"列表本身"，而不是越界或空。
	if sel := m.Selection(); sel.Kind != ListRefKind {
		t.Errorf("空列表上跳转后选中应为列表本身，实际 %+v", sel)
	}
}

// TestJumpHintMentionsKeys 验证下栏提示里有 g/G。
//
// 提示与能力必须一致：能按的键要写在提示里，
// 否则用户永远不会发现它（这正是"光标只悬停"模型下提示的职责）。
func TestJumpHintMentionsKeys(t *testing.T) {
	src := newMemSource(t, testNow())
	src.addTodo("有条目", model.KindFixed)
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftTop)

	if !hasHint(m.Hints(), "g/G") {
		t.Errorf("下栏提示里应出现 g/G，实际 %v", m.Hints())
	}
	if !strings.Contains(m.View(), "g/G") {
		t.Errorf("渲染出的下栏也应含 g/G：\n%s", lastLines(m.View(), 2))
	}
}
