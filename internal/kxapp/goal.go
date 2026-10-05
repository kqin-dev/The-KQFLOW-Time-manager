package kxapp

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
	"github.com/kqin-dev/kxflow/tile"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// GoalPackID / GoalTileID 是本包与成员的 ID。
const (
	GoalPackID = "kqflow.goal"
	GoalTileID = "kqflow.goal.list"
)

// goalPack 是 GOAL 整合包：一个列表磁贴。
//
// 它同样 Provides 选中能力：目标也能被打标签、被设 DDL，
// 所以"有可选中条目"这个标记由 TODO 与 GOAL 两个包共同提供（design §5.1）。
type goalPack struct {
	src   Source
	state *HostState
}

// NewGoalPack 创建 GOAL 包。
func NewGoalPack(src Source, st *HostState) plugin.Pack {
	return &goalPack{src: src, state: st}
}

func (p *goalPack) ID() string              { return GoalPackID }
func (p *goalPack) Name() string            { return "长期目标" }
func (p *goalPack) Version() semver.Version { return semver.MustParse("0.1.0") }
func (p *goalPack) EngineAPI() semver.Range { return engineRange }
func (p *goalPack) Enabled() bool           { return true }
func (p *goalPack) Requires() []string      { return nil }
func (p *goalPack) Conflicts() []string     { return nil }
func (p *goalPack) Provides() []string      { return []string{CapItemSelection} }
func (p *goalPack) Members() []plugin.Plugin {
	return []plugin.Plugin{&tilePluginSpec{
		mf: plugin.Manifest{
			ID: GoalTileID, Name: "目标", Kind: plugin.KindTile,
			Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
			Slots: plugin.SlotPreference{Anchor: geometry.AnchorRightTop, Priority: 20},
		},
		newComp: func(s svc.Services) plugin.Component {
			return &goalTile{src: p.src, state: p.state, svc: s}
		},
	}}
}

func (p *goalPack) Assemble(s svc.Services) (plugin.Assembled, error) {
	tiles := make([]plugin.Component, 0, 1)
	for _, m := range p.Members() {
		comp, err := m.New(s)
		if err != nil {
			return nil, err
		}
		tiles = append(tiles, comp)
	}
	return &goalAssembled{p: p, tiles: tiles}, nil
}

type goalAssembled struct {
	p     *goalPack
	tiles []plugin.Component
}

func (a *goalAssembled) Pack() plugin.Pack                  { return a.p }
func (a *goalAssembled) Kernel() plugin.Kernel              { return nil }
func (a *goalAssembled) Tiles() []plugin.Component          { return a.tiles }
func (a *goalAssembled) BoardOptions() []plugin.BoardOption { return nil }
func (a *goalAssembled) ContextOptions() []plugin.ContextOption {
	return nil
}
func (a *goalAssembled) Services() []plugin.Service { return nil }
func (a *goalAssembled) Dispose()                   {}

// goalTile 是 GOAL 列表磁贴。
type goalTile struct {
	src   Source
	state *HostState
	svc   svc.Services
}

func (t *goalTile) Title() string { return "目标" }

func (t *goalTile) Render(ctx plugin.RenderCtx) {
	goals := GoalList(t.src)
	cursor := ClampCursor(t.state.GoalCursor, len(goals))
	if len(goals) == 0 {
		ctx.Canvas.Text(ctx.Rect.X, ctx.Rect.Y,
			canvas.Truncate("（还没有目标）", ctx.Rect.W), tile.StyleMuted)
		return
	}
	// 光标可见性按**显示行数**倒推（长目标会折行，按条目数算会算少一行）。
	rowW := itemRowWidth(ctx.Rect)
	start := 0
	if rows := goalRowCounts(goals, t.src, rowW); len(rows) > 0 {
		start = firstVisibleByRows(rows, cursor, ctx.Rect.H)
	}
	y := ctx.Rect.Y
	for i := start; i < len(goals); i++ {
		if y >= ctx.Rect.Y1() {
			return
		}
		line := goalLine(goals[i], t.src)
		style := tile.StyleMuted
		prefix := strings.Repeat(" ", markerWidth)
		if i == cursor && ctx.Focus {
			prefix, style = "▸ ", tile.StyleTitleFocused
		} else if goals[i].Done {
			style = tile.StyleBorderDim
		}
		y = drawWrappedInset(ctx, y, ctx.Rect, prefix, strings.Repeat(" ", listIndentWidth), line, style)
	}
}

