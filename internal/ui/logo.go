package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// LogoLines 是 Kairos 的 ASCII 艺术字（ANSI Shadow 风格），等宽字体下渲染最佳。
var LogoLines = []string{
	`██   ██  █████  ██ ██████  ██████  ███████ `,
	`██  ██  ██   ██ ██ ██   ██ ██    ██ ██      `,
	`█████   ███████ ██ ██████  ██    ██ ███████ `,
	`██  ██  ██   ██ ██ ██   ██ ██    ██      ██ `,
	`██   ██ ██   ██ ██ ██   ██  ██████  ███████ `,
}

// LogoCompactLines 是窄终端下使用的紧凑版本。
var LogoCompactLines = []string{
	`█▄▀ ▄▀▄ █ █▀▄ █▀▄ ▄▀▀`,
	`█ █ █▀█ █ █▀▄ █▄▀ ▄██`,
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
// phase 让渐变随时间移动，形成流动的动效；total 是动画周期。
func GradientLogo(from, to lipgloss.Color, phase float64, compact bool) string {
	lines := LogoLines
	if compact {
		lines = LogoCompactLines
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
