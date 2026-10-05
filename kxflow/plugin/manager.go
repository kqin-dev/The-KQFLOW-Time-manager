package plugin

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
)

// RejectReason 是拒绝装载的原因类别。
//
// 分类而不是只留一句话，是为了让"为什么我的 DDL 没出现"能被**程序化地**
// 回答（设置页要按类别展示，而人类读的说明放在 Detail 里）。
type RejectReason uint8

const (
	// RejectEngineAPI 引擎版本不匹配。
	RejectEngineAPI RejectReason = iota
	// RejectKernelDuplicate 已经有内核了（内核是单例）。
	RejectKernelDuplicate
	// RejectMissingCapability 依赖的能力没有被任何已装载的包提供。
	RejectMissingCapability
	// RejectConflict 与另一个包显式冲突。
	RejectConflict
	// RejectDuplicateID 包 ID 重复。
	RejectDuplicateID
	// RejectDataSchema 要求的数据结构版本低于数据实际版本。
	RejectDataSchema
	// RejectDisabled 用户关掉了它（不是错误，但也要有记录）。
	RejectDisabled
	// RejectInvalid 包自身声明有缺陷（开发者错误）。
	RejectInvalid
	// RejectAssembleFailed 装配失败（Assemble 返回错误或返回 nil）。
	RejectAssembleFailed
)

// String 返回中文说明，供界面与报告直接显示。
func (r RejectReason) String() string {
	switch r {
	case RejectEngineAPI:
		return "引擎版本不匹配"
	case RejectKernelDuplicate:
		return "已有内核"
	case RejectMissingCapability:
		return "缺少依赖能力"
	case RejectConflict:
		return "与其它包冲突"
	case RejectDuplicateID:
		return "包 ID 重复"
	case RejectDataSchema:
		return "数据结构版本不兼容"
	case RejectDisabled:
		return "已被关闭"
	case RejectInvalid:
		return "包声明有缺陷"
	case RejectAssembleFailed:
		return "装配失败"
	}
	return "未知原因"
}

// Rejection 是一条拒绝记录。**必须可解释**：只说"没装上"等于没有信息。
type Rejection struct {
	PackID string
	Pack   string // 显示名
	Reason RejectReason
	Detail string // 人读的说明，含具体缺失的能力标记或版本号
}

// String 便于测试与日志。
func (r Rejection) String() string {
	return fmt.Sprintf("%s（%s）：%s —— %s", r.Pack, r.PackID, r.Reason, r.Detail)
}

// LoadReport 是一次装载的完整结果。
//
// 设计原则：**"少一个包"必须仍然可用，"起不来"才是事故**。
// 因此装载期的失败一律记录在 Rejected 里，不 panic 也不整体失败；
// 只有"包自身声明有缺陷"（开发者错误）才由 ValidatePack 在装载期就拦住。
type LoadReport struct {
	// Loaded 是装载成功的包 ID，顺序即装配顺序（依赖在前）。
	Loaded []string
	// Rejected 是未能装载的包及原因。
	Rejected []Rejection
	// Disabled 是用户显式关掉的包 ID（不是错误）。
	Disabled []string
	// KernelID 是最终生效的内核包 ID（无内核时为空）。
	KernelID string
	// Capabilities 是装载完成后实际可用的能力标记集合（排序去重）。
	Capabilities []string
}

// OK 报告是否至少有一个内核，且没有任何"非关闭"的拒绝。
func (r LoadReport) OK() bool {
	if r.KernelID == "" {
		return false
	}
	for _, rj := range r.Rejected {
		if rj.Reason != RejectDisabled {
			return false
		}
	}
	return true
}

// RejectionsOf 返回指定原因的拒绝记录，便于设置页按类别展示。
func (r LoadReport) RejectionsOf(reason RejectReason) []Rejection {
	var out []Rejection
	for _, rj := range r.Rejected {
		if rj.Reason == reason {
			out = append(out, rj)
		}
	}
	return out
}

// Explain 生成人读的装载摘要，直接可用于启动报告或设置页。
func (r LoadReport) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "已装载 %d 个包", len(r.Loaded))
	if r.KernelID != "" {
		fmt.Fprintf(&b, "（内核 %s）", r.KernelID)
	} else {
		b.WriteString("（**无内核**）")
	}
	if len(r.Disabled) > 0 {
		fmt.Fprintf(&b, "；已关闭：%s", strings.Join(r.Disabled, "、"))
	}
	if len(r.Rejected) > 0 {
		b.WriteString("\n未装载：")
		for _, rj := range r.Rejected {
			fmt.Fprintf(&b, "\n  · %s", rj)
		}
	}
	return b.String()
}

