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
		// 下面三个是"日常真正要用的动作"：加、改、删。
		// 它们与列表**同属一个包**，因此认识列表类型——
		// 这正是"包内可以有机耦合"的用法（添加动作天然要知道加到哪个列表）。
		//
		// 加/改/删都做成**联动选项**：作用对象是"光标所在的列表/条目"，
		// 由引擎广播的 Selection 传达，它们不需要认识任何磁贴。
		&ctxOptionSpec{
			mf: plugin.Manifest{
				ID: AddTaskOptionID, Name: "添加子任务", Kind: plugin.KindContextOption,
				Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
			},
			opt: &todoAddTaskOption{src: p.src, state: p.state},
		},
		&ctxOptionSpec{
			mf: plugin.Manifest{
				ID: AddTodoOptionID, Name: "添加待办", Kind: plugin.KindContextOption,
				Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
			},
			opt: &todoAddOption{src: p.src, state: p.state, kind: model.KindFixed},
		},
		&ctxOptionSpec{
			mf: plugin.Manifest{
				ID: EditTodoOptionID, Name: "重命名待办", Kind: plugin.KindContextOption,
				Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
			},
			opt: &todoRenameOption{src: p.src, state: p.state},
		},
		&ctxOptionSpec{
			mf: plugin.Manifest{
				ID: DelTodoOptionID, Name: "删除待办", Kind: plugin.KindContextOption,
				Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
			},
			opt: &todoDeleteOption{src: p.src, state: p.state},
		},
	}
}

