package plugin

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
)

// engine 是本测试假定的引擎版本。
var engine = semver.MustParse("0.1.0")

// TestLoadRejectsEngineVersionMismatch 验证"包 ↔ 引擎"这一类冲突。
//
// 关键在于：**只有这个包被拒，其它包照常启动**——CLI 工具少一个包必须仍然可用。
func TestLoadRejectsEngineVersionMismatch(t *testing.T) {
	good := newPack("kqflow.good")
	good.members = []Plugin{tilePlugin("kqflow.good.tile", "好的磁贴", geometry.AnchorLeftTop, 0)}

	bad := newPack("kqflow.bad")
	bad.members = []Plugin{tilePlugin("kqflow.bad.tile", "坏的磁贴", geometry.AnchorRightTop, 0)}
	// 包与成员一起声明要 0.5.x：这样它是一条**合法但装不上当前引擎**的声明，
	// 被拒的原因才是"引擎版本不匹配"，而不是"声明有缺陷"。
	bad.setEngine(">=0.5 <0.6")

	m := NewManager(engine)
	rep := m.Load(0, good, bad)
	t.Logf("报告详情：%s", rep.Explain())

	if len(rep.Loaded) != 1 || rep.Loaded[0] != "kqflow.good" {
		t.Fatalf("应只装载 kqflow.good，实际 %s", describeReport(rep))
	}
	rj := rep.RejectionsOf(RejectEngineAPI)
	if len(rj) != 1 || rj[0].PackID != "kqflow.bad" {
		t.Fatalf("应拒绝 kqflow.bad（引擎版本），实际 %s", describeReport(rep))
	}
	// 拒绝原因必须**可解释**：要能看出"谁要什么、引擎是什么"。
	if !strings.Contains(rj[0].Detail, "0.5") || !strings.Contains(rj[0].Detail, "0.1.0") {
		t.Errorf("拒绝说明应含具体版本号，实际 %q", rj[0].Detail)
	}
}

// TestServiceProvidesCapability 验证包可以提供能力标记。
func TestServiceProvidesCapability(t *testing.T) {
	p := newPack("kqflow.notify")
	p.provides = []string{"notify.channel"}
	p.members = []Plugin{servicePlugin("kqflow.notify.svc")}

	m := NewManager(engine)
	rep := m.Load(0, p)
	if !rep.OK() && rep.KernelID == "" {
		// 没有内核时 OK() 为 false 是预期的；这里只关心能力登记。
		t.Log("（本用例无内核，忽略 OK）")
	}
	if len(rep.Capabilities) != 1 || rep.Capabilities[0] != "notify.channel" {
		t.Fatalf("能力应被登记为 notify.channel，实际 %v", rep.Capabilities)
	}
}

// TestCapabilityGraphAndOrder 验证能力图不动点：被依赖者先装载。
//
// 这是"关掉 TODO 与 GOAL 后 DDL 为什么没出现"要能被回答的机制基础。
func TestCapabilityGraphAndOrder(t *testing.T) {
	order := 0
	// 故意把依赖方放在参数前面：装配顺序必须由依赖关系决定，而不是注册顺序。
	ddl := newPack("kqflow.ddl")
	ddl.requires = []string{"item.selection"}
	ddl.assembleOrder = &order
	ddl.members = []Plugin{ctxPlugin("kqflow.ddl.ctx")}

	todo := newPack("kqflow.todo")
	todo.provides = []string{"item.selection"}
	todo.assembleOrder = &order
	todo.members = []Plugin{tilePlugin("kqflow.todo.fixed", "固定 TODO", geometry.AnchorLeftTop, 0)}

	m := NewManager(engine)
	rep := m.Load(0, ddl, todo)

	if len(rep.Rejected) != 0 {
		t.Fatalf("两个包都应装载，实际 %s", describeReport(rep))
	}
	// 装载列表即装配顺序：提供者在前。
	if len(rep.Loaded) != 2 || rep.Loaded[0] != "kqflow.todo" || rep.Loaded[1] != "kqflow.ddl" {
		t.Fatalf("装配顺序应为 [kqflow.todo kqflow.ddl]（被依赖者在前），实际 %v", rep.Loaded)
	}
}

