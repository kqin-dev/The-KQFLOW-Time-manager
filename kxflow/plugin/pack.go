package plugin

import (
	"fmt"
	"strings"

	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
)

// Plugin 是所有插件的共同接口。
//
// 注意它**只管身份与实例化**：渲染与事件在 Component 上，冲突声明在 Manifest 上。
// 这样同一份插件实现可以在不同包里被复用（包决定它与谁协作）。
type Plugin interface {
	// Manifest 声明身份与版本需求。引擎与包管理器**只信它**。
	Manifest() Manifest
	// New 由包在装配时调用，注入宿主能力并返回可用的组件。
	//
	// 只有会渲染的插件才需要返回非 nil 组件；选项与服务的组件语义见
	// BoardOption / ContextOption / Service 各自的接口。
	New(svc svc.Services) (Component, error)
}

// BoardOption 是**看板选项**：常驻中栏选项列表的入口。
//
// 与 ContextOption 的唯一区别是"存在条件"——它永远在，不需要选中上下文。
// 例：设置 / 帮助 / 历史 / 退出 / 随手记。
type BoardOption interface {
	// Label 是列表里显示的名字（如"设置 / Settings"）。
	Label() string
	// Order 决定在选项列表里的位置，小的在前；同值按 ID 字典序。
	Order() int
	// Activate 返回要借调舞台显示的视图。
	//
	// 与 ContextOption.Activate 一样，这是选项获得界面的**唯一**途径：
	// 它不能自己往画布上画东西，也不能改根模型的状态——
	// 于是"二级内容只占中栏"对选项也是机制而非约定。
	Activate(svc.Services) (View, error)
}

// ContextOption 是**联动选项**：只在当前选中上下文满足条件时才出现。
//
// 例：给选中条目打标签、给选中条目设截止时间。
//
// 关键设计：它**不认识任何磁贴**。谁被选中由引擎维护在 Selection 里，
// 它只声明"我要哪类选中对象、需要哪些能力"，引擎负责在选中变化时
// 重新计算"哪些联动选项适用"（design §4.2、§5.2）。
type ContextOption interface {
	// AppliesTo 声明适用于哪类选中对象（对应 Selection.Kind）。
	// 返回空表示"任何选中都适用"。
	AppliesTo() []string
	// Requires 声明需要的能力标记（对应 Selection.Can）。
	Requires() []string
	// Label 用当前选中生成显示文本，可以带上选中项名字。
	Label(s Selection) string
	// Order 决定在选项列表里的位置。
	Order() int
	// Activate 返回要借调舞台显示的视图；sel 是当前选中。
	//
	// 这是选项获得界面的**唯一**途径（与看板选项一致）：
	// 它不能自己往画布上画，也不能改根模型状态。
	Activate(sel Selection, s svc.Services) (View, error)
}

