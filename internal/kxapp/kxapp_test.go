package kxapp

import (
	"fmt"
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

// TestDashboardShowsContextOptions 是"选项可被发现"的验收测试。
//
// 这条来自实机反馈：用户选中了一个待办，但中栏**没有多出**任何东西——
// 联动选项在机制上是出现了（OptionKeys 里有），可界面上没有任何地方
// 告诉用户"现在按 1 能打标签"，于是功能等于不存在。
//
// 现在内核的看板会列出选项，因此这里直接断言**渲染输出里出现了选项文字**。
func TestDashboardShowsContextOptions(t *testing.T) {
	src := newMemSource(t, testNow())
	todo := src.addTodo("拿快递", model.KindFloating)

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 34)

	// 未选中时：看板要明确给出**怎么操作**（按 l），而不是留白。
	// 只写"选中后会出现操作"是不够的——用户仍不知道按哪个键打开它。
	before := m.View()
	if !strings.Contains(before, "l 操作") {
		t.Errorf("未选中时看板应提示按 l 打开操作，实际输出：\n%s", before)
	}

	// 选中浮动待办（我们的数据里它就是"拿快递"）。
	m.SetFocus(geometry.AnchorLeftBottom)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})
	if sel := m.Selection(); sel.ID != todo.ID {
		t.Fatalf("应选中「拿快递」，实际 %+v", sel)
	}

	after := m.View()
	// 关键断言：**联动选项的文字必须出现在界面上**。
	for _, want := range []string{"打标签", "设截止时间"} {
		if !strings.Contains(after, want) {
			t.Errorf("选中后看板上应出现「%s」，实际输出：\n%s", want, after)
		}
	}
	// 键位也要显示出来，否则用户仍然不知道怎么触发。
	bindings := m.OptionKeys()
	if len(bindings) == 0 {
		t.Fatal("选中后应有可用选项")
	}
	if !strings.Contains(after, bindings[0].Key) {
		t.Errorf("看板上应显示键位 %q，实际输出：\n%s", bindings[0].Key, after)
	}
	// 选中项的名字应出现在选项标签里（"打标签「拿快递」"）。
	if !strings.Contains(after, "拿快递") {
		t.Errorf("选项标签应带上选中项名字，实际输出：\n%s", after)
	}
	// 渲染必须仍然干净。
	if !m.CanvasClean() {
		t.Errorf("画布诊断不干净：%s", m.Diagnostics())
	}
}

// TestOptionMenuIsAPendingTransaction 固化"未决事务"的语义（实机反馈）。
//
// 用户在真机上遇到的问题：
//
//	我 Tab 离开，选项还在舞台上——也就是说选项现在是随着光标触发改变的。
//
// 那说明选项被做成了"光标的副产品"。现在它们是**显式事务**：
//
//  1. 选中条目后按 l 才打开（不是光标一动就弹）；
//  2. 打开后独占焦点：tab 不再把用户带走（会提示先处理）；
//  3. 菜单里可以用方向键选择、回车执行；
//  4. esc 关闭事务，焦点交还给原磁贴。
func TestOptionMenuIsAPendingTransaction(t *testing.T) {
	src := newMemSource(t, testNow())
	src.addTodo("拿快递", model.KindFloating)

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 34)

	m.SetFocus(geometry.AnchorLeftBottom)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"}) // 选中

	// ① 光标移动本身**不**打开任何界面。
	if m.Stage().Borrowing() {
		t.Fatal("移动光标不该打开界面（选项不是光标的副产品）")
	}
	// 但选项确实已经可用（联动选项存在）。
	if len(m.ContextOptions()) == 0 {
		t.Fatal("选中后应存在联动选项")
	}

	// ② 按 l 打开事务。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "l"})
	if !m.Stage().Borrowing() {
		t.Fatal("按 l 应打开操作菜单（未决事务）")
	}
	if m.Focus() != geometry.AnchorStage {
		t.Errorf("打开事务后焦点应在舞台上，实际 %v", m.Focus())
	}
	out := m.View()
	for _, want := range []string{"可用操作", "打标签", "设截止时间"} {
		if !strings.Contains(out, want) {
			t.Errorf("事务界面里应出现 %q：\n%s", want, out)
		}
	}

	// ③ 事务独占焦点：tab 不能把用户带走。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "tab"})
	if m.Focus() != geometry.AnchorStage {
		t.Errorf("未决事务期间 tab 不该切换焦点，实际焦点 %v", m.Focus())
	}
	if !m.Stage().Borrowing() {
		t.Error("未决事务期间 tab 不该关闭它")
	}

	// ④ 菜单里用方向键选择、esc 取消。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "k"})
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if m.Stage().Borrowing() {
		t.Error("esc 应关闭事务")
	}
	if m.Focus() != geometry.AnchorLeftBottom {
		t.Errorf("关闭事务后焦点应交还给原磁贴（左下），实际 %v", m.Focus())
	}
}

