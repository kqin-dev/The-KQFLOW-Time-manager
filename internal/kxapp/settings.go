package kxapp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
	"github.com/kqin-dev/kxflow/tile"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/config"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// SettingPackID / SettingOptionID 是设置包与它的看板选项。
const (
	SettingPackID   = "kqflow.settings"
	SettingOptionID = "kqflow.settings.board"
)

// settingPack 是设置整合包：**只含一个看板选项**，没有磁贴。
//
// 设置不该占看板空间——它是"偶尔进去改一下"的东西。
// 这也说明包不一定非要含磁贴（与 kqflow.labels 只含联动选项同理）。
type settingPack struct {
	src   Source
	state *HostState
}

// NewSettingPack 创建设置包。
func NewSettingPack(src Source, st *HostState) plugin.Pack {
	return &settingPack{src: src, state: st}
}

func (p *settingPack) ID() string              { return SettingPackID }
func (p *settingPack) Name() string            { return "设置" }
func (p *settingPack) Version() semver.Version { return semver.MustParse("0.1.0") }
func (p *settingPack) EngineAPI() semver.Range { return engineRange }
func (p *settingPack) Enabled() bool           { return true }
func (p *settingPack) Provides() []string      { return nil }
func (p *settingPack) Requires() []string      { return nil }
func (p *settingPack) Conflicts() []string     { return nil }

func (p *settingPack) Members() []plugin.Plugin {
	return []plugin.Plugin{&boardOptionSpec{
		mf: plugin.Manifest{
			ID: SettingOptionID, Name: "设置", Kind: plugin.KindBoardOption,
			Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
		},
		opt: &settingOption{src: p.src, state: p.state},
	}}
}

func (p *settingPack) Assemble(s svc.Services) (plugin.Assembled, error) {
	a := &simpleAssembled{p: p}
	for _, m := range p.Members() {
		if spec, ok := m.(*boardOptionSpec); ok {
			a.board = append(a.board, spec.opt)
		}
	}
	return a, nil
}

// settingOption 是"设置"看板选项。
type settingOption struct {
	src   Source
	state *HostState
}

func (o *settingOption) Label() string { return "设置 / Settings" }
func (o *settingOption) Order() int    { return 50 }

func (o *settingOption) Activate(svc.Services) (plugin.View, error) {
	return newSettingsView(o.src), nil
}

// Setting 是设置页的一行。
//
// 做成"数据驱动"的列表而不是一串 if：加一项设置就加一条记录，
// 不用碰导航、渲染、按键分派。v2.1.0 的设置页也是这个形状
// （settingItems 表 + 一个 Edit 回调），沿用它是为了**行为一致**：
// 用户在新旧界面上看到的项、改法、限制都应当一样。
type Setting struct {
	// Label 是条目名。
	Label string
	// Value 返回当前值的展示文本。
	Value func(src Source) string
	// Parse 把用户输入解析并写入配置；返回错误则原样保留旧值。
	//
	// 为 nil 表示只读展示（例如数据目录）。
	Parse func(src Source, input string) error
	// Hint 是编辑时的输入提示。
	Hint string
}

