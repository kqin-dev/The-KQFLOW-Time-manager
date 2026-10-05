// Package svc 定义引擎与宿主之间的副作用边界。
//
// 引擎本身保持纯粹：不碰文件、不播声音、不发网络请求。所有副作用都经由
// 宿主实现的 Services 接口，这样带来三个直接好处：
//
//  1. **引擎可测试**：测试用假 Services 就能跑完整帧，不需要真实文件系统。
//  2. **副作用无法藏在渲染里**：Effect 只能由 Update 发出（见 design §3.5）。
//     v2.1.0 把响铃挂在看板的渲染路径上，导致"停在设置页就不响"——
//     在新架构里那个错误**没有地方可以犯**。
//  3. **引擎可独立分发**：它不认识 KQFLOW 的数据格式。
package svc

import "time"

// Capability 是一组能力标记的集合。
//
// 用标记而不是具体类型，是为了让"联动选项"这种东西判断自己是否适用时
// 不必认识宿主的具体类型（见 plugin.ContextOption）。
type Capability []string

// Has 报告是否具备某个能力标记。
func (c Capability) Has(name string) bool {
	for _, x := range c {
		if x == name {
			return true
		}
	}
	return false
}

// HasAll 报告是否同时具备全部标记。
func (c Capability) HasAll(names []string) bool {
	for _, n := range names {
		if !c.Has(n) {
			return false
		}
	}
	return true
}

// Services 是宿主提供给引擎的能力集合。
//
// 实现者只应实现自己用得到的方法；引擎对每个能力的缺失都必须能优雅降级
// （例如没有 Clock 就用系统时间，没有 Persist 就只改内存）。
type Services interface {
	// Clock 返回当前时间。引擎的所有"现在"都必须走这里，
	// 否则测试无法确定性地验证跨日界线、跨时段这类逻辑。
	Clock() time.Time

	// Persist 是唯一的写盘通道。引擎不关心落盘格式，也不直接碰文件。
	//
	// 注意：这是**同步**接口。耗时写入应当由宿主实现为快速返回
	// （真正落盘可以异步），否则会卡住动画帧。
	Persist(req PersistRequest) error

	// Effect 播一次副作用（响铃、流光触发、系统通知、手机推送）。
	// 引擎不实现平台细节。
	Effect(e Effect)

	// Capabilities 报告当前环境能力，供样式降级与"要不要显示某项"使用。
	Capabilities() Capability
}

// PersistRequest 描述一次持久化请求。
//
// 用 Request 而不是直接传业务对象：引擎不认识 KQFLOW 的 DayData/Goal 等类型，
// 宿主在实现 Persist 时把它们翻译成自己的存储调用。
type PersistRequest struct {
	// PluginID 是发起方，供宿主记录与排错。
	PluginID string
	// Kind 是数据类别（如 "day" / "goals" / "config"），由宿主约定。
	Kind string
	// Payload 是宿主自己的数据对象；引擎只负责传递，不做解释。
	Payload any
}

// EffectKind 是副作用类别。
type EffectKind uint8

const (
	// EffectBell 终端响铃。
	EffectBell EffectKind = iota
	// EffectFlash 视觉提醒（如 logo 流光）。
	EffectFlash
	// EffectPush 外部推送（如手机通知）。
	EffectPush
	// EffectToast 界面内一次性提示。
	EffectToast
)

// Effect 是一次副作用请求。
type Effect struct {
	Kind EffectKind
	Text string
	// Data 留给宿主扩展（例如推送的标题、频道）。
	Data map[string]any
}

// Noop 是一个什么都不做的 Services 实现，用于测试与"未注入"的降级路径。
//
// 它存在的意义是：引擎不该因为宿主少给一个能力就 panic。
type Noop struct{}

// Clock 返回零时间之外的真实时间（保证动画帧能推进）。
func (Noop) Clock() time.Time { return time.Now() }

// Persist 丢弃写入请求。
func (Noop) Persist(PersistRequest) error { return nil }

// Effect 丢弃副作用。
func (Noop) Effect(Effect) {}

// Capabilities 报告没有任何额外能力。
func (Noop) Capabilities() Capability { return nil }
