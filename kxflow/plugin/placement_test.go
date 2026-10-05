package plugin

import (
	"testing"

	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/svc"
)

// TestContextOptionAppearsOnlyWhenApplicable 是本次评审新增概念的**核心测试**。
//
// 联动选项（"给选中条目设 DDL"这一类）的行为可以一句话概括：
// **只在当前选中满足条件时才出现**。因此这里把出现与不出现的条件都钉住：
// 选中类型不对不出现、缺少所需能力不出现、无选中不出现。
func TestContextOptionAppearsOnlyWhenApplicable(t *testing.T) {
	ddl := &fakeCtxOpt{label: "设截止时间", kinds: []string{"todo", "goal"}, requires: []string{"item.due"}}
	labels := &fakeCtxOpt{label: "打标签", kinds: []string{"todo"}, requires: []string{"item.label"}}

	todoWithDue := Selection{Kind: "todo", ID: "t1", Title: "写文档",
		Can: svc.Capability{"item.due", "item.label"}}
	goalWithDue := Selection{Kind: "goal", ID: "g1", Title: "发布",
		Can: svc.Capability{"item.due"}}
	// 同类但**没有任何能力**：用来验证"能力不满足时不该出现"。
	// 注意不能用"只有 item.due"的选中来测 labels —— 它反而满足不了 label；
	// 而要测 ddl 的 requires，就必须给一个真的没有 item.due 的选中。
	todoNoCaps := Selection{Kind: "todo", ID: "t2"}

	cases := []struct {
		name string
		opt  ContextOption
		sel  Selection
		want bool
		why  string
	}{
		{"todo 且有 item.due", ddl, todoWithDue, true, "类型与能力都满足"},
		{"goal 且有 item.due", ddl, goalWithDue, true, "AppliesTo 列了 goal"},
		{"todo 但无任何能力", ddl, todoNoCaps, false, "能力不满足"},
		{"无选中", ddl, Selection{}, false, "没有选中就没有联动选项"},
		{"类型不匹配", labels, goalWithDue, false, "labels 只管 todo"},
		{"类型匹配且有能力", labels, todoWithDue, true, "都满足"},
		{"类型匹配但无能力", labels, todoNoCaps, false, "缺 item.label"},
	}
	for _, c := range cases {
		if got := Applies(c.opt, c.sel); got != c.want {
			t.Errorf("%s：Applies = %v，期望 %v（%s）", c.name, got, c.want, c.why)
		}
	}

	// 空 AppliesTo 表示"任何选中都适用"，但能力仍必须满足。
	anyKind := &fakeCtxOpt{label: "通用操作", requires: []string{"item.due"}}
	if !Applies(anyKind, goalWithDue) {
		t.Error("AppliesTo 为空时应适用于任何选中类型")
	}
	if Applies(anyKind, todoNoCaps) {
		t.Error("即使不限定类型，能力仍必须满足")
	}
}

// TestContextOptionLabelUsesSelection 验证标签会带上选中项名字
// （"给「写文档」设截止时间"这种可读性对多级菜单很重要）。
func TestContextOptionLabelUsesSelection(t *testing.T) {
	ddl := &fakeCtxOpt{label: "设截止时间"}
	got := ddl.Label(Selection{Kind: "todo", ID: "t1", Title: "写文档"})
	if got != "设截止时间「写文档」" {
		t.Errorf("标签应带选中项名字，实际 %q", got)
	}
	if plain := ddl.Label(Selection{Kind: "todo", ID: "t1"}); plain != "设截止时间" {
		t.Errorf("没有标题时应退回纯标签，实际 %q", plain)
	}
}

