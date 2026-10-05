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

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// CapItemSelection 是"有可选中条目"这个能力标记。
//
// 联动选项（设 DDL、打标签）以此为前提：没有条目可选，它们就没有作用对象。
// 见 design §5.1 的依赖图。
const CapItemSelection = "item.selection"

// CapItemDue / CapItemLabel 是选中项支持的具体操作能力。
const (
	CapItemDue   = "item.due"
	CapItemLabel = "item.label"
)

// TodoID / TodoFixedID / TodoFloatingID 是本包与成员的 ID。
const (
	TodoPackID     = "kqflow.todo"
	TodoFixedID    = "kqflow.todo.fixed"
	TodoFloatingID = "kqflow.todo.floating"
)

// todoPack 是 TODO 整合包：固定与临时两个磁贴。
//
// **它们是同一个包的两个磁贴，不是两个包**（design §5.1 明确纠正过这一点）：
// 它们是一个功能的两半，拆成两个包会让用户能关掉一半、
// 留下一个语义残缺的功能。而"显示哪个、放哪儿"仍由视图配置决定。
type todoPack struct {
	src   Source
	state *HostState
}

// NewTodoPack 创建 TODO 包。
func NewTodoPack(src Source, st *HostState) plugin.Pack {
	return &todoPack{src: src, state: st}
}

func (p *todoPack) ID() string              { return TodoPackID }
func (p *todoPack) Name() string            { return "每日待办" }
func (p *todoPack) Version() semver.Version { return semver.MustParse("0.1.0") }
func (p *todoPack) EngineAPI() semver.Range { return engineRange }
func (p *todoPack) Enabled() bool           { return true }
func (p *todoPack) Requires() []string      { return nil }
func (p *todoPack) Conflicts() []string     { return nil }

// Provides 声明本包提供"可选中条目"。
//
// 这一条是 DDL / 标签包能否生效的前提：装载器据此判断
// "有没有组件会接纳它们"（没有就是警告，不是错误）。
func (p *todoPack) Provides() []string { return []string{CapItemSelection} }

func (p *todoPack) Members() []plugin.Plugin {
	return []plugin.Plugin{
		&tilePluginSpec{
			mf: plugin.Manifest{
				ID: TodoFixedID, Name: "固定待办", Kind: plugin.KindTile,
				Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
				Slots: plugin.SlotPreference{Anchor: geometry.AnchorLeftTop, Priority: 20},
			},
			newComp: func(s svc.Services) plugin.Component {
				return &todoTile{src: p.src, state: p.state, kind: model.KindFixed,
					title: "TODAY · 固定", svc: s}
			},
		},
		&tilePluginSpec{
			mf: plugin.Manifest{
				ID: TodoFloatingID, Name: "临时待办", Kind: plugin.KindTile,
				Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
				Slots: plugin.SlotPreference{Anchor: geometry.AnchorLeftBottom, Priority: 10},
			},
			newComp: func(s svc.Services) plugin.Component {
				return &todoTile{src: p.src, state: p.state, kind: model.KindFloating,
					title: "TODAY · 临时", svc: s}
			},
		},
	}
}

func (p *todoPack) Assemble(s svc.Services) (plugin.Assembled, error) {
	a := &todoAssembled{p: p}
	for _, m := range p.Members() {
		comp, err := m.New(s)
		if err != nil {
			return nil, err
		}
		a.tiles = append(a.tiles, comp)
	}
	return a, nil
}

type todoAssembled struct {
	p     *todoPack
	tiles []plugin.Component
}

func (a *todoAssembled) Pack() plugin.Pack                  { return a.p }
func (a *todoAssembled) Kernel() plugin.Kernel              { return nil }
func (a *todoAssembled) Tiles() []plugin.Component          { return a.tiles }
func (a *todoAssembled) BoardOptions() []plugin.BoardOption { return nil }
func (a *todoAssembled) ContextOptions() []plugin.ContextOption {
	return nil
}
func (a *todoAssembled) Services() []plugin.Service { return nil }
func (a *todoAssembled) Dispose()                   {}

// todoTile 是"固定"或"临时"待办列表。
type todoTile struct {
	src   Source
	state *HostState
	kind  model.Kind
	title string
	svc   svc.Services
}