// TestMenuEnterOpensEditorAndEscReturnsToMenu 验证事务内的两级结构。
//
// 在菜单里回车打开真正的编辑界面后，esc 应当**先退回菜单**而不是
// 一路退回看板——填错一个字符不必重开菜单。
func TestMenuEnterOpensEditorAndEscReturnsToMenu(t *testing.T) {
	src := newMemSource(t, testNow())
	src.addTodo("拿快递", model.KindFloating)

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 34)
	m.SetFocus(geometry.AnchorLeftBottom)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "l"})

	if depth := m.Stage().Depth(); depth != 1 {
		t.Fatalf("打开菜单后栈深应为 1，实际 %d", depth)
	}
	// 光标停在第一项（打标签），回车进入编辑界面。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	if depth := m.Stage().Depth(); depth != 2 {
		t.Fatalf("回车后应进入编辑界面（栈深 2），实际 %d", depth)
	}
	if !strings.Contains(m.View(), "按数字键切换") {
		t.Errorf("应显示标签编辑器：\n%s", m.View())
	}
	// 第一次 esc：退回菜单（仍在事务里）。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if depth := m.Stage().Depth(); depth != 1 {
		t.Fatalf("esc 应退回菜单（栈深 1），实际 %d", depth)
	}
	// 第二次 esc：关闭事务。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if m.Stage().Borrowing() {
		t.Error("第二次 esc 应关闭事务")
	}
}

// TestStageIsInFocusRingWhileBorrowing 验证"tab 能回到舞台"（实机反馈第 3 条）。
//
//	用户的原话：理论上应该可以通过 TAB 回到舞台按方向键。
func TestStageIsInFocusRingWhileBorrowing(t *testing.T) {
	src := newMemSource(t, testNow())
	src.addTodo("拿快递", model.KindFloating)

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 34)
	m.SetFocus(geometry.AnchorLeftBottom)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})

	// 未借调时舞台不在焦点环里（它是底色，不是一种磁贴）。
	for i := 0; i < 10; i++ {
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "tab"})
		if m.Focus() == geometry.AnchorStage {
			t.Fatal("栈空时舞台不该进入焦点环（看板是底色，不是磁贴）")
		}
	}

	// 借调之后（帮助页），tab 应当能转到舞台。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "3"}) // 打开"帮助"
	if !m.Stage().Borrowing() {
		t.Fatal("应借调打开帮助页")
	}
	seen := false
	for i := 0; i < 12; i++ {
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "tab"})
		if m.Focus() == geometry.AnchorStage {
			seen = true
			break
		}
	}
	if !seen {
		t.Error("借调期间 tab 应当能转到舞台")
	}
}

