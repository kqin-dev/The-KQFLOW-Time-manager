package plugin

import (
	"fmt"

	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
)

// 本文件是测试用的假插件与假包。
//
// 刻意做成"最小可用"而不是通用框架：测试要读起来像在描述真实场景
// （"一个只有联动选项的包"），而不是像在配置一个 mock 系统。

// fakeComp 是假组件。
type fakeComp struct {
	title string
	// renders 记录它被渲染了几次，用于断言"没装载的包不会渲染"。
	renders int
}

func (c *fakeComp) Title() string { return c.title }
func (c *fakeComp) Render(ctx RenderCtx) {
	c.renders++
}
func (c *fakeComp) Update(ctx EventCtx, ev Event) Action { return None() }

// fakePlugin 是假插件（可声明成任意 Kind）。
type fakePlugin struct {
	mf Manifest
}

func (p *fakePlugin) Manifest() Manifest { return p.mf }
func (p *fakePlugin) New(s svc.Services) (Component, error) {
	return &fakeComp{title: p.mf.Name}, nil
}

// fakeBoardOpt 是假看板选项。
type fakeBoardOpt struct {
	label string
	order int
	// view 为 nil 时 Activate 返回一个固定页面。
	view View
	err  error
}

func (o *fakeBoardOpt) Label() string { return o.label }
func (o *fakeBoardOpt) Order() int    { return o.order }

func (o *fakeBoardOpt) Activate(svc.Services) (View, error) {
	if o.err != nil {
		return nil, o.err
	}
	if o.view == nil {
		o.view = &TestView{ViewName: o.label, Lines: []string{o.label}}
	}
	return o.view, nil
}

// fakeCtxOpt 是假联动选项：这就是"给选中条目设 DDL"那一类。
type fakeCtxOpt struct {
	label    string
	kinds    []string
	requires []string
	order    int
	// activated 记录最近一次被打开时的选中项，供测试断言"传对了对象"。
	activated Selection
	// activatedCount 记录被打开的次数。
	activatedCount int
}

func (o *fakeCtxOpt) AppliesTo() []string { return o.kinds }
func (o *fakeCtxOpt) Requires() []string  { return o.requires }
func (o *fakeCtxOpt) Label(s Selection) string {
	if s.Title != "" {
		return o.label + "「" + s.Title + "」"
	}
	return o.label
}
func (o *fakeCtxOpt) Order() int { return o.order }

func (o *fakeCtxOpt) Activate(sel Selection, s svc.Services) (View, error) {
	o.activated = sel
	o.activatedCount++
	return &TestView{ViewName: o.label, Lines: []string{o.label, sel.Title}}, nil
}

// fakeService 是假服务。
type fakeService struct {
	name          string
	started       bool
	stopped       bool
	startErr      error
	givenServices svc.Services
}

func (s *fakeService) Name() string { return s.name }
func (s *fakeService) Start(v svc.Services) error {
	s.started = true
	s.givenServices = v
	return s.startErr
}
func (s *fakeService) Stop() { s.stopped = true }

// fakePack 是假整合包。
type fakePack struct {
	id       string
	name     string
	version  string
	engine   string
	members  []Plugin
	provides []string
	requires []string
	// conflicts 是互斥的包 ID。
	conflicts []string
	// enabled 为 false 表示用户关掉了它。
	enabled bool
	// assembleErr 让它装配失败。
	assembleErr error
	// assembleOrder 指向一个共享计数器，装配时自增，用于断言依赖顺序。
	assembleOrder *int
	// kernel 为真时该包提供内核。
	kernel bool
	// boardOpts / ctxOpts / services 是包提供的非磁贴成员。
	boardOpts []BoardOption
	ctxOpts   []ContextOption
	services  []Service
	// disjointTiles 为真时 Tiles() 返回的组件数与成员声明数不一致
	// （用于验证"数量对不上时不猜身份"）。
	disjointTiles bool
}

func newPack(id string) *fakePack {
	return &fakePack{
		id: id, name: id, version: "0.1.0", engine: ">=0.1 <0.2",
		enabled: true,
	}
}