// TestMissingCapabilityIsExplained 验证缺依赖时**说清缺哪一个标记**。
//
// 这是"可解释"的核心：只说"没装上"等于没有信息。
func TestMissingCapabilityIsExplained(t *testing.T) {
	ddl := newPack("kqflow.ddl")
	ddl.requires = []string{"item.selection"}
	ddl.members = []Plugin{ctxPlugin("kqflow.ddl.ctx")}

	m := NewManager(engine)
	rep := m.Load(0, ddl)

	if rep.Loaded != nil {
		t.Fatalf("缺依赖的包不应装载，实际 %s", describeReport(rep))
	}
	rj := rep.RejectionsOf(RejectMissingCapability)
	if len(rj) != 1 {
		t.Fatalf("应有一条缺能力记录，实际 %s", describeReport(rep))
	}
	if !strings.Contains(rj[0].Detail, "item.selection") {
		t.Errorf("缺能力说明必须点出具体标记，实际 %q", rj[0].Detail)
	}
}

// TestDisabledPackIsNotAnError 验证用户关闭的包记在 Disabled 而不是 Rejected。
//
// 这条很重要：关闭不是错误，但**必须留记录**——否则"我明明设了它却没了"
// 永远没有答案。
func TestDisabledPackIsNotAnError(t *testing.T) {
	off := newPack("kqflow.note")
	off.enabled = false
	off.members = []Plugin{tilePlugin("kqflow.note.tile", "随手记", geometry.AnchorRightTop, 0)}

	core := newPack("kqflow.core")
	core.kernel = true
	core.members = []Plugin{kernelPlugin("kqflow.kernel")}

	m := NewManager(engine)
	rep := m.Load(0, core, off)

	if len(rep.Disabled) != 1 || rep.Disabled[0] != "kqflow.note" {
		t.Fatalf("关闭的包应记入 Disabled，实际 %s", describeReport(rep))
	}
	if len(rep.RejectionsOf(RejectMissingCapability)) != 0 {
		t.Error("被关闭不应被当成缺能力错误")
	}
	if rep.KernelID != "kqflow.core" {
		t.Fatalf("内核应是 kqflow.core，实际 %q", rep.KernelID)
	}
}

// TestSecondKernelRejected 验证内核单例。
func TestSecondKernelRejected(t *testing.T) {
	k1 := newPack("kqflow.core")
	k1.kernel = true
	k1.members = []Plugin{kernelPlugin("kqflow.kernel")}

	k2 := newPack("other.core")
	k2.kernel = true
	k2.members = []Plugin{kernelPlugin("other.kernel")}

	m := NewManager(engine)
	rep := m.Load(0, k1, k2)

	if rep.KernelID != "kqflow.core" {
		t.Fatalf("第一个内核应生效，实际 %q", rep.KernelID)
	}
	if len(rep.RejectionsOf(RejectKernelDuplicate)) != 1 {
		t.Fatalf("第二个内核应被拒，实际 %s", describeReport(rep))
	}
}

// TestFirstKernelWinsEvenIfItFails 验证内核单例的判定顺序是对的：
// 第一个内核**因版本不匹配被拒**后，第二个必须能顶上来。
//
// 这是一个真实的边界：如果把"内核唯一"检查放在版本检查之前，
// 就会出现"两个内核都装不上，程序没有内核"的结果——而其实第二个是好的。
func TestFirstKernelWinsEvenIfItFails(t *testing.T) {
	k1 := newPack("old.core")
	k1.kernel = true
	k1.members = []Plugin{kernelPlugin("old.kernel")}
	k1.setEngine(">=0.9 <1.0") // 合法声明，但当前引擎 0.1 装不上

	k2 := newPack("new.core")
	k2.kernel = true
	k2.members = []Plugin{kernelPlugin("new.kernel")}

	m := NewManager(engine)
	rep := m.Load(0, k1, k2)

	if rep.KernelID != "new.core" {
		t.Fatalf("第一个内核装不上时，第二个应生效，实际内核=%q（报告 %s）", rep.KernelID, describeReport(rep))
	}
	if len(rep.RejectionsOf(RejectEngineAPI)) != 1 {
		t.Fatalf("第一个内核应因版本被拒，实际 %s", describeReport(rep))
	}
}