// Applies 报告该联动选项在当前选中下是否适用。
//
// 判定逻辑放在这里而不是各处自己写：**"是否适用"必须只有一处实现**，
// 否则"这个选项该不该出现"会在不同地方得出不同答案（v2.1.0 的
// "同一个动作写在两处就会按一下触发"是同一类问题的前车之鉴）。
func Applies(o ContextOption, s Selection) bool {
	if s.Empty() {
		return false
	}
	kinds := o.AppliesTo()
	if len(kinds) > 0 {
		matched := false
		for _, k := range kinds {
			if k == s.Kind {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return s.Can.HasAll(o.Requires())
}

// Service 是**服务插件**：不渲染，只提供能力（提醒调度、持久化钩子等）。
type Service interface {
	// Name 是服务的显示名。
	Name() string
	// Start 在包装配完成后调用一次（挂定时器、订阅事件都在这里）。
	Start(svc svc.Services) error
	// Stop 在包卸载时调用；实现必须是幂等的。
	Stop()
}

// Pack 是**装载单位**：一整套有机结合、彼此有调用关系的插件。
//
// 这是评审后新增的一层（design §4.1）。它的存在理由是：
// 一个功能天然横跨多种插件类型（DDL = 磁贴 + 联动选项），
// 若只有"插件"一层，它们的内部连线只能塞进内核——那等于把耦合藏进内核。
//
// 边界：
//   - 包**内**成员可以任意互相调用（一起写、一起发版），引擎不管；
//   - 包**间**只允许 Requires/Provides/Conflicts 三种声明，不允许直接调用。
type Pack interface {
	// ID 全局唯一，建议 "kqflow.todo"。
	ID() string
	// Name 是显示名（中文），给用户看。
	Name() string
	// Version 是包自身版本（功能整体的版本，用户看这个）。
	Version() semver.Version
	// EngineAPI 是整个包要求的引擎版本范围。
	//
	// 计算规则：**包内全部成员范围的交集，最严的那个生效**（design §4.3）。
	// 由本包自己声明，管理器会与成员的 EngineAPI 交叉校验。
	EngineAPI() semver.Range
	// Members 返回包内全部插件。它们**不单独开关**，只随包一起装载或卸载。
	Members() []Plugin
	// Provides 声明本包提供哪些能力标记（供别的包 Requires）。
	Provides() []string
	// Requires 声明依赖哪些能力标记；任一缺失则**整包拒绝装载**。
	Requires() []string
	// Conflicts 声明与哪些**包 ID** 互斥。
	Conflicts() []string
	// Assemble 把包内成员的互相引用接好（注入 Services、共享包内状态、
	// 解析选中依赖）。引擎不解释它的内部结构。
	Assemble(svc svc.Services) (Assembled, error)
	// Enabled 报告用户是否启用了本包（默认布局下由宿主配置决定）。
	// 返回 false 的包**不装载**，但会出现在报告的 Disabled 里——
	// 这样"我明明设了它却没了"永远有答案。
	Enabled() bool
}

// Assembled 是装载完成的包：引擎从这里取到已经装好的组件。
type Assembled interface {
	// Pack 返回它的来源包。
	Pack() Pack
	// Kernel 返回内核组件；非内核包返回 nil。
	Kernel() Kernel
	// Tiles 返回本包提供的磁贴（按各自 Slots 参与槽位安置）。
	Tiles() []Component
	// BoardOptions 返回本包提供的看板选项。
	BoardOptions() []BoardOption
	// ContextOptions 返回本包提供的联动选项。
	ContextOptions() []ContextOption
	// Services 返回本包提供的服务。
	Services() []Service
	// Dispose 释放包内资源（定时器、连接等）。实现必须是幂等的。
	Dispose()
}

// Kernel 是内核插件（单例），本身也是一个包。
//
// 把它做成"只有一个成员的包"是为了让装载路径只有一条：不必为内核开特例，
// 内核也能自然享受版本与冲突裁决。它额外提供 LOGO 与默认看板视图。
type Kernel interface {
	Component
	// PowerBy 返回中栏要渲染的"Power by ..."字样。
	PowerBy() string
	// Dashboard 返回栈空时显示的底层视图（req.md 说的 Dashboard）。
	//
	// 注意它与内核**组件本身**是两样东西：组件是"渲染单位"，
	// 而 Dashboard 是"舞台的底色"。合成一个会让"内核要不要占槽位"
	// 变成一个说不清的问题——内核本来就不占槽位。
	Dashboard() View
	// BoardOptions 返回内核自带的全局看板选项（设置/帮助/历史/退出）。
	//
	// 它们是**内核自带**而不是"选项插件"：任何基于 KXFLOW 的产品都需要它们，
	// 让它们归属某个可卸载的包没有意义（design §5.3）。
	BoardOptions() []BoardOption
}

// ---------- 校验与辅助 ----------

// ValidatePack 检查包的声明是否自洽。返回的错误都是**开发者错误**：
// 这类问题应当当场发现，而不是让用户看到一个"莫名其妙装不上"的包。
func ValidatePack(p Pack) error {
	if p == nil {
		return fmt.Errorf("包为 nil")
	}
	if strings.TrimSpace(p.ID()) == "" {
		return fmt.Errorf("包缺少 ID")
	}
	if strings.TrimSpace(p.Name()) == "" {
		return fmt.Errorf("包 %s 缺少显示名", p.ID())
	}
	if p.EngineAPI().IsAny() {
		return fmt.Errorf("包 %s 未声明 EngineAPI 版本范围", p.ID())
	}
	members := p.Members()
	if len(members) == 0 {
		return fmt.Errorf("包 %s 没有任何成员插件（空包没有意义，无法渲染也无法开关）", p.ID())
	}

	seen := map[string]bool{}
	kernels := 0
	for _, m := range members {
		if m == nil {
			return fmt.Errorf("包 %s 含 nil 成员", p.ID())
		}
		mf := m.Manifest()
		if err := mf.Validate(); err != nil {
			return fmt.Errorf("包 %s：%w", p.ID(), err)
		}
		if seen[mf.ID] {
			return fmt.Errorf("包 %s 内插件 ID 重复：%s", p.ID(), mf.ID)
		}
		seen[mf.ID] = true
		if mf.Kind == KindKernel {
			kernels++
		}
		// 包声明的引擎范围必须**完全包含**每个成员的要求。
		//
		// 否则包声明是假的：会出现"包说支持 0.1，里面的成员其实要 >=0.2"
		// 这种自相矛盾——装载时看着没问题，实际运行到某个版本就炸。
		if !p.EngineAPI().CoversRange(mf.EngineAPI) {
			return fmt.Errorf("包 %s 声明的引擎范围 %s 未覆盖成员 %s 的要求 %s",
				p.ID(), p.EngineAPI(), mf.ID, mf.EngineAPI)
		}
	}
	if kernels > 1 {
		return fmt.Errorf("包 %s 含 %d 个内核插件；内核是单例，一个包最多一个", p.ID(), kernels)
	}
	// 自己要用的能力自己提供了，说明声明写错了（通常是想写 Requires）。
	self := map[string]bool{}
	for _, c := range p.Provides() {
		self[c] = true
	}
	for _, c := range p.Requires() {
		if self[c] {
			return fmt.Errorf("包 %s 同时 Provides 与 Requires 能力 %q：这是自相矛盾的声明", p.ID(), c)
		}
	}
	for i, c := range p.Provides() {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("包 %s 的 Provides 第 %d 项为空", p.ID(), i)
		}
	}
	for i, c := range p.Requires() {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("包 %s 的 Requires 第 %d 项为空", p.ID(), i)
		}
	}
	return nil
}

// RequiresEngineVersion 在评审后已不再需要：包与成员的范围关系改用
// semver.Range.Covers 精确判定（见 ValidatePack），比折算成一个代表版本可靠。
