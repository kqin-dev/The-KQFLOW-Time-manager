package theme

import (
	"testing"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/tile"
)

// TestStyleIDsMatchTileContract 守住"主题与组件之间的下标契约"。
//
// 为什么需要这条测试：样式表是"下标 → 样式"的查表，而下标由 tile 包用常量
// 声明。一旦两边错位，程序**不会报任何错**——只会颜色不对、边框不亮、
// 标题不显眼。这类"不报错但结果错"的问题只能靠断言挡住。
func TestStyleIDsMatchTileContract(t *testing.T) {
	th := Default()
	if th.Styles == nil {
		t.Fatal("主题必须带样式表")
	}
	if got := th.Styles.Len(); got != tile.StyleCount {
		t.Fatalf("样式表长度应为 %d（tile.StyleCount），实际 %d", tile.StyleCount, got)
	}
	// 每个声明的下标都必须落在样式表范围内。
	for id := canvas.StyleID(0); int(id) < tile.StyleCount; id++ {
		if int(id) >= th.Styles.Len() {
			t.Errorf("下标 %d 超出样式表长度 %d", id, th.Styles.Len())
		}
	}
	// 下标 0 必须"不施加样式"，否则默认文本会被染色（整屏都会变）。
	if got := th.Styles.Render(tile.StyleDefault, "x"); got != "x" {
		t.Errorf("默认样式必须原样返回，实际 %q", got)
	}
}

// TestAllStyleIDsDistinct 验证常量值互不相同。
//
// 重复的下标会让两个视觉概念共用同一个样式，改一个就影响另一个。
func TestAllStyleIDsDistinct(t *testing.T) {
	ids := map[canvas.StyleID]string{
		tile.StyleDefault:       "Default",
		tile.StyleAccent:        "Accent",
		tile.StyleMuted:         "Muted",
		tile.StyleBorder:        "Border",
		tile.StyleBorderFocused: "BorderFocused",
		tile.StyleBorderDim:     "BorderDim",
		tile.StyleTitle:         "Title",
		tile.StyleTitleFocused:  "TitleFocused",
		tile.StyleTitleDim:      "TitleDim",
		tile.StyleStatus:        "Status",
		tile.StyleHintKey:       "HintKey",
		tile.StyleHint:          "Hint",
		tile.StyleBarFilled:     "BarFilled",
		tile.StyleBarEmpty:      "BarEmpty",
		tile.StyleError:         "Error",
		tile.StyleWarn:          "Warn",
		tile.StyleText:          "Text",
	}
	if len(ids) != tile.StyleCount {
		t.Fatalf("声明的名字数 %d 应等于 StyleCount %d（漏了常量或写重了）",
			len(ids), tile.StyleCount)
	}
}

// TestThemeStyleHelperMatchesPalette 验证 Style() 与样式表用的是同一份契约。
//
// Style() 是按契约重建的（不从样式表里读回来），因此两边可能悄悄漂移。
// 这里用"渲染同一段文本是否得到相同转义"来交叉验证。
func TestThemeStyleHelperMatchesPalette(t *testing.T) {
	th := Default()
	for id := canvas.StyleID(0); int(id) < tile.StyleCount; id++ {
		viaTable := th.Styles.Render(id, "文本")
		viaHelper := th.Style(id).Render("文本")
		if viaTable != viaHelper {
			t.Errorf("下标 %d：样式表与 Style() 结果不一致\n表=%q\n助手=%q",
				id, viaTable, viaHelper)
		}
	}
}

// TestStyleHelperHandlesOutOfRange 验证越界下标不 panic。
//
// 主题是给别人用的公共入口，越界取值应当退化成"不施加样式"而不是崩掉。
func TestStyleHelperHandlesOutOfRange(t *testing.T) {
	th := Default()
	if got := th.Style(canvas.StyleID(tile.StyleCount + 10)).Render("x"); got != "x" {
		t.Errorf("越界下标应退化为不施加样式，实际 %q", got)
	}
	var nilTheme *Theme
	if got := nilTheme.Style(1).Render("x"); got != "x" {
		t.Errorf("nil 主题不应 panic，实际 %q", got)
	}
}

// TestDefaultPaletteIsComplete 验证默认配色的每个字段都填了。
//
// 空颜色在 lipgloss 里是"继承终端默认色"，不会报错——但会让界面在
// 某些终端上变成一片同色。填全是对"看不出问题的问题"的防线。
func TestDefaultPaletteIsComplete(t *testing.T) {
	p := DefaultPalette()
	fields := map[string]string{
		"Bg": string(p.Bg), "Surface": string(p.Surface), "Overlay": string(p.Overlay),
		"Text": string(p.Text), "Muted": string(p.Muted), "Primary": string(p.Primary),
		"Secondary": string(p.Secondary), "Success": string(p.Success),
		"Warning": string(p.Warning), "Danger": string(p.Danger),
		"Border": string(p.Border), "BorderHi": string(p.BorderHi),
	}
	for name, v := range fields {
		if v == "" {
			t.Errorf("配色字段 %s 为空；空颜色会让终端观感不可预期", name)
		}
	}
}