// TestConflictRejected 验证包间显式冲突，且**双向都能发现**。
func TestConflictRejected(t *testing.T) {
	a := newPack("kqflow.a")
	a.members = []Plugin{tilePlugin("kqflow.a.tile", "A", geometry.AnchorLeftTop, 0)}
	b := newPack("kqflow.b")
	b.members = []Plugin{tilePlugin("kqflow.b.tile", "B", geometry.AnchorLeftTop, 0)}
	// 只由 b 单向声明与 a 冲突。
	b.conflicts = []string{"kqflow.a"}

	m := NewManager(engine)
	rep := m.Load(0, a, b)

	if len(rep.Loaded) != 1 || rep.Loaded[0] != "kqflow.a" {
		t.Fatalf("先到者应生效，实际 %s", describeReport(rep))
	}
	if len(rep.RejectionsOf(RejectConflict)) != 1 {
		t.Fatalf("单向声明的冲突也必须被发现，实际 %s", describeReport(rep))
	}
}

// TestDuplicatePackIDRejected 验证包 ID 重复的处理：先到先得。
func TestDuplicatePackIDRejected(t *testing.T) {
	p1 := newPack("kqflow.same")
	p1.members = []Plugin{tilePlugin("kqflow.same.t1", "第一个", geometry.AnchorLeftTop, 0)}
	p2 := newPack("kqflow.same")
	p2.members = []Plugin{tilePlugin("kqflow.same.t2", "第二个", geometry.AnchorRightTop, 0)}

	m := NewManager(engine)
	rep := m.Load(0, p1, p2)

	if len(rep.Loaded) != 1 {
		t.Fatalf("只应装载一个，实际 %s", describeReport(rep))
	}
	if len(rep.RejectionsOf(RejectDuplicateID)) != 1 {
		t.Fatalf("重复 ID 应被拒，实际 %s", describeReport(rep))
	}
}

// TestDataSchemaGuard 验证数据版本保守策略：包要求低于数据实际版本时拒绝。
//
// 宁可不用，也不写坏数据——沿用 v2.1.0 的做法。
func TestDataSchemaGuard(t *testing.T) {
	p := newPack("kqflow.todo")
	p.members = []Plugin{&fakePlugin{mf: Manifest{
		ID: "kqflow.todo.fixed", Name: "固定 TODO", Kind: KindTile,
		Version:    semver.MustParse("0.1.0"),
		EngineAPI:  semver.MustRange(">=0.1 <0.2"),
		DataSchema: 1,
		Slots:      SlotPreference{Anchor: geometry.AnchorLeftTop},
	}}}

	m := NewManager(engine)
	// 数据文件已是 v2：按 v1 写的包不能碰它。
	rep := m.Load(2, p)
	rj := rep.RejectionsOf(RejectDataSchema)
	if len(rj) != 1 {
		t.Fatalf("数据版本更高的包应被拒，实际 %s", describeReport(rep))
	}
	if !strings.Contains(rj[0].Detail, "v1") || !strings.Contains(rj[0].Detail, "v2") {
		t.Errorf("说明应含两个版本号，实际 %q", rj[0].Detail)
	}
	// 数据版本相同或更低时必须放行，否则会误伤。
	m2 := NewManager(engine)
	if rep2 := m2.Load(1, p); len(rep2.Loaded) != 1 {
		t.Fatalf("数据版本相同时应放行，实际 %s", describeReport(rep2))
	}
}

// TestAssembleFailureIsRecorded 验证装配失败不会让程序崩，只记报告。
func TestAssembleFailureIsRecorded(t *testing.T) {
	p := newPack("kqflow.broken")
	p.members = []Plugin{tilePlugin("kqflow.broken.tile", "坏的", geometry.AnchorLeftTop, 0)}
	p.assembleErr = errFake("装配失败：故意")

	m := NewManager(engine)
	rep := m.Load(0, p)

	if len(rep.Loaded) != 0 {
		t.Fatalf("装配失败的包不应算装载成功，实际 %s", describeReport(rep))
	}
	rj := rep.RejectionsOf(RejectAssembleFailed)
	if len(rj) != 1 || !strings.Contains(rj[0].Detail, "故意") {
		t.Fatalf("应记录装配失败原因，实际 %s", describeReport(rep))
	}
	// 能力也不能被登记（它还依赖它提供的能力）——否则会出现"能力在、
	// 包却不在"的悬空状态，别的包会因此装上一个依赖并不存在的包。
	if len(rep.Capabilities) != 0 {
		t.Errorf("装配失败的包不应登记能力，实际 %v", rep.Capabilities)
	}
}

