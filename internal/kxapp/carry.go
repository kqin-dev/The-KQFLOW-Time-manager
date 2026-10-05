package kxapp

import (
	"fmt"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
	"github.com/kqin-dev/kxflow/tile"
)

// CarryOptionID 是"继承昨日"看板选项的 ID。
const CarryOptionID = "kqflow.carry"

// CarryPackID 是继承包（只含一个看板选项）。
const CarryPackID = "kqflow.carry.pack"

// carryPack 提供"继承昨日"。
//
// 它是**看板选项**而不是联动选项：继承是"对今天这个整体"的动作，
// 与当前选中哪一条无关，也不该要求用户先选中什么。
type carryPack struct {
	src   Source
	state *HostState
}

// NewCarryPack 创建继承包。
func NewCarryPack(src Source, st *HostState) plugin.Pack {
	return &carryPack{src: src, state: st}
}

func (p *carryPack) ID() string              { return CarryPackID }
func (p *carryPack) Name() string            { return "继承昨日" }
func (p *carryPack) Version() semver.Version { return semver.MustParse("0.1.0") }
func (p *carryPack) EngineAPI() semver.Range { return engineRange }
func (p *carryPack) Enabled() bool           { return true }
func (p *carryPack) Provides() []string      { return nil }
func (p *carryPack) Requires() []string      { return nil }
func (p *carryPack) Conflicts() []string     { return nil }

func (p *carryPack) Members() []plugin.Plugin {
	return []plugin.Plugin{&boardOptionSpec{
		mf: plugin.Manifest{
			ID: CarryOptionID, Name: "继承昨日", Kind: plugin.KindBoardOption,
			Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
		},
		opt: &carryOption{src: p.src, state: p.state},
	}}
}

func (p *carryPack) Assemble(s svc.Services) (plugin.Assembled, error) {
	a := &simpleAssembled{p: p}
	for _, m := range p.Members() {
		if spec, ok := m.(*boardOptionSpec); ok {
			a.board = append(a.board, spec.opt)
		}
	}
	return a, nil
}

// carryOption 是"继承昨日"看板选项。
type carryOption struct {
	src   Source
	state *HostState
}

func (o *carryOption) Label() string {
	// 标签里带上"昨日是哪天"：用户按下去之前就该知道继承的是哪一天，
	// 而不是等做完才从提示里看到（几天没打开时尤其重要）。
	if prev := o.src.PrevDay(); prev != "" {
		return "继承昨日（" + prev + "）…"
	}
	return "继承昨日…"
}

func (o *carryOption) Order() int { return 20 }

func (o *carryOption) Activate(svc.Services) (plugin.View, error) {
	return newCarryView(o.src, o.state), nil
}

// carryChoice 是继承菜单里的一项。
type carryChoice struct {
	label string
	mode  string // "fixed" / "floating" / "both" / ""（不继承）
}

// newCarryView 构造继承选择界面。
//
// 形态与 2.1.0 的 askCarry 一致：四选一（固定 / 未完成的临时 / 两者 / 都不），
// 并且**每一项都带上数量**——"继承 3 项"比"继承昨日固定待办"有信息量得多，
// 用户据此决定要不要做。
func newCarryView(src Source, st *HostState) plugin.View {
	cursor := 0
	var status string

	// choices 每次都重新算：数量要反映**当下**的数据。
	choices := func() []carryChoice {
		prevDay := src.PrevDay()
		if prevDay == "" {
			return nil
		}
		fixed, floating := carryCounts(src, prevDay)
		return []carryChoice{
			{label: fmt.Sprintf("两者都继承（固定 %d 项 + 未完成 %d 项）", fixed, floating), mode: "both"},
			{label: fmt.Sprintf("只拉固定待办（%d 项）", fixed), mode: "fixed"},
			{label: fmt.Sprintf("只继承未完成的待办（%d 项）", floating), mode: "floating"},
			{label: "都不继承", mode: ""},
		}
	}

	return &plugin.ViewFunc{
		ViewName:    "继承昨日",
		FocusLockFn: func() bool { return true },
		HintFn: func(plugin.RenderCtx) []plugin.KeyHint {
			return []plugin.KeyHint{
				{Key: "j/k", Desc: "选择"},
				{Key: "enter", Desc: "确认"},
				{Key: "esc", Desc: "取消"},
			}
		},
		RenderFn: func(ctx plugin.RenderCtx) {
			y := ctx.Rect.Y
			put := func(s string, style canvas.StyleID) {
				y = drawWrapped(ctx, y, ctx.Rect, s, style)
			}
			prev := src.PrevDay()
			put("从 "+orDash(prev)+" 继承", tile.StyleTitle)
			put("", tile.StyleMuted)

			list := choices()
			if len(list) == 0 {
				put("没有可继承的昨日数据。", tile.StyleWarn)
				put("（可能是第一次使用，或昨天没有记录）", tile.StyleMuted)
				return
			}
			cur := ClampCursor(cursor, len(list))
			for i, c := range list {
				if y >= ctx.Rect.Y1() {
					break
				}
				mark, style := "  ", tile.StyleMuted
				if i == cur {
					mark, style = "▸ ", tile.StyleTitleFocused
				}
				put(mark+c.label, style)
			}
			if status != "" && y < ctx.Rect.Y1() {
				put("", tile.StyleMuted)
				put(status, tile.StyleStatus)
			}
			if y < ctx.Rect.Y1() {
				put("", tile.StyleMuted)
				put("同名条目不会重复添加；子任务一并带过来并重置为未完成。", tile.StyleMuted)
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			list := choices()
			if len(list) == 0 {
				switch ev.Key {
				case "esc", "q", "enter", " ":
					return plugin.None(), true
				}
				return plugin.None(), false
			}
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
				c := list[cur]
				if c.mode == "" {
					// "都不继承"：记下"问过了"，这样以后不再反复打扰
					//（与 2.1.0 的 skipCarry 同一个语义）。
					if data := src.Day(); data != nil {
						data.CarryAsked = true
					}
					return plugin.Persist(CarryPackID, "day", nil), true
				}
				n, err := src.Carry(c.mode)
				if err != nil {
					status = "继承失败：" + err.Error()
					return plugin.None(), false
				}
				// 继承是**对整天的改动**，光标要夹回合法范围。
				st.GoalCursor = ClampCursor(st.GoalCursor, len(src.Goals()))
				return plugin.Toast(fmt.Sprintf("已继承 %d 项", n)), true
			}
			return plugin.None(), false
		},
	}
}

// carryCounts 统计昨日可继承的项数（与 store 的规则保持一致）。
//
// ⚠️ 这里只用于**展示**，真正的继承规则在 store.CarryFixed/CarryFloating。
// 两处口径必须一致，否则会出现"说好继承 3 项、实际只进来 2 项"。
// 因此这里刻意用同样的判据：固定项全带、临时项只带未完成的。
// （真实数字以继承后的提示为准——那里返回的是 store 的实际结果。）
func carryCounts(src Source, prevDay string) (fixed, floating int) {
	days, err := src.RecentDays(30)
	if err != nil {
		return 0, 0
	}
	for _, d := range days {
		if d == nil || d.Day != prevDay {
			continue
		}
		fixed = len(d.Fixed)
		for _, t := range d.Floating {
			if !t.Done {
				floating++
			}
		}
		return fixed, floating
	}
	return 0, 0
}
