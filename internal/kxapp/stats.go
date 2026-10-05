package kxapp

import (
	"fmt"
	"strings"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
	"github.com/kqin-dev/kxflow/tile"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
)

// StatsPackID / StatsTileID 是统计包与它的磁贴。
const (
	StatsPackID = "kqflow.stats"
	StatsTileID = "kqflow.stats.tile"
)

// NotePackID / NoteTileID 是随手记包与它的磁贴。
const (
	NotePackID = "kqflow.note"
	NoteTileID = "kqflow.note.tile"
)

// statsPack 是统计整合包：一个"连续 7 天"柱状图磁贴。
//
// 它声明 Requires(CapFocusSession)：柱状图读的是专注时长，
// 没有计时（或没有会话记录）时它只能画空气。这条依赖用于演示
// "提供者被关闭"与"根本没有提供者"两种警告的区别。
type statsPack struct {
	src   Source
	state *HostState
}

// NewStatsPack 创建统计包。
func NewStatsPack(src Source, st *HostState) plugin.Pack {
	return &statsPack{src: src, state: st}
}

func (p *statsPack) ID() string              { return StatsPackID }
func (p *statsPack) Name() string            { return "统计" }
func (p *statsPack) Version() semver.Version { return semver.MustParse("0.1.0") }
func (p *statsPack) EngineAPI() semver.Range { return engineRange }
func (p *statsPack) Enabled() bool           { return true }
func (p *statsPack) Provides() []string      { return nil }
func (p *statsPack) Requires() []string      { return nil }
func (p *statsPack) Conflicts() []string     { return nil }

func (p *statsPack) Members() []plugin.Plugin {
	return []plugin.Plugin{&tilePluginSpec{
		mf: plugin.Manifest{
			ID: StatsTileID, Name: "近 7 天", Kind: plugin.KindTile,
			Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
			Slots: plugin.SlotPreference{Anchor: geometry.AnchorCenterDockLeft, Priority: 10},
		},
		newComp: func(s svc.Services) plugin.Component {
			return &statsTile{src: p.src, svc: s}
		},
	}}
}

func (p *statsPack) Assemble(s svc.Services) (plugin.Assembled, error) {
	tiles := make([]plugin.Component, 0, 1)
	for _, m := range p.Members() {
		comp, err := m.New(s)
		if err != nil {
			return nil, err
		}
		tiles = append(tiles, comp)
	}
	return &simpleAssembled{p: p, tiles: tiles}, nil
}

// statsTile 画出最近 7 天的专注时长柱状图。
//
// 数据来源是**当日数据里的会话记录**：统计口径与 v2.1.0 保持一致
// （只有 ended 非空的记录计入，专注时长读 Session.Focus）。
// 注意它只读今天的数据——跨天读取要经过存储层，属于后续工作；
// 这里先把"磁贴能画真实业务数据"这件事做实。
type statsTile struct {
	src Source
	svc svc.Services
}

func (t *statsTile) Title() string { return "近 7 天" }

func (t *statsTile) Render(ctx plugin.RenderCtx) {
	days := t.recentFocus()
	if len(days) == 0 {
		ctx.Canvas.Text(ctx.Rect.X, ctx.Rect.Y,
			canvas.Truncate("（还没有专注记录）", ctx.Rect.W), tile.StyleMuted)
		return
	}
	// 高度预算：标题一行 + 柱状图 + 轴 + 标签。
	barH := ctx.Rect.H - 2
	if barH < 1 {
		barH = 1
	}
	maxVal := 0.0
	for _, d := range days {
		if d.Minutes > maxVal {
			maxVal = d.Minutes
		}
	}
	// 横向：每天一列（两根字符宽），装不下就只画能装下的天数。
	colW := 2
	perDay := ctx.Rect.W / colW
	show := len(days)
	if show > perDay {
		show = perDay
	}
	days = days[len(days)-show:]

	base := ctx.Rect.Y + barH
	for i, d := range days {
		x := ctx.Rect.X + i*colW
		h := 0
		if maxVal > 0 {
			h = int(float64(barH) * (d.Minutes / maxVal))
		}
		if h == 0 && d.Minutes > 0 {
			h = 1 // 有记录就至少给一格，否则看起来像没有
		}
		for k := 0; k < h; k++ {
			y := base - 1 - k
			if y < ctx.Rect.Y {
				break
			}
			ctx.Canvas.Text(x, y, "██", tile.StyleBarFilled)
		}
		// 日期标签只显示"日"，两列刚好放得下两位数。
		if base < ctx.Rect.Y1() {
			label := d.Day
			if len(label) >= 2 {
				label = label[len(label)-2:]
			}
			ctx.Canvas.Text(x, base, label, tile.StyleMuted)
		}
	}
}