func (t *todoTile) Title() string { return t.title }

// Render 画出列表，选中项高亮。
//
// 注意它只画**内容区**（ctx.Rect 已经去掉了边框与标题行）：
// 外框与标题由 tile.DrawTile 统一负责，"卡片框大小不定"因此不可能出现。
func (t *todoTile) Render(ctx plugin.RenderCtx) {
	items := TodoList(t.src, t.kind)
	cursor := ClampCursor(t.cursorIndex(), len(items))
	if len(items) == 0 {
		ctx.Canvas.Text(ctx.Rect.X, ctx.Rect.Y,
			canvas.Truncate("（今天还没有条目）", ctx.Rect.W), tile.StyleMuted)
		return
	}
	// 滚动：光标必须始终可见，否则用户会以为"按了没反应"。
	//
	// 这里**按显示行数**倒推起点，而不是按条目数：一条长待办折行后占两行，
	// 若还按条目数算，光标那一条就可能只露出半行，
	// 或者末尾几条挤掉光标所在条（用户看不到自己选中的是谁）。
	rows := make([]int, len(items))
	rowW := itemRowWidth(ctx.Rect)
	for i, it := range items {
		rows[i] = itemRows(it, rowW)
	}
	start := firstVisibleByRows(rows, cursor, ctx.Rect.H)
	y := ctx.Rect.Y
	for i := start; i < len(items); i++ {
		if y >= ctx.Rect.Y1() {
			return
		}
		line := todoLine(items[i])
		style := tile.StyleMuted
		prefix := strings.Repeat(" ", markerWidth)
		if i == cursor && ctx.Focus {
			prefix, style = "▸ ", tile.StyleTitleFocused
		} else if items[i].Done {
			style = tile.StyleBorderDim
		}
		// 折行而不是截断：长标题在小磁贴里被砍一半最难读。
		y = drawWrappedInset(ctx, y, ctx.Rect, prefix, strings.Repeat(" ", listIndentWidth), line, style)
	}
}

// markerWidth 是"记号 + 一个空格"的显示宽度（滚动计算与绘制必须用同一个值）。
const markerWidth = 2

// itemRows 返回一条待办折行后占几行（rowW 已是正文可用宽度）。
//
// 它必须与绘制路径用**同一个**可用宽度与同一个折行函数，
// 否则"占几行"的判定会与画出来的不一致（见 textAreaWidth 的说明）。
func itemRows(t *model.Todo, rowW int) int {
	if rowW < 1 {
		return 1
	}
	if n := len(canvas.Wrap(todoLine(t), rowW)); n > 0 {
		return n
	}
	return 1
}

// todoLine 生成一条待办的显示文本。
func todoLine(t *model.Todo) string {
	mark := "○"
	if t.Done {
		mark = "✔"
	} else if t.Status == model.StatusDoing {
		mark = "◐"
	}
	line := fmt.Sprintf("%s %s", mark, t.Title)
	if d := t.Due; d != "" {
		line += "  ⏰" + d
	}
	if done, total := t.Progress(); total > 0 {
		line += fmt.Sprintf("  (%d/%d)", done, total)
	}
	return line
}

// cursorIndex 返回本磁贴对应的光标。
func (t *todoTile) cursorIndex() int {
	if t.kind == model.KindFixed {
		return t.state.fixedCursor()
	}
	return t.state.floatingCursor()
}

// setCursor 设置本磁贴对应的光标。
func (t *todoTile) setCursor(v int) {
	if t.kind == model.KindFixed {
		t.state.setFixedCursor(v)
		return
	}
	t.state.setFloatingCursor(v)
}

// KeyHints 申报"光标停在这个列表上时能按什么"。
//
// 这是用户点明的那件事：**光标只有悬停作用**，但下栏提示要跟着它变。
// 因此空列表与有列表给出的提示不同——空列表上 j/k 与勾选都没有意义，
// 提示里就不该出现它们（否则用户按了没反应，会以为程序坏了）。
func (t *todoTile) KeyHints(plugin.RenderCtx) []plugin.KeyHint {
	if len(TodoList(t.src, t.kind)) == 0 {
		// 列表为空：只能等用户先加条目，这里如实说明"没什么可按的"。
		return nil
	}
	return []plugin.KeyHint{
		{Key: "j/k", Desc: "移动"},
		{Key: "space", Desc: "勾选"},
		{Key: "l", Desc: "操作"},
	}
}

