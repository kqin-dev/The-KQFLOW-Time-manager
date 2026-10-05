package kxapp

import (
	"fmt"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/svc"
	"github.com/kqin-dev/kxflow/tile"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// 条目编辑相关的插件 ID。
const (
	AddTodoOptionID  = "kqflow.todo.add"
	AddTaskOptionID  = "kqflow.todo.addtask"
	DelTodoOptionID  = "kqflow.todo.del"
	AddGoalOptionID  = "kqflow.goal.add"
	DelGoalOptionID  = "kqflow.goal.del"
	EditTodoOptionID = "kqflow.todo.edit"
	EditGoalOptionID = "kqflow.goal.edit"
	// AddOptionOrder 是"添加待办"在菜单里的位置。
	//
	// 排在标签（10）与"添加子任务"（12）**后面**：三者都常用，
	// 但"对当前这一条动手"（打标签、拆子任务）比"往列表里加新的"
	// 更贴近用户此刻盯着的那一条。
	AddOptionOrder    = 15
	DeleteOptionOrder = 60
)

// 这些是"真正的日常操作"：**没有它们，引擎就不能替代 2.1.0**。
// 之前几轮做的是渲染与交互框架，但用户每天要做的第一件事是"记一条待办"——
// 因此这一轮先把它补齐，而不是去做更好看的历史页。
//
// 它们都是**联动选项**（作用在"当前列表"这个上下文上）：
//   - 添加：作用在光标所在的那个列表（固定/临时/目标），列表身份由磁贴决定；
//   - 删除/重命名：作用在光标所指的那一条。
//
// 与 kqflow.ddl / kqflow.labels 的区别是它们**不需要外部能力**：
// 列表本身就提供"可选中条目"，而它们与列表同属一个包（kqflow.todo / kqflow.goal），
// 因此可以直接拿到列表类型。

// todoAddOption 是"添加待办"联动选项。
//
// 它由 todoPack 提供（与列表同包），因此**认识列表类型**——
// 这正是"包内可以有机耦合"的用法：添加动作天然要知道"加到哪个列表"。
type todoAddOption struct {
	src   Source
	state *HostState
	kind  model.Kind
}

// AppliesTo 同时认"条目"与"列表本身"。
//
// 列表为空时选中是"列表本身"（见 listSelection）：而空列表恰恰最需要
// "添加"，因此它必须在这两种情况下都出现。
func (o *todoAddOption) AppliesTo() []string { return []string{"todo", ListRefKind} }

// Requires 返回 nil —— 这一点很关键，别再改回 CapItemSelection。
//
// CapItemSelection 是"**别的**包提供了可选中条目"这个能力标记，
// 由列表磁贴通过 Selection.Can 声明（todo 磁贴声明的是 item.due/item.label）。
// 而"添加/删除/重命名"是列表**自己的**操作，不需要外部组件接纳——
// 声明 Requires(CapItemSelection) 会让它们在菜单里彻底消失
// （实测症状：菜单里只剩打标签/设截止时间，加删改一条都不见）。
func (o *todoAddOption) Requires() []string { return nil }
func (o *todoAddOption) Order() int         { return AddOptionOrder }

func (o *todoAddOption) Label(plugin.Selection) string {
	if o.kind == model.KindFixed {
		return "添加固定待办…"
	}
	return "添加临时待办…"
}

func (o *todoAddOption) Activate(sel plugin.Selection, services svc.Services) (plugin.View, error) {
	// 加到**光标所在的那个列表**，而不是这个选项自己的默认 kind。
	//
	// 同一个"添加待办"选项要服务固定与临时两个列表，因此目标列表
	// 只能来自当前上下文（HostState.FocusList，由列表磁贴在获得焦点时记下）。
	// 用一个选项覆盖两个列表，好过让用户面对"添加固定待办 /
	// 添加临时待办"两条几乎一样的菜单项——菜单越长越难选。
	kind := o.kind
	switch o.state.FocusList {
	case string(model.KindFixed):
		kind = model.KindFixed
	case string(model.KindFloating):
		kind = model.KindFloating
	}
	return newAddTodoView(o.src, o.state, kind), nil
}

// newAddTodoView 是"添加待办"输入界面。
//
// 添加成功后**不关界面**：连着记几条是常态（2.1.0 的添加框也是留在原地）。
// 关掉的方式是 esc，或者输入空内容回车（表示"记完了"）。
func newAddTodoView(src Source, st *HostState, kind model.Kind) plugin.View {
	input := ""
	var status string
	which := "固定"
	if kind == model.KindFloating {
		which = "临时"
	}

	return &plugin.ViewFunc{
		ViewName: "添加" + which + "待办",
		HintFn: func(plugin.RenderCtx) []plugin.KeyHint {
			return []plugin.KeyHint{
				{Key: "enter", Desc: "添加"},
				{Key: "esc", Desc: "记完了"},
			}
		},
		RenderFn: func(ctx plugin.RenderCtx) {
			y := ctx.Rect.Y
			put := func(s string, style canvas.StyleID) {
				y = drawWrapped(ctx, y, ctx.Rect, s, style)
			}
			put("添加"+which+"待办", tile.StyleTitle)
			put("", tile.StyleMuted)
			put("直接输入内容，回车添加（可以连续添加几条）", tile.StyleMuted)
			put("", tile.StyleMuted)
			put("输入："+input+"▏", tile.StyleAccent)
			if status != "" {
				put("", tile.StyleMuted)
				put(status, tile.StyleStatus)
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			switch ev.Key {
			case "esc":
				return plugin.None(), true
			case "enter":
				title := model.Sanitize(input, false)
				if title == "" {
					// 空内容回车 = "记完了"：这是不打字就能退出的方式。
					return plugin.None(), true
				}
				data := src.Day()
				if data == nil {
					status = "没有当天数据，无法添加"
					return plugin.None(), false
				}
				todo := model.NewTodo(title, kind, data.Day, ec.Now)
				if kind == model.KindFixed {
					data.Fixed = append(data.Fixed, todo)
				} else {
					data.Floating = append(data.Floating, todo)
				}
				// 光标跟到新条目上（长列表里刚加的那条常常在末尾）。
				if kind == model.KindFixed {
					st.setFixedCursor(len(data.Fixed) - 1)
				} else {
					st.setFloatingCursor(len(data.Floating) - 1)
				}
				input = ""
				status = "已添加：" + title
				return plugin.Persist(TodoPackID, "day", data), false
			case "backspace":
				input = trimLastRune(input)
				return plugin.None(), false
			}
			for _, r := range ev.Runes {
				if isSettingRune(r) {
					input += string(r)
				}
			}
			if len(ev.Runes) == 0 && len(ev.Key) == 1 {
				if r := rune(ev.Key[0]); isSettingRune(r) {
					input += ev.Key
				}
			}
			return plugin.None(), false
		},
	}
}

// todoAddTaskOption 是"添加子任务"联动选项。
//
// 2.1.0 用裸键 `t` 做这件事；v3 把它放进统一入口（`l`）。
// 这里保留的是**能力**，换掉的是触发方式——这是刻意的设计差异，
// 已在 docs/kxflow-parity.md 里记明。
type todoAddTaskOption struct {
	src   Source
	state *HostState
}

func (o *todoAddTaskOption) AppliesTo() []string { return []string{"todo"} }
func (o *todoAddTaskOption) Requires() []string  { return nil }

// Order 让它排在"添加待办"（15）**之前**：往当前条目里加子任务
// 比往列表里加新条目更贴近用户此刻盯着的那一条（与标签的取舍同理）。
func (o *todoAddTaskOption) Order() int { return 12 }

func (o *todoAddTaskOption) Label(sel plugin.Selection) string {
	if sel.Title == "" {
		return "添加子任务…"
	}
	return "给「" + sel.Title + "」加子任务…"
}

func (o *todoAddTaskOption) Activate(sel plugin.Selection, services svc.Services) (plugin.View, error) {
	return newAddTaskView(o.src, o.state, sel.ID), nil
}

// newAddTaskView 是"添加子任务"输入界面。
//
// 与添加待办同形：可以**连续添加**（回车加一条，esc 记完了），
// 因为"拆解一件事"通常一次就要写好几条，来回开关最打断思路。
func newAddTaskView(src Source, st *HostState, todoID string) plugin.View {
	input := ""
	var status string
	return &plugin.ViewFunc{
		ViewName: "添加子任务",
		HintFn: func(plugin.RenderCtx) []plugin.KeyHint {
			return []plugin.KeyHint{
				{Key: "enter", Desc: "添加"},
				{Key: "esc", Desc: "记完了"},
			}
		},
		RenderFn: func(ctx plugin.RenderCtx) {
			y := ctx.Rect.Y
			put := func(s string, style canvas.StyleID) {
				y = drawWrapped(ctx, y, ctx.Rect, s, style)
			}
			put("添加子任务", tile.StyleTitle)
			put("", tile.StyleMuted)
			put("直接输入内容，回车添加（可以连续添加几条）", tile.StyleMuted)
			put("", tile.StyleMuted)
			put("输入："+input+"▏", tile.StyleAccent)
			if status != "" {
				put("", tile.StyleMuted)
				put(status, tile.StyleStatus)
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			switch ev.Key {
			case "esc":
				return plugin.None(), true
			case "enter":
				title := model.Sanitize(input, false)
				if title == "" {
					return plugin.None(), true
				}
				data := src.Day()
				if data == nil {
					status = "没有当天数据，无法添加"
					return plugin.None(), false
				}
				todo := data.Find(todoID)
				if todo == nil {
					status = "条目已经不在了"
					return plugin.None(), false
				}
				todo.Tasks = append(todo.Tasks, model.NewTask(title))
				// 父条目状态是**派生**的（见 SyncFromTasks），加完要重算。
				todo.SyncFromTasks(ec.Now)
				// 记下"下次 enter 该进哪个条目的子任务"，并且光标落在刚加的那条上。
				st.SubtaskOwner = todoID
				st.SubtaskCursor = len(todo.Tasks) - 1
				input = ""
				status = "已添加：" + title
				return plugin.Persist(TodoPackID, "day", data), false
			case "backspace":
				input = trimLastRune(input)
				return plugin.None(), false
			}
			for _, r := range ev.Runes {
				if isSettingRune(r) {
					input += string(r)
				}
			}
			if len(ev.Runes) == 0 && len(ev.Key) == 1 {
				if r := rune(ev.Key[0]); isSettingRune(r) {
					input += ev.Key
				}
			}
			return plugin.None(), false
		},
	}
}

// todoDeleteOption 是"删除当前待办"联动选项。
type todoDeleteOption struct {
	src   Source
	state *HostState
}

func (o *todoDeleteOption) AppliesTo() []string { return []string{"todo"} }
func (o *todoDeleteOption) Requires() []string  { return nil }
func (o *todoDeleteOption) Order() int          { return DeleteOptionOrder }

func (o *todoDeleteOption) Label(sel plugin.Selection) string {
	if sel.Title == "" {
		return "删除待办…"
	}
	return "删除「" + sel.Title + "」…"
}

func (o *todoDeleteOption) Activate(sel plugin.Selection, services svc.Services) (plugin.View, error) {
	return newConfirmDeleteView(o.src, o.state, sel, deleteTodo), nil
}

// todoRenameOption 是"重命名当前待办"联动选项。
type todoRenameOption struct {
	src   Source
	state *HostState
}

func (o *todoRenameOption) AppliesTo() []string { return []string{"todo"} }
func (o *todoRenameOption) Requires() []string  { return nil }
func (o *todoRenameOption) Order() int          { return 50 }

func (o *todoRenameOption) Label(sel plugin.Selection) string {
	if sel.Title == "" {
		return "重命名…"
	}
	return "重命名「" + sel.Title + "」…"
}

func (o *todoRenameOption) Activate(sel plugin.Selection, services svc.Services) (plugin.View, error) {
	return newRenameView(o.src, o.state, sel, renameTodo), nil
}

// goalAddOption 是"添加目标"看板选项。
//
// 它挂在**看板**而不是联动：目标列表可能为空，而"空列表上也得能加"
// 是基本要求（否则新用户进不来）。联动选项在"没有可选中项"时不出现。
type goalAddOption struct {
	src   Source
	state *HostState
}

func (o *goalAddOption) Label() string { return "添加目标…" }
func (o *goalAddOption) Order() int    { return 45 }

func (o *goalAddOption) Activate(svc.Services) (plugin.View, error) {
	return newAddGoalView(o.src, o.state), nil
}

// newAddGoalView 是"添加目标"输入界面。
func newAddGoalView(src Source, st *HostState) plugin.View {
	input := ""
	var status string
	return &plugin.ViewFunc{
		ViewName: "添加目标",
		HintFn: func(plugin.RenderCtx) []plugin.KeyHint {
			return []plugin.KeyHint{
				{Key: "enter", Desc: "添加"},
				{Key: "esc", Desc: "记完了"},
			}
		},
		RenderFn: func(ctx plugin.RenderCtx) {
			y := ctx.Rect.Y
			put := func(s string, style canvas.StyleID) {
				y = drawWrapped(ctx, y, ctx.Rect, s, style)
			}
			put("添加目标", tile.StyleTitle)
			put("", tile.StyleMuted)
			put("目标不随天重置，会一直留在右栏", tile.StyleMuted)
			put("", tile.StyleMuted)
			put("输入："+input+"▏", tile.StyleAccent)
			if status != "" {
				put("", tile.StyleMuted)
				put(status, tile.StyleStatus)
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			switch ev.Key {
			case "esc":
				return plugin.None(), true
			case "enter":
				title := model.Sanitize(input, false)
				if title == "" {
					return plugin.None(), true
				}
				g := model.NewGoal(title, ec.Now)
				goals := append(src.Goals(), *g)
				if setter, ok := src.(goalSetter); ok {
					setter.SetGoals(goals)
				}
				// 光标跟到新目标上。
				st.GoalCursor = len(goals) - 1
				input = ""
				status = "已添加：" + title
				return plugin.Persist(GoalPackID, "goals", nil), false
			case "backspace":
				input = trimLastRune(input)
				return plugin.None(), false
			}
			for _, r := range ev.Runes {
				if isSettingRune(r) {
					input += string(r)
				}
			}
			if len(ev.Runes) == 0 && len(ev.Key) == 1 {
				if r := rune(ev.Key[0]); isSettingRune(r) {
					input += ev.Key
				}
			}
			return plugin.None(), false
		},
	}
}

// goalDeleteOption 是"删除当前目标"联动选项。
type goalDeleteOption struct {
	src   Source
	state *HostState
}

func (o *goalDeleteOption) AppliesTo() []string { return []string{"goal"} }
func (o *goalDeleteOption) Requires() []string  { return nil }
func (o *goalDeleteOption) Order() int          { return DeleteOptionOrder }

func (o *goalDeleteOption) Label(sel plugin.Selection) string {
	if sel.Title == "" {
		return "删除目标…"
	}
	return "删除「" + sel.Title + "」…"
}

func (o *goalDeleteOption) Activate(sel plugin.Selection, services svc.Services) (plugin.View, error) {
	return newConfirmDeleteView(o.src, o.state, sel, deleteGoal), nil
}

// goalRenameOption 是"重命名当前目标"联动选项。
type goalRenameOption struct {
	src   Source
	state *HostState
}

func (o *goalRenameOption) AppliesTo() []string { return []string{"goal"} }
func (o *goalRenameOption) Requires() []string  { return nil }
func (o *goalRenameOption) Order() int          { return 50 }

func (o *goalRenameOption) Label(sel plugin.Selection) string {
	if sel.Title == "" {
		return "重命名…"
	}
	return "重命名「" + sel.Title + "」…"
}

func (o *goalRenameOption) Activate(sel plugin.Selection, services svc.Services) (plugin.View, error) {
	return newRenameView(o.src, o.state, sel, renameGoal), nil
}

// ---------- 删除：必须二次确认 ----------

// newConfirmDeleteView 是删除确认界面。
//
// **删除不可撤销**（我们没有回收站），因此必须二次确认——
// 而确认界面要把"删的是哪一条"写清楚：用户点错一项时的唯一补救
// 就是这一行字。
func newConfirmDeleteView(src Source, st *HostState, sel plugin.Selection, do func(Source, *HostState, plugin.Selection) error) plugin.View {
	var errText string
	return &plugin.ViewFunc{
		ViewName:    "确认删除",
		FocusLockFn: func() bool { return true }, // 破坏性操作：独占焦点
		HintFn: func(plugin.RenderCtx) []plugin.KeyHint {
			return []plugin.KeyHint{
				{Key: "y", Desc: "确认删除"},
				{Key: "esc", Desc: "取消"},
			}
		},
		RenderFn: func(ctx plugin.RenderCtx) {
			y := ctx.Rect.Y
			put := func(s string, style canvas.StyleID) {
				y = drawWrapped(ctx, y, ctx.Rect, s, style)
			}
			put("确认删除", tile.StyleError)
			put("", tile.StyleMuted)
			put("将要删除："+sel.Title, tile.StyleWarn)
			if sel.Kind == "todo" {
				put("（只删今天这一条；子任务与标签会一起删掉）", tile.StyleMuted)
			} else {
				put("（目标会从列表里移除；已归档的那份不受影响）", tile.StyleMuted)
			}
			put("", tile.StyleMuted)
			put("删除后无法撤销。按 y 确认，esc 取消。", tile.StyleMuted)
			if errText != "" {
				put("", tile.StyleMuted)
				put(errText, tile.StyleError)
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			switch ev.Key {
			case "esc", "n", "q":
				return plugin.None(), true
			case "y", "Y":
				if err := do(src, st, sel); err != nil {
					errText = err.Error()
					return plugin.None(), false
				}
				return plugin.Persist(TodoPackID, "day", nil), true
			}
			return plugin.None(), false
		},
	}
}

// deleteTodo 删除一条待办。
func deleteTodo(src Source, st *HostState, sel plugin.Selection) error {
	data := src.Day()
	if data == nil || sel.ID == "" {
		return fmt.Errorf("找不到要删除的条目")
	}
	removed := false
	strip := func(list []*model.Todo) []*model.Todo {
		out := list[:0]
		for _, t := range list {
			if t.ID == sel.ID {
				removed = true
				continue
			}
			out = append(out, t)
		}
		return out
	}
	data.Fixed = strip(data.Fixed)
	data.Floating = strip(data.Floating)
	if !removed {
		return fmt.Errorf("条目已经不在了（可能已被删除）")
	}
	// 光标可能指向被删掉的位置，夹一下。
	st.setFixedCursor(ClampCursor(st.fixedCursor(), len(data.Fixed)))
	st.setFloatingCursor(ClampCursor(st.floatingCursor(), len(data.Floating)))
	if st.SelectedTodo == sel.ID {
		st.selectTodo("")
	}
	return nil
}

// deleteGoal 删除一个活跃目标。
//
// 只从**活跃列表**里删；已归档的那一份不动——那是历史记录，
// 删了就等于篡改"那天我完成了什么"。
func deleteGoal(src Source, st *HostState, sel plugin.Selection) error {
	goals := src.Goals()
	kept := make([]model.Goal, 0, len(goals))
	removed := false
	for _, g := range goals {
		if g.ID == sel.ID {
			removed = true
			continue
		}
		kept = append(kept, g)
	}
	if !removed {
		return fmt.Errorf("目标已经不在了（可能已被删除）")
	}
	if setter, ok := src.(goalSetter); ok {
		setter.SetGoals(kept)
	}
	st.GoalCursor = ClampCursor(st.GoalCursor, len(kept))
	if st.SelectedGoal == sel.ID {
		st.SelectedGoal = ""
	}
	return nil
}

// ---------- 重命名 ----------

// newRenameView 是重命名输入界面。
func newRenameView(src Source, st *HostState, sel plugin.Selection, do func(Source, *HostState, plugin.Selection, string) error) plugin.View {
	input := ""
	var errText string
	return &plugin.ViewFunc{
		ViewName: "重命名",
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
			put("重命名", tile.StyleTitle)
			put("", tile.StyleMuted)
			put("原名："+sel.Title, tile.StyleMuted)
			put("", tile.StyleMuted)
			put("输入："+input+"▏", tile.StyleAccent)
			if errText != "" {
				put("", tile.StyleMuted)
				put(errText, tile.StyleError)
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			switch ev.Key {
			case "esc":
				return plugin.None(), true
			case "enter":
				title := model.Sanitize(input, false)
				if title == "" {
					errText = "名字不能为空"
					return plugin.None(), false
				}
				if err := do(src, st, sel, title); err != nil {
					errText = err.Error()
					return plugin.None(), false
				}
				return plugin.Persist(TodoPackID, "day", nil), true
			case "backspace":
				input = trimLastRune(input)
				errText = ""
				return plugin.None(), false
			}
			for _, r := range ev.Runes {
				if isSettingRune(r) {
					input += string(r)
				}
			}
			if len(ev.Runes) == 0 && len(ev.Key) == 1 {
				if r := rune(ev.Key[0]); isSettingRune(r) {
					input += ev.Key
				}
			}
			errText = ""
			return plugin.None(), false
		},
	}
}

// renameTodo 改一条待办的名字。
func renameTodo(src Source, st *HostState, sel plugin.Selection, title string) error {
	data := src.Day()
	if data == nil {
		return fmt.Errorf("没有当天数据")
	}
	t := data.Find(sel.ID)
	if t == nil {
		return fmt.Errorf("条目已经不在了")
	}
	t.Title = title
	return nil
}

// renameGoal 改一个目标的名字。
//
// 同时更新 Tag（标题的稳定指纹）：它是"继承时避免同名混淆"的依据，
// 名字改了却留着旧指纹，会让后续继承指向错的目标。
func renameGoal(src Source, st *HostState, sel plugin.Selection, title string) error {
	goals := src.Goals()
	found := false
	for i := range goals {
		if goals[i].ID == sel.ID {
			goals[i].Title = title
			goals[i].Tag = model.TagOf(title)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("目标已经不在了")
	}
	if setter, ok := src.(goalSetter); ok {
		setter.SetGoals(goals)
	}
	return nil
}
