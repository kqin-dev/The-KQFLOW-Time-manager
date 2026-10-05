package kxapp

import (
	"github.com/kqin-dev/kxflow"
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
		NewStatsPack(l.src, l.state),
		NewNotePack(l.src, l.state),
	}
}

// Build 装载全部包并返回引擎模型。
//
// 返回的 *kxflow.Model 可以直接接 bubbletea（见 Engine 的说明），
// 也可以在测试里离屏渲染。
func (l *Loader) Build() (*kxflow.Model, *Services, *plugin.LoadReport) {
	services := NewServices(l.src)
	m := kxflow.New(kxflow.Config{
		EngineAPI: EngineAPI,
		Services:  services,
		Layout:    layout.DefaultConfig(),
		View:      l.viewConfig(),
		Packs:     l.Packs(),
	})
	// 让内核的看板能列出可用选项（数字键提示）。
	if k := m.Manager().Kernel(); k != nil {
		if kk, ok := k.(interface {
			SetOptionSource(func() []kxflow.OptionBinding)
		}); ok {
			kk.SetOptionSource(m.OptionKeys)
		}
	}
	rep := m.Report()
	return m, services, &rep
}

// viewConfig 从配置里读出视图配置（磁贴摆放与显隐）。
//
// 目前返回默认布局（各磁贴用自己声明的槽位）。等有了设置界面再读用户配置——
// 但接口先留出来，因为"视图"是 req.md 明确要求的能力。
func (l *Loader) viewConfig() plugin.ViewConfig {
	return plugin.NewViewConfig()
}
