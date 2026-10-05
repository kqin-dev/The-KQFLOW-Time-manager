package kxapp

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// TestSubtaskEnterAndExit 验证 enter 进子任务、esc 退回条目。
//
// 这是 2.1.0 里每天在用的功能（app.go:648 enterSubTasks），
// 也是检查表里"切默认前的必补项"第一条。
func TestSubtaskEnterAndExit(t *testing.T) {
	src := newMemSource(t, testNow())
	todo := src.addTodo("写设计文档", model.KindFixed)
	todo.Tasks = []model.Task{model.NewTask("列提纲"), model.NewTask("写初稿")}

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftTop)

	// 列表态：看不到子任务。
	if strings.Contains(m.View(), "列提纲") {
		t.Fatalf("列表态不该直接显示子任务：\n%s", m.View())
	}

	// enter 进入子任务模式。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	out := m.View()
	if !strings.Contains(out, "列提纲") || !strings.Contains(out, "写初稿") {
		t.Fatalf("进入子任务后应列出子任务：\n%s", out)
	}
	if !strings.Contains(out, "写设计文档") {
		t.Errorf("子任务视图应标出「这是谁的子任务」：\n%s", out)
	}
	// 选中应当变成子任务。
	if sel := m.Selection(); sel.Kind != "subtask" {
		t.Errorf("子任务模式下选中的应是 subtask，实际 %q", sel.Kind)
	}

	// esc 退回条目（而不是关掉别的层）。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	out = m.View()
	if strings.Contains(out, "列提纲") {
		t.Errorf("esc 后应退回条目列表：\n%s", out)
	}
	if sel := m.Selection(); sel.Kind != "todo" {
		t.Errorf("退回后选中的应回到 todo，实际 %q", sel.Kind)
	}
}

// TestSubtaskEscDoesNotPopStage 验证子任务里的 esc 不会顺手关掉借调层。
//
// 这是 esc 的"两层含义"冲突：引擎把它当"退出上一层"，
// 而子任务模式需要它做"从子任务退回条目"。
// 引擎在分派 esc 之前必须先问磁贴（plugin.ModalOwner），
// 否则按一次 esc 会把两层一起退掉。
func TestSubtaskEscDoesNotPopStage(t *testing.T) {
	src := newMemSource(t, testNow())
	todo := src.addTodo("写设计文档", model.KindFixed)
	todo.Tasks = []model.Task{model.NewTask("列提纲")}

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftTop)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	// 子任务模式开着时，esc 归磁贴，不该被引擎用掉。
	depth := m.Stage().Depth()
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if m.Stage().Depth() != depth {
		t.Errorf("子任务里的 esc 不该改变栈深：%d → %d", depth, m.Stage().Depth())
	}
}

// TestSubtaskToggleSyncsParent 验证勾选子任务会**回写父条目状态**。
//
// 父条目的完成状态是**派生**的（all ⇒ 完成、some ⇒ 进行中）。
// 各写各的一定会不一致，而那种不一致在界面上表现为
// "子任务全勾了、父条目还没完成"——用户会以为数据坏了。
func TestSubtaskToggleSyncsParent(t *testing.T) {
	src := newMemSource(t, testNow())
	todo := src.addTodo("写设计文档", model.KindFixed)
	todo.Tasks = []model.Task{model.NewTask("列提纲"), model.NewTask("写初稿")}

	l := NewLoader(src, src.Config())
	m, services, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftTop)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	// 勾第一条：父条目应当变成"进行中"，但**未完成**。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: " "})
	if !todo.Tasks[0].Done() {
		t.Fatal("第一条子任务应当被勾上")
	}
	if todo.Done {
		t.Error("只完成一半时父条目不该算完成")
	}
	if todo.Status != model.StatusDoing {
		t.Errorf("部分完成时父条目状态应为 doing，实际 %q", todo.Status)
	}
	if services.Saves == 0 {
		t.Error("勾选子任务应当落盘")
	}

	// 再勾第二条：父条目应当自动完成。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: " "})
	if !todo.Done || todo.Status != model.StatusDone {
		t.Errorf("子任务全完成时父条目也应完成，实际 done=%v status=%q",
			todo.Done, todo.Status)
	}

	// 取消一条：父条目应当被拉回来。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: " "})
	if todo.Done {
		t.Error("取消一条子任务后父条目不该还是完成")
	}
	if todo.Status != model.StatusDoing {
		t.Errorf("取消一条后父条目应为 doing，实际 %q", todo.Status)
	}
}

