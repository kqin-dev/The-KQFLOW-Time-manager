// Package model 定义 Kairos 的核心数据结构。
//
// 设计要点：
//   - TODO / GOAL / TASK 都是“条目”，用 ID 互相引用，重命名不会断链。
//   - GOAL 与日期无关，存放在 goals.json；TODO / TASK / 专注记录按日分库。
//   - 所有时间都以 UTC 纳秒存储，只有展示时才转成本地时间。
package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// SchemaVersion 是数据格式版本，用于将来的迁移。
const SchemaVersion = 1

// Kind 区分长期固定 TODO 与临时 TODO（见需求 13）。
type Kind string

const (
	// KindFixed 是左栏上半部分的长期固定 TODO。
	KindFixed Kind = "fixed"
	// KindFloating 是左栏下半部分较不固定的 TODO。
	KindFloating Kind = "floating"
)

// Status 是条目的状态。
type Status string

const (
	// StatusTodo 未开始。
	StatusTodo Status = "todo"
	// StatusDoing 正在进行。
	StatusDoing Status = "doing"
	// StatusDone 已完成。
	StatusDone Status = "done"
)

// Task 是 TODO 或 GOAL 的分解子项（见需求 8）。
type Task struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status Status `json:"status"`
	// DoneAt 仅在 Status == StatusDone 时有值。
	DoneAt *time.Time `json:"done_at,omitempty"`
}

// NewTask 创建一个新的子任务。
func NewTask(title string) Task {
	return Task{ID: NewID("task"), Title: strings.TrimSpace(title), Status: StatusTodo}
}

// Done 返回子任务是否已完成。
func (t Task) Done() bool { return t.Status == StatusDone }

// Todo 是某一日的待办事项。
type Todo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  Kind   `json:"kind"`
	// Status 是主状态；Done 与 DoneAt 保留一份冗余，便于统计与 UI 快速判断。
	Status Status     `json:"status"`
	Done   bool       `json:"done"`
	DoneAt *time.Time `json:"done_at,omitempty"`
	Tasks  []Task     `json:"tasks,omitempty"`
	// GoalRef 指向继承来源的 GOAL（见需求 15）；GoalTag 是它的稳定指纹。
	// 即使将来出现两个同名 GOAL，标签也不会混淆。
	GoalRef string `json:"goal_ref,omitempty"`
	GoalTag string `json:"goal_tag,omitempty"`
	// CreatedAt / OrigDay 记录条目诞生的时刻与所属逻辑日，便于历史回溯。
	CreatedAt time.Time `json:"created_at"`
	OrigDay   string    `json:"orig_day"`
	// CarriedFrom 记录该条目是从哪一日继承过来的（空表示当日新建）。
	CarriedFrom string `json:"carried_from,omitempty"`
	// Notes 保留给用户补充说明。
	Notes string `json:"notes,omitempty"`
}

// NewTodo 创建一个新的待办。
func NewTodo(title string, kind Kind, day string, now time.Time) *Todo {
	return &Todo{
		ID:        NewID("todo"),
		Title:     strings.TrimSpace(title),
		Kind:      kind,
		Status:    StatusTodo,
		CreatedAt: now,
		OrigDay:   day,
	}
}

// Progress 返回子任务完成数 m 与总数 n。
func (t *Todo) Progress() (done, total int) {
	for _, task := range t.Tasks {
		if task.Done() {
			done++
		}
	}
	return done, len(t.Tasks)
}

// Toggle 在完成与未完成之间切换，并同步状态、完成时刻与所有子任务。
func (t *Todo) Toggle(now time.Time) {
	if t.Done {
		t.Done = false
		t.DoneAt = nil
		t.Status = StatusTodo
		for i := range t.Tasks {
			t.Tasks[i].Status = StatusTodo
			t.Tasks[i].DoneAt = nil
		}
		return
	}
	done := now
	t.Done = true
	t.DoneAt = &done
	t.Status = StatusDone
	for i := range t.Tasks {
		if !t.Tasks[i].Done() {
			at := now
			t.Tasks[i].DoneAt = &at
		}
		t.Tasks[i].Status = StatusDone
	}
}

