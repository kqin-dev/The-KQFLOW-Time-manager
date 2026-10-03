// Package ui 实现 Kairos 的终端界面。
package ui

import "github.com/charmbracelet/lipgloss"

// Theme 是一套配色。参考 LazyVim 的深色调，保证在暗色终端里足够醒目。
type Theme struct {
	Bg        lipgloss.Color
	Surface   lipgloss.Color
	Overlay   lipgloss.Color
	Text      lipgloss.Color
	Muted     lipgloss.Color
	Primary   lipgloss.Color // 主强调色，用于选中与标题
	Secondary lipgloss.Color // 次强调色，用于标签
	Success   lipgloss.Color // 已完成
	Warning   lipgloss.Color
	Danger    lipgloss.Color
	Border    lipgloss.Color
	BorderHi  lipgloss.Color // 聚焦时的边框
}

// DefaultTheme 返回 Kairos 的默认配色。
var DefaultTheme = Theme{
	Bg:        lipgloss.Color("#101418"),
	Surface:   lipgloss.Color("#161b22"),
	Overlay:   lipgloss.Color("#1f2630"),
	Text:      lipgloss.Color("#e6edf3"),
	Muted:     lipgloss.Color("#7d8590"),
	Primary:   lipgloss.Color("#7aa2f7"),
	Secondary: lipgloss.Color("#bb9af7"),
	Success:   lipgloss.Color("#9ece6a"),
	Warning:   lipgloss.Color("#e0af68"),
	Danger:    lipgloss.Color("#f7768e"),
	Border:    lipgloss.Color("#2a3038"),
	BorderHi:  lipgloss.Color("#7aa2f7"),
}

// Styles 汇总界面用到的全部样式，避免在渲染时反复构造。
type Styles struct {
	Theme Theme

	Title    lipgloss.Style
	Subtitle lipgloss.Style
	Muted    lipgloss.Style
	Text     lipgloss.Style
	Accent   lipgloss.Style
	Tag      lipgloss.Style
	Done     lipgloss.Style
	DoneTag  lipgloss.Style

	Panel        lipgloss.Style
	PanelFocused lipgloss.Style
	PanelTitle   lipgloss.Style

	Row        lipgloss.Style
	RowCursor  lipgloss.Style
	RowDone    lipgloss.Style
	RowDoneCur lipgloss.Style

	Menu       lipgloss.Style
	MenuCursor lipgloss.Style

	Hint    lipgloss.Style
	HintKey lipgloss.Style
	Badge   lipgloss.Style

	BarFilled lipgloss.Style
	BarFocus  lipgloss.Style
	BarBreak  lipgloss.Style
	BarEmpty  lipgloss.Style
	BarCursor lipgloss.Style

	OK    lipgloss.Style
	Warn  lipgloss.Style
	Error lipgloss.Style

	Modal       lipgloss.Style
	ModalTitle  lipgloss.Style
	ModalCursor lipgloss.Style
}

// NewStyles 依据主题构造样式集合。
func NewStyles(t Theme) *Styles {
	s := &Styles{Theme: t}

	s.Title = lipgloss.NewStyle().Foreground(t.Primary).Bold(true)
	s.Subtitle = lipgloss.NewStyle().Foreground(t.Muted)
	s.Muted = lipgloss.NewStyle().Foreground(t.Muted)
	s.Text = lipgloss.NewStyle().Foreground(t.Text)
	s.Accent = lipgloss.NewStyle().Foreground(t.Secondary)
	s.Tag = lipgloss.NewStyle().Foreground(t.Secondary)
	s.Done = lipgloss.NewStyle().Foreground(t.Success)
	s.DoneTag = lipgloss.NewStyle().Foreground(t.Muted)

	s.Panel = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Border).
		Padding(0, 1)
	s.PanelFocused = s.Panel.BorderForeground(t.BorderHi)
	s.PanelTitle = lipgloss.NewStyle().Foreground(t.Muted).Bold(true)

	s.Row = lipgloss.NewStyle().Foreground(t.Text)
	s.RowCursor = lipgloss.NewStyle().Foreground(t.Text).Background(t.Overlay).Bold(true)
	s.RowDone = lipgloss.NewStyle().Foreground(t.Muted).Strikethrough(true)
	s.RowDoneCur = lipgloss.NewStyle().Foreground(t.Success).Background(t.Overlay).Strikethrough(true)

	s.Menu = lipgloss.NewStyle().Foreground(t.Muted)
	s.MenuCursor = lipgloss.NewStyle().Foreground(t.Bg).Background(t.Primary).Bold(true)

	s.Hint = lipgloss.NewStyle().Foreground(t.Muted)
	s.HintKey = lipgloss.NewStyle().Foreground(t.Primary).Bold(true)
	s.Badge = lipgloss.NewStyle().Foreground(t.Bg).Background(t.Primary).Bold(true).Padding(0, 1)

	s.BarFilled = lipgloss.NewStyle().Foreground(t.Primary)
	s.BarFocus = lipgloss.NewStyle().Foreground(t.Success)
	s.BarBreak = lipgloss.NewStyle().Foreground(t.Warning)
	s.BarEmpty = lipgloss.NewStyle().Foreground(t.Border)
	s.BarCursor = lipgloss.NewStyle().Foreground(t.Text).Bold(true)

	s.OK = lipgloss.NewStyle().Foreground(t.Success)
	s.Warn = lipgloss.NewStyle().Foreground(t.Warning)
	s.Error = lipgloss.NewStyle().Foreground(t.Danger)

	s.Modal = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(t.Primary).
		Padding(1, 2)
	s.ModalTitle = lipgloss.NewStyle().Foreground(t.Primary).Bold(true)
	s.ModalCursor = lipgloss.NewStyle().Foreground(t.Bg).Background(t.Primary)
	return s
}

// compactWidth 是开始收紧浮层内边距的终端宽度。窄终端下把 2 列内边距收到 1 列，
// 能多出 2 列放内容，明显减少中文被截断的情况。
const compactWidth = 92

// modalStyle 返回浮层样式：窄终端下用更省空间的内边距。
func (s *Styles) modalStyle(compact bool) lipgloss.Style {
	if compact {
		return s.Modal.Padding(1, 1)
	}
	return s.Modal
}

// pageStyle 返回整页样式：窄终端下用更省空间的内边距。
func (s *Styles) pageStyle(compact bool) lipgloss.Style {
	if compact {
		return s.PanelFocused.Padding(0, 1)
	}
	return s.PanelFocused
}

// MarkDone / MarkTodo 是勾选框字符。
const (
	MarkDone  = "✔"
	MarkTodo  = "○"
	MarkDoing = "◐"
)