// newSettingsView 构建设置页。
//
// 交互与 2.1.0 一致：j/k 移动、enter 编辑、esc 返回。
// 编辑时**另开一层视图**（叠在设置页之上），因此 esc 只退回设置页，
// 不必重新滚到原来那一项。
func newSettingsView(src Source) plugin.View {
	cursor := 0
	var status string

	items := func() []Setting { return settingItems() }

	return &plugin.ViewFunc{
		ViewName: "设置",
		HintFn: func(plugin.RenderCtx) []plugin.KeyHint {
			return []plugin.KeyHint{
				{Key: "j/k", Desc: "移动"},
				{Key: "enter", Desc: "编辑"},
				{Key: "esc", Desc: "返回"},
			}
		},
		RenderFn: func(ctx plugin.RenderCtx) {
			list := items()
			cur := ClampCursor(cursor, len(list))

			y := ctx.Rect.Y
			put := func(s string, style canvas.StyleID) {
				y = drawWrapped(ctx, y, ctx.Rect, s, style)
			}
			put("设置", tile.StyleTitle)
			put("", tile.StyleMuted)
			for i, it := range list {
				if y >= ctx.Rect.Y1() {
					break
				}
				mark, style := "  ", tile.StyleMuted
				if i == cur {
					mark, style = "▸ ", tile.StyleTitleFocused
				}
				// 值单独一列：标签可能很长，折行时值要能跟上。
				line := maround(it.Label, it.Value(src))
				put(mark+line, style)
			}
			if y < ctx.Rect.Y1() {
				put("", tile.StyleMuted)
				put("j/k 移动 · enter 编辑 · esc 返回", tile.StyleHint)
			}
			if status != "" && y < ctx.Rect.Y1() {
				put(status, tile.StyleWarn)
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			list := items()
			cur := ClampCursor(cursor, len(list))
			switch ev.Key {
			case "esc", "q":
				return plugin.None(), true
			case "j", "down":
				cursor = MoveCursor(cur, 1, len(list))
				return plugin.None(), false
			case "k", "up":
				cursor = MoveCursor(cur, -1, len(list))
				return plugin.None(), false
			case "enter", " ":
				it := list[cur]
				if it.Parse == nil {
					status = "「" + it.Label + "」是只读项"
					return plugin.None(), false
				}
				// 编辑界面叠在设置页之上：esc 回来时还在原来那一项上。
				return plugin.Borrow(newSettingEditor(it, src)), false
			}
			return plugin.None(), false
		},
	}
}

// maround 把标签与值排成一行（值靠右用分隔符标出）。
func maround(label, value string) string {
	if value == "" {
		return label
	}
	return label + "  →  " + value
}

// newSettingEditor 是某一项的编辑界面。
//
// 它只做三件事：显示当前值与提示、收集输入、回车时交给该项的 Parse。
// 解析失败就**把错误显示出来并留着输入**，而不是静默丢弃——
// 用户敲了半天的值被清空是最让人恼火的一种交互。
func newSettingEditor(it Setting, src Source) plugin.View {
	input := ""
	var parseErr string

	return &plugin.ViewFunc{
		ViewName: "设置·" + it.Label,
		HintFn: func(plugin.RenderCtx) []plugin.KeyHint {
			return []plugin.KeyHint{
				{Key: "enter", Desc: "确认"},
				{Key: "esc", Desc: "取消"},
			}
		},
		RenderFn: func(ctx plugin.RenderCtx) {
			y := ctx.Rect.Y
			put := func(s string, style canvas.StyleID) {
				y = drawWrapped(ctx, y, ctx.Rect, s, style)
			}
			put(it.Label, tile.StyleTitle)
			put("", tile.StyleMuted)
			put("当前值："+it.Value(src), tile.StyleStatus)
			put("", tile.StyleMuted)
			put(it.Hint, tile.StyleMuted)
			put("", tile.StyleMuted)
			put("输入："+input+"▏", tile.StyleAccent)
			if parseErr != "" {
				put("", tile.StyleMuted)
				put(parseErr, tile.StyleError)
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			switch ev.Key {
			case "esc":
				return plugin.None(), true
			case "enter":
				if err := it.Parse(src, input); err != nil {
					parseErr = err.Error()
					return plugin.None(), false
				}
				// 改的是配置（偏好），因此走"存配置"这条路，
				// 而不是日数据——两者是两个文件（见 Source.SaveConfig）。
				return plugin.Persist(SettingPackID, "config", nil), true
			case "backspace":
				input = trimLastRune(input)
				parseErr = ""
				return plugin.None(), false
			}
			for _, r := range ev.Runes {
				if isSettingRune(r) {
					input += string(r)
				}
			}
			// 兼容"只有 Key 没有 Runes"的实现。
			if len(ev.Runes) == 0 && len(ev.Key) == 1 {
				if r := rune(ev.Key[0]); isSettingRune(r) {
					input += ev.Key
				}
			}
			parseErr = ""
			return plugin.None(), false
		},
	}
}

// isSettingRune 报告字符是否属于设置输入允许的集合。
//
// 比 DDL 宽：昵称、字条都是自由文本。但仍然拒绝控制字符
// （它们会以 \u0000 的形式写进 JSON，看不见又让数据变脆）。
func isSettingRune(r rune) bool {
	return !isControlRune(r) && r >= 0x20
}

// ---------- 设置项表 ----------

// settingItems 返回设置项。
//
// 顺序按"改完最可能立刻想调的"排：计时四项在最前（先调好再开始专注），
// 然后是日界线、昵称这类与当天数据有关的，最后是展示开关。
func settingItems() []Setting {
	return []Setting{
		{
			Label: "专注时长（分钟）",
			Value: func(s Source) string { return strconv.Itoa(s.Config().FocusMinutes()) },
			Hint:  "输入 1-600 之间的整数",
			Parse: parseIntSetting("专注时长", 1, 600,
				func(c *config.Config, n int) { c.DefaultFocus = n }),
		},
		{
			Label: "休息时长（分钟）",
			Value: func(s Source) string { return strconv.Itoa(s.Config().BreakMinutes()) },
			Hint:  "输入 1-600 之间的整数",
			Parse: parseIntSetting("休息时长", 1, 600,
				func(c *config.Config, n int) { c.DefaultBreak = n }),
		},
		{
			Label: "番茄钟段数（专注 + 休息为一轮）",
			Value: func(s Source) string { return strconv.Itoa(s.Config().EffectivePomodoroCycles()) },
			Hint:  "输入 1-99 之间的整数",
			Parse: parseIntSetting("番茄钟段数", 1, 99,
				func(c *config.Config, n int) { c.PomodoroCycles = n }),
		},
		{
			Label: "倒计时时长（分钟）",
			Value: func(s Source) string { return strconv.Itoa(s.Config().CountdownMinutes()) },
			Hint:  "输入 1-600 之间的整数（0 表示跟随专注时长）",
			Parse: parseIntSetting("倒计时时长", 0, 600,
				func(c *config.Config, n int) { c.CountdownMin = n }),
		},
		{
			Label: "日界线（新的一天从几点开始）",
			Value: func(s Source) string { return s.Config().DayCutoff },
			Hint:  "输入 HH:MM，例如 04:00",
			Parse: parseCutoffSetting,
		},
		{
			Label: "昵称（显示在问候语里）",
			Value: func(s Source) string { return orDash(s.Config().Nickname) },
			Hint:  "输入昵称（留空则清除）",
			Parse: func(s Source, input string) error {
				s.Config().Nickname = model.Sanitize(input, false)
				return nil
			},
		},
		{
			Label: "自定义字条（每行一条）",
			Value: func(s Source) string { return strconv.Itoa(len(s.Config().Quotes)) + " 条" },
			Hint:  "多条用 ; 分隔（例如 专注;先做完再说）",
			Parse: parseQuotesSetting,
		},
		{
			Label: "看板展示随手记",
			Value: func(s Source) string { return onOff(s.Config().ShowNote) },
			Hint:  "输入 开 或 关",
			Parse: func(s Source, input string) error {
				v, err := parseOnOff(input)
				if err != nil {
					return err
				}
				s.Config().ShowNote = v
				return nil
			},
		},
		{
			Label: "数据目录",
			Value: func(s Source) string { return dataDirOf(s) },
			// Parse 为 nil：只读展示。
			// 换数据目录是个"重启才生效"的重活，不该在一个输入框里做。
		},
	}
}

// parseIntSetting 造一个整数设置项的解析器。
func parseIntSetting(name string, min, max int, apply func(*config.Config, int)) func(Source, string) error {
	return func(s Source, input string) error {
		input = strings.TrimSpace(input)
		if input == "" {
			return fmt.Errorf("请输入一个数字")
		}
		n, err := strconv.Atoi(input)
		if err != nil {
			return fmt.Errorf("「%s」需要是整数", input)
		}
		if n < min || n > max {
			return fmt.Errorf("应在 %d-%d 之间，实际 %d", min, max, n)
		}
		apply(s.Config(), n)
		return nil
	}
}

// parseCutoffSetting 解析并校验日界线。
//
// 校验复用 model.ParseDueTime（同一个 "HH:MM" 口径）：日界线解析错了
// 会让"今天是哪一天"整片错位，而这类错误在界面上极难看出来——
// 因此宁可在这里用已有的、被测过的解析器，也不自己写一遍。
func parseCutoffSetting(s Source, input string) error {
	normalized, err := model.ParseDueTime(input)
	if err != nil {
		return err
	}
	if normalized == "" {
		return fmt.Errorf("请输入 HH:MM，例如 04:00")
	}
	s.Config().DayCutoff = normalized
	return nil
}

// parseQuotesSetting 解析自定义字条（用 ; 分隔成多行）。
func parseQuotesSetting(s Source, input string) error {
	input = strings.TrimSpace(input)
	if input == "" {
		s.Config().Quotes = nil
		return nil
	}
	var quotes []string
	for _, part := range strings.Split(input, ";") {
		if q := model.Sanitize(part, false); q != "" {
			quotes = append(quotes, q)
		}
	}
	if len(quotes) == 0 {
		return fmt.Errorf("至少要有一条非空字条")
	}
	s.Config().Quotes = quotes
	return nil
}

// parseOnOff 解析开关值。
func parseOnOff(input string) (bool, error) {
	switch strings.TrimSpace(input) {
	case "开", "on", "true", "1", "是":
		return true, nil
	case "关", "off", "false", "0", "否":
		return false, nil
	}
	return false, fmt.Errorf("请输入 开 或 关")
}

func onOff(v bool) string {
	if v {
		return "开"
	}
	return "关"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（未设置）"
	}
	return s
}

// dataDirOf 返回数据目录（仅用于展示）。
func dataDirOf(s Source) string {
	if p, ok := s.(*storeSource); ok && p.paths != nil {
		return p.paths.Root
	}
	return "（未知）"
}
