package plugin

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
)

// RejectReason 是**拒绝装载**的原因类别：包进不去，属于错误。
//
// 与 WarnReason 的区别是刻意的（评审后引入）：
//   - 拒绝 = 这个包**有问题**（声明缺陷、版本不匹配、与人冲突）；
//   - 警告 = 这个包**没问题**，但当前环境下用不上（比如"没有任何组件接纳它"）。
//
// 混为一谈的代价是真实的：曾经把"没有 host 接纳"记成"包声明有缺陷"，
// 用户看到的是"我的标签包坏了"，而真相是"它好好的，只是你没开 TODO/GOAL"。
// 这不是有问题，而是「没有水瓶给水」。
type RejectReason uint8

const (
	// RejectEngineAPI 引擎版本不匹配。
	RejectEngineAPI RejectReason = iota
	// RejectKernelDuplicate 已经有内核了（内核是单例）。
	RejectKernelDuplicate
	// RejectConflict 与另一个包显式冲突。
	RejectConflict
	// RejectDuplicateID 包 ID 重复。
	RejectDuplicateID
	// RejectDataSchema 要求的数据结构版本低于数据实际版本。
	RejectDataSchema
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
	case RejectConflict:
		return "与其它包冲突"
	case RejectDuplicateID:
		return "包 ID 重复"
	case RejectDataSchema:
		return "数据结构版本不兼容"
	case RejectInvalid:
		return "包声明有缺陷"
	case RejectAssembleFailed:
		return "装配失败"
	}
	return "未知原因"
}

// WarnReason 是**警告**的原因类别：包是正常的，只是当前没起作用。
type WarnReason uint8

const (
	// WarnDisabled 用户关掉了它。不是问题，但必须留记录
	// （否则"我明明设了它却没了"永远没有答案）。
	WarnDisabled WarnReason = iota
	// WarnNoHost 没有任何组件愿意接纳它。
	//
	// 这是被漏掉过的一类：像"标签"这种联动选项包，必须有人上报选中上下文
	// 它才有用。若用户既没开 TODO 也没开 GOAL，它启用了、装载了，
	// 却**永远不会出现**。它没有坏，只是没有接纳它的组件。
	WarnNoHost
	// WarnProviderDisabled 它需要的组件存在，但被用户关掉了。
	//
	// 与 WarnNoHost 分开是为了给出**可操作**的指引：
	// "去开启 X 包"比"缺少能力标记 foo"对用户有用得多。
	WarnProviderDisabled
	// WarnTileUnplaced 它的磁贴没有位置可放（槽位满了）。
	WarnTileUnplaced
)

// String 返回中文说明。
func (r WarnReason) String() string {
	switch r {
	case WarnDisabled:
		return "已被关闭"
	case WarnNoHost:
		return "没有接纳它的组件"
	case WarnProviderDisabled:
		return "依赖的组件被关闭"
	case WarnTileUnplaced:
		return "磁贴没有位置可放"
	}
	return "未知原因"
}

// Warning 是一条警告：**包是正常的**，只是当前环境里没起作用。
type Warning struct {
	PackID string
	Pack   string
	Reason WarnReason
	Detail string // 人读的说明，含具体缺哪个组件/能力标记
}

// String 便于测试与日志。
func (w Warning) String() string {
	return fmt.Sprintf("%s（%s）：%s —— %s", w.Pack, w.PackID, w.Reason, w.Detail)
}

// Rejection 是一条拒绝记录：包**有问题**，装不上。
// **必须可解释**：只说"没装上"等于没有信息。
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
// 因此装载期的失败一律记录在 Rejected 里，不 panic 也不整体失败。
//
// 三类结果，语义各不相同（不要混）：
//
//	Loaded    装载成功且已装配
//	Inactive  **声明正常、依赖也满足**，但当前环境里没有组件接纳它（见 WarnNoHost）
//	Rejected  包有问题，装载被拒
//	Disabled  用户主动关闭
type LoadReport struct {
	// Loaded 是装载成功的包 ID，顺序即装配顺序（依赖在前）。
	Loaded []string
	// Inactive 是"装载成功但当前不起作用"的包 ID。
	//
	// 它们**不是错误**：包本身没问题，只是没有接纳它的组件，
	// 或者接纳它的那个包被用户关掉了。引擎照常启动，用户看到一条警告。
	Inactive []string
	// Rejected 是未能装载的包及原因（包有问题）。
	Rejected []Rejection
	// Warnings 是全部警告（含 Disabled 与 Inactive 的原因）。
	Warnings []Warning
	// Disabled 是用户显式关掉的包 ID（不是错误）。
	Disabled []string
	// KernelID 是最终生效的内核包 ID（无内核时为空）。
	KernelID string
	// Capabilities 是装载完成后实际可用的能力标记集合（排序去重）。
	Capabilities []string
}