func (p *todoPack) Assemble(s svc.Services) (plugin.Assembled, error) {
	a := &todoAssembled{p: p}
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

type todoAssembled struct {
	p     *todoPack
	tiles []plugin.Component
	ctx   []plugin.ContextOption
}

func (a *todoAssembled) Pack() plugin.Pack                  { return a.p }
func (a *todoAssembled) Kernel() plugin.Kernel              { return nil }
func (a *todoAssembled) Tiles() []plugin.Component          { return a.tiles }
func (a *todoAssembled) BoardOptions() []plugin.BoardOption { return nil }
func (a *todoAssembled) ContextOptions() []plugin.ContextOption {
	return a.ctx
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
	// 子任务模式：只画**这一条**的子任务，顶层列表暂时让位。
	//
	// 为什么不是"列表下面追加子任务"：磁贴可能只有几行高，
	// 混在一起会两者都看不清。而用户进入子任务模式时，
	// 他关心的本来就只有这一个条目。
	if t.subtaskActiveFor(items, cursor) {
		t.renderSubtasks(ctx, items[cursor])
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

// subtaskActiveFor 报告"是否应当以子任务模式渲染"。
//
// 三重校验（缺一个都会画出错东西）：
//   - 模式开着；
//   - 光标所指的那一条**就是**当初进入时的那一条（SubtaskOwner）；
//   - 它确实有子任务。
//
// 第二条尤其重要：用户可能在子任务模式里 ququ 顶层光标变了
// （例如别处刷新），那时再画"这个条目的子任务"就是驴唇不对马嘴。
func (t *todoTile) subtaskActiveFor(items []*model.Todo, cursor int) bool {
	if !t.state.SubtaskActive || cursor >= len(items) {
		return false
	}
	it := items[cursor]
	if it.ID != t.state.SubtaskOwner || len(it.Tasks) == 0 {
		return false
	}
	return true
}

// renderSubtasks 画出子任务列表（带一个"从哪来"的标题行）。
func (t *todoTile) renderSubtasks(ctx plugin.RenderCtx, todo *model.Todo) {
	cur := ClampCursor(t.state.SubtaskCursor, len(todo.Tasks))
	rowW := itemRowWidth(ctx.Rect)
	y := ctx.Rect.Y

	// 标题行：告诉用户"现在看的是谁的子任务"。
	// 没有它，用户会以为顶层列表的内容变了。
	head := "▾ " + DisplayTitle(todo.Title)
	if n := len(canvas.Wrap(head, ctx.Rect.W)); n > 0 && y < ctx.Rect.Y1() {
		y = drawWrappedInset(ctx, y, ctx.Rect, "", "", head, tile.StyleStatus)
	}

	rows := make([]int, len(todo.Tasks))
	for i := range todo.Tasks {
		rows[i] = taskRows(&todo.Tasks[i], rowW)
	}
	avail := ctx.Rect.H - (y - ctx.Rect.Y)
	if avail < 1 {
		return
	}
	start := firstVisibleByRows(rows, cur, avail)
	for i := start; i < len(todo.Tasks); i++ {
		if y >= ctx.Rect.Y1() {
			return
		}
		task := &todo.Tasks[i]
		style := tile.StyleMuted
		prefix := strings.Repeat(" ", markerWidth+listIndentWidth)
		if i == cur && ctx.Focus {
			prefix, style = "  ▸ ", tile.StyleTitleFocused
		} else if task.Done() {
			style = tile.StyleBorderDim
		}
		y = drawWrappedInset(ctx, y, ctx.Rect, prefix,
			strings.Repeat(" ", markerWidth+listIndentWidth+2), taskLine(task), style)
	}
}

// taskLine 生成一条子任务的显示文本。
func taskLine(task *model.Task) string {
	mark := "○"
	if task.Done() {
		mark = "✔"
	} else if task.Status == model.StatusDoing {
		mark = "◐"
	}
	return fmt.Sprintf("%s %s", mark, DisplayTitle(task.Title))
}

// taskRows 返回一条子任务折行后占几行。
func taskRows(task *model.Task, rowW int) int {
	if rowW < 1 {
		return 1
	}
	if n := len(canvas.Wrap(taskLine(task), rowW)); n > 0 {
		return n
	}
	return 1
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
//
// 标题走 DisplayTitle：老数据里可能带不可见的控制字符（见其说明）。
func todoLine(t *model.Todo) string {
	mark := "○"
	if t.Done {
		mark = "✔"
	} else if t.Status == model.StatusDoing {
		mark = "◐"
	}
	line := fmt.Sprintf("%s %s", mark, DisplayTitle(t.Title))
	if d := t.Due; d != "" {
		line += "  ⏰" + d
	}
	if done, total := t.Progress(); total > 0 {
		line += fmt.Sprintf("  (%d/%d)", done, total)
	}
	return line
}

// OwnsEsc 报告"子任务模式占着 esc"。
//
// 引擎在分派 esc 之前会问它（见 plugin.ModalOwner）：没有它，
// 子任务模式里的 esc 会被引擎当成"退出上一层界面"处理掉，
// 而子任务模式还开着——用户按 esc 想退出一层，实际退出了两层（或零层）。
func (t *todoTile) OwnsEsc() bool {
	items := TodoList(t.src, t.kind)
	return t.subtaskActiveFor(items, ClampCursor(t.cursorIndex(), len(items)))
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
	items := TodoList(t.src, t.kind)
	if len(items) == 0 {
		// 列表为空：只能等用户先加条目，这里如实说明"没什么可按的"。
		return nil
	}
	if t.subtaskActiveFor(items, ClampCursor(t.cursorIndex(), len(items))) {
		// 子任务模式是**另一套按键**，提示必须跟着换——
		// 否则用户会按 j/k 以为在动子任务，实际提示里写的是别的东西。
		return []plugin.KeyHint{
			{Key: "j/k", Desc: "选择子任务"},
			{Key: "space", Desc: "勾选子任务"},
			{Key: "esc", Desc: "退回条目"},
		}
	}
	return []plugin.KeyHint{
		{Key: "j/k", Desc: "移动"},
		{Key: "space", Desc: "勾选"},
		{Key: "enter", Desc: "子任务"},
		{Key: "l", Desc: "操作"},
	}
}

// FocusSelection 回报"本磁贴获得焦点时选中的是谁"——就是光标所指那一条。
//
// 光标初始为 0，因此 **tab 过来就等于选中了第一条**（用户直觉如此）。
// 列表为空时返回零值 Selection，表示"这里没有东西可选"——
// 此时按 l 只会看到通用选项，不会误作用到别的磁贴的条目上。
func (t *todoTile) FocusSelection(plugin.RenderCtx) plugin.Selection {
	// 记下"光标在哪个列表里"：添加待办要知道加到哪儿（见 HostState.FocusList）。
	t.state.FocusList = string(t.kind)
	items := TodoList(t.src, t.kind)
	cur := ClampCursor(t.cursorIndex(), len(items))
	if cur >= len(items) {
		t.state.selectTodo("")
		// 列表为空：上报"这个**列表**被选中"。
		//
		// 不能返回空选中——那样 Applies 会让**所有**联动选项消失，
		// 连"添加"都不出现，用户就永远加不进第一条（真实的死锁）。
		ref := ListRefFixed
		if t.kind == model.KindFloating {
			ref = ListRefFloating
		}
		return listSelection(ref)
	}
	item := items[cur]
	t.state.selectTodo(item.ID)
	// 子任务模式：选中的是**子任务**，不是父条目。
	//
	// 这样联动选项会自然收敛（它们匹配 "todo"，因此不再出现），
	// 而以后要加"作用于子任务"的选项时，匹配 "subtask" 即可。
	// 关键是：父条目仍然通过 Owner 可查（SubtaskOwner），
	// 因此"给这个子任务设截止时间"这类需求不会因为换了 Kind 而做不到。
	if t.subtaskActiveFor(items, cur) {
		tasks := item.Tasks
		tc := ClampCursor(t.state.SubtaskCursor, len(tasks))
		if tc < len(tasks) {
			return plugin.Selection{
				Kind: "subtask", ID: tasks[tc].ID, Title: DisplayTitle(tasks[tc].Title),
			}
		}
	}
	return plugin.Selection{
		Kind: "todo", ID: item.ID, Title: DisplayTitle(item.Title),
		Can: svc.Capability{CapItemDue, CapItemLabel},
	}
}

// Update 处理按键：移动光标、勾选、上报选中。
//
// 选中始终 = **光标所在的那一条**。移动光标会重新上报，因此按 l 打开的
// 事务一定作用在"用户眼睛看到的那一条"上——不会出现"高亮在 A、
// 操作作用在 B"这种最难查的错位。
func (t *todoTile) Update(ctx plugin.EventCtx, ev plugin.Event) plugin.Action {
	items := TodoList(t.src, t.kind)
	// 子任务模式有自己的一套按键，因此先分流（与 2.1.0 的 taskActive 同理）。
	if t.subtaskActiveFor(items, ClampCursor(t.cursorIndex(), len(items))) {
		return t.updateSubtasks(ctx, ev, items)
	}
	switch ev.Key {
	case "j", "down":
		t.setCursor(MoveCursor(t.cursorIndex(), 1, len(items)))
		return t.selectCurrent()
	case "k", "up":
		t.setCursor(MoveCursor(t.cursorIndex(), -1, len(items)))
		return t.selectCurrent()
	case "esc":
		// esc 在列表模式下不做事：它属于"退回"语义，而列表就是最外层。
		// 交给引擎（它会去关借调层或什么都不做）。
		return plugin.None()
	case "enter":
		// enter 进入子任务模式（与 2.1.0 一致：enter 是"进入下级"）。
		//
		// 与 space 的分工：space 勾选整条，enter 进入它的下级。
		// 两者都常用，因此不能合并——合并了必然有一个要绕路。
		return t.enterSubtasks(items)
	case " ", "L":
		return t.toggle(ctx, items)
	}
	return plugin.None()
}

// enterSubtasks 进入子任务模式。
func (t *todoTile) enterSubtasks(items []*model.Todo) plugin.Action {
	cur := ClampCursor(t.cursorIndex(), len(items))
	if cur >= len(items) {
		return plugin.None()
	}
	it := items[cur]
	if len(it.Tasks) == 0 {
		// 没有子任务时如实告诉用户怎么办（2.1.0 的提示也是这句）。
		// 静默无反应会让人以为程序坏了。
		return plugin.Toast("该项还没有子任务，按 t 添加")
	}
	t.state.SubtaskActive = true
	t.state.SubtaskOwner = it.ID
	t.state.SubtaskCursor = ClampCursor(t.state.SubtaskCursor, len(it.Tasks))
	return t.selectCurrent()
}

// updateSubtasks 处理子任务模式下的按键。
func (t *todoTile) updateSubtasks(ctx plugin.EventCtx, ev plugin.Event, items []*model.Todo) plugin.Action {
	cur := ClampCursor(t.cursorIndex(), len(items))
	todo := items[cur]
	switch ev.Key {
	case "esc":
		// 退出子任务模式（先退这一层，不是直接关程序——引擎的 esc 只
		// 在没有人消费时才会往上走，而这里我们消费它）。
		t.state.SubtaskActive = false
		t.state.SubtaskOwner = ""
		return t.selectCurrent()
	case "j", "down":
		t.state.SubtaskCursor = MoveCursor(t.state.SubtaskCursor, 1, len(todo.Tasks))
		return t.selectCurrent()
	case "k", "up":
		t.state.SubtaskCursor = MoveCursor(t.state.SubtaskCursor, -1, len(todo.Tasks))
		return t.selectCurrent()
	case " ", "enter":
		return t.toggleSubtask(ctx, todo)
	}
	return plugin.None()
}

// toggleSubtask 勾选当前子任务，并回写父条目状态。
//
// 回写必须走 model.Todo.SyncFromTasks：父条目的完成状态是**派生**的
// （全部子任务完成 ⇒ 父完成；部分完成 ⇒ 父回到进行中）。各写各的
// 一定会不一致，而那种不一致在界面上表现为"子任务全勾了父条目还没完成"。
func (t *todoTile) toggleSubtask(ctx plugin.EventCtx, todo *model.Todo) plugin.Action {
	cur := ClampCursor(t.state.SubtaskCursor, len(todo.Tasks))
	if cur >= len(todo.Tasks) {
		return plugin.None()
	}
	task := &todo.Tasks[cur]
	if task.Done() {
		task.Status = model.StatusTodo
		task.DoneAt = nil
	} else {
		at := ctx.Now
		task.Status = model.StatusDone
		task.DoneAt = &at
	}
	todo.SyncFromTasks(ctx.Now)
	return plugin.Persist(TodoPackID, "day", t.src.Day())
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
	return plugin.Select(t.FocusSelection(plugin.RenderCtx{}))
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