// SyncFromTasks 在子任务被逐条勾选后，回写父条目的完成状态。
func (t *Todo) SyncFromTasks(now time.Time) {
	all, any := true, false
	for _, task := range t.Tasks {
		if task.Done() {
			any = true
		} else {
			all = false
		}
	}
	switch {
	case len(t.Tasks) > 0 && all:
		if !t.Done {
			at := now
			t.DoneAt = &at
		}
		t.Done, t.Status = true, StatusDone
	case t.Done:
		// 存在未完成子项时，父条目回到未完成。
		t.Done, t.DoneAt, t.Status = false, nil, StatusTodo
	default:
		if any {
			t.Status = StatusDoing
		} else {
			t.Status = StatusTodo
		}
	}
}

// Goal 是与日期无关的长期目标。
type Goal struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Tag 是标题的稳定指纹，用于继承时打标签，避免同名目标混淆。
	Tag     string     `json:"tag"`
	Status  Status     `json:"status"`
	Done    bool       `json:"done"`
	DoneAt  *time.Time `json:"done_at,omitempty"`
	Tasks   []Task     `json:"tasks,omitempty"`
	Created time.Time  `json:"created_at"`
	// ArchivedDay 在目标被勾选后由日界线逻辑填入，表示归档到哪一天（见需求 10）。
	ArchivedDay string `json:"archived_day,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

// NewGoal 创建一个新目标。
func NewGoal(title string, now time.Time) *Goal {
	title = strings.TrimSpace(title)
	return &Goal{
		ID:      NewID("goal"),
		Title:   title,
		Tag:     TagOf(title),
		Status:  StatusTodo,
		Created: now,
	}
}

// Progress 返回子任务完成数 m 与总数 n。
func (g *Goal) Progress() (done, total int) {
	for _, task := range g.Tasks {
		if task.Done() {
			done++
		}
	}
	return done, len(g.Tasks)
}

// Toggle 切换目标完成状态；归档日由调用方按当前逻辑日填写。
func (g *Goal) Toggle(now time.Time, day string) {
	if g.Done {
		g.Done, g.DoneAt, g.Status, g.ArchivedDay = false, nil, StatusTodo, ""
		for i := range g.Tasks {
			g.Tasks[i].Status = StatusTodo
			g.Tasks[i].DoneAt = nil
		}
		return
	}
	at := now
	g.Done, g.DoneAt, g.Status = true, &at, StatusDone
	g.ArchivedDay = day
}

// GoalBook 是全部目标的集合，存放在与日期无关的 goals.json。
type GoalBook struct {
	SchemaVersion int    `json:"schema_version"`
	Goals         []Goal `json:"goals"`
}

// TimerKind 是计时模式（见需求 16）。
type TimerKind string

const (
	// TimerCountUp 正计时，不设终点。
	TimerCountUp TimerKind = "countup"
	// TimerCountDown 倒计时。
	TimerCountDown TimerKind = "countdown"
	// TimerPomodoro 番茄钟，专注与休息交替。
	TimerPomodoro TimerKind = "pomodoro"
	// TimerCustom 自定义时段，可自定义状态段名称。
	TimerCustom TimerKind = "custom"
)

// Segment 是计时时段中的一段，含名称与时长。
type Segment struct {
	Name string        `json:"name"`
	Kind string        `json:"kind,omitempty"` // focus / break
	Dur  time.Duration `json:"dur"`
}

// Plan 是一次计时任务的完整描述。
type Plan struct {
	Kind     TimerKind `json:"kind"`
	Segments []Segment `json:"segments"`
	// Cycle 表示番茄钟的轮数，仅用于展示。
	Cycle int `json:"cycle,omitempty"`
}

// Total 返回全时段总时长；正计时返回 0，表示没有终点。
func (p Plan) Total() time.Duration {
	var sum time.Duration
	for _, s := range p.Segments {
		sum += s.Dur
	}
	return sum
}

// SegmentAt 返回某个已流逝时长落在哪一段，以及段内已过去的时长。
func (p Plan) SegmentAt(elapsed time.Duration) (idx int, seg Segment, within time.Duration) {
	acc := time.Duration(0)
	for i, s := range p.Segments {
		if elapsed < acc+s.Dur {
			return i, s, elapsed - acc
		}
		acc += s.Dur
	}
	if len(p.Segments) == 0 {
		return 0, Segment{}, elapsed
	}
	last := len(p.Segments) - 1
	return last, p.Segments[last], p.Segments[last].Dur
}

// Session 是一次真实的计时记录，中断或结束时都会落盘（见需求 17、21）。
type Session struct {
	ID       string        `json:"id"`
	Plan     Plan          `json:"plan"`
	TodoRef  string        `json:"todo_ref,omitempty"`
	TodoName string        `json:"todo_name,omitempty"`
	Started  time.Time     `json:"started"`
	Ended    *time.Time    `json:"ended,omitempty"`
	Elapsed  time.Duration `json:"elapsed"`
	// Completed 表示这段计时是否完整走完，未走完即为中断。
	Completed bool `json:"completed"`
	// SegmentName 记录结束时所在的时段名，便于统计专注与休息。
	SegmentName string `json:"segment_name,omitempty"`
	SegmentKind string `json:"segment_kind,omitempty"`
}

// FocusRecord 是归档后的专注时长，按日统计今日专注小时数（见需求 18）。
type FocusRecord struct {
	TodoRef  string        `json:"todo_ref,omitempty"`
	TodoName string        `json:"todo_name,omitempty"`
	Dur      time.Duration `json:"dur"`
	Kind     string        `json:"kind,omitempty"` // focus / break
	At       time.Time     `json:"at"`
}

// Activity 记录用户对某条目投入过的累计时间。
type Activity struct {
	Name     string        `json:"name"`
	First    time.Time     `json:"first"`
	Last     time.Time     `json:"last"`
	Total    time.Duration `json:"total"`
	Sessions int           `json:"sessions"`
}

// Archive 保存某一逻辑日的完整数据。
//
// Sessions 是当日的计时记录，也是专注时长的唯一来源，避免多处各存一份导致重复统计。
type Archive struct {
	Day      string    `json:"day"`
	Goals    []Goal    `json:"goals,omitempty"` // 本日完成的 GOAL 归档（见需求 10）
	Sessions []Session `json:"sessions,omitempty"`
}

// DayData 是一日的数据库文件内容。
type DayData struct {
	SchemaVersion int                  `json:"schema_version"`
	Day           string               `json:"day"`
	Fixed         []*Todo              `json:"fixed"`
	Floating      []*Todo              `json:"floating"`
	Archive       Archive              `json:"archive"`
	Activity      map[string]*Activity `json:"activity,omitempty"`
	// CarryAsked 记录当天是否已经问过“是否继承昨日”，避免反复打扰。
	CarryAsked bool      `json:"carry_asked"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// NewDayData 创建一个空的当日数据库。