// dayFocus 是某一天的专注时长。
type dayFocus struct {
	Day     string
	Minutes float64
}

// recentFocus 返回最近若干天的专注时长（当前只填今天）。
func (t *statsTile) recentFocus() []dayFocus {
	data := t.src.Day()
	if data == nil {
		return nil
	}
	focus, _ := data.FocusTotal()
	out := []dayFocus{{Day: data.Day, Minutes: focus.Minutes()}}
	return out
}

func (t *statsTile) Update(plugin.EventCtx, plugin.Event) plugin.Action { return plugin.None() }

// ---------- 随手记 ----------

// notePack 是随手记整合包：一个"今日随手记预览"磁贴。
//
// 说明：**编辑**随手记需要多行输入框，那是 M5 的交互类工作；
// 这一版先做只读预览，把"包能读真实数据并渲染"补齐。
type notePack struct {
	src   Source
	state *HostState
}

// NewNotePack 创建随手记包。
func NewNotePack(src Source, st *HostState) plugin.Pack {
	return &notePack{src: src, state: st}
}

func (p *notePack) ID() string              { return NotePackID }
func (p *notePack) Name() string            { return "随手记" }
func (p *notePack) Version() semver.Version { return semver.MustParse("0.1.0") }
func (p *notePack) EngineAPI() semver.Range { return engineRange }
func (p *notePack) Enabled() bool           { return true }
func (p *notePack) Provides() []string      { return nil }
func (p *notePack) Requires() []string      { return nil }
func (p *notePack) Conflicts() []string     { return nil }

func (p *notePack) Members() []plugin.Plugin {
	return []plugin.Plugin{&tilePluginSpec{
		mf: plugin.Manifest{
			ID: NoteTileID, Name: "随手记", Kind: plugin.KindTile,
			Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
			Slots: plugin.SlotPreference{Anchor: geometry.AnchorCenterDockRight, Priority: 10},
		},
		newComp: func(s svc.Services) plugin.Component {
			return &noteTile{src: p.src, svc: s}
		},
	}}
}

func (p *notePack) Assemble(s svc.Services) (plugin.Assembled, error) {
	tiles := make([]plugin.Component, 0, 1)
	for _, m := range p.Members() {
		comp, err := m.New(s)
		if err != nil {
			return nil, err
		}
		tiles = append(tiles, comp)
	}
	return &simpleAssembled{p: p, tiles: tiles}, nil
}

// noteTile 展示今日随手记的前几行。
type noteTile struct {
	src Source
	svc svc.Services
}

func (t *noteTile) Title() string { return "随手记" }

func (t *noteTile) Render(ctx plugin.RenderCtx) {
	data := t.src.Day()
	if data == nil || strings.TrimSpace(data.Note) == "" {
		ctx.Canvas.Text(ctx.Rect.X, ctx.Rect.Y,
			canvas.Truncate("（今天还没写）", ctx.Rect.W), tile.StyleMuted)
		return
	}
	y := ctx.Rect.Y
	for _, line := range strings.Split(data.Note, "\n") {
		if y >= ctx.Rect.Y1() {
			return
		}
		// 折行而不是截断：随手记是自由文本，一行往往长过磁贴宽度。
		y = drawWrapped(ctx, y, ctx.Rect, line, tile.StyleMuted)
	}
}

func (t *noteTile) Update(plugin.EventCtx, plugin.Event) plugin.Action { return plugin.None() }

// ---------- 共用装配结果 ----------

// simpleAssembled 是"只有磁贴、没有选项、没有服务"的包的装配结果。
//
// 它存在的意义是去掉样板：统计、随手记这类包结构完全一样，
// 各写一份 Assembled 实现只会让人以为它们之间有区别。
type simpleAssembled struct {
	p     plugin.Pack
	tiles []plugin.Component
	ctx   []plugin.ContextOption
	board []plugin.BoardOption
	svcs  []plugin.Service
}

func (a *simpleAssembled) Pack() plugin.Pack                  { return a.p }
func (a *simpleAssembled) Kernel() plugin.Kernel              { return nil }
func (a *simpleAssembled) Tiles() []plugin.Component          { return a.tiles }
func (a *simpleAssembled) BoardOptions() []plugin.BoardOption { return a.board }
func (a *simpleAssembled) ContextOptions() []plugin.ContextOption {
	return a.ctx
}
func (a *simpleAssembled) Services() []plugin.Service { return a.svcs }
func (a *simpleAssembled) Dispose()                   {}

// 让 fmt 与 clock 在本文件被用到（诊断输出用）。
var _ = fmt.Sprintf
var _ = clock.HumanDuration
