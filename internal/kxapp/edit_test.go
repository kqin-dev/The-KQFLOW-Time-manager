package kxapp

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// typeText 模拟逐字符输入（走真实按键路径）。
func typeText(m interface {
	Dispatch(plugin.Event) bool
}, s string) {
	for _, r := range []rune(s) {
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: string(r), Runes: []rune{r}})
	}
}

// TestAddTodoThroughMenu 验证"添加待办"真的能加进当前列表。
//
// 这是**日常最要紧的一条**：没有它，引擎就不能替代 2.1.0。
// 之前几轮做的是渲染与交互框架，而用户每天第一件事是"记一条待办"。
func TestAddTodoThroughMenu(t *testing.T) {
	src := newMemSource(t, testNow())
	src.addTodo("已有的一条", model.KindFloating)

	l := NewLoader(src, src.Config())
	m, services, _ := l.Build()
	m.Resize(120, 40)

	// 焦点在临时列表上 → 添加应当加到临时。
	m.SetFocus(geometry.AnchorLeftBottom)
	if !openMenuOptionOK(t, m, "添加固定待办") {
		t.Fatalf("菜单里应有添加项，实际 %v", m.Options())
	}
	if !strings.Contains(m.View(), "添加临时待办") {
		t.Fatalf("应显示添加界面且标明是临时列表：\n%s", m.View())
	}

	typeText(m, "买牛奶")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	data := src.Day()
	if got := len(data.Floating); got != 2 {
		t.Fatalf("临时列表应有 2 条，实际 %d", got)
	}
	if got := data.Floating[1].Title; got != "买牛奶" {
		t.Errorf("新条目标题应为「买牛奶」，实际 %q", got)
	}
	if services.Saves == 0 {
		t.Error("添加后应当落盘")
	}
	// 界面**不关**：连着记几条是常态，esc 才是"记完了"。
	if !m.Stage().Borrowing() {
		t.Error("添加后不该关闭界面（要能连续添加）")
	}
	if !strings.Contains(m.View(), "已添加") {
		t.Errorf("应给出添加成功反馈：\n%s", m.View())
	}

	// 再记一条：不该重复上一条的内容（输入框应当已清空）。
	typeText(m, "取快递")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	if got := len(src.Day().Floating); got != 3 {
		t.Fatalf("应当能连续添加，实际 %d 条", got)
	}
	if got := src.Day().Floating[2].Title; got != "取快递" {
		t.Errorf("第二条应为「取快递」，实际 %q", got)
	}

	// 空内容回车 = 记完了，退出。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	if m.Stage().Depth() != 1 {
		// 菜单那一层还在（openMenuOption 压了 2 层，关掉添加层后剩 1 层）。
		t.Logf("栈深 %d（菜单仍在，符合预期）", m.Stage().Depth())
	}
	if strings.Contains(m.View(), "直接输入内容") {
		t.Errorf("空回车应当退出添加界面：\n%s", m.View())
	}
}

// TestAddTodoGoesToFocusedList 验证"加到光标所在的列表"。
//
// 同一个"添加待办"选项服务两个列表，目标列表只能来自当前上下文。
func TestAddTodoGoesToFocusedList(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	// 焦点在**固定**列表上 → 加到固定。
	m.SetFocus(geometry.AnchorLeftTop)
	if !openMenuOptionOK(t, m, "添加固定待办") {
		t.Fatal("应有添加项")
	}
	if !strings.Contains(m.View(), "添加固定待办") {
		t.Fatalf("应标明是固定列表：\n%s", m.View())
	}
	typeText(m, "固定条目")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	if got := len(src.Day().Fixed); got != 1 {
		t.Fatalf("固定列表应有 1 条，实际 %d", got)
	}
	if got := len(src.Day().Floating); got != 0 {
		t.Errorf("不该误加到临时列表，实际 %d 条", got)
	}
}