// Manager 负责装载整合包、裁决冲突、安置磁贴。
//
// 装载被刻意拆成**三个互不重叠的阶段**（见 Load 的说明）：
// 单包体检 → 依赖不动点定序 → 按序装配。混在一起写会出现"装配顺序取决于
// 参数顺序""冲突判定看错了对象"这类难以察觉的错误——它们真的发生过。
type Manager struct {
	engineAPI semver.Version
	kernel    Kernel
	packs     []Assembled
	report    LoadReport
}

// NewManager 创建管理器。engineAPI 是**当前引擎**的版本。
func NewManager(engineAPI semver.Version) *Manager {
	return &Manager{engineAPI: engineAPI, report: LoadReport{}}
}

// EngineAPI 返回当前引擎版本。
func (m *Manager) EngineAPI() semver.Version { return m.engineAPI }

// Report 返回最近一次装载的报告。
func (m *Manager) Report() LoadReport { return m.report }

// Packs 返回装载成功的包（装配顺序）。
func (m *Manager) Packs() []Assembled { return m.packs }

// Kernel 返回生效的内核（可能为 nil——此时引擎应当显示"未装载内核"而不是崩）。
func (m *Manager) Kernel() Kernel { return m.kernel }

// candidate 是装载过程中的一个候选包。
type candidate struct {
	pack Pack
	// reject 非空表示它在单包体检阶段就被否决了。
	reject *Rejection
	// admitted 表示它通过了依赖不动点。
	admitted bool
}

// Load 按 design §4.4 的表逐条裁决并装配。
//
// 三个阶段（顺序是刻意的）：
//
//  1. **单包体检**：只看这个包自己——声明是否自洽、引擎版本、ID 是否重复、
//     用户是否关闭、数据版本。冲突与内核唯一**不放这里**，因为它们要跟
//     "最终真的装上了哪些包"比，而不是跟"传进来哪些包"比。
//  2. **依赖不动点定序**：反复扫描，把"Requires 都已被满足"的包依次接纳。
//     接纳顺序即拓扑序（被依赖者在前），因此**不需要单独做拓扑排序**；
//     环依赖的包永远等不到依赖，会被如实报成缺能力而不是死循环。
//  3. **按序装配**：按接纳顺序调用 Assemble，并在此刻裁决冲突与内核唯一。
//     放在这里是因为此刻"谁真的装上了"才确定——用传入顺序判决会冤枉好包。
//
// dataSchema 是数据文件的实际版本（0 表示不检查）。
func (m *Manager) Load(dataSchema int, packs ...Pack) LoadReport {
	m.kernel = nil
	m.packs = nil
	rep := LoadReport{}

	cands := m.precheck(dataSchema, packs, &rep)
	order := m.admit(cands, &rep)
	m.assemble(cands, order, &rep)
	m.finishRejections(cands, order, &rep)

	rep.Capabilities = providedCapabilities(order)
	m.report = rep
	return rep
}

// precheck 是第 1 阶段：只依据包自身的信息做体检。
func (m *Manager) precheck(dataSchema int, packs []Pack, rep *LoadReport) []*candidate {
	cands := make([]*candidate, 0, len(packs))
	seenID := map[string]bool{}
	for _, p := range packs {
		c := &candidate{pack: p}
		cands = append(cands, c)

		if p == nil {
			c.reject = &Rejection{Pack: "(nil)", Reason: RejectInvalid, Detail: "包为 nil"}
			continue
		}
		if err := ValidatePack(p); err != nil {
			c.reject = &Rejection{PackID: p.ID(), Pack: p.Name(), Reason: RejectInvalid, Detail: err.Error()}
			continue
		}
		if !p.Enabled() {
			rep.Disabled = append(rep.Disabled, p.ID())
			c.reject = &Rejection{PackID: p.ID(), Pack: p.Name(), Reason: RejectDisabled,
				Detail: "用户在配置里关闭了这个包"}
			continue
		}
		if seenID[p.ID()] {
			c.reject = &Rejection{PackID: p.ID(), Pack: p.Name(), Reason: RejectDuplicateID,
				Detail: "已有同 ID 的包，后到者被拒绝（先到先得）"}
			continue
		}
		seenID[p.ID()] = true

		if !p.EngineAPI().Match(m.engineAPI) {
			c.reject = &Rejection{PackID: p.ID(), Pack: p.Name(), Reason: RejectEngineAPI,
				Detail: fmt.Sprintf("包要求引擎 %s，当前引擎 %s", p.EngineAPI(), m.engineAPI)}
			continue
		}
		// 数据版本：保守策略——包要求低于数据实际版本时拒绝，
		// 宁可不用也不写坏数据（沿用 v2.1.0 的做法）。
		if schema, ok := maxDataSchema(p); ok && dataSchema > 0 && schema < dataSchema {
			c.reject = &Rejection{PackID: p.ID(), Pack: p.Name(), Reason: RejectDataSchema,
				Detail: fmt.Sprintf("包按数据结构 v%d 编写，数据文件已是 v%d；升级程序或移走数据",
					schema, dataSchema)}
			continue
		}
	}
	return cands
}