// TestLWithoutOptionsExplains 验证没有可打开项时**说清原因**。
//
// 按键不能"按了没反应"：用户会以为程序卡了。
func TestLWithoutOptionsExplains(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 34)

	// 没有任何条目 → 没有选中 → 按 l 应当给出提示而不是静默。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "l"})
	if m.Stage().Borrowing() {
		t.Error("没有可操作项时不该打开空菜单")
	}
	out := m.View()
	if !strings.Contains(out, "先选中") {
		t.Errorf("按 l 而无可操作项时应给出提示：\n%s", out)
	}
}

// TestSaveFailureIsSurfaced 验证落盘失败会**显示给用户**。
//
// 实机反馈里出现过这个现象（沙盒里 Access is denied）：
//
//	保存失败：创建临时文件失败: open …\.2026-10.json.tmp…: Access is denied.
//
// 它是否在沙盒里"正常"并不重要——重要的是这条信息必须出现在界面上：
// 用户改了东西却不知道没存上，是数据工具最不能接受的一类沉默失败。
func TestSaveFailureIsSurfaced(t *testing.T) {
	src := newMemSource(t, testNow())
	src.addTodo("拿快递", model.KindFloating)
	src.saveErr = fmt.Errorf("创建临时文件失败: Access is denied")

	l := NewLoader(src, src.Config())
	m, services, _ := l.Build()
	m.Resize(120, 34)
	m.SetFocus(geometry.AnchorLeftBottom)
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})

	// 勾选会触发落盘（Persist），落盘失败必须变成界面上的提示。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: " "})
	if services.Saves != 0 {
		t.Fatalf("落盘失败时不该计入成功次数，实际 %d", services.Saves)
	}
	out := m.View()
	if !strings.Contains(out, "保存失败") {
		t.Errorf("落盘失败必须显示给用户，实际输出：\n%s", out)
	}
	if !strings.Contains(out, "Access is denied") {
		t.Errorf("提示里应带上底层原因（用户据此才能定位）：\n%s", out)
	}
	if !m.CanvasClean() {
		t.Errorf("画布诊断不干净：%s", m.Diagnostics())
	}
}

// TestBoardOptionsHaveNoDuplicates 是"选项重复"那次事故的墓碑。
//
// 内核本身也是一个已装载的包，而 Manager.BoardOptions 既从 m.kernel
// 收它的选项，又遍历所有包再收一遍——于是每个内核选项出现两次，
// 看板上是"1 帮助 / 2 帮助 / 3 关于 / 4 关于"，按 1 和按 2 效果一样。
func TestBoardOptionsHaveNoDuplicates(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 34)

	opts := m.BoardOptions()
	seen := map[string]int{}
	for _, o := range opts {
		seen[o.Label()]++
	}
	for label, n := range seen {
		if n > 1 {
			t.Errorf("看板选项 %q 出现了 %d 次，应当只有一次", label, n)
		}
	}
	// 键位是按键去重的最后一道防线：同一个键不能对应两个选项。
	keys := map[string]int{}
	for _, b := range m.OptionKeys() {
		keys[b.Key]++
	}
	for k, n := range keys {
		if n > 1 {
			t.Errorf("键位 %q 绑定了 %d 个选项", k, n)
		}
	}
	if len(opts) == 0 {
		t.Fatal("内核应至少提供帮助与关于两个看板选项")
	}
}

