package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// LogoLines 是 KQFLOW 的 ASCII 艺术字（ANSI Shadow 风格），等宽字体下渲染最佳。
// 宽度 52 列、6 行。
//
// 宽度很关键：中间栏内容区的最小宽度是 44 列，所以这幅字只在
// 终端 ≥ 120 列时用得上（见 columnLayout 的 minCenterWidth）。
// 更窄的终端会自动换用下面两档小字模，绝不让它折行。
var LogoLines = []string{
	`██╗  ██╗ ██████╗ ███████╗██╗      ██████╗ ██╗    ██╗`,
	`██║ ██╔╝██╔═══██╗██╔════╝██║     ██╔═══██╗██║    ██║`,
	`█████╔╝ ██║   ██║█████╗  ██║     ██║   ██║██║ █╗ ██║`,
	`██╔═██╗ ██║▄▄ ██║██╔══╝  ██║     ██║   ██║██║███╗██║`,
	`██║  ██╗╚██████╔╝██║     ███████╗╚██████╔╝╚███╔███╔╝`,
	`╚═╝  ╚═╝ ╚══▀▀═╝ ╚═╝     ╚══════╝ ╚═════╝  ╚══╝╚══╝ `,
}

// LogoCompactLines 是中等宽度（44 列以内）用的紧凑版本，宽度 25。
var LogoCompactLines = []string{
	`█▄▀ █▀▀ █▀▀ █   █▀▄ █ █`,
	`█ █ ▀▀█ █▀▀ █   █▀▄ █▄█`,
}

// LogoMiniLines 是中间栏很窄时的最小版本，宽度 17。
var LogoMiniLines = []string{
	`█▄▀ █▀▀ █▀▀ █ █▀▄`,
	`█ █ ▀▀█ █▀▀ █ █▄▀`,
}

// logoMargin 是字模两侧要留出的空白列数（每侧）。
//
// 不留余量的话，宽字模会顶到面板边框上，看起来像被夹住。
// 留出余量的代价是：字模宽 52 列时，中间栏内容区需要 54 列才用完整版。
const logoMargin = 1

// pickLogo 依据可用宽度选择能完整放下的 Logo。
//
// 关键点：Logo 行一旦超过可用宽度就会被折行，整幅字会被拆得看不出形状，
// 所以宁可换用更小的版本，也不能让它折行。
func pickLogo(avail int) []string {
	usable := avail - logoMargin*2
	for _, candidate := range [][]string{LogoLines, LogoCompactLines, LogoMiniLines} {
		w := 0
		for _, line := range candidate {
			if n := lipgloss.Width(line); n > w {
				w = n
			}
		}
		if w <= usable {
			return candidate
		}
	}
	return nil
}

// LogoWidth 返回完整 Logo 的显示宽度。
func LogoWidth() int {
	w := 0
	for _, line := range LogoLines {
		if n := lipgloss.Width(line); n > w {
			w = n
		}
	}
	return w
}

// GradientLogo 用给定的两个端点颜色，把 Logo 渲染成横向渐变。
//
// phase 让渐变随时间移动，形成流动的动效；avail 是可用宽度，
// 太窄时自动换用更小的字形，放不下则返回空字符串。
func GradientLogo(from, to lipgloss.Color, phase float64, avail int) string {
	lines := pickLogo(avail)
	if len(lines) == 0 {
		return ""
	}
	width := 1
	for _, line := range lines {
		if n := lipgloss.Width(line); n > width {
			width = n
		}
	}

	var out []string
	for _, line := range lines {
		var b strings.Builder
		for i, r := range line {
			if r == ' ' || r == '\u00a0' {
				b.WriteRune(r)
				continue
			}
			t := float64(i) / float64(width)
			// 让渐变随时间来回摆动。
			t = math.Mod(t+phase, 1.0)
			col := lerpColor(from, to, wave(t))
			b.WriteString(lipgloss.NewStyle().Foreground(col).Render(string(r)))
		}
		out = append(out, b.String())
	}
	return strings.Join(out, "\n")
}

// wave 把 0..1 映射成 0..1..0 的往返曲线，使渐变看起来像呼吸的波纹。
func wave(t float64) float64 {
	return 0.5 - 0.5*math.Cos(2*math.Pi*t)
}

// lerpColor 在两个 #rrggbb 颜色之间线性插值。
func lerpColor(a, b lipgloss.Color, t float64) lipgloss.Color {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	ar, ag, ab := hexRGB(string(a))
	br, bg, bb := hexRGB(string(b))
	r := int(math.Round(float64(ar) + (float64(br)-float64(ar))*t))
	g := int(math.Round(float64(ag) + (float64(bg)-float64(ag))*t))
	bl := int(math.Round(float64(ab) + (float64(bb)-float64(ab))*t))
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", clamp8(r), clamp8(g), clamp8(bl)))
}

func hexRGB(s string) (int, int, int) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 0, 0, 0
	}
	var r, g, b int
	fmt.Sscanf(s[0:2], "%x", &r)
	fmt.Sscanf(s[2:4], "%x", &g)
	fmt.Sscanf(s[4:6], "%x", &b)
	return r, g, b
}

func clamp8(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
