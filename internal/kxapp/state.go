package kxapp

import (
	"strings"

	"github.com/kqin-dev/kxflow/plugin"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// HostState 是**各包共享**的界面状态。
//
// 为什么共享一份而不是每个包自己存：
//
//   - 光标与选中必须一致。用户在 TODO 列表里选中某一项，联动选项
//     （设 DDL、打标签）才有作用对象；如果每个磁贴各存一份光标，
//     "当前选中的是谁"就会出现多个答案——而 v2.1.0 的教训里
//     "同一个动作写在两处"正是这类问题的同族。
//
//   - 它刻意**不是**引擎的一部分。引擎只认识 Selection（谁被选中、
//     支持哪些操作）；"光标在第几行""计时是否在跑"是 KQFLOW 的业务概念。
//
// 所有包共享同一个 *HostState 指针，由 Loader 在装配时注入。
type HostState struct {
	// TodoCursor 是"固定待办"列表里的光标位置。
	TodoCursor int
	// TodoFloatCursor 是"临时待办"列表里的光标位置。
	//
	// 两个列表各有各的光标：共用一个会让"切到另一半再切回来"时位置乱跳。
	TodoFloatCursor int
	// GoalCursor 是 GOAL 列表里的光标位置。
	GoalCursor int
	// SelectedTodo / SelectedGoal 记录当前选中的条目 ID（空表示没选）。
	//
	// 存 ID 而不是指针：指针会随着切片扩容失效（v2.1.0 为此专门写过注释），
	// 而 ID 在任何时候都能重新查到当前对象。
	SelectedTodo string
	SelectedGoal string

	// Timer 是专注计时器（见 timer.go）。
	//
	// 它是**进程内状态**而不是落盘状态：计时中的会话不写盘，
	// 只有结束时才把结果写进当天记录。这样"程序崩了"最多丢一次计时，
	// 而不会在数据里留下一条永远没结束的会话。
	Timer Timer

	// FocusList 记录"光标现在停在哪个列表里"（model.KindFixed /
	// model.KindFloating / 目标用 "goal"）。
	//
	// 为什么需要它："添加待办"要知道加到**哪个列表**。Selection 里只有
	// 条目 ID，没有"它是固定还是临时"——而这件事只有列表磁贴自己知道。
	// 让磁贴在获得焦点/移动光标时记下来，是这里唯一不重复实现的办法
	//（另一条路是让引擎去问磁贴，但那会把"列表"这个概念塞进引擎）。
	FocusList string

	// SubtaskActive 为真表示当前处于**子任务模式**（光标在某个条目的
	// 子任务列表里，而不是在顶层列表里）。
	//
	// 与 2.1.0 的 taskActive 同一个概念，但对齐了 v3 的交互模型：
	// 它是一个**模式**（enter 进入、esc 退出），而不是一层借调界面——
	// 因为子任务列表就画在同一个磁贴里，用户视线不用移动。
	SubtaskActive bool
	// SubtaskOwner 是进入子任务模式时那个条目的 ID。
	//
	// 记 ID 而不是下标：下标会随"别处删了一条"而失效，
	// 于是光标会莫名其妙跳到另一个条目的子任务上。
	SubtaskOwner string
	// SubtaskCursor 是子任务模式下的光标。
	SubtaskCursor int
}

// NewHostState 创建初始状态。
func NewHostState() *HostState {
	st := &HostState{}
	st.Timer.init()
	return st
}

// fixedCursor / floatingCursor 是 TODO 包用的两个光标访问器。
//
// 为什么用方法而不是暴露字段：光标必须与"当前列表长度"一起被夹紧，
// 直接把字段开放出去，调用方迟早会写出越界的光标。
func (h *HostState) fixedCursor() int    { return h.TodoCursor }
func (h *HostState) floatingCursor() int { return h.TodoFloatCursor }

// setFixedCursor / setFloatingCursor 设置光标（内部使用）。
func (h *HostState) setFixedCursor(v int)    { h.TodoCursor = v }
func (h *HostState) setFloatingCursor(v int) { h.TodoFloatCursor = v }

// selectTodo 记录当前选中的待办 ID。
func (h *HostState) selectTodo(id string) { h.SelectedTodo = id }

// ListRefKind 是"选中对象是**列表本身**"时的 Kind。
//
// 为什么需要它（一个真实的死锁）：列表为空时没有条目可选，于是选中为空，
// 而引擎的 Applies 对空选中一律返回 false —— **连"添加"都不出现**，
// 用户因此永远加不进第一条。空列表恰恰是最需要"添加"的时候。
//
// 解法是让光标在列表上时上报一个"列表本身"的选中：
//
//	Kind: ListRefKind, ID: 列表标识（"fixed"/"floating"/"goal"）
//
// 于是"添加"这类**作用于列表**的选项可以声明 AppliesTo(ListRefKind)，
// 而"删除/重命名"这类**作用于条目**的选项只认真正的条目（Kind: "todo"），
// 不会在空列表上冒出来。
const ListRefKind = "list"

// 列表标识（用作列表选中的 ID，也是 HostState.FocusList 的取值）。
const (
	ListRefFixed    = "fixed"
	ListRefFloating = "floating"
	ListRefGoal     = "goal"
)

// listSelection 造一个"列表本身被选中"的上报。
//
// 它表示"当前上下文是这个列表"——添加类选项据此出现，
// 而要求"某一条目"的选项（删除/重命名）不会出现。
func listSelection(ref string) plugin.Selection {
	return plugin.Selection{Kind: ListRefKind, ID: ref}
}

// DisplayTitle 把一段文本清成"可以安全显示"的样子。
//
// 为什么渲染层也要做一遍（构造层已经 Sanitize 过了）：
// **已经写进磁盘的老数据没法回头改**。实测用户数据里就有
// `"title": "\u0000demo\u0000GOAL"`——NUL 在终端里完全不可见，
// 却会污染任何从渲染结果里取文本的代码（诊断、断言、日志对比）。
//
// 这里是**只清显示、不改数据**：用户的文件保持原样（我们不该在
// 未经允许的情况下改他的数据），但界面上不会出现不可见字符。
func DisplayTitle(s string) string {
	if !strings.ContainsFunc(s, isControlRune) {
		return s // 绝大多数情况走这里，零分配
	}
	return strings.Map(func(r rune) rune {
		if isControlRune(r) {
			return -1
		}
		return r
	}, s)
}

// isControlRune 报告一个字符是否属于"终端里不可见但会污染文本"的控制字符。
//
// 与 model.Sanitize 的口径一致：C0、DEL、C1 都算。
// 换行与制表不算（多行文本要用，由各自的渲染逻辑处理）。
//
// 注意这里判的是 **rune**：非法 UTF-8 字节在 range 里会变成 U+FFFD，
// 那种情况应当交给编码层处理（我们只负责把合法的控制符清掉）。
func isControlRune(r rune) bool {
	return (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// TodoList 返回今日固定或临时的待办列表。
//
// 参数 kind 用 model.KindFixed / model.KindFloating。
func TodoList(src Source, kind model.Kind) []*model.Todo {
	data := src.Day()
	if data == nil {
		return nil
	}
	if kind == model.KindFixed {
		return data.Fixed
	}
	return data.Floating
}

// GoalList 返回右栏要展示的目标：活跃的 + 当日归档的。
//
// 顺序与 v2.1.0 一致（先活跃、后归档），这样"今天完成了什么"在列表末尾，
// 不会把正在做的目标挤下去。
func GoalList(src Source) []*model.Goal {
	out := make([]*model.Goal, 0, 8)
	active := src.Goals()
	for i := range active {
		out = append(out, &active[i])
	}
	if data := src.Day(); data != nil {
		for i := range data.Archive.Goals {
			out = append(out, &data.Archive.Goals[i])
		}
	}
	return out
}

// ClampCursor 把光标夹到合法范围。
//
// 每次列表变化（勾选、删除、跨日）之后都要调：光标越界会让
// "当前选中"指向一个不存在的条目，而界面上看起来只是"高亮没了"。
func ClampCursor(cur, n int) int {
	if n <= 0 {
		return 0
	}
	if cur < 0 {
		return 0
	}
	if cur >= n {
		return n - 1
	}
	return cur
}

// MoveCursor 在长度为 n 的列表里移动光标（循环）。
func MoveCursor(cur, delta, n int) int {
	if n <= 0 {
		return 0
	}
	next := (cur + delta) % n
	if next < 0 {
		next += n
	}
	return next
}

// SelectedTodoItem 返回当前选中的待办（没选中或已消失则返回 nil）。
func (h *HostState) SelectedTodoItem(src Source) *model.Todo {
	if h == nil || h.SelectedTodo == "" {
		return nil
	}
	for _, kind := range []model.Kind{model.KindFixed, model.KindFloating} {
		for _, t := range TodoList(src, kind) {
			if t.ID == h.SelectedTodo {
				return t
			}
		}
	}
	return nil
}