// TestLongTextIsWrappedNotTruncated 是"关于磁贴只剩半句话"的墓碑。
//
// 实机反馈：磁贴里那句"（焦点在本磁贴时按 enter 会上报选中）"
// 被截断成"（焦点在本磁贴时按 enter 会上"。这里断言折行的语义：
// 长文本要么**完整出现**（跨行），要么因为行数不够而整段不出现，
// 绝不出现"半句话"。
//
// 实现上刻意**不依赖骨架布局**：`Model.Layout()` 返回的是最近一次渲染
// 算出的骨架，没渲染过就是零值（这一条本人踩过两次，测试因此自己出错）。
// 因此这里直接在一个固定宽度的矩形里渲染真正的 todoTile。
func TestLongTextIsWrappedNotTruncated(t *testing.T) {
	src := newMemSource(t, testNow())

	// 造一个必然折行的标题：按"磁贴内容区 21 列"推导（见下方矩形宽度）。
	const contentW = 21
	rowW := itemRowWidth(geometry.Rect{W: contentW})
	if rowW < 6 {
		t.Fatalf("正文只有 %d 列，用例无意义", rowW)
	}
	perLine := rowW / 2 // 汉字占 2 列
	title := strings.Repeat("中", perLine+2)
	src.addTodo(title, model.KindFloating)

	tile := &todoTile{src: src, state: NewHostState(), kind: model.KindFloating, title: "临时"}
	wantWrapped := canvas.Wrap(title, rowW)
	if len(wantWrapped) < 2 {
		t.Fatalf("标题应当折行，实际只占 %d 行（正文 %d 列，标题 %d 字）",
			len(wantWrapped), rowW, len([]rune(title)))
	}

	// 渲染到一块够高的矩形里（高度保证所有折行都放得下）。
	out := renderInRect(t, contentW+4, 6+len(wantWrapped), func(ctx plugin.RenderCtx) {
		tile.Render(ctx)
	})
	var rendered strings.Builder
	for _, l := range out {
		rendered.WriteString(l)
	}
	flat := flattenForTextMatch(rendered.String())
	if !strings.Contains(flat, flattenForTextMatch(title)) {
		t.Errorf("长标题应完整出现在折行后的输出里（标题 %d 字，正文 %d 列，应折 %d 行）\n实际输出：\n%s",
			len([]rune(title)), rowW, len(wantWrapped), strings.Join(out, "\n"))
	}
	// 断言每一段都真的出现：只查整体拼接会掩盖"中间掉了一段"。
	for i, seg := range wantWrapped {
		if seg == "" {
			continue
		}
		if !strings.Contains(flat, flattenForTextMatch(seg)) {
			t.Errorf("折行后的第 %d 段 %q 没有出现在输出里\n实际输出：\n%s",
				i, seg, strings.Join(out, "\n"))
		}
	}
}

// flattenForTextMatch 把一帧输出压成"只含可见文字"的连续字符串。
//
// 用途是断言"文字完整出现（可能跨行、可能被边框字符隔开）"：
// 直接对多行输出做 Contains 会因为换行而失败，而去掉换行又会把
// 相邻两行的边框字符混进来（例如"│TODA│"）。
// 因此这里只保留中日韩文字、字母与数字。
func flattenForTextMatch(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 0x4E00 && r <= 0x9FFF: // CJK 统一表意文字
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestOptionsNeverCollideWithDock 验证"看板不侵占停靠区"。
//
// 这条是 60×16 那次事故的回归测试：舞台曾经用 Primary（含停靠区高度）
// 而不是 Center.Stage，于是看板的文字直接画在停靠区磁贴上面，
// 画布诊断报出 8 次"覆盖已有内容"。
func TestOptionsNeverCollideWithDock(t *testing.T) {
	src := newMemSource(t, testNow())
	src.addTodo("拿快递", model.KindFloating)
	src.addTodo("写文档", model.KindFixed)
	src.data.Note = "随手记内容"

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	// 选中一项，让看板上多出几行选项文字——最容易压到停靠区的正是它。
	for i := 0; i < 3; i++ {
		m.SetFocus(geometry.AnchorLeftBottom)
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})
	}
	for _, size := range []struct{ w, h int }{
		{60, 16}, {70, 20}, {80, 24}, {100, 30}, {120, 40},
	} {
		m.Resize(size.w, size.h)
		_ = m.View()
		if !m.CanvasClean() {
			t.Errorf("%dx%d：画布诊断不干净：%s", size.w, size.h, m.Diagnostics())
		}
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
