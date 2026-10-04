package model

import (
	"fmt"
	"strings"
)

// 收藏的自定义专注方案（见需求 2）。
//
// 需求原文：每次自定义专注都要重新设置，应该维护一个数据库用作用户收藏的
// 方案，可以直接调用收藏好的搭配，也可以调出来作为模板改。
//
// 实现上「收藏的方案」就是一个带了 Label 的 Plan：调出来直接开始，或者装进
// 自定义时段编辑器当模板改（见界面层的「收藏的方案」菜单）。所以这里不再另造
// 一个类型，只提供校验、命名与去重这类与数据结构相关的操作。

// MaxPlanSegments 是单个方案允许的最大段数。
//
// 上限存在的意义是防止手改数据或反复追加把界面撑坏；自定义编辑器本身至少要求
// 两段（单段请用倒计时，见 commitCustom）。
const MaxPlanSegments = 12

// MaxPlanLabelRunes 是方案名长度上限。
const MaxPlanLabelRunes = 20

// NormalizePlanLabel 清洗方案名。
func NormalizePlanLabel(s string) string {
	cleaned := strings.Join(strings.Fields(Sanitize(s, false)), " ")
	r := []rune(strings.TrimSpace(cleaned))
	if len(r) > MaxPlanLabelRunes {
		r = r[:MaxPlanLabelRunes]
	}
	return string(r)
}

// AutoPlanLabel 依据方案内容生成一个可读的默认名字。
//
// 例：`深度工作 + 2 段`。用户想改名可以自己输入；起一个像样的默认名比让用户
// 面对一串「方案 1 / 方案 2」友好得多。
func AutoPlanLabel(p Plan) string {
	if len(p.Segments) == 0 {
		return "空方案"
	}
	first := strings.TrimSpace(p.Segments[0].Name)
	if first == "" {
		first = "时段"
	}
	if rest := len(p.Segments) - 1; rest > 0 {
		// 名字太长时先截断，避免「很长的名字 + 2 段」整体超长。
		r := []rune(first)
		if len(r) > MaxPlanLabelRunes-6 {
			r = r[:MaxPlanLabelRunes-6]
			first = string(r)
		}
		return fmt.Sprintf("%s + %d 段", first, rest)
	}
	return first
}

// PlanValid 报告方案是否可以收藏：至少一段、每段时长大于 0 且有名字。
//
// 比自定义编辑器宽松一点（那里要求至少两段）：收藏的是「配方」，用户可能
// 想把一段长专注也存下来备用。
func PlanValid(p Plan) bool {
	if len(p.Segments) == 0 || len(p.Segments) > MaxPlanSegments {
		return false
	}
	for _, s := range p.Segments {
		if s.Dur <= 0 || strings.TrimSpace(s.Name) == "" {
			return false
		}
	}
	return true
}

// ClonePlan 深拷贝一份方案，避免「调出来改」时改到收藏里的原件。
//
// Segments 是切片，直接赋值会让两边共享底层数组——这正是「当模板改」最容易
// 踩的坑：改了模板，收藏也跟着变了。
func ClonePlan(p Plan) Plan {
	out := p
	out.Segments = make([]Segment, len(p.Segments))
	copy(out.Segments, p.Segments)
	return out
}

// PlanSummary 返回方案的紧凑描述，用于菜单与确认提示。
func PlanSummary(p Plan) string {
	if len(p.Segments) == 0 {
		return "空方案"
	}
	names := make([]string, 0, len(p.Segments))
	for _, s := range p.Segments {
		names = append(names, s.Name)
	}
	return fmt.Sprintf("%d 段 · %s", len(p.Segments), strings.Join(names, " → "))
}