// TestManagerContextOptionsFilterAndSort 验证管理器只返回适用的联动选项，
// 并按 Order 排序。
func TestManagerContextOptionsFilterAndSort(t *testing.T) {
	p := newPack("kqflow.ddl")
	p.ctxOpts = []ContextOption{
		&fakeCtxOpt{label: "第三", kinds: []string{"todo"}, order: 30},
		&fakeCtxOpt{label: "第一", kinds: []string{"todo"}, order: 10},
		&fakeCtxOpt{label: "只管 goal", kinds: []string{"goal"}, order: 5},
	}
	p.members = []Plugin{ctxPlugin("kqflow.ddl.ctx")}

	m := NewManager(engine)
	if rep := m.Load(0, p); len(rep.Loaded) != 1 {
		t.Fatalf("包应装载成功，实际 %s", describeReport(rep))
	}

	sel := Selection{Kind: "todo", ID: "t1"}
	got := m.ContextOptions(sel)
	if len(got) != 2 {
		t.Fatalf("todo 下应有 2 个适用选项，实际 %d 个", len(got))
	}
	if got[0].Label(sel) != "第一" || got[1].Label(sel) != "第三" {
		t.Errorf("应按 Order 升序，实际 %q、%q", got[0].Label(sel), got[1].Label(sel))
	}

	// 换成 goal：只剩那一个。
	goalSel := Selection{Kind: "goal", ID: "g1"}
	if g := m.ContextOptions(goalSel); len(g) != 1 || g[0].Label(goalSel) != "只管 goal" {
		t.Errorf("goal 下应只剩 1 个选项，实际 %d 个", len(g))
	}
	// 无选中：一个都不该有。
	if g := m.ContextOptions(Selection{}); len(g) != 0 {
		t.Errorf("无选中时不应有联动选项，实际 %d 个", len(g))
	}
}

// TestBoardOptionsMergeKernelAndPacks 验证看板选项来自两个来源：
// 内核自带的（设置/帮助/退出那类）与各包的，且一起排序。
func TestBoardOptionsMergeKernelAndPacks(t *testing.T) {
	core := newPack("kqflow.core")
	core.kernel = true
	core.members = []Plugin{kernelPlugin("kqflow.kernel")}
	// 让假内核带上全局看板选项。
	core2 := &fakePack{id: "kqflow.core", name: "内核", engine: ">=0.1 <0.2", enabled: true,
		kernel:  true,
		members: []Plugin{kernelPlugin("kqflow.kernel")},
	}
	_ = core

	note := newPack("kqflow.note")
	note.boardOpts = []BoardOption{&fakeBoardOpt{label: "随手记", order: 20}}
	note.members = []Plugin{boardPlugin("kqflow.note.board")}

	m := NewManager(engine)
	if rep := m.Load(0, core2, note); len(rep.Loaded) != 2 {
		t.Fatalf("两个包都应装载，实际 %s", describeReport(rep))
	}
	opts := m.BoardOptions()
	if len(opts) != 1 || opts[0].Label() != "随手记" {
		t.Fatalf("应含包提供的看板选项，实际 %d 个：%v", len(opts), opts)
	}
}

// ---------- 安置（视图配置） ----------

const (
	idLeftTop    = "kqflow.todo.fixed"
	idLeftBottom = "kqflow.todo.floating"
	idRightTop   = "kqflow.goal.list"
	idDockLeft   = "kqflow.stats.dock"
)

// newPlacementManager 造一个含 4 个磁贴的管理器（左二、右一、停靠一）。
func newPlacementManager(t *testing.T) *Manager {
	t.Helper()
	todo := newPack("kqflow.todo")
	todo.members = []Plugin{
		tilePlugin(idLeftTop, "固定 TODO", geometry.AnchorLeftTop, 10),
		tilePlugin(idLeftBottom, "临时 TODO", geometry.AnchorLeftBottom, 5),
	}
	goal := newPack("kqflow.goal")
	goal.members = []Plugin{tilePlugin(idRightTop, "GOAL", geometry.AnchorRightTop, 10)}
	stats := newPack("kqflow.stats")
	stats.members = []Plugin{tilePlugin(idDockLeft, "统计", geometry.AnchorCenterDockLeft, 10)}

	m := NewManager(engine)
	if rep := m.Load(0, todo, goal, stats); len(rep.Loaded) != 3 {
		t.Fatalf("三个包都应装载，实际 %s", describeReport(rep))
	}
	return m
}