// TestCyclicDependencyExplained 验证环依赖被如实报告，而不是死循环。
func TestCyclicDependencyExplained(t *testing.T) {
	a := newPack("kqflow.a")
	a.provides = []string{"cap.a"}
	a.requires = []string{"cap.b"}
	a.members = []Plugin{tilePlugin("kqflow.a.tile", "A", geometry.AnchorLeftTop, 0)}

	b := newPack("kqflow.b")
	b.provides = []string{"cap.b"}
	b.requires = []string{"cap.a"}
	b.members = []Plugin{tilePlugin("kqflow.b.tile", "B", geometry.AnchorRightTop, 0)}

	m := NewManager(engine)
	rep := m.Load(0, a, b)

	if len(rep.Loaded) != 0 {
		t.Fatalf("环依赖的包都不应装载，实际 %s", describeReport(rep))
	}
	if len(rep.RejectionsOf(RejectMissingCapability)) != 2 {
		t.Fatalf("两个包都应被报缺能力，实际 %s", describeReport(rep))
	}
}

// TestEmptyPackRejected 验证空包是开发者错误，被拦下。
func TestEmptyPackRejected(t *testing.T) {
	p := newPack("kqflow.empty")
	m := NewManager(engine)
	rep := m.Load(0, p)
	rj := rep.RejectionsOf(RejectInvalid)
	if len(rj) != 1 || !strings.Contains(rj[0].Detail, "没有任何成员") {
		t.Fatalf("空包应被当成声明缺陷拦下，实际 %s", describeReport(rep))
	}
}

// TestPackEngineRangeMustCoverMembers 验证"包声明的范围必须覆盖成员要求"。
//
// 正确的关系是**包声明 ⊇ 每个成员的要求**：包声明是"我这个整包支持哪些引擎版本"
// 的对外承诺，它必须至少和内部最宽的成员要求一样宽；否则承诺就是假的——
// 引擎升到某个版本时包声称支持、成员却按另一个版本的行为写，运行期才炸。
//
// 注意"成员要求更窄"（成员 >=0.1 <0.2、包 >=0.1 <0.3）是**合法**的，
// 那是"成员更保守"，不是矛盾。这条测试用三个包把三种关系都钉住。
func TestPackEngineRangeMustCoverMembers(t *testing.T) {
	// ① 说谎的包：包说 <0.2，成员却声明 <0.3 —— 成员要求超出包承诺。
	liar := newPack("kqflow.liar")
	liar.engine = ">=0.1 <0.2"
	liarMember := tilePlugin("kqflow.liar.tile", "磁贴", geometry.AnchorLeftTop, 0)
	liarMember.mf.EngineAPI = semver.MustRange(">=0.1 <0.3")
	liar.members = []Plugin{liarMember}
	// 注意这里**不能**用 setEngine：那会把成员也一起改掉，就不是"说谎"了。

	m := NewManager(engine)
	rep := m.Load(0, liar)
	rj := rep.RejectionsOf(RejectInvalid)
	if len(rj) != 1 || !strings.Contains(rj[0].Detail, "未覆盖") {
		t.Fatalf("包声明未覆盖成员要求时应被拦下，实际 %s", describeReport(rep))
	}

	// ② 声明与成员一致的包：必须放行。
	exact := newPack("kqflow.exact")
	exact.engine = ">=0.1 <0.2"
	exact.members = []Plugin{tilePlugin("kqflow.exact.tile", "磁贴", geometry.AnchorLeftTop, 0)}
	mgr := NewManager(engine)
	if rep2 := mgr.Load(0, exact); len(rep2.Loaded) != 1 {
		t.Fatalf("声明与成员一致时应放行，实际 %s", describeReport(rep2))
	}

	// ③ 成员更窄的包：同样必须放行（成员更保守不是矛盾）。
	narrow := newPack("kqflow.narrow")
	narrow.engine = ">=0.1 <0.3"
	narrowMember := tilePlugin("kqflow.narrow.tile", "磁贴", geometry.AnchorLeftTop, 0)
	narrowMember.mf.EngineAPI = semver.MustRange(">=0.1 <0.2")
	narrow.members = []Plugin{narrowMember}
	mgr2 := NewManager(engine)
	if rep3 := mgr2.Load(0, narrow); len(rep3.Loaded) != 1 {
		t.Fatalf("成员要求更窄时应放行，实际 %s", describeReport(rep3))
	}
}

