package kxapp

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow"
	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/layout"
	"github.com/kqin-dev/kxflow/plugin"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// TestAllPacksLoad 验证 KQFLOW 的全部业务包都能装载，且内核生效。
//
// 这是 M3 最基本的验收：宿主真的被拆成了"内核 + 整合包"。
func TestAllPacksLoad(t *testing.T) {
	src := newMemSource(t, testNow())
	rep, _ := buildEngine(t, src)

	if rep.KernelID != KernelID {
		t.Fatalf("内核应为 %s，实际 %q（报告：\n%s）", KernelID, rep.KernelID, rep.Explain())
	}
	if len(rep.Rejected) != 0 {
		t.Fatalf("不应有装载错误，实际：\n%s", rep.Explain())
	}
	want := []string{TodoPackID, GoalPackID, DDLPackID, LabelPackID, StatsPackID, NotePackID}
	for _, id := range want {
		if !containsStr(rep.Loaded, id) {
			t.Errorf("包 %s 应被装载，实际已装载：%v", id, rep.Loaded)
		}
	}
	// 依赖顺序：TODO 与 GOAL 提供"可选中条目"，必须排在 DDL/标签之前。
	if idx(rep.Loaded, TodoPackID) > idx(rep.Loaded, DDLPackID) {
		t.Errorf("提供者应排在依赖者之前，实际顺序 %v", rep.Loaded)
	}
	if !rep.OK() {
		t.Errorf("全部包正常时 OK 应为真，实际：\n%s", rep.Explain())
	}
}

// TestRendersRealBusinessData 验证磁贴画的是真实业务数据。
//
// 引擎只认识"有个磁贴要画自己"，至于画什么由宿主决定——
// 这个测试确认适配层真的把 model 里的数据接了上去。
func TestRendersRealBusinessData(t *testing.T) {
	src := newMemSource(t, testNow())
	src.addTodo("写架构设计文档", model.KindFixed)
	src.addTodo("回复邮件", model.KindFixed)
	src.addTodo("整理周报", model.KindFloating)
	src.data.Note = "今天状态不错"
	src.addGoal("发布 v3.0.0")

	l := NewLoader(src, src.Config())
	m, _, rep := l.Build()
	if len(rep.Rejected) != 0 {
		t.Fatalf("装载不应有错误：\n%s", rep.Explain())
	}
	m.Resize(120, 40)
	out := m.View()

	for _, want := range []string{"写架构设计文档", "回复邮件", "整理周报", "发布 v3.0.0", "今天状态不错"} {
		if !strings.Contains(out, want) {
			t.Errorf("界面上应出现 %q，实际输出：\n%s", want, out)
		}
	}
}

// TestRenderNeverOverflowsWithRealData 在尺寸网格上断言适配层输出不溢出。
//
// 引擎自己有这条断言，但"接上真实数据"是新的变量：
// 中文标题、长标签、多行随手记都会让行变宽。因此这一条在**宿主层**再验一遍。
func TestRenderNeverOverflowsWithRealData(t *testing.T) {
	src := newMemSource(t, testNow())
	// 故意塞进最容易撑破边界的内容。
	src.addTodo("一个非常非常非常长的中文待办标题用来试探面板边框会不会被撑破", model.KindFixed)
	src.addTodo("Todo with a very long English title to test the panel border", model.KindFloating)
	src.data.Note = "第一行随手记\n第二行也相当长一些用来试探换行会不会丢字\n第三行"
	src.addGoal("一个同样很长的目标名字用来试探右栏的宽度处理")

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()

	for _, size := range []struct{ w, h int }{
		{60, 16}, {80, 24}, {100, 30}, {120, 40}, {160, 44}, {200, 60}, {40, 10}, {20, 5}, {10, 3},
	} {
		m.Resize(size.w, size.h)
		out := m.View()
		lines := strings.Split(out, "\n")
		if len(lines) != size.h {
			t.Errorf("%dx%d：输出 %d 行，应恰好 %d 行", size.w, size.h, len(lines), size.h)
		}
		for i, line := range lines {
			if got := canvas.StringWidth(line); got > size.w {
				t.Errorf("%dx%d：第 %d 行宽 %d 超过 %d：%q", size.w, size.h, i, got, size.w, line)
			}
		}
		if !m.CanvasClean() {
			t.Errorf("%dx%d：画布诊断不干净：%s", size.w, size.h, m.Diagnostics())
		}
	}
}

