package kxapp

import (
	"github.com/kqin-dev/kxflow"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/layout"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/semver"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/config"
)

// EngineAPI 是本适配层编译时对应的引擎版本。
var EngineAPI = semver.MustParse("0.1.0")

// Loader 负责把 KQFLOW 的业务包装配成一个可运行的引擎模型。
//
// 它是"引擎-内核-插件"这三层在宿主侧的汇合点，需要的东西很少：
// 一个数据源（读写业务数据）、一份配置（视图与包开关）、一个尺寸。
type Loader struct {
	src   Source
	state *HostState
	cfg   *config.Config
}

// NewLoader 创建装载器。
func NewLoader(src Source, cfg *config.Config) *Loader {
	return &Loader{src: src, state: NewHostState(), cfg: cfg}
}

// State 返回共享的界面状态（宿主需要读光标/选中时用）。
func (l *Loader) State() *HostState { return l.state }

// Packs 返回全部要装载的整合包。
//
// **顺序不重要**：装载器会用依赖关系算出真实的装配顺序
// （被依赖者在前），因此这里按可读性排序即可。
// 这一条是被测试逼出来的——早先实现里装配顺序等于参数顺序。
func (l *Loader) Packs() []plugin.Pack {
	return []plugin.Pack{
		newKernelPack(l.src, l.state),
		NewTodoPack(l.src, l.state),
		NewGoalPack(l.src, l.state),
		NewDDLPack(l.src, l.state),
		NewLabelPack(l.src, l.state),
		NewTimerPack(l.src, l.state),
		NewStatsPack(l.src, l.state),
		NewNotePack(l.src, l.state),
		NewSettingPack(l.src, l.state),
		NewHistoryPack(l.src),
		NewCarryPack(l.src, l.state),
	}
}

// Build 装载全部包并返回引擎模型。
//
// 返回的 *kxflow.Model 可以直接接 bubbletea（见 Engine 的说明），
// 也可以在测试里离屏渲染。
func (l *Loader) Build() (*kxflow.Model, *Services, *plugin.LoadReport) {
	services := NewServices(l.src)
	packs := l.Packs()
	m := kxflow.New(kxflow.Config{
		EngineAPI: EngineAPI,
		Services:  services,
		Layout:    layout.DefaultConfig(),
		View:      l.viewConfig(),
		Packs:     packs,
	})
	// 不需要在这里把选项列表交给内核：引擎在装配时已经调用了
	// Kernel.SetOptionSource，因此看板天然能列出当前可用选项。
	// （早先这里用类型断言偷偷注入，结果与引擎的接口撞了——
	//  现在这是引擎的正式职责，宿主不必操心。）

	// 把"读/改视图"接给界面布局选项。
	//
	// 必须在 m 建好之后：视图设置要问引擎"有哪些磁贴""现在怎么摆的"。
	// 用闭包而不是把 m 存进包里——包不该持有引擎，它只该拿着两个函数
	//（读快照、应用新配置），这样包与引擎的耦合面就是一个 struct。
	for _, p := range packs {
		if sp, ok := p.(*settingPack); ok {
			sp.BindViewEngine(
				func() viewEngine { return viewEngineFrom(m) },
				func(vc plugin.ViewConfig) { m.ApplyViewConfig(vc) },
			)
		}
	}

	rep := m.Report()
	return m, services, &rep
}

// viewConfig 从配置里读出视图配置（磁贴摆放与显隐）。
//
// 存档格式故意保持**极简**（锚点名 → 插件 ID），因为它是给用户手改的：
//
//	"view": {
//	  "slots":   {"左上": "kqflow.todo.fixed", "右上": "kqflow.goal.list"},
//	  "hidden":  ["kqflow.note.tile"],
//	  "dock":    true
//	}
//
// 不用 plugin.ViewConfig 直接序列化（那样会把 geometry.Anchor 的
// 数字枚举写进文件——用户看不懂，而且枚举一改老数据就废了）。
func (l *Loader) viewConfig() plugin.ViewConfig {
	vc := plugin.NewViewConfig()
	raw := l.cfg.View
	if len(raw) == 0 {
		return vc
	}
	// 只有存档里**明确写了** slots 才认为"用户指派过槽位"。
	//
	// 这一点很关键：装配阶段会根据"槽位是否已被用户指派"来决定
	// 要不要按优先级自动安置。如果没写过 slots 也返回一个空 map，
	// 就会得出"用户把每个槽位都指派成空"的结论——**所有磁贴都不安置**，
	// 界面只剩一个空壳。我第一版就是这么写的，实测症状正是"槽位全空、
	// 磁贴一个都不见了"。
	if slots, ok := raw["slots"].(map[string]any); ok {
		for name, pidAny := range slots {
			pid, _ := pidAny.(string)
			if pid == "" {
				continue
			}
			if a, ok := anchorByName(name); ok {
				vc = vc.WithSlot(a, pid)
			}
		}
	}
	if hidden, ok := raw["hidden"].([]any); ok {
		for _, h := range hidden {
			if pid, ok := h.(string); ok && pid != "" {
				vc = vc.WithHidden(pid)
			}
		}
	}
	if dock, ok := raw["dock"].(bool); ok {
		vc.DockVisible = dock
	}
	return vc
}

// anchorByName 把存档里的锚点名字翻译回枚举。
//
// 用名字（"左上"）而不是数字：数字枚举一改，老配置文件里的槽位
// 就会集体错位——而那种错位没有任何报错，只是东西放错了地方。
// 名字是人与程序都能读的稳定标识（geometry.Anchor.String 已经提供）。
func anchorByName(name string) (geometry.Anchor, bool) {
	for _, a := range geometry.AllAnchors {
		if a.String() == name {
			return a, true
		}
	}
	return geometry.AnchorUnset, false
}

// SaveViewConfig 把视图配置写回 cfg（调用方随后走 SaveConfig 落盘）。
func SaveViewConfig(cfg *config.Config, vc plugin.ViewConfig) {
	slots := map[string]any{}
	for a, pid := range vc.Slots {
		if pid != "" {
			slots[a.String()] = pid
		}
	}
	hidden := make([]any, 0, len(vc.Hidden))
	for _, h := range vc.Hidden {
		hidden = append(hidden, h)
	}
	cfg.View = map[string]any{
		"slots":  slots,
		"hidden": hidden,
		"dock":   vc.DockVisible,
	}
}