func NewDayData(day string, now time.Time) *DayData {
	return &DayData{
		SchemaVersion: SchemaVersion,
		Day:           day,
		Archive:       Archive{Day: day},
		Activity:      map[string]*Activity{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// All 返回固定与临时两栏中的全部待办，顺序为先固定后临时。
func (d *DayData) All() []*Todo {
	out := make([]*Todo, 0, len(d.Fixed)+len(d.Floating))
	out = append(out, d.Fixed...)
	out = append(out, d.Floating...)
	return out
}

// Counts 返回左边栏的 (已完成, 总数)。
func (d *DayData) Counts() (done, total int) {
	for _, t := range d.All() {
		if t.Done {
			done++
		}
	}
	return done, len(d.Fixed) + len(d.Floating)
}

// Find 按 ID 查找待办。
func (d *DayData) Find(id string) *Todo {
	for _, t := range d.All() {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// FocusTotal 返回当日的专注与休息总时长。
//
// 只统计已经结束的计时：仍在进行中的那一段由看板实时显示，不计入当日总计，
// 否则进行中的计时会随时间不断重复累加。
func (d *DayData) FocusTotal() (focus, rest time.Duration) {
	for _, r := range d.Archive.Sessions {
		if r.Ended == nil {
			continue
		}
		if r.SegmentKind == "break" {
			rest += r.Elapsed
		} else {
			focus += r.Elapsed
		}
	}
	return focus, rest
}

// NewID 生成带前缀的短唯一 ID，形如 todo_3f2a91c4d0b7。
func NewID(prefix string) string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

// TagOf 计算标题的稳定指纹，用于继承 GOAL 时打标签（见需求 15）。
func TagOf(title string) string {
	h := fnv1a(strings.ToLower(strings.TrimSpace(title)))
	return fmt.Sprintf("#%06x", h&0xFFFFFF)
}

func fnv1a(s string) uint32 {
	const (
		offset = 2166136261
		prime  = 16777619
	)
	h := uint32(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	return h
}
