package kxapp

import (
	"fmt"
	"time"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
	"github.com/kqin-dev/kxflow/tile"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// DDLPackID 与两个成员的 ID。
const (
	DDLPackID   = "kqflow.ddl"
	DDLPanelID  = "kqflow.ddl.panel"
	DDLOptionID = "kqflow.ddl.ctx"
)

// ddlPack 是**截止时间整合包**：一个面板磁贴 + 一个联动选项。
//
// 这是"整合包"这个概念最好的例子（design §5.1）：DDL 天然横跨两种插件类型——
//   - 磁贴：看板上的截止时间面板，让你一眼看到什么要到期了；
//   - 联动选项：给**当前选中条目**设截止时间。
//
// 两半拆开都不成立（只有面板没法设时间、只有设置项看不到结果），
// 因此它们必须同属一个包、一起开关。而它们与 TODO / GOAL 之间**没有**
// 直接调用关系：只通过引擎的 Selection 交互——这就是"包内有机耦合、
// 包间声明式解耦"。
type ddlPack struct {
	src   Source
	state *HostState
}

// NewDDLPack 创建 DDL 包。
func NewDDLPack(src Source, st *HostState) plugin.Pack {
	return &ddlPack{src: src, state: st}
}

func (p *ddlPack) ID() string              { return DDLPackID }
func (p *ddlPack) Name() string            { return "截止时间" }
func (p *ddlPack) Version() semver.Version { return semver.MustParse("0.1.0") }
func (p *ddlPack) EngineAPI() semver.Range { return engineRange }
func (p *ddlPack) Enabled() bool           { return true }
func (p *ddlPack) Conflicts() []string     { return nil }

// Requires 声明必须有人提供"可选中条目"。
//
// 没有 TODO 也没有 GOAL 时，本包会装载失败吗？**不会**——
// 它会进 Inactive 并给一条警告："已启用但没有任何组件会接纳它"。
// 这不是错误，是「没有水瓶给水」（design §4.5）。
func (p *ddlPack) Requires() []string { return []string{CapItemSelection} }

func (p *ddlPack) Provides() []string { return nil }

func (p *ddlPack) Members() []plugin.Plugin {
	return []plugin.Plugin{
		&tilePluginSpec{
			mf: plugin.Manifest{
				ID: DDLPanelID, Name: "截止时间", Kind: plugin.KindTile,
				Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
				Slots: plugin.SlotPreference{Anchor: geometry.AnchorRightBottom, Priority: 10},
			},
			newComp: func(s svc.Services) plugin.Component {
				return &ddlPanel{src: p.src, svc: s}
			},
		},
		&ctxOptionSpec{
			mf: plugin.Manifest{
				ID: DDLOptionID, Name: "设截止时间", Kind: plugin.KindContextOption,
				Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
			},
			opt: &ddlOption{src: p.src, state: p.state},
		},
	}
}

func (p *ddlPack) Assemble(s svc.Services) (plugin.Assembled, error) {
	a := &ddlAssembled{p: p}
	for _, m := range p.Members() {
		switch spec := m.(type) {
		case *tilePluginSpec:
			comp, err := spec.New(s)
			if err != nil {
				return nil, err
			}
			a.tiles = append(a.tiles, comp)
		case *ctxOptionSpec:
			a.ctx = append(a.ctx, spec.opt)
		}
	}
	return a, nil
}

type ddlAssembled struct {
	p     *ddlPack
	tiles []plugin.Component
	ctx   []plugin.ContextOption
}

func (a *ddlAssembled) Pack() plugin.Pack                  { return a.p }
func (a *ddlAssembled) Kernel() plugin.Kernel              { return nil }
func (a *ddlAssembled) Tiles() []plugin.Component          { return a.tiles }
func (a *ddlAssembled) BoardOptions() []plugin.BoardOption { return nil }
func (a *ddlAssembled) ContextOptions() []plugin.ContextOption {
	return a.ctx
}
func (a *ddlAssembled) Services() []plugin.Service { return nil }
func (a *ddlAssembled) Dispose()                   {}

// ctxOptionSpec 是"声明 + 联动选项实现"组成的插件。
type ctxOptionSpec struct {
	mf  plugin.Manifest
	opt plugin.ContextOption
}

func (p *ctxOptionSpec) Manifest() plugin.Manifest { return p.mf }
func (p *ctxOptionSpec) New(svc.Services) (plugin.Component, error) {
	return nil, nil // 选项不渲染组件
}

// ---------- 磁贴：截止时间面板 ----------

// ddlPanel 列出所有设了 DDL 的条目，按紧迫程度排序。
type ddlPanel struct {
	src Source
	svc svc.Services
}

func (t *ddlPanel) Title() string { return "截止时间" }

func (t *ddlPanel) Render(ctx plugin.RenderCtx) {
	entries := dueEntries(t.src)
	if len(entries) == 0 {
		ctx.Canvas.Text(ctx.Rect.X, ctx.Rect.Y,
			canvas.Truncate("（没有设置截止时间）", ctx.Rect.W), tile.StyleMuted)
		return
	}
	y := ctx.Rect.Y
	for _, e := range entries {
		if y >= ctx.Rect.Y1() {
			return
		}
		style := tile.StyleMuted
		switch e.State {
		case model.DueOverdue:
			style = tile.StyleError
		case model.DueSoon:
			style = tile.StyleWarn
		}
		mark := dueMark(e.State)
		line := fmt.Sprintf("%s %s %s", mark, humanDue(e.Left), e.Title)
		if len(line) > 0 {
			ctx.Canvas.Text(ctx.Rect.X, y, " "+canvas.Truncate(line, ctx.Rect.W-1), style)
		}
		y++
	}
}

// humanDue 把"距截止还有多久"格式化成**一眼能懂**的说法。
//
// 不能直接用 clock.HumanDuration：它是给"今天专注了多久"设计的，
// 只会往小时上堆。实测一个 31 天后的目标被显示成「751h 50m」——
// 数字没错，但用户得自己在脑子里做除法，等于没显示。
//
// 分级：
//
//	< 1 分钟   → "即将到期"
//	< 1 小时   → "23m"
//	< 1 天     → "2h30m"
//	≥ 1 天     → "31天3h"（不足一小时就只说天数）
func humanDue(d time.Duration) string {
	if d < 0 {
		// 超时的量取绝对值显示，"超了多久"比"负多久"好懂。
		d = -d
	}
	switch {
	case d < time.Minute:
		return "即将到期"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		days := int(d.Hours()) / 24
		h := int(d.Hours()) % 24
		if h == 0 {
			return fmt.Sprintf("%d天", days)
		}
		return fmt.Sprintf("%d天%dh", days, h)
	}
}

// dueMark 给出到期状态的记号；不同状态用不同字符，低色彩终端也看得出来。
func dueMark(s model.DueState) string {
	switch s {
	case model.DueOverdue:
		return "!"
	case model.DueSoon:
		return "▲"
	default:
		return "·"
	}
}

// Update 面板本身不处理按键（它只是展示）。
func (t *ddlPanel) Update(plugin.EventCtx, plugin.Event) plugin.Action { return plugin.None() }

// ---------- 联动选项：给选中条目设截止时间 ----------

// ddlOption 是"给当前选中条目设截止时间"的联动选项。
//
// 关键点：它**不认识任何磁贴**。谁被选中由引擎的 Selection 广播过来，
// 它只声明"我要哪类选中对象"（todo / goal）以及"需要什么能力"（item.due）。
// 因此 TODO 磁贴、GOAL 磁贴、将来某个别的包提供的列表，
// 都能触发它——而它自己完全不知道那些磁贴的存在。
type ddlOption struct {
	src   Source
	state *HostState
}

// AppliesTo 声明适用于待办与目标两类选中对象。
func (o *ddlOption) AppliesTo() []string { return []string{"todo", "goal"} }

// Requires 声明需要"可设截止时间"这个能力。
func (o *ddlOption) Requires() []string { return []string{CapItemDue} }

// Label 用选中项名字生成可读文本。
func (o *ddlOption) Label(s plugin.Selection) string {
	if s.Title == "" {
		return "设截止时间"
	}
	return "设截止时间「" + s.Title + "」"
}

// Order 决定它在联动选项列表里的位置。
func (o *ddlOption) Order() int { return 20 }

// Activate 借调中栏显示设置向导。
//
// 返回的是要在舞台上展示的视图——这就是 req.md 说的"向中栏请求界面"。
func (o *ddlOption) Activate(s plugin.Selection, services svc.Services) (plugin.View, error) {
	// 两种粒度分开（需求 4 明确要求）：
	//   TODO 是每天重置的，所以只问时分；
	//   GOAL 不随天重置，所以问年月日。
	isTodo := s.Kind == "todo"
	return newDDLWizard(o.src, s, isTodo, services), nil
}

// newDDLWizard 构造设置截止时间的向导视图。
//
// 它是一个**独立的视图**：渲染与按键都在自己身上，
// 不像 v2.1.0 那样把"当前在填什么"塞进根模型的字段里再靠 view 枚举分派。
func newDDLWizard(src Source, sel plugin.Selection, isTodo bool, services svc.Services) plugin.View {
	input := ""
	hint := "输入 HH:MM（例如 18:30），回车确认，esc 取消"
	if !isTodo {
		hint = "输入 YYYY-MM-DD（例如 2026-10-31），回车确认，esc 取消"
	}
	var parseErr string

	return &plugin.ViewFunc{
		ViewName: "设置截止时间",
		RenderFn: func(ctx plugin.RenderCtx) {
			putLines(ctx, []string{
				"设置截止时间",
				"",
				"  条目：" + sel.Title,
				"  类型：" + sel.Kind + "（" + map[bool]string{true: "每天重置，只需时分", false: "不随天重置，需要日期"}[isTodo] + "）",
				"",
				"  " + hint,
				"",
				"  当前输入：" + input + "▏",
			})
			if parseErr != "" {
				y := ctx.Rect.Y + 8
				if y < ctx.Rect.Y1() {
					ctx.Canvas.Text(ctx.Rect.X, y, canvas.Truncate("  "+parseErr, ctx.Rect.W), tile.StyleError)
				}
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			switch ev.Key {
			case "esc":
				return plugin.None(), true
			case "enter":
				if err := applyDue(src, sel, input, isTodo, ec.Now); err != nil {
					parseErr = err.Error()
					return plugin.None(), false
				}
				return plugin.Persist(DDLPackID, "day", src.Day()), true
			case "backspace":
				input = trimLastRune(input)
				parseErr = ""
				return plugin.None(), false
			}
			// 只接受可打印字符：DDL 是纯数字与分隔符，别的输入直接忽略。
			for _, r := range ev.Runes {
				if isDueRune(r) {
					input += string(r)
				}
			}
			// bubbletea 对普通按键也会给 Runes，这里兼容"只有 Key 没有 Runes"的实现。
			if len(ev.Runes) == 0 && isDueRune(rune(0)) == false {
				if len(ev.Key) == 1 && isDueRune(rune(ev.Key[0])) {
					input += ev.Key
				}
			}
			parseErr = ""
			return plugin.None(), false
		},
	}
}

// isDueRune 报告字符是否属于 DDL 输入允许的集合。
func isDueRune(r rune) bool {
	return (r >= '0' && r <= '9') || r == ':' || r == '-'
}

// trimLastRune 去掉最后一个字符（按 rune，不是字节）。
func trimLastRune(s string) string {
	rs := []rune(s)
	if len(rs) == 0 {
		return s
	}
	return string(rs[:len(rs)-1])
}

// applyDue 把输入解析并写入条目。
//
// 解析器用 model 里那两个（ParseDueTime / ParseDueDate），不在这里重写：
// 它们处理过"用 fmt.Sscanf 解析 HH:MM 导致日界线永远失败"那次事故，
// 只能有一个实现。
func applyDue(src Source, sel plugin.Selection, input string, isTodo bool, now time.Time) error {
	if sel.ID == "" {
		return fmt.Errorf("没有选中的条目")
	}
	var normalized string
	var err error
	if isTodo {
		normalized, err = model.ParseDueTime(input)
	} else {
		normalized, err = model.ParseDueDate(input)
	}
	if err != nil {
		return err
	}
	if normalized == "" {
		return fmt.Errorf("请输入时间或日期")
	}
	// 找到条目并写入。两类条目分别在自己的列表里找。
	if data := src.Day(); data != nil {
		for _, t := range data.All() {
			if t.ID == sel.ID {
				t.SetDue(normalized)
				return nil
			}
		}
	}
	goals := src.Goals()
	for i := range goals {
		if goals[i].ID == sel.ID {
			goals[i].SetDue(normalized)
			if setter, ok := src.(goalSetter); ok {
				setter.SetGoals(goals)
			}
			return nil
		}
	}
	return fmt.Errorf("找不到选中的条目（可能已被删除）")
}
