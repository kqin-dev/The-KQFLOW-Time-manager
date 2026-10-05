// Package theme 提供配色与样式表。
//
// 它把"样式"变成**下标 → 样式**的查表（配合 canvas 的逐格样式），
// 因此绘制代码里不再出现带 ANSI 的字符串——v2.1.0 那套
// "先上色再按显示宽度反推"的绕行代码（stripANSI / displayRange）由此可以整批删掉。
package theme

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/tile"
)

// Palette 是一套配色。
//
// 默认值沿用的是 KQFLOW v2.1.0 的深色配色（参考 LazyVim）：用户对它没有意见，
// 换配色属于"改观感"，不该在架构重构里夹带。
type Palette struct {
	Bg        lipgloss.Color
	Surface   lipgloss.Color
	Overlay   lipgloss.Color
	Text      lipgloss.Color
	Muted     lipgloss.Color
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Success   lipgloss.Color
	Warning   lipgloss.Color
	Danger    lipgloss.Color
	Border    lipgloss.Color
	BorderHi  lipgloss.Color
}

// DefaultPalette 返回 KQFLOW 的默认配色。
func DefaultPalette() Palette {
	return Palette{
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
}

// Theme 是配色 + 已经建好的样式表。
type Theme struct {
	Palette Palette
	// Styles 是指向下标的样式表，索引与 tile 包里的 Style* 常量对应。
	Styles *canvas.LipglossPalette
}

// New 依据配色构造一套样式表。
//
// **下标必须与 tile 包的 Style* 常量一致**：它们是引擎与主题之间的契约，
// 由 TestThemeStyleIDsMatchTileContract 守着。样式表不是"随便加的列表"，
// 加一个样式就要在那边加一个常量，否则两边会悄悄错位。
func New(p Palette) *Theme {
	muted := lipgloss.NewStyle().Foreground(p.Muted)
	accent := lipgloss.NewStyle().Foreground(p.Primary)

	styles := []lipgloss.Style{
		tile.StyleDefault:       lipgloss.NewStyle(), // 0：不施加样式
		tile.StyleAccent:        accent.Bold(true),
		tile.StyleMuted:         muted,
		tile.StyleBorder:        lipgloss.NewStyle().Foreground(p.Border),
		tile.StyleBorderFocused: lipgloss.NewStyle().Foreground(p.BorderHi),
		tile.StyleBorderDim:     lipgloss.NewStyle().Foreground(p.Border).Faint(true),
		tile.StyleTitle:         muted.Bold(true),
		tile.StyleTitleFocused:  accent.Bold(true),
		tile.StyleTitleDim:      muted.Faint(true),
		tile.StyleStatus:        lipgloss.NewStyle().Foreground(p.Success).Bold(true),
		tile.StyleHintKey:       accent.Bold(true),
		tile.StyleHint:          muted,
		tile.StyleBarFilled:     lipgloss.NewStyle().Foreground(p.Primary),
		tile.StyleBarEmpty:      lipgloss.NewStyle().Foreground(p.Border),
		tile.StyleError:         lipgloss.NewStyle().Foreground(p.Danger),
		tile.StyleWarn:          lipgloss.NewStyle().Foreground(p.Warning),
		tile.StyleText:          lipgloss.NewStyle().Foreground(p.Text),
	}
	return &Theme{Palette: p, Styles: canvas.NewLipglossPalette(styles)}
}

// Default 返回默认主题。
func Default() *Theme { return New(DefaultPalette()) }

// Style 返回某个下标对应的样式（越界时返回零值样式）。
//
// 视图实现需要"给一段自己拼好的文本上色"时用它；但更推荐的做法是
// 把文本画进画布时直接传样式下标——那样带样式的宽度问题根本不会出现。
func (t *Theme) Style(id canvas.StyleID) lipgloss.Style {
	if t == nil || t.Styles == nil || int(id) >= t.Styles.Len() {
		return lipgloss.NewStyle()
	}
	// 借 Palette 的 Render 通道取回带样式的文本，再退掉文本本身不可行，
	// 因此这里直接按约定的下标重建（下标是契约，见 New 的说明）。
	return styleAt(t.Palette, id)
}

// styleAt 按契约下标重建样式。
//
// 为什么不从 LipglossPalette 里读回来：那是**渲染通道**（只提供
// Render(id, text)），不是样式仓库。把它当仓库用会逼着画布包暴露内部切片，
// 反而把两层的边界弄糊。这里按同一份契约重建，并由一个测试守着两边一致。
func styleAt(p Palette, id canvas.StyleID) lipgloss.Style {
	muted := lipgloss.NewStyle().Foreground(p.Muted)
	accent := lipgloss.NewStyle().Foreground(p.Primary)
	switch id {
	case tile.StyleAccent:
		return accent.Bold(true)
	case tile.StyleMuted:
		return muted
	case tile.StyleBorder:
		return lipgloss.NewStyle().Foreground(p.Border)
	case tile.StyleBorderFocused:
		return lipgloss.NewStyle().Foreground(p.BorderHi)
	case tile.StyleTitle:
		return muted.Bold(true)
	case tile.StyleTitleFocused:
		return accent.Bold(true)
	case tile.StyleStatus:
		return lipgloss.NewStyle().Foreground(p.Success).Bold(true)
	case tile.StyleHintKey:
		return accent.Bold(true)
	case tile.StyleHint:
		return muted
	case tile.StyleError:
		return lipgloss.NewStyle().Foreground(p.Danger)
	case tile.StyleWarn:
		return lipgloss.NewStyle().Foreground(p.Warning)
	case tile.StyleText:
		return lipgloss.NewStyle().Foreground(p.Text)
	}
	return lipgloss.NewStyle()
}