// setEnabled 把包装成"用户启用/关闭"的状态，并返回自身便于链式书写。
//
// 为什么要一个方法而不是直接写 `p.enabled = false`：
// 直接赋值在**别的测试文件**里做不到（字段是包内私有的，跨文件同一个包内
// 其实可以，但读起来像在改内部状态），而且更重要的是一旦写成别的字段名，
// 编译器不会报错——测试会"绿得莫名其妙"或"红得莫名其妙"。
// 提供一个有名字的入口，语义与拼写都只有一处。
func (p *fakePack) setEnabled(on bool) *fakePack {
	p.enabled = on
	return p
}

// setEngine 把包与**全部成员**的引擎范围都设成 r。
//
// 必须一起设：ValidatePack 会校验"包声明 ⊇ 成员要求"，
// 只改包不改成员会让这个包变成"声明有缺陷"，于是测试会因为**另一个原因**
// 被拒——那种"红了但红错了地方"的结果最容易误导人。
func (p *fakePack) setEngine(r string) *fakePack {
	p.engine = r
	rg := semver.MustRange(r)
	for _, m := range p.members {
		if fp, ok := m.(*fakePlugin); ok {
			fp.mf.EngineAPI = rg
		}
	}
	return p
}

func (p *fakePack) ID() string          { return p.id }
func (p *fakePack) Name() string        { return p.name }
func (p *fakePack) Enabled() bool       { return p.enabled }
func (p *fakePack) Provides() []string  { return p.provides }
func (p *fakePack) Requires() []string  { return p.requires }
func (p *fakePack) Conflicts() []string { return p.conflicts }
func (p *fakePack) Version() semver.Version {
	return semver.MustParse(p.version)
}
func (p *fakePack) EngineAPI() semver.Range { return semver.MustRange(p.engine) }
func (p *fakePack) Members() []Plugin       { return p.members }

func (p *fakePack) Assemble(s svc.Services) (Assembled, error) {
	if p.assembleErr != nil {
		return nil, p.assembleErr
	}
	a := &fakeAssembled{pack: p, svc: s}
	if p.assembleOrder != nil {
		*p.assembleOrder++
		a.order = *p.assembleOrder
	}
	// 按成员声明顺序构造组件，维持 Tiles() 的顺序契约。
	for _, m := range p.members {
		if m == nil {
			continue
		}
		if m.Manifest().Kind == KindTile {
			a.tiles = append(a.tiles, &fakeComp{title: m.Manifest().Name})
		}
	}
	if p.disjointTiles {
		// 故意多返回一个组件：它的身份未知，引擎必须把它标成无名而不是张冠李戴。
		a.tiles = append(a.tiles, &fakeComp{title: "多余组件"})
	}
	if p.kernel {
		a.kernel = &fakeKernel{fakeComp: fakeComp{title: "KQFLOW"}}
	}
	a.boardOptions = append(a.boardOptions, p.boardOpts...)
	a.contextOptions = append(a.contextOptions, p.ctxOpts...)
	a.services = append(a.services, p.services...)
	return a, nil
}

// fakeAssembled 是装配结果。
type fakeAssembled struct {
	pack           *fakePack
	svc            svc.Services
	tiles          []Component
	kernel         Kernel
	boardOptions   []BoardOption
	contextOptions []ContextOption
	services       []Service
	disposed       bool
	order          int
}

func (a *fakeAssembled) Pack() Pack                  { return a.pack }
func (a *fakeAssembled) Kernel() Kernel              { return a.kernel }
func (a *fakeAssembled) Tiles() []Component          { return a.tiles }
func (a *fakeAssembled) BoardOptions() []BoardOption { return a.boardOptions }
func (a *fakeAssembled) ContextOptions() []ContextOption {
	return a.contextOptions
}
func (a *fakeAssembled) Services() []Service { return a.services }
func (a *fakeAssembled) Dispose()            { a.disposed = true }

// fakeKernel 是假内核。
type fakeKernel struct {
	fakeComp
	boardOptions []BoardOption
	dashboard    View
	// optionSource 记录引擎注入的查询函数，供测试断言"引擎确实注入了"。
	optionSource func() []OptionBindingView
}