// TestSubtaskCursorStaysInBounds 验证子任务光标不会越界。
func TestSubtaskCursorStaysInBounds(t *testing.T) {
	src := newMemSource(t, testNow())
	todo := src.addTodo("只有一条子任务", model.KindFixed)
	todo.Tasks = []model.Task{model.NewTask("唯一")}

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftTop)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	// 连按 j 不该把光标推到列表外。
	for i := 0; i < 5; i++ {
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: " "})
	if !todo.Tasks[0].Done() {
		t.Error("光标应当停在唯一那条上，空格应勾上它")
	}
}

// TestSubtaskEmptyGivesToast 验证没有子任务时**明确告知**怎么办。
//
// 静默无反应会让人以为程序坏了（2.1.0 的提示也是这句）。
func TestSubtaskEmptyGivesToast(t *testing.T) {
	src := newMemSource(t, testNow())
	src.addTodo("没有子任务", model.KindFixed)

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftTop)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	if !strings.Contains(m.View(), "还没有子任务") {
		t.Errorf("应当提示「该项还没有子任务」：\n%s", m.View())
	}
}

// TestSubtaskHintsFollowMode 验证下栏提示在子任务模式下跟着换。
func TestSubtaskHintsFollowMode(t *testing.T) {
	src := newMemSource(t, testNow())
	todo := src.addTodo("有条目", model.KindFixed)
	todo.Tasks = []model.Task{model.NewTask("子任务一")}

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftTop)

	listHints := m.Hints()
	if !hasHint(listHints, "enter") {
		t.Errorf("列表态应提示 enter 进子任务，实际 %v", listHints)
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	subHints := m.Hints()
	if !hasHint(subHints, "esc") {
		t.Errorf("子任务态应提示 esc 退回，实际 %v", subHints)
	}
	if !hasHint(subHints, "space") {
		t.Errorf("子任务态应提示 space 勾选，实际 %v", subHints)
	}
}

// TestAddSubtaskThroughMenu 验证"加子任务"选项真的能加进去。
func TestAddSubtaskThroughMenu(t *testing.T) {
	src := newMemSource(t, testNow())
	todo := src.addTodo("写设计文档", model.KindFixed)

	l := NewLoader(src, src.Config())
	m, services, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftTop)

	// 聚焦时应自动选中第一条，因此菜单里就有"加子任务"。
	if !openMenuOptionOK(t, m, "加子任务") {
		t.Fatalf("菜单里应有「加子任务」，实际 %v", m.Options())
	}
	for _, r := range []rune("列提纲") {
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: string(r), Runes: []rune{r}})
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	if len(todo.Tasks) != 1 {
		t.Fatalf("应加入 1 条子任务，实际 %d", len(todo.Tasks))
	}
	if todo.Tasks[0].Title != "列提纲" {
		t.Errorf("子任务标题不对：%q", todo.Tasks[0].Title)
	}
	if services.Saves == 0 {
		t.Error("加子任务后应当落盘")
	}

	// 连续添加：输入框应当已清空。
	for _, r := range []rune("写初稿") {
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: string(r), Runes: []rune{r}})
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	if len(todo.Tasks) != 2 {
		t.Fatalf("应当能连续添加，实际 %d 条", len(todo.Tasks))
	}
	if todo.Tasks[1].Title != "写初稿" {
		t.Errorf("第二条标题不对：%q", todo.Tasks[1].Title)
	}
}

// TestSubtaskButtonsDoNotLeakIntoTopList 验证退出子任务后 j/k 回到顶层列表。
func TestSubtaskButtonsDoNotLeakIntoTopList(t *testing.T) {
	src := newMemSource(t, testNow())
	first := src.addTodo("甲", model.KindFixed)
	first.Tasks = []model.Task{model.NewTask("甲的子任务")}
	src.addTodo("乙", model.KindFixed)

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftTop)

	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"}) // 进甲的子任务
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})   // 退回列表
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})     // 移到乙

	if sel := m.Selection(); sel.Title != "乙" {
		t.Errorf("退回列表后 j 应当移到「乙」，实际选中 %q", sel.Title)
	}
}