// TestManifestRequiresExplicitEngineAPI 验证未声明 EngineAPI 的插件被拦下。
//
// 这是 pitfalls 的教训：校验器遇到"读不懂/没声明"的输入时默认应当拦住，
// 否则将来引擎抬版本会静默装上一个不兼容的插件。
func TestManifestRequiresExplicitEngineAPI(t *testing.T) {
	mf := Manifest{ID: "x.y", Name: "没写版本", Kind: KindService}
	if err := mf.Validate(); err == nil {
		t.Fatal("未声明 EngineAPI 必须被拦下")
	}

	// 非磁贴不应声明槽位偏好（那是磁贴专属）。
	mf2 := Manifest{
		ID: "x.z", Name: "选项", Kind: KindBoardOption,
		EngineAPI: semver.MustRange(">=0.1 <0.2"),
		Slots:     SlotPreference{Anchor: geometry.AnchorLeftTop},
	}
	if err := mf2.Validate(); err == nil {
		t.Fatal("非磁贴声明槽位偏好应被拦下")
	}
}

// TestCapabilitySetHasAll 覆盖能力标记集合的判定（联动选项依赖它）。
func TestCapabilitySetHasAll(t *testing.T) {
	c := svc.Capability{"item.selection", "item.due"}
	if !c.HasAll([]string{"item.selection"}) {
		t.Error("单项应命中")
	}
	if !c.HasAll([]string{"item.selection", "item.due"}) {
		t.Error("多项全命中应为真")
	}
	if c.HasAll([]string{"item.selection", "nope"}) {
		t.Error("有缺项应为假")
	}
	if !c.HasAll(nil) {
		t.Error("空要求应视为满足")
	}
}

// TestPackSelfContradictionRejected 验证"自己提供自己需要的能力"这种自相矛盾被拦下。
func TestPackSelfContradictionRejected(t *testing.T) {
	p := newPack("kqflow.weird")
	p.provides = []string{"cap.x"}
	p.requires = []string{"cap.x"}
	p.members = []Plugin{tilePlugin("kqflow.weird.tile", "怪包", geometry.AnchorLeftTop, 0)}

	m := NewManager(engine)
	rep := m.Load(0, p)
	if len(rep.RejectionsOf(RejectInvalid)) != 1 {
		t.Fatalf("自相矛盾的能力声明应被拦下，实际 %s", describeReport(rep))
	}
}

// TestUnloadDisposesPacks 验证卸载会释放每个包（退出路径）。
func TestUnloadDisposesPacks(t *testing.T) {
	p := newPack("kqflow.todo")
	p.members = []Plugin{tilePlugin("kqflow.todo.fixed", "固定", geometry.AnchorLeftTop, 0)}

	m := NewManager(engine)
	m.Load(0, p)
	if len(m.Packs()) != 1 {
		t.Fatal("应装载一个包")
	}
	asm := m.Packs()[0].(*fakeAssembled)
	m.Unload()
	if !asm.disposed {
		t.Error("卸载时包应被 Dispose")
	}
	if len(m.Packs()) != 0 || m.Kernel() != nil {
		t.Error("卸载后不应留下包或内核")
	}
	// 幂等：再卸载一次不应 panic。
	m.Unload()
}

// errFake 是一个简单的错误类型，避免测试依赖 fmt.Errorf 的措辞。
type errFake string

func (e errFake) Error() string { return string(e) }
