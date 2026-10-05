package canvas

import "github.com/charmbracelet/lipgloss"

// Frame 是一套边框字符。
//
// 引擎自带常见的几套；插件需要特殊边框时可以自己构造 Frame 字面量。
// 用字符集而不是 lipgloss.Border，是为了让边框成为**画布上的格子**——
// 这样"弹窗把左右面板边框切出断口"就能被 Collisions 诊断直接抓到。
type Frame struct {
	TopLeft     rune
	TopRight    rune
	BottomLeft  rune
	BottomRight rune
	Horizontal  rune
	Vertical    rune
	// Tee 系列目前未使用，留给今后的连接线与分隔。
	TeeDown  rune
	TeeUp    rune
	TeeRight rune
	TeeLeft  rune
	Cross    rune
}

// FrameRounded 是圆角边框（KQFLOW 的默认面板边框）。
var FrameRounded = Frame{
	TopLeft: '╭', TopRight: '╮', BottomLeft: '╰', BottomRight: '╯',
	Horizontal: '─', Vertical: '│',
	TeeDown: '┬', TeeUp: '┴', TeeRight: '├', TeeLeft: '┤', Cross: '┼',
}

// FrameSquare 是直角细线边框。
var FrameSquare = Frame{
	TopLeft: '┌', TopRight: '┐', BottomLeft: '└', BottomRight: '┘',
	Horizontal: '─', Vertical: '│',
	TeeDown: '┬', TeeUp: '┴', TeeRight: '├', TeeLeft: '┤', Cross: '┼',
}

// FrameHeavy 是粗线边框，用于强调当前焦点（比只换颜色在低色彩终端里更明显）。
var FrameHeavy = Frame{
	TopLeft: '┏', TopRight: '┓', BottomLeft: '┗', BottomRight: '┛',
	Horizontal: '━', Vertical: '┃',
	TeeDown: '┳', TeeUp: '┻', TeeRight: '┣', TeeLeft: '┫', Cross: '╋',
}

// FrameDouble 是双线边框，用于模态浮层。
var FrameDouble = Frame{
	TopLeft: '╔', TopRight: '╗', BottomLeft: '╚', BottomRight: '╝',
	Horizontal: '═', Vertical: '║',
	TeeDown: '╦', TeeUp: '╩', TeeRight: '╠', TeeLeft: '╣', Cross: '╬',
}

// Palette 把画布里的样式下标翻译成真正带色/带属性的字符串。
//
// 引擎只依赖这个最小接口，不依赖具体的样式实现：主题包提供实现，
// 测试可以提供一个恒等实现（把样式全抹掉，方便断言纯文本）。
type Palette interface {
	// Render 对一段纯文本施加 st 对应的样式。
	// st 为 DefaultStyle 时必须原样返回（不打任何转义）。
	Render(st StyleID, text string) string
	// Len 返回样式表容量（含 DefaultStyle），用于越界兜底。
	Len() int
}

// PlainPalette 不施加任何样式：用于测试与「无色终端」。
type PlainPalette struct{}

// Render 原样返回文本。
func (PlainPalette) Render(_ StyleID, text string) string { return text }

// Len 报告只有默认样式。
func (PlainPalette) Len() int { return 1 }

// LipglossPalette 是基于 lipgloss 的样式表实现。
//
// 下标 0 恒为"不施加样式"，与 DefaultStyle 对应。
type LipglossPalette struct {
	styles []lipgloss.Style
}

// NewLipglossPalette 用给定的样式表构造调色板；下标 0 必须是零值样式。
func NewLipglossPalette(styles []lipgloss.Style) *LipglossPalette {
	cp := make([]lipgloss.Style, len(styles))
	copy(cp, styles)
	if len(cp) == 0 {
		cp = append(cp, lipgloss.NewStyle())
	}
	return &LipglossPalette{styles: cp}
}

// Render 对文本施加样式；下标越界时退回不加样式。
func (p *LipglossPalette) Render(st StyleID, text string) string {
	if text == "" || st == DefaultStyle || int(st) >= len(p.styles) {
		return text
	}
	return p.styles[st].Render(text)
}

// Len 返回样式表长度。
func (p *LipglossPalette) Len() int { return len(p.styles) }

// Add 追加一个样式并返回它的下标。
//
// 供主题构建期使用（运行期不应再改动样式表，否则同一帧里的下标含义会漂移）。
func (p *LipglossPalette) Add(s lipgloss.Style) StyleID {
	p.styles = append(p.styles, s)
	return StyleID(len(p.styles) - 1)
}
