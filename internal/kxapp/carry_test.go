package kxapp

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/plugin"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// yesterday 造一个"昨天"的日数据（含固定、临时、已完成、子任务）。
func yesterday(day string) *model.DayData {
	d := model.NewDayData(day, testNow())
	d.Fixed = append(d.Fixed, func() *model.Todo {
		t := model.NewTodo("每日阅读", model.KindFixed, day, testNow())
		t.Tasks = []model.Task{model.NewTask("读 10 页")}
		return t
	}())
	unfinished := model.NewTodo("写周报", model.KindFloating, day, testNow())
	unfinished.Tasks = []model.Task{model.NewTask("收集数据")}
	d.Floating = append(d.Floating, unfinished)
	done := model.NewTodo("已经做完的", model.KindFloating, day, testNow())
	done.Done, done.Status = true, model.StatusDone
	d.Floating = append(d.Floating, done)
	return d
}

// TestCarryFromYesterday 验证继承昨日的三种模式。
//
// 这是 2.1.0 的 askCarry（app.go:887），也是检查表里"切默认前必补项"
// 第三条——每天开工第一步就是"把昨天没做完的拉过来"。
func TestCarryFromYesterday(t *testing.T) {
	src := newMemSource(t, testNow())
	src.extraDays = []*model.DayData{yesterday("2026-10-02")}

	l := NewLoader(src, src.Config())
	m, _, rep := l.Build()
	if !containsStr(rep.Loaded, CarryPackID) {
		t.Fatalf("继承包应被装载，实际 %v", rep.Loaded)
	}
	m.Resize(120, 40)

	// 菜单里的标签要带上"昨日是哪天"。
	if !openMenuOptionOK(t, m, "继承昨日") {
		t.Fatalf("菜单里应有继承昨日，实际 %v", m.Options())
	}
	out := m.View()
	if !strings.Contains(out, "2026-10-02") {
		t.Errorf("应点明从哪一天继承：\n%s", out)
	}
	// 每一项都要带数量——用户据此决定要不要做。
	for _, want := range []string{"固定 1", "未完成 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("选择项应含数量 %q：\n%s", want, out)
		}
	}

	// 默认项是"两者都继承"。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	data := src.Day()
	if len(data.Fixed) != 1 || data.Fixed[0].Title != "每日阅读" {
		t.Errorf("固定项继承失败：%+v", data.Fixed)
	}
	if len(data.Floating) != 1 || data.Floating[0].Title != "写周报" {
		t.Errorf("未完成的临时项应被继承：%+v", data.Floating)
	}
	// 已完成的不该被继承。
	for _, x := range data.Floating {
		if x.Title == "已经做完的" {
			t.Error("已完成的临时项不该被继承")
		}
	}
	// 来源标记要写上（以后能查出"这条是从哪天带过来的"）。
	if data.Fixed[0].CarriedFrom != "2026-10-02" {
		t.Errorf("应记下来源日期，实际 %q", data.Fixed[0].CarriedFrom)
	}
	// 子任务要一并带过来。
	if len(data.Fixed[0].Tasks) != 1 {
		t.Errorf("固定项的子任务应一并继承，实际 %d 条", len(data.Fixed[0].Tasks))
	}
	if len(data.Floating[0].Tasks) != 1 {
		t.Errorf("临时项的子任务应一并继承，实际 %d 条", len(data.Floating[0].Tasks))
	}
	// 带过来的子任务必须是**未完成**状态（昨天的进度不该自动算今天完成）。
	for _, task := range data.Fixed[0].Tasks {
		if task.Done() {
			t.Error("继承来的子任务应当是未完成状态")
		}
	}
}

// TestCarryOnlyFixedOrOnlyFloating 验证"只拉固定 / 只继承未完成"两条路。
func TestCarryOnlyFixedOrOnlyFloating(t *testing.T) {
	for _, tc := range []struct {
		name      string
		moves     int // 从"两者都继承"往下移几格
		wantFixed int
		wantFloat int
	}{
		{"只拉固定待办", 1, 1, 0},
		{"只继承未完成的待办", 2, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := newMemSource(t, testNow())
			src.extraDays = []*model.DayData{yesterday("2026-10-02")}
			l := NewLoader(src, src.Config())
			m, _, _ := l.Build()
			m.Resize(120, 40)

			if !openMenuOptionOK(t, m, "继承昨日") {
				t.Fatal("菜单里应有继承昨日")
			}
			for i := 0; i < tc.moves; i++ {
				m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})
			}
			m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

			data := src.Day()
			if len(data.Fixed) != tc.wantFixed {
				t.Errorf("固定项应 %d 条，实际 %d", tc.wantFixed, len(data.Fixed))
			}
			if len(data.Floating) != tc.wantFloat {
				t.Errorf("临时项应 %d 条，实际 %d", tc.wantFloat, len(data.Floating))
			}
		})
	}
}