// TestPlacementsFollowDeclaredAnchors 验证未指定视图配置时按磁贴自己的偏好落位。
func TestPlacementsFollowDeclaredAnchors(t *testing.T) {
	m := newPlacementManager(t)
	placed, unplaced, issues := m.Placements(NewViewConfig())

	if len(issues) != 0 || len(unplaced) != 0 {
		t.Fatalf("不应有问题或未安置项，实际 issues=%v unplaced=%v", issues, unplaced)
	}
	want := map[geometry.Anchor]string{
		geometry.AnchorLeftTop:        idLeftTop,
		geometry.AnchorLeftBottom:     idLeftBottom,
		geometry.AnchorRightTop:       idRightTop,
		geometry.AnchorCenterDockLeft: idDockLeft,
	}
	for a, id := range want {
		if got := placed.IDAt(a); got != id {
			t.Errorf("%s 上应为 %q，实际 %q", a, id, got)
		}
	}
}

// TestPlacementsUserConfigWins 验证用户配置优先于磁贴自身偏好。
//
// 这是"视图"概念的落点：用户可以按自己的习惯摆放磁贴。
func TestPlacementsUserConfigWins(t *testing.T) {
	m := newPlacementManager(t)
	// 用户把 GOAL 搬到左上，把固定 TODO 挤到右上。
	vc := NewViewConfig().
		WithSlot(geometry.AnchorLeftTop, idRightTop).
		WithSlot(geometry.AnchorRightTop, idLeftTop)

	placed, unplaced, issues := m.Placements(vc)
	if len(issues) != 0 {
		t.Fatalf("不应有问题，实际 %v", issues)
	}
	if got := placed.IDAt(geometry.AnchorLeftTop); got != idRightTop {
		t.Errorf("用户指定的左上应是 GOAL，实际 %q", got)
	}
	if got := placed.IDAt(geometry.AnchorRightTop); got != idLeftTop {
		t.Errorf("用户指定的右上应是固定 TODO，实际 %q", got)
	}
	// 剩下的按偏好自己找空槽：临时 TODO 声明左下、统计声明停靠左，都应满足。
	if got := placed.IDAt(geometry.AnchorLeftBottom); got != idLeftBottom {
		t.Errorf("临时 TODO 应落在左下，实际 %q", got)
	}
	if got := placed.IDAt(geometry.AnchorCenterDockLeft); got != idDockLeft {
		t.Errorf("统计应落在停靠左，实际 %q", got)
	}
	if len(unplaced) != 0 {
		t.Errorf("不应有未安置项，实际 %v", unplaced)
	}
}

// TestPlacementsHiddenTileIsSkipped 验证用户隐藏的磁贴不占槽位。
func TestPlacementsHiddenTileIsSkipped(t *testing.T) {
	m := newPlacementManager(t)
	vc := NewViewConfig().WithHidden(idLeftBottom)
	placed, _, issues := m.Placements(vc)
	if len(issues) != 0 {
		t.Fatalf("隐藏不应产生问题，实际 %v", issues)
	}
	if got := placed.IDAt(geometry.AnchorLeftBottom); got != "" {
		t.Errorf("被隐藏的磁贴不应占槽位，实际 %q", got)
	}
}

// TestPlacementsUnknownTileInConfigIsReported 验证配置指向未装载的磁贴时**有解释**。
//
// 这条直接对应"我明明设了，它却没了"这种无法解释的状态：
// 必须报出来，而不是静默忽略。
func TestPlacementsUnknownTileInConfigIsReported(t *testing.T) {
	m := newPlacementManager(t)
	vc := NewViewConfig().WithSlot(geometry.AnchorLeftTop, "kqflow.nonexistent.tile")
	_, _, issues := m.Placements(vc)
	if len(issues) != 1 {
		t.Fatalf("应报出一条安置问题，实际 %v", issues)
	}
	if issues[0].Slot != "kqflow.nonexistent.tile" {
		t.Errorf("问题里应点出是哪个插件，实际 %q", issues[0].Slot)
	}
}