// goalRowCounts 返回每条目标折行后占用的显示行数。
//
// 与绘制用**同一个**折行函数（canvas.Wrap + 同一个可用宽度），
// 因此两者不可能对"占几行"有分歧——这正是之前栽过的地方：
// 滚动按一个宽度算、绘制按另一个宽度算，长条目就会算少一行。
func goalRowCounts(goals []*model.Goal, src Source, rowWidth int) []int {
	out := make([]int, len(goals))
	for i, g := range goals {
		if rowWidth < 1 {
			out[i] = 1
			continue
		}
		if n := len(canvas.Wrap(goalLine(g, src), rowWidth)); n > 0 {
			out[i] = n
			continue
		}
		out[i] = 1
	}
	return out
}

// goalLine 生成一条目标的显示文本。
func goalLine(g *model.Goal, src Source) string {
	mark := "○"
	if g.Done {
		mark = "✔"
	} else if g.Status == model.StatusDoing {
		mark = "◐"
	}
	line := fmt.Sprintf("%s %s", mark, g.Title)
	if g.Due != "" {
		line += "  ⏰" + g.Due
	}
	// 归档的目标标出来源，避免和进行中的混在一起看不出来。
	if g.ArchivedDay != "" {
		line += "  (" + g.ArchivedDay + " 归档)"
	}
	_ = src
	return line
}

func (t *goalTile) Update(ctx plugin.EventCtx, ev plugin.Event) plugin.Action {
	goals := GoalList(t.src)
	switch ev.Key {
	case "j", "down":
		t.state.GoalCursor = MoveCursor(t.state.GoalCursor, 1, len(goals))
		return t.selectCurrent()
	case "k", "up":
		t.state.GoalCursor = MoveCursor(t.state.GoalCursor, -1, len(goals))
		return t.selectCurrent()
	case " ", "enter":
		return t.toggle(ctx, goals)
	}
	return plugin.None()
}

// toggle 勾选/取消勾选目标，并按需求 10 在 goals.json 与当日归档之间搬移。
func (t *goalTile) toggle(ctx plugin.EventCtx, goals []*model.Goal) plugin.Action {
	cur := ClampCursor(t.state.GoalCursor, len(goals))
	if cur >= len(goals) {
		return plugin.None()
	}
	g := goals[cur]
	data := t.src.Day()
	if data == nil {
		return plugin.None()
	}
	if g.ArchivedDay != "" {
		// 取消完成：从归档取回活跃列表。
		restored := *g
		restored.Done, restored.DoneAt, restored.Status, restored.ArchivedDay = false, nil, model.StatusTodo, ""
		removeArchivedGoal(data, restored.ID)
		appendActiveGoal(t.src, restored)
	} else {
		// 完成：移入当日归档。
		done := *g
		done.Toggle(ctx.Now, data.Day)
		removeActiveGoal(t.src, done.ID)
		archiveGoal(data, done)
	}
	// 两个文件都要写：目标在 goals.json 与当日归档之间移动。
	return plugin.Persist(GoalPackID, "goals", goalPersist{
		Day:   data,
		Goals: t.src.Goals(),
	})
}

func (t *goalTile) selectCurrent() plugin.Action {
	goals := GoalList(t.src)
	cur := ClampCursor(t.state.GoalCursor, len(goals))
	if cur >= len(goals) {
		t.state.SelectedGoal = ""
		return plugin.Select(plugin.Selection{})
	}
	g := goals[cur]
	t.state.SelectedGoal = g.ID
	return plugin.Select(plugin.Selection{
		Kind: "goal", ID: g.ID, Title: g.Title,
		Can: svc.Capability{CapItemDue, CapItemLabel},
	})
}