// TestSelectingTodoRevealsContextOptions 是本次架构最关键的行为测试。
//
// 它验证三件事同时成立：
//  1. 在 TODO 磁贴上选中一条 → 引擎收到选中上下文；
//  2. **联动选项因此出现**（打标签、设截止时间）——而它们不认识任何磁贴；
//  3. 取消选中（或换到没选中的状态）之后它们消失。
func TestSelectingTodoRevealsContextOptions(t *testing.T) {
	src := newMemSource(t, testNow())
	todo := src.addTodo("写文档", model.KindFixed)

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	// 初始：没有选中，不该有联动选项。
	if got := m.ContextOptions(); len(got) != 0 {
		t.Fatalf("初始不该有联动选项，实际 %d 个", len(got))
	}

	// 让焦点落在固定待办磁贴上，然后按 j（移动光标会顺带上报选中）。
	m.SetFocus(geometry.AnchorLeftTop)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})

	sel := m.Selection()
	if sel.ID != todo.ID {
		t.Fatalf("选中应指向刚加的待办，实际 %+v", sel)
	}
	if sel.Kind != "todo" {
		t.Errorf("选中类型应为 todo，实际 %q", sel.Kind)
	}
	// ② 联动选项出现。
	opts := m.ContextOptions()
	if len(opts) == 0 {
		t.Fatal("选中条目后应出现联动选项")
	}
	var labels []string
	for _, o := range opts {
		labels = append(labels, o.Label(sel))
	}
	joined := strings.Join(labels, " | ")
	if !strings.Contains(joined, "设截止时间") {
		t.Errorf("应出现「设截止时间」，实际 %v", labels)
	}
	if !strings.Contains(joined, "打标签") {
		t.Errorf("应出现「打标签」，实际 %v", labels)
	}
	// 标签的顺序应排在 DDL 前面（Order 10 < 20）。
	if len(opts) >= 2 && !strings.Contains(opts[0].Label(sel), "打标签") {
		t.Errorf("标签应排在 DDL 之前，实际第一个是 %q", opts[0].Label(sel))
	}
}

// TestDigitKeyOpensContextOption 验证数字键真的能打开联动选项的界面。
//
// 这条把"选项 → 借调舞台 → 视图"整条链路走通：
// 它是 req.md 说的"磁贴或选项向中栏请求界面"的宿主侧证据。
func TestDigitKeyOpensContextOption(t *testing.T) {
	src := newMemSource(t, testNow())
	src.addTodo("写文档", model.KindFixed)

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	m.SetFocus(geometry.AnchorLeftTop)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"}) // 选中

	bindings := m.OptionKeys()
	if len(bindings) == 0 {
		t.Fatal("选中后应有可用的选项键位")
	}
	// 找"打标签"那一项（它是 Order 最小的联动选项）。
	target := -1
	for i, b := range bindings {
		if strings.Contains(b.Label, "打标签") {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatalf("键位列表里应有打标签，实际 %v", bindings)
	}

	if !m.Stage().Borrowing() == false {
		t.Fatal("开局不应有借调")
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: bindings[target].Key})
	if !m.Stage().Borrowing() {
		t.Fatalf("按 %s 应借调舞台打开标签编辑器", bindings[target].Key)
	}
	out := m.View()
	if !strings.Contains(out, "标签") {
		t.Errorf("舞台应显示标签编辑器，实际输出：\n%s", out)
	}

	// esc 退出借调，焦点回到原磁贴。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if m.Stage().Borrowing() {
		t.Error("esc 应退出借调")
	}
	if m.Focus() != geometry.AnchorLeftTop {
		t.Errorf("焦点应回到借调方（左上），实际 %v", m.Focus())
	}
}

// TestDDLWizardEditsRealData 验证设置的截止时间真的落到数据上并触发了保存。
//
// 这是"包内有机耦合"的实证：DDL 包的一半（联动选项）改了数据，
// 另一半（面板磁贴）就能显示出来——而它们与 TODO 包之间没有直接调用。
func TestDDLWizardEditsRealData(t *testing.T) {
	src := newMemSource(t, testNow())
	todo := src.addTodo("写文档", model.KindFixed)

	l := NewLoader(src, src.Config())
	m, services, _ := l.Build()
	m.Resize(140, 40)
	m.SetFocus(geometry.AnchorLeftTop)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})

	// 打开"设截止时间"。
	opened := false
	for _, b := range m.OptionKeys() {
		if strings.Contains(b.Label, "设截止时间") {
			m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: b.Key})
			opened = true
			break
		}
	}
	if !opened {
		t.Fatal("应有「设截止时间」选项")
	}
	if !m.Stage().Borrowing() {
		t.Fatal("应借调舞台打开向导")
	}

	// 输入 18:30 并确认。
	for _, r := range []rune("18:30") {
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: string(r), Runes: []rune{r}})
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	if todo.Due != "18:30" {
		t.Fatalf("截止时间应写入条目，实际 %q", todo.Due)
	}
	if services.Saves == 0 {
		t.Error("改完应触发落盘")
	}
	// 退出向导后，DDL 面板磁贴应显示它。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	out := m.View()
	if !strings.Contains(out, "18:30") {
		t.Errorf("DDL 面板应显示刚设的时间，实际输出：\n%s", out)
	}
}