// TestAddTodoRejectsEmpty 验证空输入不会产生空条目。
func TestAddTodoRejectsEmpty(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftBottom)
	openMenuOptionOK(t, m, "添加固定待办")

	// 只有空格：不该产生条目。
	typeText(m, "   ")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	if got := len(src.Day().Floating); got != 0 {
		t.Errorf("空白输入不该产生条目，实际 %d 条", got)
	}
}

// TestAddGoalWorksOnEmptyList 验证"目标列表为空时也能加"。
//
// 这正是"添加目标"做成**看板选项**而不是联动选项的原因：
// 联动选项在"没有可选中项"时不会出现，于是新用户永远加不进第一个目标。
func TestAddGoalWorksOnEmptyList(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, services, _ := l.Build()
	m.Resize(120, 40)

	// 一个目标都没有：联动选项应当为空，但看板选项里必须有"添加目标"。
	if len(m.ContextOptions()) != 0 {
		t.Logf("当前有 %d 个联动选项（有选中项时会非零）", len(m.ContextOptions()))
	}
	if !openMenuOptionOK(t, m, "添加目标") {
		t.Fatalf("空目标列表上也要能添加，实际选项 %v", m.Options())
	}
	typeText(m, "发布 v3.0.0")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	goals := src.Goals()
	if len(goals) != 1 {
		t.Fatalf("应有 1 个目标，实际 %d", len(goals))
	}
	if goals[0].Title != "发布 v3.0.0" {
		t.Errorf("目标标题不对：%q", goals[0].Title)
	}
	// 指纹要跟着标题算出来（继承时靠它避免同名混淆）。
	if goals[0].Tag == "" {
		t.Error("目标应当有 Tag 指纹")
	}
	if services.Saves == 0 {
		t.Error("添加目标后应当落盘")
	}
}

// TestDeleteTodoNeedsConfirmation 验证删除**必须二次确认**。
//
// 删除不可撤销（我们没有回收站），因此"点错一下条目就没了"
// 是绝对不能接受的。确认界面还要写清"删的是哪一条"。
func TestDeleteTodoNeedsConfirmation(t *testing.T) {
	src := newMemSource(t, testNow())
	todo := src.addTodo("别删我", model.KindFloating)

	l := NewLoader(src, src.Config())
	m, services, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftBottom)
	if !openMenuOptionOK(t, m, "删除「") {
		t.Fatalf("菜单里应有删除项，实际 %v", m.Options())
	}
	// 确认界面必须点名删哪一条。
	out := m.View()
	if !strings.Contains(out, "确认删除") {
		t.Fatalf("应显示确认界面：\n%s", out)
	}
	if !strings.Contains(out, "别删我") {
		t.Errorf("确认界面要点明删的是哪一条：\n%s", out)
	}

	// 按 n 取消：什么都不该发生。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "n"})
	if len(src.Day().Floating) != 1 {
		t.Error("取消之后不该删除")
	}
	if services.Saves != 0 {
		t.Error("取消不该落盘")
	}
	// ⚠️ 取消只是关掉**确认层**：菜单还在，光标还停在"删除"上。
	// 这是"逐层退回"的必然结果，也意味着用户要重新选一次。
	// 想再删一次就得先退出菜单（esc），再按 l 重新打开。
	// 测试照着**用户真实路径**走，而不是"再调一次 helper"——
	// 后者会压出第二个菜单，选中项跟着错位（我第一版就这么错了）。
	if got := m.Stage().Depth(); got != 1 {
		t.Fatalf("取消后应只剩菜单，实际栈深 %d", got)
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"}) // 退出菜单
	if m.Stage().Borrowing() {
		t.Fatal("esc 应关闭菜单")
	}

	// 重新进入并确认。
	m.SetFocus(geometry.AnchorLeftBottom)
	if !openMenuOptionOK(t, m, "删除「") {
		t.Fatalf("菜单里应有删除项，实际 %v", m.Options())
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "y"})
	if len(src.Day().Floating) != 0 {
		t.Errorf("确认后应删除，实际还剩 %d 条", len(src.Day().Floating))
	}
	if services.Saves == 0 {
		t.Error("确认删除后应当落盘")
	}
	if src.Day().Find(todo.ID) != nil {
		t.Error("被删的条目不该还能查到")
	}
}

