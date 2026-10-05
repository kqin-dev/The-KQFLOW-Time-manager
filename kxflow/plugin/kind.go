// Package plugin 实现 KXFLOW 的插件与整合包体系。
//
// 两层模型（见 docs/kxflow-design.md §4.1）：
//
//	整合包 Pack          ← 装载 / 版本 / 冲突 / **用户开关** 的单位
//	  └── 插件 Plugin     ← 引擎认识的**渲染与事件**单位
//	        ├── Tile           磁贴：占槽位
//	        ├── BoardOption    看板选项：常驻中栏选项列表
//	        ├── ContextOption  联动选项：只在特定"选中上下文"成立时才有意义
//	        └── Service        服务：不渲染，只提供能力
//
// 为什么必须是两层：一个功能天然会横跨多种插件类型。DDL 就是活证据——
// 它是「看板上的 DDL 面板」+「给选中条目设截止时间」的合成体，
// 拆开一半都不成立。如果只有"插件"这一层，它们的内部连线就只能塞进内核，
// 那等于把耦合藏进内核——正是本引擎要消灭的东西。
//
// 边界因此非常清楚：
//   - **包内**：成员之间可以任意互相调用（一起写、一起发版），引擎不管；
//   - **包间**：只允许声明式依赖（Requires/Provides）与冲突（Conflicts），
//     不允许直接调用。
package plugin

// Kind 是插件种类。
//
// 选项被刻意拆成两种，差别只在"**存在条件**"（design §4.2）：
//   - BoardOption 永远在（设置/帮助/退出）；
//   - ContextOption 只在"当前选中"满足条件时才出现（给选中条目打标签 / 设 DDL）。
//
// 混为一谈会让"这个选项现在该不该出现"变成一个只能靠 if 判断的模糊问题。
type Kind uint8

const (
	// KindKernel 内核插件：把引擎"武装"成某个 CLI 工具（单例）。
	KindKernel Kind = iota
	// KindTile 磁贴插件：占用一个槽位。
	KindTile
	// KindBoardOption 看板选项：常驻中栏选项列表。
	KindBoardOption
	// KindContextOption 联动选项：依赖当前选中上下文。
	KindContextOption
	// KindService 服务：不渲染，只提供能力（提醒、持久化钩子等）。
	KindService
)

// String 便于装载报告可读。
func (k Kind) String() string {
	switch k {
	case KindKernel:
		return "内核"
	case KindTile:
		return "磁贴"
	case KindBoardOption:
		return "看板选项"
	case KindContextOption:
		return "联动选项"
	case KindService:
		return "服务"
	}
	return "未知"
}

// IsOption 报告它是否属于"选项"这一类（两类选项的公共判断）。
func (k Kind) IsOption() bool { return k == KindBoardOption || k == KindContextOption }

// Renderable 报告这种插件是否会自己往画布上画东西。
func (k Kind) Renderable() bool {
	return k == KindKernel || k == KindTile
}