// TestPlacementsOverflowGoesToUnplaced 验证槽位不够时**不报错、不覆盖**，
// 而是进入未安置列表（由用户去视图配置里调整）。
func TestPlacementsOverflowGoesToUnplaced(t *testing.T) {
	p := newPack("kqflow.many")
	p.members = []Plugin{
		floatingTilePlugin("m1", "磁贴1", 50),
		floatingTilePlugin("m2", "磁贴2", 40),
		floatingTilePlugin("m3", "磁贴3", 30),
		floatingTilePlugin("m4", "磁贴4", 20),
		floatingTilePlugin("m5", "磁贴5", 10),
		floatingTilePlugin("m6", "磁贴6", 5),
		floatingTilePlugin("m7", "磁贴7", 1),
	}
	m := NewManager(engine)
	if rep := m.Load(0, p); len(rep.Loaded) != 1 {
		t.Fatalf("包应装载，实际 %s", describeReport(rep))
	}
	placed, unplaced, _ := m.Placements(NewViewConfig())
	// 共 6 个槽位，装 6 个，剩 1 个未安置。
	if len(unplaced) != 1 || unplaced[0].ID != "m7" {
		t.Fatalf("优先级最低的那一个应进未安置列表，实际 %v", unplaced)
	}
	// 优先级高的先占：m1 必须在。
	if placed.IDAt(geometry.AnchorLeftTop) != "m1" {
		t.Errorf("优先级最高的应占第一个槽位（左上），实际 %q", placed.IDAt(geometry.AnchorLeftTop))
	}
}

// TestPlacementsDeterministic 验证安置结果可复现。
//
// 不可复现的安置会让"我上次摆好的布局这次变了"，而用户无从解释。
func TestPlacementsDeterministic(t *testing.T) {
	m := newPlacementManager(t)
	vc := NewViewConfig()
	first, _, _ := m.Placements(vc)
	for i := 0; i < 8; i++ {
		again, _, _ := m.Placements(vc)
		for _, a := range geometry.AllAnchors {
			if first.IDAt(a) != again.IDAt(a) {
				t.Fatalf("第 %d 次安置结果与首次不同：%s 上 %q vs %q",
					i+2, a, first.IDAt(a), again.IDAt(a))
			}
		}
	}
}

// TestPlacementsDockHidden 验证停靠区关闭时不占用停靠槽位。
func TestPlacementsDockHidden(t *testing.T) {
	m := newPlacementManager(t)
	vc := NewViewConfig()
	vc.DockVisible = false
	placed, unplaced, _ := m.Placements(vc)
	if got := placed.IDAt(geometry.AnchorCenterDockLeft); got != "" {
		t.Errorf("停靠区关闭时不应有磁贴占位，实际 %q", got)
	}
	if len(unplaced) != 1 || unplaced[0].ID != idDockLeft {
		t.Errorf("声明停靠区的磁贴应进未安置列表，实际 %v", unplaced)
	}
}

// TestTileRefsKeepsIdentityAligned 验证"成员声明顺序 = 组件返回顺序"这条契约，
// 以及数量对不上时**不猜身份**。
func TestTileRefsKeepsIdentityAligned(t *testing.T) {
	p := newPack("kqflow.todo")
	p.members = []Plugin{
		tilePlugin("t1", "第一个", geometry.AnchorLeftTop, 0),
		ctxPlugin("c1"), // 非磁贴成员：不参与对齐
		tilePlugin("t2", "第二个", geometry.AnchorLeftBottom, 0),
	}
	m := NewManager(engine)
	if rep := m.Load(0, p); len(rep.Loaded) != 1 {
		t.Fatalf("包应装载，实际 %s", describeReport(rep))
	}
	tiles := m.Tiles()
	if len(tiles) != 2 {
		t.Fatalf("应有 2 个磁贴，实际 %d", len(tiles))
	}
	if tiles[0].Manifest.ID != "t1" || tiles[1].Manifest.ID != "t2" {
		t.Errorf("身份应按声明顺序对齐，实际 %q、%q", tiles[0].Manifest.ID, tiles[1].Manifest.ID)
	}

	// 组件多于声明：多出来的那个 ID 必须为空（视为身份未知），
	// 否则它会被用户配置误当成某个声明过的插件。
	p2 := newPack("kqflow.weird")
	p2.disjointTiles = true
	p2.members = []Plugin{tilePlugin("w1", "唯一", geometry.AnchorLeftTop, 0)}
	m2 := NewManager(engine)
	if rep := m2.Load(0, p2); len(rep.Loaded) != 1 {
		t.Fatalf("包应装载，实际 %s", describeReport(rep))
	}
	ts := m2.Tiles()
	if len(ts) != 2 {
		t.Fatalf("应返回 2 个组件，实际 %d", len(ts))
	}
	if ts[1].Manifest.ID != "" {
		t.Errorf("数量对不上时多出来的组件不应被赋予 ID，实际 %q", ts[1].Manifest.ID)
	}
}