// admit 是第 2 阶段：依赖不动点，返回**确定的装配顺序**。
//
// 注意接纳与装配是分开的：接纳只需要能力图信息（不需要真的构造组件），
// 因此可以先把顺序算准，再统一装配。曾经把两者混在一个循环里，
// 结果是"装配顺序取决于参数顺序"——一个只能靠测试才发现的错误。
func (m *Manager) admit(cands []*candidate, rep *LoadReport) []*candidate {
	provided := map[string]bool{}
	var order []*candidate
	for progress := true; progress; {
		progress = false
		for _, c := range cands {
			if c.reject != nil || c.admitted {
				continue
			}
			if missing := firstMissing(c.pack.Requires(), provided); missing != "" {
				continue
			}
			c.admitted = true
			order = append(order, c)
			progress = true
			for _, cap := range c.pack.Provides() {
				provided[cap] = true
			}
		}
	}
	return order
}

// assemble 是第 3 阶段：按序装配，并在此刻裁决冲突与内核唯一。
func (m *Manager) assemble(cands []*candidate, order []*candidate, rep *LoadReport) {
	// installed 记录"已经装上的包"，值是该候选（这样才能查到对方向我方声明的冲突）。
	installed := map[string]*candidate{}
	for _, c := range order {
		p := c.pack
		// 内核唯一：内核是单例，先到者生效。
		if hasKernel(p) && m.kernel != nil {
			c.admitted = false
			c.reject = &Rejection{PackID: p.ID(), Pack: p.Name(), Reason: RejectKernelDuplicate,
				Detail: "已经有内核包生效，内核是单例"}
			continue
		}
		// 显式冲突：只与**真的装上了**的包比。
		if other, ok := conflictWith(p, installed); ok {
			c.admitted = false
			c.reject = &Rejection{PackID: p.ID(), Pack: p.Name(), Reason: RejectConflict,
				Detail: fmt.Sprintf("与包 %s 互斥", other)}
			continue
		}
		asm, err := p.Assemble(svc.Noop{})
		if err != nil || asm == nil {
			detail := "Assemble 返回 nil"
			if err != nil {
				detail = err.Error()
			}
			c.admitted = false
			c.reject = &Rejection{PackID: p.ID(), Pack: p.Name(),
				Reason: RejectAssembleFailed, Detail: detail}
			continue
		}
		installed[p.ID()] = c
		m.packs = append(m.packs, asm)
		rep.Loaded = append(rep.Loaded, p.ID())
		if k := asm.Kernel(); k != nil && m.kernel == nil {
			m.kernel = k
			rep.KernelID = p.ID()
		}
	}
}

// finishRejections 把没能装载的包整理成报告。
//
// 依赖等不到的包要**如实点出缺哪一个标记**——这正是
// "关掉 TODO 与 GOAL 后 DDL 为什么没出现"的答案。
func (m *Manager) finishRejections(cands []*candidate, order []*candidate, rep *LoadReport) {
	provided := map[string]bool{}
	for _, c := range order {
		if c.admitted {
			for _, cap := range c.pack.Provides() {
				provided[cap] = true
			}
		}
	}
	for _, c := range cands {
		if c.reject != nil {
			rep.Rejected = append(rep.Rejected, *c.reject)
			continue
		}
		if c.admitted {
			continue
		}
		missing := firstMissing(c.pack.Requires(), provided)
		detail := fmt.Sprintf("依赖的能力标记 %q 没有任何已装载的包提供", missing)
		if missing == "" {
			// Requires 都满足了却没被接纳：只可能是环依赖。
			detail = fmt.Sprintf("依赖 %v 形成环，无法确定装配顺序", c.pack.Requires())
		}
		rep.Rejected = append(rep.Rejected, Rejection{
			PackID: c.pack.ID(), Pack: c.pack.Name(),
			Reason: RejectMissingCapability, Detail: detail,
		})
	}
}

