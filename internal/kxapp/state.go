package kxapp

import (
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