// goalPersist 是一次"目标发生了移动"的持久化请求。
//
// 它同时携带日数据与目标列表：因为完成/取消完成会**同时**改两个文件，
// 分两次请求会出现"只写成功了一半"的中间状态。
type goalPersist struct {
	Day   *model.DayData
	Goals []model.Goal
}

// ---------- 目标在存储之间的搬移（与 v2.1.0 语义一致） ----------

func appendActiveGoal(src Source, g model.Goal) {
	// Source 的 Goals() 返回副本，因此这里要通过 SaveGoals 写回。
	goals := append(src.Goals(), g)
	saveGoalsTo(src, goals)
}

func removeActiveGoal(src Source, id string) {
	rest := make([]model.Goal, 0, len(src.Goals()))
	for _, g := range src.Goals() {
		if g.ID != id {
			rest = append(rest, g)
		}
	}
	saveGoalsTo(src, rest)
}

func archiveGoal(data *model.DayData, g model.Goal) {
	filtered := data.Archive.Goals[:0]
	for _, existing := range data.Archive.Goals {
		if existing.ID != g.ID {
			filtered = append(filtered, existing)
		}
	}
	data.Archive.Goals = append(filtered, g)
}

func removeArchivedGoal(data *model.DayData, id string) {
	filtered := data.Archive.Goals[:0]
	for _, g := range data.Archive.Goals {
		if g.ID != id {
			filtered = append(filtered, g)
		}
	}
	data.Archive.Goals = filtered
}

// saveGoalsTo 把目标列表写回数据源。
//
// 需要一个"写回"入口，而 Source 只有 SaveGoals()（写当前列表）。
// 因此这里用 setter 接口做一次窄化断言：真实实现支持替换列表，
// 内存实现（测试用）也支持。不支持时静默跳过——不 panic，
// 因为"目标没保存"是功能问题，不该变成崩溃。
func saveGoalsTo(src Source, goals []model.Goal) {
	if setter, ok := src.(goalSetter); ok {
		setter.SetGoals(goals)
	}
}

// goalSetter 是"可替换目标列表"的数据源能力。
type goalSetter interface {
	SetGoals([]model.Goal)
}

// ---------- 到期状态 ----------

// dueEntry 是"设了截止时间"的一行，用于 DDL 面板。
type dueEntry struct {
	Item  model.Ddl
	State model.DueState
	Left  time.Duration
	Title string
	Kind  string
}

// dueEntries 列出所有设了有效 DDL 的条目，按紧迫程度排序。
//
// 排序规则与 v2.1.0 一致：超时的排最前（超得越久越前），
// 然后按剩余时间由小到大。格式非法的 DDL 不列出（model.DueStatus 判定）。
func dueEntries(src Source) []dueEntry {
	now := src.Now()
	cfg := src.Config()
	cut, loc := cfg.Cutoff(), cfg.Location()

	out := make([]dueEntry, 0, 8)
	add := func(item model.Ddl, kind string) {
		state, left := model.DueStatus(item.DueText(), item.DueIsTodo(), now, cut, loc)
		if state == model.DueNone {
			return
		}
		out = append(out, dueEntry{
			Item: item, State: state, Left: left,
			Title: item.ItemTitle(), Kind: kind,
		})
	}
	if data := src.Day(); data != nil {
		for _, t := range data.All() {
			add(t, "TODO")
		}
	}
	for _, g := range GoalList(src) {
		if g.Done {
			continue // 已完成的目标不用再盯 DDL
		}
		add(g, "GOAL")
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].State == model.DueOverdue) != (out[j].State == model.DueOverdue) {
			return out[i].State == model.DueOverdue
		}
		return out[i].Left < out[j].Left
	})
	return out
}