// TestCarryNeitherJustCloses 验证"都不继承"只关界面、不加东西。
func TestCarryNeitherJustCloses(t *testing.T) {
	src := newMemSource(t, testNow())
	src.extraDays = []*model.DayData{yesterday("2026-10-02")}
	l := NewLoader(src, src.Config())
	m, services, _ := l.Build()
	m.Resize(120, 40)

	if !openMenuOptionOK(t, m, "继承昨日") {
		t.Fatal("菜单里应有继承昨日")
	}
	for i := 0; i < 3; i++ { // 移到最后一项"都不继承"
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	data := src.Day()
	if len(data.Fixed)+len(data.Floating) != 0 {
		t.Errorf("「都不继承」不该加入任何条目：固定 %d 临时 %d",
			len(data.Fixed), len(data.Floating))
	}
	// ⚠️ 借调层还在（菜单那一层在下面撑着），因此不能断言 Borrowing()==false。
	// 这里断言的是"继承界面本身已经关掉"——它关掉后就显示菜单了。
	if strings.Contains(m.View(), "都不继承") {
		t.Errorf("选完之后应当关闭继承界面：\n%s", m.View())
	}
	// 要记下"问过了"，否则以后会反复打扰（与 2.1.0 的 skipCarry 同义）。
	if !data.CarryAsked {
		t.Error("「都不继承」应当记下 CarryAsked")
	}
	if services.Saves == 0 {
		t.Error("选择结果应当落盘")
	}
}

// TestCarryDeduplicatesByTitle 验证同名条目不会重复继承。
//
// 这是 store 里已有的规则（CarryFixed/CarryFloating 都按小写标题去重），
// 引擎层必须走同一条路——规则有两份实现就会不一致。
func TestCarryDeduplicatesByTitle(t *testing.T) {
	src := newMemSource(t, testNow())
	src.extraDays = []*model.DayData{yesterday("2026-10-02")}
	// 今天已经有同名条目了。
	src.addTodo("每日阅读", model.KindFixed)

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	if !openMenuOptionOK(t, m, "继承昨日") {
		t.Fatal("菜单里应有继承昨日")
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	data := src.Day()
	count := 0
	for _, x := range data.Fixed {
		if x.Title == "每日阅读" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("同名条目不该重复添加，实际 %d 条", count)
	}
}

// TestCarryWithoutYesterdayExplains 验证没有昨日数据时**说明原因**。
func TestCarryWithoutYesterdayExplains(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	if !openMenuOptionOK(t, m, "继承昨日") {
		t.Fatal("菜单里应有继承昨日（即使没有可继承的，入口也该在）")
	}
	if !strings.Contains(m.View(), "没有可继承的昨日数据") {
		t.Errorf("应说明为什么没得继承：\n%s", m.View())
	}
	// 这种状态下任意键应当能退出，不能把用户困住。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	if strings.Contains(m.View(), "没有可继承的昨日数据") {
		t.Error("没有可继承数据时按回车应当关闭界面")
	}
}

// TestCarryEscCancels 验证 esc 取消继承。
func TestCarryEscCancels(t *testing.T) {
	src := newMemSource(t, testNow())
	src.extraDays = []*model.DayData{yesterday("2026-10-02")}
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	if !openMenuOptionOK(t, m, "继承昨日") {
		t.Fatal("菜单里应有继承昨日")
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if strings.Contains(m.View(), "从 2026-10-02 继承") {
		t.Error("esc 应当关闭继承界面")
	}
	data := src.Day()
	if len(data.Fixed)+len(data.Floating) != 0 {
		t.Error("esc 取消不该继承任何条目")
	}
}

// TestCarryIsIndependentOfSelection 验证继承是**全局动作**，不需要先选中条目。
//
// 它是看板选项而不是联动选项：继承是"对今天这个整体"的动作，
// 不该要求用户先选中什么（否则空列表上就没法继承——而那正是最需要的时候）。
func TestCarryIsIndependentOfSelection(t *testing.T) {
	src := newMemSource(t, testNow())
	src.extraDays = []*model.DayData{yesterday("2026-10-02")}
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	// 空列表 → 选中是"列表本身"，但继承项照样要在。
	if !openMenuOptionOK(t, m, "继承昨日") {
		t.Fatalf("空列表上也必须能继承，实际选项 %v", m.Options())
	}
}