// OK 报告引擎是否可以正常工作：有内核，且没有任何**错误**。
//
// 注意它**不看**警告与 Inactive：一个包没起作用不影响引擎可用，
// 把警告算进来会让"少一个可选功能"变成"启动失败"，方向就错了。
func (r LoadReport) OK() bool {
	if r.KernelID == "" {
		return false
	}
	return len(r.Rejected) == 0
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

// WarningsOf 返回指定原因的警告，便于设置页按类别展示。
func (r LoadReport) WarningsOf(reason WarnReason) []Warning {
	var out []Warning
	for _, w := range r.Warnings {
		if w.Reason == reason {
			out = append(out, w)
		}
	}
	return out
}

// Explain 生成人读的装载摘要，直接可用于启动报告或设置页。
//
// 措辞刻意区分"错误"与"警告"：把这两类用同一个词说出来，
// 会让用户以为"我的标签包坏了"，而它只是没被任何组件接纳。
func (r LoadReport) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "已装载 %d 个包", len(r.Loaded))
	if r.KernelID != "" {
		fmt.Fprintf(&b, "（内核 %s）", r.KernelID)
	} else {
		b.WriteString("（**无内核**）")
	}
	if len(r.Inactive) > 0 {
		fmt.Fprintf(&b, "；%d 个包当前未起作用：%s",
			len(r.Inactive), strings.Join(r.Inactive, "、"))
	}
	if len(r.Warnings) > 0 {
		b.WriteString("\n警告（包本身正常，当前未起作用）：")
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "\n  · %s", w)
		}
	}
	if len(r.Rejected) > 0 {
		b.WriteString("\n错误（这些包有问题，未能装载）：")
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
	// reject 非空表示它在单包体检阶段就被否决了（包**有问题**）。
	reject *Rejection
	// admitted 表示它通过了依赖不动点。
	admitted bool
	// done 表示它已经被定性过（关闭或已产出警告），不需要再判。
	done bool
}

