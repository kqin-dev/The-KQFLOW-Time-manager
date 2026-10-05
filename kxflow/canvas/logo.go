package canvas

import "strings"

// LogoLines 返回按可用宽度降档的 LOGO 文本行。
//
// 为什么要降档而不是截断：LOGO 被截成 "K Q F L" 看起来像渲染坏了，
// 而它在小尺寸下本来就不是必需信息。三档从宽到窄：
//
//	宽  ╭──────────────╮ / │  K Q F L O W  │ / ╰──────────────╯
//	中  [ K Q F L O W ]
//	窄  KQFLOW
//
// 每行宽度都**不超过**给定宽度，因此调用方可以直接逐行画，不必再判断
// （判断一次少一次"某天某个尺寸下把边框撑破"）。
func LogoLines(width int) []string {
	if width < 1 {
		return nil
	}
	const word = "K Q F L O W"
	wide := []string{
		"╭" + strings.Repeat("─", len(word)+2) + "╮",
		"│ " + word + " │",
		"╰" + strings.Repeat("─", len(word)+2) + "╯",
	}
	if StringWidth(wide[0]) <= width {
		return wide
	}
	mid := "[ " + word + " ]"
	if StringWidth(mid) <= width {
		return []string{mid}
	}
	narrow := "KQFLOW"
	if StringWidth(narrow) <= width {
		return []string{narrow}
	}
	return []string{Truncate(narrow, width)}
}