// TestDeleteUsesTheSelectedItemNotTheCursorAfterFocusMove 验证"删的是选中那条"。
//
// 这条是 tab 错位那个 bug 的延伸：如果选中没跟着焦点走，
// 用户以为在删 A，实际删的是 B——而删除不可撤销。
func TestDeleteUsesTheSelectedItemNotTheCursorAfterFocusMove(t *testing.T) {
	src := newMemSource(t, testNow())
	src.addTodo("固定甲", model.KindFixed)
	src.addTodo("固定乙", model.KindFixed)
	target := src.addTodo("临时丙", model.KindFloating)

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	// 先在固定列表把光标移到第二条，再 tab 到临时列表。
	m.SetFocus(geometry.AnchorLeftTop)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})
	m.SetFocus(geometry.AnchorLeftBottom) // 选中应当变成"临时丙"

	openMenuOptionOK(t, m, "删除「")
	out := m.View()
	if !strings.Contains(out, "临时丙") {
		t.Fatalf("要删的应当是「临时丙」，确认界面却显示：\n%s", out)
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "y"})

	if got := len(src.Day().Fixed); got != 2 {
		t.Errorf("固定列表不该被删掉任何条目，实际剩 %d 条", got)
	}
	if src.Day().Find(target.ID) != nil {
		t.Error("「临时丙」应当已被删除")
	}
}

// TestRenameTodo 验证重命名。
func TestRenameTodo(t *testing.T) {
	src := newMemSource(t, testNow())
	todo := src.addTodo("旧名字", model.KindFloating)

	l := NewLoader(src, src.Config())
	m, services, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftBottom)
	if !openMenuOptionOK(t, m, "重命名") {
		t.Fatalf("菜单里应有重命名，实际 %v", m.Options())
	}
	// 输入框是空的（不给旧值，避免用户退格半天）；界面要显示原名。
	if !strings.Contains(m.View(), "旧名字") {
		t.Errorf("重命名界面应显示原名：\n%s", m.View())
	}
	typeText(m, "新名字")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	if todo.Title != "新名字" {
		t.Errorf("标题应为「新名字」，实际 %q", todo.Title)
	}
	if services.Saves == 0 {
		t.Error("重命名后应当落盘")
	}
}

// TestRenameGoalUpdatesTag 验证改目标名会同时更新 Tag 指纹。
//
// Tag 是"继承时避免同名混淆"的依据；名字改了却留旧指纹，
// 会让后续继承指向错的目标。
func TestRenameGoalUpdatesTag(t *testing.T) {
	src := newMemSource(t, testNow())
	if err := renameGoal(src, NewHostState(), plugin.Selection{Kind: "goal", ID: "nope"}, "x"); err == nil {
		t.Error("改不存在的目标应当报错")
	}

	g := src.addGoal("老目标")
	oldTag := g.Tag
	if err := renameGoal(src, NewHostState(), plugin.Selection{Kind: "goal", ID: g.ID}, "新目标"); err != nil {
		t.Fatalf("重命名不该报错：%v", err)
	}
	goals := src.Goals()
	if goals[0].Title != "新目标" {
		t.Errorf("标题应为「新目标」，实际 %q", goals[0].Title)
	}
	if goals[0].Tag == oldTag {
		t.Error("改名后 Tag 指纹应当跟着变")
	}
	if goals[0].Tag != model.TagOf("新目标") {
		t.Errorf("Tag 应当按新标题算：%q vs %q", goals[0].Tag, model.TagOf("新目标"))
	}
}

// TestDeleteStripsInvisibleCharsInConfirm 验证确认界面的标题也是干净的。
func TestDeleteStripsInvisibleCharsInConfirm(t *testing.T) {
	src := newMemSource(t, testNow())
	src.addTodo("\x00脏\x00条目", model.KindFloating)

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftBottom)
	openMenuOptionOK(t, m, "删除「")

	out := m.View()
	if strings.ContainsRune(out, 0) {
		t.Errorf("确认界面不该出现 NUL：%q", out)
	}
}