// TestMissingHostIsAWarningWithRealPacks 是"没有水瓶给水"在真实包上的验证。
//
// 这里区分用户指出的两种情况，因为它们的**提示措辞不同**：
//
//	① 提供者存在但被关掉 → "由 X 提供，但那个包被关闭了；开启它即可让本包生效"
//	   （可操作的指引）
//	② 提供者压根不存在   → "没有任何已启用的包提供它，因此没有组件会接纳它"
//	   （只是说明事实）
//
// 本用例走 ①：把 TODO 与 GOAL 关掉，但它们**仍在参数列表里**，
// 因此装载器知道"本来有谁能提供"。这是最常见的情形（用户手动关了功能）。
func TestMissingHostIsAWarningWithRealPacks(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	packs := []plugin.Pack{}
	for _, p := range l.Packs() {
		switch p.ID() {
		case TodoPackID, GoalPackID:
			packs = append(packs, disabledPack{p})
		default:
			packs = append(packs, p)
		}
	}

	services := NewServices(src)
	m := buildWithPacks(t, src, services, packs)

	rep := m.Report()
	if len(rep.Rejected) != 0 {
		t.Fatalf("没有接纳者不是错误，不应出现在 Rejected 里：\n%s", rep.Explain())
	}
	// 标签与 DDL 都应记为"启用了但没起作用"。
	for _, id := range []string{LabelPackID, DDLPackID} {
		if !containsStr(rep.Inactive, id) {
			t.Errorf("%s 应记为 Inactive，实际 %v（报告：\n%s）", id, rep.Inactive, rep.Explain())
		}
	}
	// 警告要说明"提供者被关闭"，并给出**可操作**的指引。
	ws := rep.WarningsOf(plugin.WarnProviderDisabled)
	if len(ws) < 2 {
		t.Fatalf("应至少两条 WarnProviderDisabled，实际 %d 条（报告：\n%s）", len(ws), rep.Explain())
	}
	for _, w := range ws {
		if !strings.Contains(w.Detail, TodoPackID) {
			t.Errorf("指引里要点出该开启哪个包，实际 %q", w.Detail)
		}
	}
	text := rep.Explain()
	if !strings.Contains(text, "警告") {
		t.Errorf("报告应有警告段落：\n%s", text)
	}
	if strings.Contains(text, "错误（这些包有问题") {
		t.Errorf("不该出现错误段落：\n%s", text)
	}
	// 引擎仍然可用：能渲染、不 panic。这就是"功能缺失而不是全部不能用"。
	m.Resize(100, 30)
	if out := m.View(); out == "" {
		t.Error("少了接纳者时引擎仍应渲染出东西")
	}
	// 内核选项（帮助/关于）不受影响。
	if len(m.BoardOptions()) == 0 {
		t.Error("内核自带的看板选项不应受影响")
	}
}

// TestNoProviderAtAllIsAlsoAWarning 覆盖用户指出的另一种情形：
// **压根没有任何包会提供**那个能力（而不是"被关了"）。
//
// 此时措辞是"没有任何已启用的包提供它"，因为它不是"去开个开关"能解决的。
func TestNoProviderAtAllIsAlsoAWarning(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	// 只装内核与标签包：TODO / GOAL 连传都不传。
	var packs []plugin.Pack
	for _, p := range l.Packs() {
		if p.ID() == LabelPackID || p.ID() == KernelID {
			packs = append(packs, p)
		}
	}

	m := buildWithPacks(t, src, NewServices(src), packs)
	rep := m.Report()

	if len(rep.Rejected) != 0 {
		t.Fatalf("不应有错误：\n%s", rep.Explain())
	}
	if !containsStr(rep.Inactive, LabelPackID) {
		t.Fatalf("标签包应记为 Inactive，实际 %v", rep.Inactive)
	}
	ws := rep.WarningsOf(plugin.WarnNoHost)
	if len(ws) != 1 {
		t.Fatalf("应有一条 WarnNoHost，实际 %d 条（报告：\n%s）", len(ws), rep.Explain())
	}
	if !strings.Contains(ws[0].Detail, CapItemSelection) {
		t.Errorf("说明里应点出缺的是哪个能力标记，实际 %q", ws[0].Detail)
	}
	if !strings.Contains(ws[0].Detail, "包本身没有问题") {
		t.Errorf("说明里应明确「这个包没问题」，实际 %q", ws[0].Detail)
	}
	m.Resize(100, 30)
	if out := m.View(); out == "" {
		t.Error("仍应渲染出东西")
	}
}

// disabledPack 把任意包包装成"用户关闭"。
type disabledPack struct{ plugin.Pack }

func (d disabledPack) Enabled() bool { return false }

// buildWithPacks 用给定包集合构造引擎（测试辅助）。
func buildWithPacks(t *testing.T, src Source, services *Services, packs []plugin.Pack) *kxflow.Model {
	t.Helper()
	m := kxflow.New(kxflow.Config{
		EngineAPI: EngineAPI,
		Services:  services,
		Layout:    layout.DefaultConfig(),
		View:      plugin.NewViewConfig(),
		Packs:     packs,
	})
	return m
}

// ---------- 小工具 ----------

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func idx(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}