func (k *fakeKernel) PowerBy() string             { return "Power by KXFLOW" }
func (k *fakeKernel) BoardOptions() []BoardOption { return k.boardOptions }

// SetOptionSource 记录注入的查询函数。
func (k *fakeKernel) SetOptionSource(f func() []OptionBindingView) { k.optionSource = f }

// Dashboard 返回底层视图；未显式给出时用一个固定的假视图。
func (k *fakeKernel) Dashboard() View {
	if k.dashboard == nil {
		k.dashboard = &TestView{ViewName: "看板", Lines: []string{"KQFLOW", "Power by KXFLOW"}}
	}
	return k.dashboard
}

// ---------- 构造辅助 ----------

// tilePlugin 造一个磁贴插件。
func tilePlugin(id, name string, anchor geometry.Anchor, priority int) *fakePlugin {
	return &fakePlugin{mf: Manifest{
		ID: id, Name: name, Kind: KindTile,
		Version:   semver.MustParse("0.1.0"),
		EngineAPI: semver.MustRange(">=0.1 <0.2"),
		Slots:     SlotPreference{Anchor: anchor, Priority: priority},
	}}
}

// floatingTilePlugin 造一个**不声明锚点**的磁贴（只给优先级）。
//
// 与 tilePlugin 分开是因为后者会把 AnchorUnset 一并塞进 SlotPreference，
// 而 SlotPreference{Anchor: AnchorUnset, Priority: 5} 与"只想给优先级"
// 在字面上无法区分——用两个函数比用一个带默认值的函数更难写错。
func floatingTilePlugin(id, name string, priority int) *fakePlugin {
	return &fakePlugin{mf: Manifest{
		ID: id, Name: name, Kind: KindTile,
		Version:   semver.MustParse("0.1.0"),
		EngineAPI: semver.MustRange(">=0.1 <0.2"),
		Slots:     SlotPreference{Priority: priority},
	}}
}

// kernelPlugin 造一个内核插件。
func kernelPlugin(id string) *fakePlugin {
	return &fakePlugin{mf: Manifest{
		ID: id, Name: "内核", Kind: KindKernel,
		Version:   semver.MustParse("0.1.0"),
		EngineAPI: semver.MustRange(">=0.1 <0.2"),
	}}
}

// servicePlugin 造一个服务插件。
func servicePlugin(id string) *fakePlugin {
	return &fakePlugin{mf: Manifest{
		ID: id, Name: id, Kind: KindService,
		Version:   semver.MustParse("0.1.0"),
		EngineAPI: semver.MustRange(">=0.1 <0.2"),
	}}
}

// ctxPlugin 造一个联动选项插件。
func ctxPlugin(id string) *fakePlugin {
	return &fakePlugin{mf: Manifest{
		ID: id, Name: id, Kind: KindContextOption,
		Version:   semver.MustParse("0.1.0"),
		EngineAPI: semver.MustRange(">=0.1 <0.2"),
	}}
}

// boardPlugin 造一个看板选项插件。
func boardPlugin(id string) *fakePlugin {
	return &fakePlugin{mf: Manifest{
		ID: id, Name: id, Kind: KindBoardOption,
		Version:   semver.MustParse("0.1.0"),
		EngineAPI: semver.MustRange(">=0.1 <0.2"),
	}}
}

// describeReport 把报告压成一行，便于断言里直接比较。
//
// 必须同时包含 warnings 与 inactive：只看 loaded/rejected 的话，
// 断言失败时输出会是"什么都没有"，根本看不出是类别判错了还是包没进来。
func describeReport(r LoadReport) string {
	out := fmt.Sprintf("loaded=%v inactive=%v kernel=%s disabled=%v",
		r.Loaded, r.Inactive, r.KernelID, r.Disabled)
	for _, w := range r.Warnings {
		out += fmt.Sprintf(" | WARN %s:%s", w.PackID, w.Reason)
	}
	for _, rj := range r.Rejected {
		out += fmt.Sprintf(" | ERR %s:%s", rj.PackID, rj.Reason)
	}
	return out
}
