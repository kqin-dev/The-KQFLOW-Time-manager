package canvas

import (
	"strings"
	"testing"
)

// TestLogoLinesNeverExceedWidth 断言 LOGO 的每一行都不超过给定宽度。
//
// 这是实机问题的落点：LOGO 原先是一条写死的宽框，在窄中栏里被截成
// "K Q F L"——看起来像渲染坏了，而它本来就不该在那种宽度下画那么大。
// 现在按宽度降档，因此"每行都放得下"是硬保证。
func TestLogoLinesNeverExceedWidth(t *testing.T) {
	for w := 0; w <= 60; w++ {
		lines := LogoLines(w)
		for i, l := range lines {
			if got := StringWidth(l); got > w {
				t.Errorf("宽 %d：第 %d 行宽 %d 越界：%q", w, i, got, l)
			}
		}
		if w == 0 && len(lines) != 0 {
			t.Errorf("宽 0 时应返回空，实际 %v", lines)
		}
		if w > 0 && len(lines) == 0 {
			t.Errorf("宽 %d 时不该返回空", w)
		}
	}
}

// TestLogoLinesDegrades 固化三档降级行为。
//
// 顺序是"宽 → 中 → 窄"：宽度不够时先换单行方括号，再退化成纯文字。
// 断言"越窄用的行数不会变多"，否则矮面板里 LOGO 会挤掉真正的内容。
func TestLogoLinesDegrades(t *testing.T) {
	wide := LogoLines(60)
	if len(wide) != 3 {
		t.Fatalf("宽 60 应给出 3 行框，实际 %d 行：%v", len(wide), wide)
	}
	if !strings.Contains(wide[1], "K Q F L O W") {
		t.Errorf("宽版中间行应含完整字样，实际 %q", wide[1])
	}

	// 逐步变窄：行数只能减少或不变，绝不变多。
	prev := len(wide)
	for w := 60; w >= 1; w-- {
		n := len(LogoLines(w))
		if n > prev {
			t.Fatalf("宽 %d 时行数从 %d 增到 %d，应当只减不增", w, prev, n)
		}
		prev = n
	}

	// 极窄时退化成纯文字，且仍然可读（不是空串）。
	narrow := LogoLines(6)
	if len(narrow) != 1 || narrow[0] == "" {
		t.Errorf("宽 6 时应退化成一行非空文字，实际 %v", narrow)
	}
}
