package geometry

// Anchor 标识一个物理槽位（"放东西的位置"），与"放的是什么"解耦。
//
// 这是 req.md 明确要求的一点：**位置与内容解耦**。磁贴只知道自己的 Anchor，
// 不关心自己渲染什么都写在哪一列；布局层把 Anchor 翻译成具体矩形。
//
// AnchorUnset 是零值，表示"没有指定位置"。它被刻意设计为**合法但不指向任何槽位**，
// 这样 SlotPreference 的零值就有明确含义（"随便放，按优先级排"），
// 而不需要再引入一个 *Anchor 指针。
type Anchor uint8

const (
	// AnchorUnset 表示未指定位置（零值）。
	AnchorUnset Anchor = iota
	// AnchorLeftTop 左栏上半。
	AnchorLeftTop
	// AnchorLeftBottom 左栏下半。
	AnchorLeftBottom
	// AnchorRightTop 右栏上半。
	AnchorRightTop
	// AnchorRightBottom 右栏下半。
	AnchorRightBottom
	// AnchorCenterDockLeft 中栏停靠区左半。
	AnchorCenterDockLeft
	// AnchorCenterDockRight 中栏停靠区右半。
	AnchorCenterDockRight
)

// AllAnchors 按"从左上到右下"的稳定顺序列出全部真实槽位。
//
// 顺序是**契约**：装载报告、视图配置、按优先级安置都依赖它可复现。
// 测试会断言这个顺序不变（改了它等于改了安置规则）。
var AllAnchors = []Anchor{
	AnchorLeftTop,
	AnchorLeftBottom,
	AnchorRightTop,
	AnchorRightBottom,
	AnchorCenterDockLeft,
	AnchorCenterDockRight,
}

// Valid 报告它是不是一个**已定义的**锚点取值。
//
// AnchorUnset 算**合法**：它的含义是"没有指定位置"，在
// SlotPreference 里是完全正常的用法（"随便放，按优先级排"）。
// 这里拦的是越界的取值——例如反序列化出来的野值。
// 需要"必须指向某个真实槽位"的场合请用 Placed，那里只认真实锚点。
func (a Anchor) Valid() bool {
	switch a {
	case AnchorUnset,
		AnchorLeftTop, AnchorLeftBottom, AnchorRightTop, AnchorRightBottom,
		AnchorCenterDockLeft, AnchorCenterDockRight:
		return true
	}
	return false
}

// IsSlot 报告它是否指向一个**真实槽位**（AnchorUnset 返回 false）。
func (a Anchor) IsSlot() bool { return a.Valid() && a != AnchorUnset }

// Column 报告锚点所属的栏位（left / right / center-dock）。
func (a Anchor) Column() string {
	switch a {
	case AnchorLeftTop, AnchorLeftBottom:
		return "left"
	case AnchorRightTop, AnchorRightBottom:
		return "right"
	case AnchorCenterDockLeft, AnchorCenterDockRight:
		return "dock"
	}
	return ""
}

// SlotIndex 返回它在所属栏位里的槽位下标（0 上/左，1 下/右）；未指定返回 -1。
func (a Anchor) SlotIndex() int {
	switch a {
	case AnchorLeftTop, AnchorRightTop, AnchorCenterDockLeft:
		return 0
	case AnchorLeftBottom, AnchorRightBottom, AnchorCenterDockRight:
		return 1
	}
	return -1
}

// String 便于装载报告可读。
func (a Anchor) String() string {
	switch a {
	case AnchorUnset:
		return "未指定"
	case AnchorLeftTop:
		return "左上"
	case AnchorLeftBottom:
		return "左下"
	case AnchorRightTop:
		return "右上"
	case AnchorRightBottom:
		return "右下"
	case AnchorCenterDockLeft:
		return "中栏停靠左"
	case AnchorCenterDockRight:
		return "中栏停靠右"
	}
	return "未知锚点"
}
