package kxapp

import (
	"fmt"
	"sort"
	"time"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
	"github.com/kqin-dev/kxflow/tile"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
)

// HistoryPackID / HistoryOptionID 是历史包与它的看板选项。
const (
	HistoryPackID   = "kqflow.history"
	HistoryOptionID = "kqflow.history.board"
	// HistoryDays 是历史页看多少天。
	//
	// 14 天沿用 2.1.0 的值：两周一屏，既能看到趋势又不至于长到要翻页。
	HistoryDays = 14
)

// historyPack 是历史整合包：**只含一个看板选项**。
//
// 与设置包同理：历史是"偶尔进去看一眼"的东西，不该占看板空间。
// 它也不需要提供任何能力——纯读取。
type historyPack struct {
	src Source
}

// NewHistoryPack 创建历史包。
func NewHistoryPack(src Source) plugin.Pack { return &historyPack{src: src} }

func (p *historyPack) ID() string              { return HistoryPackID }
func (p *historyPack) Name() string            { return "历史" }
func (p *historyPack) Version() semver.Version { return semver.MustParse("0.1.0") }
func (p *historyPack) EngineAPI() semver.Range { return engineRange }
func (p *historyPack) Enabled() bool           { return true }
func (p *historyPack) Provides() []string      { return nil }
func (p *historyPack) Requires() []string      { return nil }
func (p *historyPack) Conflicts() []string     { return nil }

func (p *historyPack) Members() []plugin.Plugin {
	return []plugin.Plugin{&boardOptionSpec{
		mf: plugin.Manifest{
			ID: HistoryOptionID, Name: "历史", Kind: plugin.KindBoardOption,
			Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
		},
		opt: &historyOption{src: p.src},
	}}
}

func (p *historyPack) Assemble(s svc.Services) (plugin.Assembled, error) {
	a := &simpleAssembled{p: p}
	for _, m := range p.Members() {
		if spec, ok := m.(*boardOptionSpec); ok {
			a.board = append(a.board, spec.opt)
		}
	}
	return a, nil
}

// historyOption 是"历史"看板选项。
type historyOption struct{ src Source }

func (o *historyOption) Label() string { return "历史 / History" }
func (o *historyOption) Order() int    { return 55 }

func (o *historyOption) Activate(svc.Services) (plugin.View, error) {
	return newHistoryView(o.src), nil
}

// dayStat 是一天的汇总。
type dayStat struct {
	Day      string
	Done     int
	Total    int
	Focus    time.Duration
	Goals    int
	Sessions int
	// TopItem / TopAmount 是当天投入最多的条目。
	TopItem   string
	TopAmount time.Duration
}

// collectHistory 汇总最近若干天。
//
// 聚合口径与 2.1.0 完全一致（CodeCounts / FocusTotal / Archive /
// Activity 都直接用 model 上的方法），这样两个界面的"历史"不会对不上。
func collectHistory(src Source, limit int) ([]dayStat, error) {
	days, err := src.RecentDays(limit)
	if err != nil {
		return nil, err
	}
	out := make([]dayStat, 0, len(days))
	for _, data := range days {
		if data == nil {
			continue
		}
		done, total := data.Counts()
		focus, _ := data.FocusTotal()
		st := dayStat{
			Day:      data.Day,
			Done:     done,
			Total:    total,
			Focus:    focus,
			Goals:    len(data.Archive.Goals),
			Sessions: len(data.Archive.Sessions),
		}
		type kv struct {
			name string
			dur  time.Duration
		}
		all := make([]kv, 0, len(data.Activity))
		for name, act := range data.Activity {
			if act != nil {
				all = append(all, kv{name, act.Total})
			}
		}
		// 排序用名字作稳定的第二关键字：否则同一时长的条目
		// 在不同次渲染里可能换位置（走查界面时会让人以为数据变了）。
		sort.Slice(all, func(i, j int) bool {
			if all[i].dur != all[j].dur {
				return all[i].dur > all[j].dur
			}
			return all[i].name < all[j].name
		})
		if len(all) > 0 {
			st.TopItem, st.TopAmount = all[0].name, all[0].dur
		}
		out = append(out, st)
	}
	return out, nil
}

// newHistoryView 构造历史页。
//
// 形态与 2.1.0 一致：一行一天，列是"日期 / TODO / 专注 / GOAL / 最投入"，
// 末尾给合计。窄终端下按显示宽度**折行**而不是截断——
// 表格被截掉右半边读起来比折行难受得多。
func newHistoryView(src Source) plugin.View {
	return &plugin.ViewFunc{
		ViewName: "历史",
		HintFn: func(plugin.RenderCtx) []plugin.KeyHint {
			return []plugin.KeyHint{{Key: "esc", Desc: "返回"}}
		},
		RenderFn: func(ctx plugin.RenderCtx) {
			y := ctx.Rect.Y
			put := func(s string, style canvas.StyleID) {
				y = drawWrapped(ctx, y, ctx.Rect, s, style)
			}
			put("历史 / History", tile.StyleTitle)

			stats, err := collectHistory(src, HistoryDays)
			if err != nil {
				put("读取历史失败："+err.Error(), tile.StyleError)
				return
			}
			if len(stats) == 0 {
				put("", tile.StyleMuted)
				put("还没有历史数据。完成一些待办或专注一段时间后再来看。", tile.StyleMuted)
				return
			}
			put("", tile.StyleMuted)

			// 列宽固定：日期 10 列足够（YYYY-MM-DD），其余按内容估。
			// 用固定宽度而不是按终端宽度分配：表格列错位比窄更难看，
			// 窄终端下交给折行处理。
			const (
				dayW   = 11
				ratioW = 8
				focusW = 9
				goalW  = 5
			)
			put(fmt.Sprintf("  %-*s%-*s%-*s%-*s%s",
				dayW, "日期", ratioW, "TODO", focusW, "专注", goalW, "GOAL", "最投入"),
				tile.StyleMuted)

			var totalFocus time.Duration
			var totalDone int
			// 最近的在最后，因此倒序显示（今天在最上面）。
			for i := len(stats) - 1; i >= 0; i-- {
				st := stats[i]
				totalFocus += st.Focus
				totalDone += st.Done
				ratio := "—"
				if st.Total > 0 {
					ratio = fmt.Sprintf("%d/%d", st.Done, st.Total)
				}
				top := "—"
				if st.TopItem != "" {
					top = fmt.Sprintf("%s（%s）", DisplayTitle(st.TopItem), clock.HumanDuration(st.TopAmount))
				}
				put(fmt.Sprintf("  %-*s%-*s%-*s%-*d%s",
					dayW, st.Day, ratioW, ratio, focusW, clock.HumanDuration(st.Focus),
					goalW, st.Goals, top), tile.StyleMuted)
			}
			put("", tile.StyleMuted)
			put(fmt.Sprintf("  合计：完成 %d 项待办，专注 %s", totalDone, clock.HumanDuration(totalFocus)),
				tile.StyleStatus)
			put("", tile.StyleMuted)
			put("  esc 返回", tile.StyleHint)
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			switch ev.Key {
			case "esc", "q":
				return plugin.None(), true
			}
			return plugin.None(), false
		},
	}
}