// Unload 卸载全部包（幂等）。退出时调用。
func (m *Manager) Unload() {
	for _, p := range m.packs {
		if p != nil {
			p.Dispose()
		}
	}
	m.packs = nil
	m.kernel = nil
}

// ContextOptions 返回当前选中下**适用**的全部联动选项，按 Order 排序。
//
// 判定统一走 Applies（只有一处实现），因此"这个选项该不该出现"
// 不会在不同地方得出不同答案。
func (m *Manager) ContextOptions(sel Selection) []ContextOption {
	type item struct {
		opt ContextOption
		id  string
	}
	var items []item
	for _, a := range m.packs {
		for _, o := range a.ContextOptions() {
			if o == nil || !Applies(o, sel) {
				continue
			}
			items = append(items, item{opt: o, id: a.Pack().ID()})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		oi, oj := items[i].opt.Order(), items[j].opt.Order()
		if oi != oj {
			return oi < oj
		}
		return items[i].id < items[j].id
	})
	out := make([]ContextOption, 0, len(items))
	for _, it := range items {
		out = append(out, it.opt)
	}
	return out
}

// BoardOptions 返回全部看板选项（内核自带的 + 各包的），按 Order 排序。
func (m *Manager) BoardOptions() []BoardOption {
	type item struct {
		opt BoardOption
		id  string
	}
	var items []item
	if m.kernel != nil {
		for _, o := range m.kernel.BoardOptions() {
			items = append(items, item{opt: o, id: "kernel"})
		}
	}
	for _, a := range m.packs {
		for _, o := range a.BoardOptions() {
			if o == nil {
				continue
			}
			items = append(items, item{opt: o, id: a.Pack().ID()})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		oi, oj := items[i].opt.Order(), items[j].opt.Order()
		if oi != oj {
			return oi < oj
		}
		return items[i].id < items[j].id
	})
	out := make([]BoardOption, 0, len(items))
	for _, it := range items {
		out = append(out, it.opt)
	}
	return out
}

// ---------- 内部辅助 ----------

func hasKernel(p Pack) bool {
	for _, m := range p.Members() {
		if m != nil && m.Manifest().Kind == KindKernel {
			return true
		}
	}
	return false
}

func maxDataSchema(p Pack) (int, bool) {
	max, found := 0, false
	for _, m := range p.Members() {
		if m == nil {
			continue
		}
		if s := m.Manifest().DataSchema; s > 0 {
			found = true
			if s > max {
				max = s
			}
		}
	}
	return max, found
}

// firstMissing 返回第一个未被提供的能力标记；全都有则返回空串。
//
// 只报"第一个"而不报全部：报告的用途是让用户知道**下一步该做什么**
// （去开启哪个包），一次给出一个明确的阻塞点比给一串更有用。
func firstMissing(requires []string, provided map[string]bool) string {
	for _, r := range requires {
		if !provided[r] {
			return r
		}
	}
	return ""
}

// conflictWith 报告 p 是否与**已经装上的**某个包互斥。
//
// **双向都要查**：A 声明"与 B 冲突"、B 完全没提 A，这在语义上同样是冲突，
// 而且是很容易只实现一半的地方（只查一方向时会表现为"结果取决于参数顺序"）。
func conflictWith(p Pack, installed map[string]*candidate) (string, bool) {
	for id, c := range installed {
		if id == p.ID() {
			continue
		}
		if contains(p.Conflicts(), id) || contains(c.pack.Conflicts(), p.ID()) {
			return id, true
		}
	}
	return "", false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// providedCapabilities 汇总"实际装上的包"提供的能力标记（排序去重）。
func providedCapabilities(order []*candidate) []string {
	seen := map[string]bool{}
	for _, c := range order {
		if !c.admitted {
			continue
		}
		for _, cap := range c.pack.Provides() {
			seen[cap] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// PlacementIssue 是一条安置问题（用户配了但装不上的槽位）。
type PlacementIssue struct {
	Anchor geometry.Anchor
	Slot   string // 包 ID 或插件 ID
	Detail string
}

// String 便于界面展示。
func (p PlacementIssue) String() string {
	return fmt.Sprintf("%s 的槽位 %s：%s", p.Anchor, p.Slot, p.Detail)
}
