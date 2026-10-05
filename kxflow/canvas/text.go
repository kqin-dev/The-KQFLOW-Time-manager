package canvas

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// RuneWidth 返回一个字符的显示宽度（东亚宽字符占两列，组合记号占零列）。
//
// **全项目只有这一个宽度入口**：它是 KQFLOW 历史上出过三次事故的地方
// （字节下标 / rune 下标 / 显示宽度三套坐标系混用）。约定是硬的：
// 判断超宽、截断、补空格、算光标位置，一律走这里或 lipgloss.Width。
//
// 这里刻意不用 len() 也不用 utf8.RuneLen：中文一个字占两列，按字节或按 rune
// 计数都会让列位偏一半。
func RuneWidth(r rune) int {
	if r == 0 {
		return 0
	}
	// 控制字符不占位（调用方会在 Set 里拒绝它们）。
	if isControl(r) {
		return 0
	}
	w := runewidth.RuneWidth(r)
	if w < 0 {
		w = 0
	}
	return w
}

// StringWidth 返回字符串的显示宽度。
//
// 用 runewidth.StringWidth 而不是逐 rune 累加 RuneWidth：前者对 Unicode
// 组合序列与零宽连接符的处理与终端一致（逐 rune 相加在 emoji 上会偏大）。
func StringWidth(s string) int {
	return runewidth.StringWidth(s)
}

// Truncate 按显示宽度截断字符串，不追加省略号。
//
// 只用于**纯文本**。带样式的字符串不能这样切（ANSI 转义会被切坏）——
// 新架构里这种需求应当改为"画进画布"，让画布逐格处理样式。
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if StringWidth(s) <= width {
		return s
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := RuneWidth(r)
		if w+rw > width {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String()
}

// TruncateEllipsis 按显示宽度截断并追加省略号，结果不超过 width 列。
func TruncateEllipsis(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if StringWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return Truncate(s, width-1) + "…"
}

// TruncateFromEnd 从左侧裁剪，保留末尾 width 列。
//
// 用于"光标在长行末尾"这类必须让尾部可见的场景。
func TruncateFromEnd(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if StringWidth(s) <= width {
		return s
	}
	runes := []rune(s)
	total := 0
	i := len(runes)
	for i > 0 {
		w := RuneWidth(runes[i-1])
		if total+w > width {
			break
		}
		total += w
		i--
	}
	return string(runes[i:])
}

// Wrap 按显示宽度把文本折成多行。**全项目唯一的折行实现。**
//
// 行为约定：
//   - 中文没有空格可依，因此按显示列硬折（不做单词感知）——KQFLOW 的正文以中文
//     为主，按空格折行会把一整句中文当成一个"词"，反而溢出；
//   - 已有的换行符 `\n` 是**硬换行**，必须保留；
//   - 每行右侧空白被裁掉（否则面板右边框会被看不见的空格顶出去）；
//   - 宽字符不会跨行劈开：放不下就整体移到下一行；
//   - 结果永不为空（空输入返回含一个空串的切片），调用方不必判空。
//
// 不丢字是硬要求：把结果各行拼接后去掉所有空白，必须逐字包含原文
// （见 wrap_test.go 的 TestWrapNeverLosesText）。
func Wrap(s string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	var out []string
	var cur strings.Builder
	curW := 0
	flush := func() {
		out = append(out, strings.TrimRight(cur.String(), " "))
		cur.Reset()
		curW = 0
	}
	// pushLine 在"当前行已经有内容"时才换行。
	//
	// 空行是**显式**的（来自 \n），不来自自动折行。这一条是有代价的教训：
	// width=1 折 "中文" 曾得到 ["", "中", "文"]，首行是个空串，而宿主按行渲染时
	// 会把它当成一行真实内容，矮面板里就凭空少掉一行可用空间
	// （与 v2.1.0 的"面板多一行/少一行"同族）。
	pushLine := func() {
		if cur.Len() == 0 {
			return
		}
		flush()
	}
	wroteAny := false
	for _, r := range s {
		if r == '\n' {
			// 硬换行必须保留：即使当前行为空也要产出一行（显式的空行）。
			flush()
			wroteAny = true
			continue
		}
		rw := RuneWidth(r)
		if rw == 0 {
			// 零宽字符（组合记号）：附加到当前行，不占列。
			cur.WriteRune(r)
			wroteAny = true
			continue
		}
		if curW+rw > width {
			pushLine()
		}
		// 单个字符本身比整行还宽（width==1 却遇到宽字符）：不能死循环，
		// 也不能丢字，只能让它独占一行并如实记录为折不开。
		cur.WriteRune(r)
		curW += rw
		wroteAny = true
	}
	if cur.Len() > 0 || !wroteAny {
		flush()
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

// WrapBalanced 尽量把文本折成宽度均衡的若干行。
//
// 中文没有空格，按最大宽度贪心折行常出现"最后一行只剩一个句号"。这里在
// [maxWidth/2, maxWidth] 之间试多个目标宽度，优先"行数最少"，其次"各行长度最接近"。
//
// 只在整段文本能被放进 maxWidth 内、且调用方希望观感更好时使用；
// 追求"绝不超宽"的场景直接用 Wrap。
func WrapBalanced(s string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{""}
	}
	if StringWidth(s) <= maxWidth {
		return []string{s}
	}
	// 含硬换行时不参与均衡：多段文本各自成段更可预期。
	if strings.Contains(s, "\n") {
		return Wrap(s, maxWidth)
	}
	best := Wrap(s, maxWidth)
	bestSpread := lineSpread(best)
	for target := maxWidth - 1; target >= maxWidth/2 && target > 0; target-- {
		cand := Wrap(s, target)
		spread := lineSpread(cand)
		if len(cand) < len(best) || (len(cand) == len(best) && spread < bestSpread) {
			best, bestSpread = cand, spread
		}
	}
	return best
}

// lineSpread 返回各行显示宽度的极差，用来衡量折行是否均衡。
func lineSpread(lines []string) int {
	if len(lines) == 0 {
		return 0
	}
	lo, hi := -1, 0
	for _, l := range lines {
		w := StringWidth(l)
		if lo < 0 || w < lo {
			lo = w
		}
		if w > hi {
			hi = w
		}
	}
	return hi - lo
}

// Pad 把纯文本右侧补空格到 width 列（已够宽则原样返回）。
func Pad(s string, width int) string {
	w := StringWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// PadCenter 把纯文本在 width 列内居中（放不下则原样返回，不截断）。
func PadCenter(s string, width int) string {
	w := StringWidth(s)
	if w >= width {
		return s
	}
	left := (width - w) / 2
	return strings.Repeat(" ", left) + s
}

// PadTo 把纯文本在 width 列内**左对齐并补齐**，用于选中行底色贴合文字宽度。
//
// v2.1.0 在这上面翻过车：用 pad(text, inner) 铺满整行会让底色看起来像
// "光标拖了一行半"，正确做法是只补到文字实际宽度。
func PadTo(s string, width int) string { return Pad(s, width) }

func isControl(r rune) bool {
	// 保留 tab 与换行的处理在调用方（Set/Text），这里只判定真正的控制字符。
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	return r < 0x20 || r == 0x7f
}