// Update 处理按键：移动光标、勾选、上报选中。
//
// 选中始终 = **光标所在的那一条**。移动光标会重新上报，因此按 l 打开的
// 事务一定作用在"用户眼睛看到的那一条"上——不会出现"高亮在 A、
// 操作作用在 B"这种最难查的错位。
func (t *todoTile) Update(ctx plugin.EventCtx, ev plugin.Event) plugin.Action {
	items := TodoList(t.src, t.kind)
	switch ev.Key {
	case "j", "down":
		t.setCursor(MoveCursor(t.cursorIndex(), 1, len(items)))
		return t.selectCurrent()
	case "k", "up":
		t.setCursor(MoveCursor(t.cursorIndex(), -1, len(items)))
		return t.selectCurrent()
	case " ", "enter":
		return t.toggle(ctx, items)
	}
	return plugin.None()
}

// toggle 勾选当前条目并落盘。
//
// 落盘走 ActionPersist（而不是直接调 src.Save）：这是"插件只说意图"的落点，
// 引擎据此统一处理保存失败的提示。
func (t *todoTile) toggle(ctx plugin.EventCtx, items []*model.Todo) plugin.Action {
	cur := ClampCursor(t.cursorIndex(), len(items))
	if cur >= len(items) {
		return plugin.None()
	}
	items[cur].Toggle(ctx.Now)
	return plugin.Persist(TodoPackID, "day", t.src.Day())
}

// selectCurrent 把"当前选中的是哪一条"上报给引擎。
//
// 联动选项（设 DDL、打标签）据此出现或消失——**它们不需要认识本磁贴**，
// 只认引擎广播的 Selection（design §5.2）。
func (t *todoTile) selectCurrent() plugin.Action {
	items := TodoList(t.src, t.kind)
	cur := ClampCursor(t.cursorIndex(), len(items))
	if cur >= len(items) {
		t.state.selectTodo("")
		return plugin.Select(plugin.Selection{})
	}
	item := items[cur]
	t.state.selectTodo(item.ID)
	return plugin.Select(plugin.Selection{
		Kind: "todo", ID: item.ID, Title: item.Title,
		Can: svc.Capability{CapItemDue, CapItemLabel},
	})
}

// scrollOffset 计算列表滚动偏移，保证光标可见（**按条目数**，用于单行列表）。
func scrollOffset(cursor, n, height int) int {
	if height <= 0 || n <= height {
		return 0
	}
	if cursor < height {
		return 0
	}
	off := cursor - height + 1
	if off > n-height {
		off = n - height
	}
	if off < 0 {
		off = 0
	}
	return off
}

// firstVisibleByRows 按"每条占几行"倒推出起始下标，保证 cursor 可见。
//
// 这是滚动计算与绘制之间**唯一**的共享判定：两侧都从同一组 rows 取信息，
// 因此不会出现"按条目数滚动、按显示行绘制"的错位
// （那种错位会让长条目算少一行，光标跑到可视区外）。
func firstVisibleByRows(rows []int, cursor, height int) int {
	if height <= 0 || len(rows) == 0 {
		return 0
	}
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	if cursor < 0 {
		return 0
	}
	used := 0
	start := cursor
	for start > 0 {
		need := rows[start-1]
		if need < 1 {
			need = 1
		}
		if used+need > height {
			break
		}
		used += need
		start--
	}
	return start
}

// tilePluginSpec 是一个"声明 + 构造函数"组成的磁贴插件。
//
// 用它替代给每个磁贴写一个空壳类型：本包里的磁贴都只是在同一个
// Manifest 结构上换几个字段，写三个几乎相同的类型只会增加噪声。
type tilePluginSpec struct {
	mf      plugin.Manifest
	newComp func(svc.Services) plugin.Component
}

func (p *tilePluginSpec) Manifest() plugin.Manifest { return p.mf }
func (p *tilePluginSpec) New(s svc.Services) (plugin.Component, error) {
	return p.newComp(s), nil
}
