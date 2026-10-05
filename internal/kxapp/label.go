package kxapp

import (
	"fmt"
	"strings"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
	"github.com/kqin-dev/kxflow/tile"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// LabelPackID / LabelOptionID 是本包与成员的 ID。
const (
	LabelPackID   = "kqflow.labels"
	LabelOptionID = "kqflow.labels.ctx"
)

// labelPack 是标签整合包：**只含一个联动选项**，没有磁贴。
//
// 它是 design §4.5 那个"没有水瓶给水"场景的现实来源：
// 标签必须作用于"当前选中的条目"，因此它 Requires(CapItemSelection)。
// 如果用户既没开 TODO 也没开 GOAL，这个包会：
//   - **不报错**（它没有缺陷）；
//   - 进 Inactive，并给一条警告："没有任何已启用的包提供 item.selection，
//     因此没有组件会接纳它"。
//
// 它同时证明了"一个包可以只含联动选项"这件事成立——用户视角下"标签"
// 就是一个完整功能，只不过它的作用对象由别的包提供。
type labelPack struct {
	src   Source
	state *HostState
}

// NewLabelPack 创建标签包。
func NewLabelPack(src Source, st *HostState) plugin.Pack {
	return &labelPack{src: src, state: st}
}

func (p *labelPack) ID() string              { return LabelPackID }
func (p *labelPack) Name() string            { return "标签" }
func (p *labelPack) Version() semver.Version { return semver.MustParse("0.1.0") }
func (p *labelPack) EngineAPI() semver.Range { return engineRange }
func (p *labelPack) Enabled() bool           { return true }
func (p *labelPack) Provides() []string      { return nil }
func (p *labelPack) Conflicts() []string     { return nil }

// Requires 声明必须有组件提供"可选中条目"。
func (p *labelPack) Requires() []string { return []string{CapItemSelection} }

func (p *labelPack) Members() []plugin.Plugin {
	return []plugin.Plugin{&ctxOptionSpec{
		mf: plugin.Manifest{
			ID: LabelOptionID, Name: "打标签", Kind: plugin.KindContextOption,
			Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
		},
		opt: &labelOption{src: p.src, state: p.state},
	}}
}

func (p *labelPack) Assemble(s svc.Services) (plugin.Assembled, error) {
	a := &labelAssembled{p: p}
	for _, m := range p.Members() {
		if spec, ok := m.(*ctxOptionSpec); ok {
			a.ctx = append(a.ctx, spec.opt)
		}
	}
	return a, nil
}

type labelAssembled struct {
	p   *labelPack
	ctx []plugin.ContextOption
}

func (a *labelAssembled) Pack() plugin.Pack                  { return a.p }
func (a *labelAssembled) Kernel() plugin.Kernel              { return nil }
func (a *labelAssembled) Tiles() []plugin.Component          { return nil }
func (a *labelAssembled) BoardOptions() []plugin.BoardOption { return nil }
func (a *labelAssembled) ContextOptions() []plugin.ContextOption {
	return a.ctx
}
func (a *labelAssembled) Services() []plugin.Service { return nil }
func (a *labelAssembled) Dispose()                   {}

// labelOption 是"给选中条目打标签"的联动选项。
type labelOption struct {
	src   Source
	state *HostState
}

func (o *labelOption) AppliesTo() []string { return []string{"todo", "goal"} }
func (o *labelOption) Requires() []string  { return []string{CapItemLabel} }
func (o *labelOption) Order() int          { return 10 } // 排在 DDL 前面

func (o *labelOption) Label(s plugin.Selection) string {
	if s.Title == "" {
		return "打标签"
	}
	return "打标签「" + s.Title + "」"
}

// Activate 返回标签编辑器视图。
func (o *labelOption) Activate(sel plugin.Selection, services svc.Services) (plugin.View, error) {
	target := findLabeled(o.src, sel)
	if target == nil {
		return nil, fmt.Errorf("找不到选中的条目（可能已被删除）")
	}
	return newLabelEditor(o.src, sel, target, services), nil
}

// findLabeled 找到选中条目并返回它的标签接口。
//
// TODO 与 GOAL 都实现了 model.Labeled，因此编辑器只需要认识这一个接口——
// 这就是"用一套代码处理两类条目"的落点。**不在这里重复标签的增删逻辑**：
// 上限、清洗、去重全在 model 里（数据完整性问题只能有一处实现）。
func findLabeled(src Source, sel plugin.Selection) model.Labeled {
	if sel.ID == "" {
		return nil
	}
	if sel.Kind == "goal" {
		// 目标要先从数据源取出真实对象，改完再写回：
		// Goals() 返回的是副本，直接改副本不会生效。
		//
		// 这里返回一个"转发到数据源"的包装，避免调用方误改副本。
		goals := src.Goals()
		for i := range goals {
			if goals[i].ID == sel.ID {
				return &goalLabelEditor{src: src, id: sel.ID, ref: goals[i]}
			}
		}
		return nil
	}
	if data := src.Day(); data != nil {
		for _, t := range data.All() {
			if t.ID == sel.ID {
				return t
			}
		}
	}
	return nil
}

// goalLabelEditor 把对目标标签的修改转发回数据源。
//
// 为什么需要它：`Source.Goals()` 返回切片副本（保护调用方不要意外改动存储），
// 而标签增删必须落到真实数据上。这个包装让"用 model.Labeled 统一处理两种条目"
// 与"目标要写回"两件事同时成立，而不必让编辑器认识这两者的差别。
type goalLabelEditor struct {
	src Source
	id  string
	ref model.Goal
}

func (g *goalLabelEditor) ItemTitle() string    { return g.ref.Title }
func (g *goalLabelEditor) ItemLabels() []string { return g.ref.ItemLabels() }

func (g *goalLabelEditor) SetItemLabels(labels []string) {
	g.ref.SetItemLabels(labels)
	g.flush()
}

func (g *goalLabelEditor) ToggleItemLabel(name string) bool {
	on := g.ref.ToggleItemLabel(name)
	g.flush()
	return on
}

// flush 把改动写回数据源里的目标列表。
func (g *goalLabelEditor) flush() {
	goals := g.src.Goals()
	for i := range goals {
		if goals[i].ID == g.id {
			goals[i].Labels = g.ref.Labels
			break
		}
	}
	if setter, ok := g.src.(goalSetter); ok {
		setter.SetGoals(goals)
	}
}

// digitIndex 把 "1".."9" 解析成 0..8；其它按键返回 -1。
//
// 引擎里有一份同样的实现（kxflow.digitIndex），但它不对外导出。
// 复制这三行比把引擎的内部约定暴露成公共 API 更划算：
// 这只是"按键 → 下标"的纯函数，没有需要保持一致的业务语义。
func digitIndex(key string) int {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return -1
	}
	return int(key[0] - '1')
}