// Load 按 design §4.4 的表逐条裁决并装配。
//
// 四个阶段（顺序是刻意的）：
//
//  1. **单包体检**：只看这个包自己——声明是否自洽、引擎版本、ID 是否重复、
//     用户是否关闭、数据版本。冲突与内核唯一**不放这里**，因为它们要跟
//     "最终真的装上了哪些包"比，而不是跟"传进来哪些包"比。
//  2. **依赖不动点定序**：反复扫描，把"Requires 都已被满足"的包依次接纳。
//     接纳顺序即拓扑序（被依赖者在前），因此**不需要单独做拓扑排序**。
//  3. **按序装配**：按接纳顺序调用 Assemble，并在此刻裁决冲突与内核唯一。
//  4. **给没接纳的包定性**：区分"包有问题"（错误）与
//     "包没问题但没组件接纳它"（警告 + Inactive）。这一条是评审后加的，
//     见 WarnNoHost 的说明。
//
// dataSchema 是数据文件的实际版本（0 表示不检查）。
func (m *Manager) Load(dataSchema int, packs ...Pack) LoadReport {
	m.kernel = nil
	m.packs = nil
	rep := LoadReport{}

	cands := m.precheck(dataSchema, packs, &rep)
	// 预先统计"哪些能力**本来**有人提供"，用于区分两种不同的"没起作用"：
	//   - 提供者存在、但被用户关了 → 给可操作指引（去开哪个包）
	//   - 完全没有提供者           → 只说明事实
	// 分成两张表是必须的：把被关闭的包也算进"存在"，
	// 会让依赖它的包被误判为"依赖已满足"从而照常装载
	// （实测症状：关掉 TODO 后 labels 仍然装上了，而它其实没有宿主）。
	enabled := map[string][]string{}  // 能力 → 提供它的**启用**包
	disabled := map[string][]string{} // 能力 → 提供它的**被关闭**包
	for _, c := range cands {
		if c.reject != nil {
			continue
		}
		dst := enabled
		if !c.pack.Enabled() {
			dst = disabled
		}
		for _, cap := range c.pack.Provides() {
			dst[cap] = append(dst[cap], c.pack.ID())
		}
	}

	order := m.admit(cands, &rep)
	m.assemble(cands, order, &rep)
	m.classifyRemaining(cands, order, enabled, disabled, &rep)

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
			rep.Warnings = append(rep.Warnings, Warning{
				PackID: p.ID(), Pack: p.Name(), Reason: WarnDisabled,
				Detail: "用户在配置里关闭了这个包；它本身没有问题",
			})
			// 被关闭不算"错误"，也不进 Rejected：它没有缺陷。
			c.done = true
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
			// done 表示"已经定性过"（用户关闭）。**必须一起跳过**：
			// 这类包没有声明依赖，不动点会认为"依赖全都满足"而把它接纳，
			// 于是被用户关掉的包照样装上了——一个真实发生过的错误。
			if c.reject != nil || c.admitted || c.done {
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

// classifyRemaining 给"没能装载"的包定性：是**错误**还是**警告**。
//
// 这是评审后修掉的一处语义错误。原先所有"依赖没满足"都记成
// RejectMissingCapability（错误），于是用户看到"我的标签包坏了"——
// 而它根本没坏：它启用了、声明也正常，只是**没有任何组件接纳它**
// （没有 TODO/GOAL 就没人上报选中上下文）。用户的原话很准：
// 这不是有问题，而是「没有水瓶给水」。
//
// 四种情形分得很清楚（判定顺序不能变，否则会互相遮蔽）：
//
//	① 包有问题（reject 非空）              → 错误，进 Rejected
//	② 缺的能力有**启用**的包能提供，却仍没装上 → 被卡在环里（WarnNoHost，说明是环）
//	   注意这一条必须排在③前面：环里的包，其依赖的提供者也是环里的另一员，
//	   它同样没被接纳，若先看"提供者是否被关闭"会误判。
//	③ 缺的能力只有**被关闭**的包提供        → WarnProviderDisabled + Inactive
//	   给出可操作指引："去开启 X 包"
//	④ 完全没有提供者                        → WarnNoHost + Inactive
func (m *Manager) classifyRemaining(cands []*candidate, order []*candidate, enabled, disabled map[string][]string, rep *LoadReport) {
	// 真正装上的包提供了哪些能力。
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
		// 用户关掉的包在最体检阶段就记过警告了，这里只补上 Inactive 标记。
		// **这一条必须排在最前面**：被关闭的包也常常声明 provides，
		// 于是它会出现在 disabled 能力表里，让下面的判定把它自己的依赖
		// 当成"有主人却被关了"，从而把"没人接纳"误报成"提供者被关闭"。
		if !c.pack.Enabled() {
			if !c.done {
				rep.Inactive = append(rep.Inactive, c.pack.ID())
			}
			continue
		}
		if c.done {
			continue
		}
		rep.Inactive = append(rep.Inactive, c.pack.ID())

		switch {
		case allSalvageable(c.pack.Requires(), provided, enabled):
			// ② 依赖都"本来能满足"，却仍然没被接纳 —— 只可能是环。
			rep.Warnings = append(rep.Warnings, Warning{
				PackID: c.pack.ID(), Pack: c.pack.Name(), Reason: WarnNoHost,
				Detail: fmt.Sprintf("依赖 %v 与其它包互相等待，形成环而无法确定装载顺序；"+
					"包本身没有问题", c.pack.Requires()),
			})
		case providerDisabledFor(c.pack.Requires(), disabled) != "":
			// ③ 有提供者，但被用户关了 —— 这是**可操作**的警告。
			missing := providerDisabledFor(c.pack.Requires(), disabled)
			rep.Warnings = append(rep.Warnings, Warning{
				PackID: c.pack.ID(), Pack: c.pack.Name(), Reason: WarnProviderDisabled,
				Detail: fmt.Sprintf("它需要的组件由 %s 提供，但那个包被关闭了；开启它即可让本包生效",
					strings.Join(disabled[missing], "、")),
			})
		default:
			// ④ 根本没人提供 —— 如实说明事实，不说"它有毛病"。
			missing := firstMissing(c.pack.Requires(), provided)
			rep.Warnings = append(rep.Warnings, Warning{
				PackID: c.pack.ID(), Pack: c.pack.Name(), Reason: WarnNoHost,
				Detail: fmt.Sprintf("没有任何已启用的包提供 %q，因此没有组件会接纳它；"+
					"它已启用但不会有任何作用（包本身没有问题）", missing),
			})
		}
	}
}

// allSalvageable 报告 need 里的每一项**要么已经可用，要么有启用的包能提供**。
//
// 用于区分"被环卡住"与"根本没人提供"：环里的包，其依赖在能力表里是有主的，
// 只是那个主也在环里、同样没被接纳。
func allSalvageable(need []string, provided map[string]bool, enabled map[string][]string) bool {
	for _, n := range need {
		if provided[n] {
			continue
		}
		if len(enabled[n]) == 0 {
			return false
		}
	}
	return true
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
//
// ⚠️ 内核的选项**只收一次**：内核本身也是一个已装载的包，
// 而它的看板选项在上面已经通过 m.kernel 收过了。若不跳过它，
// 同一个选项会出现两次——实测症状是看板上出现"1 帮助 / 2 帮助 /
// 3 关于 / 4 关于"，用户按 1 和按 2 效果一样，看起来像界面坏了。
func (m *Manager) BoardOptions() []BoardOption {
	type item struct {
		opt BoardOption
		id  string
	}
	var items []item
	if m.kernel != nil {
		for _, o := range m.kernel.BoardOptions() {
			if o == nil {
				continue
			}
			items = append(items, item{opt: o, id: "kernel"})
		}
	}
	for _, a := range m.packs {
		// 内核包跳过：它的选项已由 m.kernel 收过。
		if a.Kernel() != nil {
			continue
		}
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

// providerDisabledFor 返回第一个"主人存在但被用户关闭"的能力标记；没有则空串。
//
// 与 other 的 firstMissing 之区别：那个查"哪些能力已生效"，这个查
// "哪些能力的提供者被关了"。**只考虑 need 里真的声明过的标记**——
// 曾经写成"在 providers 表里找不到就返回"，于是表里根本没有的键
// 会让它返回空字符串（""），进而把"根本没人提供"误判成"提供者被关闭"。
func providerDisabledFor(need []string, disabled map[string][]string) string {
	for _, n := range need {
		if len(disabled[n]) > 0 {
			return n
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