// newLabelEditor 是标签编辑视图。
//
// 可用标签 = 内置预设 + 用户自定义 + 所有条目上已用过的（去重）。
// 这个规则来自 v2.1.0，好处是标签库不会随使用膨胀，
// 也不会出现"库里有用不上的悬空项"。
func newLabelEditor(src Source, sel plugin.Selection, target model.Labeled, services svc.Services) plugin.View {
	var status string

	available := func() []string {
		seen := map[string]bool{}
		var out []string
		add := func(name string) {
			name = model.LabelName(name)
			if name == "" || seen[name] {
				return
			}
			seen[name] = true
			out = append(out, name)
		}
		for _, n := range model.LabelPresets {
			add(n.Name)
		}
		if cfg := src.Config(); cfg != nil {
			for _, n := range cfg.CustomLabelList() {
				add(n)
			}
		}
		if data := src.Day(); data != nil {
			for _, t := range data.All() {
				for _, n := range t.Labels {
					add(n)
				}
			}
		}
		for _, g := range src.Goals() {
			for _, n := range g.Labels {
				add(n)
			}
		}
		return out
	}

	return &plugin.ViewFunc{
		ViewName: "标签",
		RenderFn: func(ctx plugin.RenderCtx) {
			labels := available()
			current := target.ItemLabels()
			curSet := map[string]bool{}
			for _, n := range current {
				curSet[n] = true
			}
			head := []string{
				"标签 · " + sel.Title,
				"",
				"  当前：" + strings.Join(current, " "),
				"",
				"  按数字键切换：",
			}
			// 借调视图通常比磁贴宽，但仍然折行而不是截断：
			// 窄终端下借调区也可能只有几十列。
			y := ctx.Rect.Y
			for _, l := range ctx.WrapLines(head) {
				ctx.Canvas.Text(ctx.Rect.X, y, l, tile.StyleMuted)
				y++
			}
			for i, name := range labels {
				if y >= ctx.Rect.Y1() || i >= 9 {
					break
				}
				mark, style := "○", tile.StyleMuted
				if curSet[name] {
					mark, style = "✔", tile.StyleStatus
				}
				line := "   " + string(rune('1'+i)) + " " + mark + " " + name
				for _, l := range ctx.Wrap(line) {
					if y >= ctx.Rect.Y1() {
						break
					}
					ctx.Canvas.Text(ctx.Rect.X, y, l, style)
					y++
				}
			}
			if status != "" && y < ctx.Rect.Y1() {
				ctx.Canvas.Text(ctx.Rect.X, y, canvas.Truncate("  "+status, ctx.Rect.W), tile.StyleMuted)
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			switch ev.Key {
			case "esc":
				return plugin.None(), true
			}
			n := digitIndex(ev.Key)
			if n < 0 {
				return plugin.None(), false
			}
			labels := available()
			if n >= len(labels) {
				return plugin.None(), false
			}
			name := labels[n]
			if target.ToggleItemLabel(name) {
				status = "已加上「" + name + "」"
			} else {
				status = "已去掉「" + name + "」"
			}
			return plugin.Persist(LabelPackID, "day", src.Day()), false
		},
	}
}
