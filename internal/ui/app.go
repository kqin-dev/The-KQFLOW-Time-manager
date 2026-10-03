package ui

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/clock"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/config"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/model"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/store"
)

// View 是当前显示的页面。
type View int

const (
	// ViewDashboard 是主看板。
	ViewDashboard View = iota
	// ViewHistory 是历史查看。
	ViewHistory
	// ViewSettings 是设置。
	ViewSettings
	// ViewHelp 是帮助。
	ViewHelp
	// ViewCarry 是“继承昨日”的确认页。
	ViewCarry
)

// Focus 标记当前获得键盘焦点的区域。
type Focus int

const (
	// FocusMenu 中间栏的选项列表。
	FocusMenu Focus = iota
	// FocusFixed 左栏上半部分的固定 TODO。
	FocusFixed
	// FocusFloating 左栏下半部分的临时 TODO。
	FocusFloating
	// FocusGoals 右栏的 GOAL 列表。
	FocusGoals
)

// App 是 Kairos 的根模型。
type App struct {
	store *store.Store
	cfg   *config.Config
	clock *clock.Clock
	st    *Styles

	width  int
	height int

	view  View
	focus Focus

	day     string // 当前逻辑日
	prevDay string
	data    *model.DayData
	goals   []model.Goal

	cursors struct {
		fixed    int
		floating int
		goals    int
		menu     int
	}
	// taskActive 为真时，j/k 在选中条目的子任务里移动；taskCursor 是子任务下标。
	taskActive bool
	taskCursor int
	// collapsed 记录被用户折叠的条目 ID，折叠后不再展开显示子任务。
	collapsed map[string]bool
	// subSelection 记录在“子任务模式”下用户是否想勾选子任务本身。
	subSelection bool
	scroll       struct{ fixed, floating, goals int }

	editor editorState

	// picks 是等待用户选择的模态框状态。
	pick *pickState

	// pendingPlan 保存已经选好、等待选择归属 TODO 的计时方案。
	pendingPlan *model.Plan

	// paths 是配置文件与数据目录的定位信息。
	paths *config.Paths

	timer *timerState

	celebrate *celebrateState

	// custom 是“自定义时段”编辑器状态（见需求 20）。
	custom *customState

	quoteIdx int
	// quoteAt 记录当前字条是何时换上的，用于定时轮换（见需求 5）。
	quoteAt   time.Time
	animPhase float64
	frame     int

	// pageScroll 是帮助/设置/历史这类二级页的滚动偏移。
	pageScroll int
	// settingsCursor 是设置页当前选中的项。
	settingsCursor int

	toast     string
	toastKind toastKind

	err error

	// quitting 标记用户已请求退出。
	quitting bool
}

type toastKind int

const (
	toastInfo toastKind = iota
	toastWarn
	toastErr
)

// Options 是创建 App 所需的依赖。
type Options struct {
	Store  *store.Store
	Config *config.Config
	Clock  *clock.Clock
	Styles *Styles
	Paths  *config.Paths
}

// NewApp 创建根模型并载入当日数据。
func NewApp(opts Options) (*App, error) {
	st := opts.Styles
	if st == nil {
		st = NewStyles(DefaultTheme)
	}
	clk := opts.Clock
	if clk == nil {
		clk = clock.New()
	}
	a := &App{
		store:     opts.Store,
		cfg:       opts.Config,
		clock:     clk,
		st:        st,
		paths:     opts.Paths,
		view:      ViewDashboard,
		focus:     FocusMenu,
		collapsed: map[string]bool{},
	}
	a.quoteIdx = rand.Intn(len(Quotes))
	a.quoteAt = clk.Now()
	if err := a.reload(); err != nil {
		return nil, err
	}
	return a, nil
}

// SetSize 直接设定窗口尺寸，便于测试与离屏渲染。
func (a *App) SetSize(width, height int) error {
	a.width, a.height = width, height
	return nil
}

// reload 依据系统时间重算逻辑日并载入数据。
//
// 需求 7：当系统时间跨过日界线后，TODAY TODO 会自动切换到新的一天。
func (a *App) reload() error {
	if a.cfg == nil {
		a.cfg = config.Default()
	}
	cut := a.cfg.Cutoff()
	loc := a.cfg.Location()
	now := a.clock.Now()

	day := clock.LogicalDay(now, cut)
	prev, err := clock.PrevDay(day, loc)
	if err != nil {
		prev = day
	}

	data, err := a.store.EnsureDay(day, now)
	if err != nil {
		return err
	}
	goals, err := a.store.Goals()
	if err != nil {
		return err
	}

	// 固定 TODO 每天自动补齐（见需求 14）；临时 TODO 需要用户确认是否继承。
	if !data.CarryAsked && day != prev {
		if _, err := a.store.CarryFixed(prev, day, now); err != nil {
			return err
		}
		if data, err = a.store.Day(day); err != nil {
			return err
		}
		if a.hasInheritableFloating(prev) {
			a.view = ViewCarry
		}
	}

	a.day, a.prevDay, a.data, a.goals = day, prev, data, goals
	// 自愈：早期版本删条目时不会清理悬空引用，这里读入后顺手修一次，
	// 让老数据在下一轮迭代后自动变干净。
	if a.data.PruneOrphans() {
		_ = a.saveDay()
	}
	a.clampCursors()
	return nil
}

// hasInheritableFloating 判断昨日是否存在未完成的临时 TODO。
func (a *App) hasInheritableFloating(prev string) bool {
	prevData, err := a.store.Day(prev)
	if err != nil || prevData == nil {
		return false
	}
	for _, t := range prevData.Floating {
		if !t.Done {
			return true
		}
	}
	return false
}

// Init 启动动画与刷新定时器。
func (a *App) Init() tea.Cmd {
	return tea.Batch(tickCmd(), animCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func animCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return animMsg{} })
}

type tickMsg struct{}
type animMsg struct{}
type savedMsg struct{ err error }

// Update 处理全部消息。
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		return a, nil

	case tickMsg:
		a.frame++
		// 每秒检查是否跨日；跨日则自动切换 TODAY TODO。
		if cur := clock.LogicalDay(a.clock.Now(), a.cfg.Cutoff()); cur != a.day {
			if err := a.reload(); err == nil {
				a.toast = fmt.Sprintf("已进入新的一天 %s", a.day)
				a.toastKind = toastInfo
			}
		}
		if a.celebrate != nil && time.Since(a.celebrate.started) > celebrateFor {
			a.celebrate = nil
		}
		return a, tickCmd()

	case animMsg:
		a.animPhase += 0.012
		if a.animPhase > 1 {
			a.animPhase -= 1
		}
		// 字条定时轮换，让它真的“滚动”起来（见需求 5）。
		if now := a.clock.Now(); now.Sub(a.quoteAt) >= quoteEvery {
			a.NextQuote()
			a.quoteAt = now
		}
		// 计时状态在动画帧里推进，保证进度条平滑动起来。
		if a.timer != nil {
			if cmd := a.timer.tick(a.clock.Now()); cmd != nil {
				return a, tea.Batch(animCmd(), cmd)
			}
		}
		return a, animCmd()

	case savedMsg:
		if m.err != nil {
			a.err = m.err
			a.toast = "保存失败：" + m.err.Error()
			a.toastKind = toastErr
		}
		return a, nil

	case timerDoneMsg:
		if a.timer != nil {
			a.timer.finished = true
			a.setToast("计时时段已完成，按 enter 结束并归档", toastInfo)
		}
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(m)
	}
	return a, nil
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// 庆祝动画播放时，任意按键优先用来中断动画，避免误触到看板上的操作（见需求 10）。
	if a.celebrate != nil && key != "ctrl+c" {
		a.celebrate = nil
		return a, nil
	}

	// 模态框优先处理按键。
	if a.pick != nil {
		return a.handlePickKey(key)
	}
	if a.editor.active {
		return a.handleEditorKey(msg)
	}
	// 自定义时段编辑器独占按键。
	if a.custom != nil {
		return a.handleCustomKey(key)
	}
	// 计时进行中，用少量按键控制计时器。
	if a.timer != nil {
		switch key {
		case " ":
			a.timer.pause(a.clock.Now())
			return a, nil
		case "esc":
			a.stopTimer(true)
			return a, saveCmd(a.saveDay)
		case "enter":
			a.stopTimer(a.timer.finished)
			return a, saveCmd(a.saveDay)
		case "ctrl+c":
			a.stopTimer(true)
			a.quitting = true
			return a, tea.Quit
		}
	}
	if a.view == ViewSettings {
		return a.handleSettingsKey(key)
	}
	if a.view == ViewHistory {
		return a.handleHistoryKey(key)
	}
	if a.view == ViewCarry {
		return a.handleCarryKey(key)
	}
	if a.view == ViewHelp {
		return a.handleHelpKey(key)
	}

	switch key {
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	case "q", "Q":
		// 退出前先确认，避免误触 q 直接丢失查看状态（见需求 8）。
		a.askQuit()
	case "?":
		a.view = ViewHelp
		a.pageScroll = 0
	case "N":
		// 大写 N 打开随手记，和 n 区分开（n 未占用，但保留给未来的新建动作）。
		a.openNote()
	// 栏位切换只保留 TAB，把 h/l 与左右方向键让给栏内操作（见问题 3）。
	case "tab":
		a.switchFocus(1)
	case "shift+tab":
		a.switchFocus(-1)
	case "j", "down":
		a.moveCursor(1)
	case "k", "up":
		a.moveCursor(-1)
	case "g", "home":
		a.jumpCursor(true)
	case "G", "end":
		a.jumpCursor(false)
	case " ":
		// 子任务模式下空格勾选子任务，否则勾选整条 TODO。
		if a.taskActive {
			return a.toggleSelectedTask()
		}
		return a.toggleCurrent()
	case "a":
		a.startAdd()
	case "A":
		a.startAddGoal()
	case "e":
		a.startEdit()
	case "d":
		a.startDelete()
	case "t":
		a.startAddTask()
	case "r":
		return a.askCarry()
	case "1":
		a.focus, a.cursors.menu = FocusFixed, 0
		return a.activateMenuItem(0)
	case "2":
		a.focus, a.cursors.menu = FocusFloating, 0
		return a.activateMenuItem(1)
	case "3":
		a.focus, a.cursors.menu = FocusGoals, 0
		return a.activateMenuItem(2)
	case "enter":
		// 计时进行中时，enter 用于结束并归档计时。
		if a.timer != nil {
			a.stopTimer(a.timer.finished)
			return a, saveCmd(a.saveDay)
		}
		if a.focus == FocusMenu {
			return a.activateMenuItem(a.cursors.menu)
		}
		if a.focus == FocusGoals {
			return a.handleGoalAction("toggle")
		}
		// TODO 栏：enter 进入子任务选择，已进入时用于勾选子任务。
		if a.taskActive {
			return a.toggleSelectedTask()
		}
		a.enterSubTasks()
	case "esc":
		// 处于子任务模式时先退回父条目，而不是弹退出确认。
		if a.taskActive {
			a.taskActive = false
			return a, nil
		}
		a.askQuit()
	case "L":
		// 大写 L 用于勾选父条目，避免和子任务勾选混淆。
		return a.toggleCurrent()
	}
	return a, nil
}

// ---------- 光标移动 ----------

func (a *App) currentLen() int {
	switch a.focus {
	case FocusFixed:
		return len(a.data.Fixed)
	case FocusFloating:
		return len(a.data.Floating)
	case FocusGoals:
		return len(a.goalList())
	default:
		return len(menuItems)
	}
}

func (a *App) currentCursor() int {
	switch a.focus {
	case FocusFixed:
		return a.cursors.fixed
	case FocusFloating:
		return a.cursors.floating
	case FocusGoals:
		return a.cursors.goals
	default:
		return a.cursors.menu
	}
}

func (a *App) setCursor(v int) {
	switch a.focus {
	case FocusFixed:
		a.cursors.fixed = v
	case FocusFloating:
		a.cursors.floating = v
	case FocusGoals:
		a.cursors.goals = v
	default:
		a.cursors.menu = v
	}
}

func (a *App) moveCursor(delta int) {
	// 处于子任务模式时，j/k 先在子任务里移动。
	if a.taskActive {
		if t := a.currentTodo(); t != nil && len(t.Tasks) > 0 {
			n := len(t.Tasks)
			a.taskCursor = (a.taskCursor + delta + n) % n
			return
		}
		a.taskActive = false
	}
	n := a.currentLen()
	if n == 0 {
		return
	}
	cur := a.currentCursor() + delta
	if cur < 0 {
		cur = n - 1
	}
	if cur >= n {
		cur = 0
	}
	a.setCursor(cur)
}

// switchFocus 切换栏位，并重置子任务选择状态。
func (a *App) switchFocus(delta int) {
	a.focus = Focus((int(a.focus) + delta + 4) % 4)
	a.taskActive = false
	a.taskCursor = 0
}

// enterSubTasks 进入子任务选择模式。
func (a *App) enterSubTasks() {
	t := a.currentTodo()
	if t == nil || len(t.Tasks) == 0 {
		a.setToast("该项还没有子任务，按 t 添加", toastWarn)
		return
	}
	a.taskActive = true
	a.taskCursor = clamp(a.taskCursor, 0, len(t.Tasks)-1)
}

// toggleSelectedTask 勾选当前选中的子任务，并回写父条目状态。
func (a *App) toggleSelectedTask() (tea.Model, tea.Cmd) {
	t := a.currentTodo()
	if t == nil || a.taskCursor >= len(t.Tasks) {
		return a, nil
	}
	now := a.clock.Now()
	task := &t.Tasks[a.taskCursor]
	if task.Done() {
		task.Status = model.StatusTodo
		task.DoneAt = nil
	} else {
		at := now
		task.Status = model.StatusDone
		task.DoneAt = &at
	}
	t.SyncFromTasks(now)
	a.afterTodoToggle()
	return a, saveCmd(a.saveDay)
}

func (a *App) jumpCursor(top bool) {
	n := a.currentLen()
	if n == 0 {
		return
	}
	if top {
		a.setCursor(0)
		return
	}
	a.setCursor(n - 1)
}

func (a *App) clampCursors() {
	if a.cursors.menu >= len(menuItems) {
		a.cursors.menu = 0
	}
	a.cursors.goals = clamp(a.cursors.goals, 0, max(0, len(a.goalList())-1))
	a.cursors.fixed = clamp(a.cursors.fixed, 0, max(0, len(a.data.Fixed)-1))
	a.cursors.floating = clamp(a.cursors.floating, 0, max(0, len(a.data.Floating)-1))
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---------- 勾选 ----------

func (a *App) toggleCurrent() (tea.Model, tea.Cmd) {
	now := a.clock.Now()
	switch a.focus {
	case FocusFixed:
		if t := a.currentTodo(); t != nil {
			t.Toggle(now)
			a.afterTodoToggle()
		}
	case FocusFloating:
		if t := a.currentTodo(); t != nil {
			t.Toggle(now)
			a.afterTodoToggle()
		}
	case FocusGoals:
		return a.handleGoalAction("toggle")
	}
	return a, saveCmd(a.saveDay)
}

// handleGoalAction 统一处理 GOAL 栏的完成与取消（见需求 10）。
func (a *App) handleGoalAction(action string) (tea.Model, tea.Cmd) {
	if action != "toggle" {
		return a, nil
	}
	return a.toggleGoal(a.clock.Now())
}

// toggleGoal 在 goals.json 与当日归档之间移动目标（见需求 10）。
//
// 完成的 GOAL 会从与日期无关的 goals.json 移到当天的归档里；
// 取消完成则把它从当天归档取回 goals.json，因此不会出现“取消或删除后仍留在归档”
// 的状态残留。
func (a *App) toggleGoal(now time.Time) (tea.Model, tea.Cmd) {
	entry, ok := a.selectedGoalEntry()
	if !ok {
		return a, nil
	}
	if entry.Archived {
		// 取消完成：从当日归档取回 goals.json。
		restored := *entry.Goal
		restored.Done = false
		restored.DoneAt = nil
		restored.Status = model.StatusTodo
		restored.ArchivedDay = ""
		for i := range restored.Tasks {
			restored.Tasks[i].Status = model.StatusTodo
			restored.Tasks[i].DoneAt = nil
		}
		a.removeArchivedGoal(restored.ID)
		a.goals = append(a.goals, restored)
		a.setToast(fmt.Sprintf("已取消完成，「%s」回到进行中的目标", restored.Title), toastInfo)
	} else {
		// 完成：移入当日归档。
		done := *entry.Goal
		done.Toggle(now, a.day)
		a.removeActiveGoal(done.ID)
		a.archiveGoal(done)
		a.setToast(fmt.Sprintf("「%s」已完成并归档到 %s", done.Title, a.day), toastInfo)
	}
	a.persistGoals()
	a.clampCursors()
	return a, saveCmd(a.saveDay)
}

// goalEntry 是右栏的一个目标条目，用索引回指真实存储位置。
//
// 不能用指向切片元素的指针：往切片里 append 会让旧指针失效，
// 之后对它的修改就写不回真正的数据了。
type goalEntry struct {
	Goal *model.Goal
	// Archived 为真表示它存在当日归档里，否则在 goals.json 里。
	Archived bool
	// Index 是在对应切片中的下标。
	Index int
}

// goalEntries 返回右栏要展示的目标：goals.json 中未归档的目标 + 当日已归档的目标。
func (a *App) goalEntries() []goalEntry {
	out := make([]goalEntry, 0, len(a.goals)+len(a.data.Archive.Goals))
	for i := range a.goals {
		out = append(out, goalEntry{Goal: &a.goals[i], Index: i})
	}
	for i := range a.data.Archive.Goals {
		out = append(out, goalEntry{Goal: &a.data.Archive.Goals[i], Archived: true, Index: i})
	}
	return out
}

// goalList 返回右栏展示的目标指针，仅用于渲染。
func (a *App) goalList() []*model.Goal {
	entries := a.goalEntries()
	out := make([]*model.Goal, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Goal)
	}
	return out
}

// selectedGoalEntry 返回当前选中的目标条目。
func (a *App) selectedGoalEntry() (goalEntry, bool) {
	entries := a.goalEntries()
	if a.cursors.goals < 0 || a.cursors.goals >= len(entries) {
		return goalEntry{}, false
	}
	return entries[a.cursors.goals], true
}

// selectedGoal 返回当前选中的目标指针。
func (a *App) selectedGoal() *model.Goal {
	e, ok := a.selectedGoalEntry()
	if !ok {
		return nil
	}
	return e.Goal
}

// removeActiveGoal 从 goals.json 中移除指定目标。
func (a *App) removeActiveGoal(id string) {
	filtered := a.goals[:0]
	for _, g := range a.goals {
		if g.ID != id {
			filtered = append(filtered, g)
		}
	}
	a.goals = filtered
}

// removeArchivedGoal 从当日归档中移除指定目标。
func (a *App) removeArchivedGoal(id string) {
	filtered := a.data.Archive.Goals[:0]
	for _, g := range a.data.Archive.Goals {
		if g.ID != id {
			filtered = append(filtered, g)
		}
	}
	a.data.Archive.Goals = filtered
}

func (a *App) afterTodoToggle() {
	done, total := a.data.Counts()
	if total > 0 && done == total {
		a.celebrate = &celebrateState{started: time.Now()}
	}
}

func (a *App) archiveGoal(g model.Goal) {
	// 已归档的目标替换同 ID 的旧记录，避免重复。
	filtered := a.data.Archive.Goals[:0]
	for _, existing := range a.data.Archive.Goals {
		if existing.ID != g.ID {
			filtered = append(filtered, existing)
		}
	}
	a.data.Archive.Goals = append(filtered, g)
}

func (a *App) saveDay() error {
	return a.store.SaveDay(a.data)
}

func (a *App) persistGoals() {
	goals := a.goals
	if err := a.store.SaveGoals(goals); err != nil {
		a.err = err
		a.toast = "保存 GOAL 失败：" + err.Error()
		a.toastKind = toastErr
	}
}

func saveCmd(fn func() error) tea.Cmd {
	return func() tea.Msg { return savedMsg{err: fn()} }
}

// ---------- 继承昨日 ----------

func (a *App) askCarry() (tea.Model, tea.Cmd) {
	if a.prevDay == "" || a.prevDay == a.day {
		a.setToast("没有可继承的昨日数据", toastWarn)
		return a, nil
	}
	prev, err := a.store.Day(a.prevDay)
	if err != nil || prev == nil {
		a.setToast("昨日没有数据", toastWarn)
		return a, nil
	}
	unfinished := 0
	for _, t := range prev.Floating {
		if !t.Done {
			unfinished++
		}
	}
	a.pick = &pickState{
		title: fmt.Sprintf("从 %s 继承", a.prevDay),
		items: []pickItem{
			{Label: fmt.Sprintf("拉取昨日固定 TODO（%d 项）", len(prev.Fixed)), Action: "fixed"},
			{Label: fmt.Sprintf("继承昨日未完成的 TODO（%d 项）", unfinished), Action: "floating"},
			{Label: "两者都继承", Action: "both"},
			{Label: "都不继承", Action: "none"},
		},
	}
	return a, nil
}

func (a *App) handleCarryKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "enter", "1":
		return a.doCarry("both")
	case "f", "2":
		return a.doCarry("fixed")
	case "x", "3":
		return a.doCarry("floating")
	case "n", "esc", "4":
		return a.skipCarry()
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	}
	return a, nil
}

func (a *App) doCarry(mode string) (tea.Model, tea.Cmd) {
	now := a.clock.Now()
	var added int
	var err error
	if mode == "fixed" || mode == "both" {
		n, e := a.store.CarryFixed(a.prevDay, a.day, now)
		if e != nil {
			err = e
		}
		added += n
	}
	if mode == "floating" || mode == "both" {
		n, e := a.store.CarryFloating(a.prevDay, a.day, now)
		if e != nil {
			err = e
		}
		added += n
	}
	if err != nil {
		a.setToast("继承失败："+err.Error(), toastErr)
	} else {
		a.setToast(fmt.Sprintf("已从 %s 继承 %d 项", a.prevDay, added), toastInfo)
	}
	if data, e := a.store.Day(a.day); e == nil {
		a.data = data
	}
	a.view = ViewDashboard
	a.clampCursors()
	return a, saveCmd(a.saveDay)
}

func (a *App) skipCarry() (tea.Model, tea.Cmd) {
	a.data.CarryAsked = true
	a.view = ViewDashboard
	a.setToast("已跳过继承，之后可以按 r 重新拉取", toastInfo)
	return a, saveCmd(a.saveDay)
}

// ---------- 模态框 ----------

func (a *App) handlePickKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "j", "down":
		a.pick.cursor = (a.pick.cursor + 1) % len(a.pick.items)
	case "k", "up":
		a.pick.cursor = (a.pick.cursor - 1 + len(a.pick.items)) % len(a.pick.items)
	case "enter":
		action := a.pick.items[a.pick.cursor].Action
		a.pick = nil
		return a.runAction(action)
	case "esc", "q":
		a.pick = nil
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	case "y":
		if a.pick.small {
			break
		}
		action := a.pick.items[0].Action
		a.pick = nil
		return a.runAction(action)
	case "n":
		if a.pick.small {
			break
		}
		action := a.pick.items[len(a.pick.items)-1].Action
		a.pick = nil
		return a.runAction(action)
	}
	return a, nil
}

func (a *App) runAction(action string) (tea.Model, tea.Cmd) {
	switch {
	case action == "fixed":
		return a.doCarry("fixed")
	case action == "floating":
		return a.doCarry("floating")
	case action == "both":
		return a.doCarry("both")
	case action == "none":
		return a.skipCarry()
	case action == "cancel":
		a.pendingPlan = nil
		a.setToast("已取消", toastInfo)
		return a, nil
	case action == "quit":
		// 计时中退出要把已用时长归档，否则这段时间白记了（见需求 21）。
		if a.timer != nil {
			a.stopTimer(a.timer.finished)
			if err := a.saveDay(); err != nil {
				a.quitting = true
				return a, tea.Quit
			}
		}
		a.quitting = true
		return a, tea.Quit
	case action == "quit_save":
		// 先把当前编辑的内容交回给它的保存逻辑，再退出。
		a.commitEditorValue()
		if a.timer != nil {
			a.stopTimer(a.timer.finished)
			if err := a.saveDay(); err != nil {
				a.quitting = true
				return a, tea.Quit
			}
		}
		a.quitting = true
		return a, tea.Quit
	case action == "quit_discard":
		// 明确丢弃随手记的改动，但计时记录仍要归档——那是真实发生过的。
		a.closeEditor()
		if a.timer != nil {
			a.stopTimer(a.timer.finished)
			if err := a.saveDay(); err != nil {
				a.quitting = true
				return a, tea.Quit
			}
		}
		a.quitting = true
		return a, tea.Quit
	case action == "close_save":
		// 保存并回到看板（不退出程序）。
		a.commitEditorValue()
		return a, nil
	case action == "close_discard":
		// 明确不保存，直接关闭编辑器。
		a.closeEditor()
		a.setToast("已放弃随手记的改动", toastInfo)
		return a, nil
	case action == "del_todo":
		return a.deleteCurrent()
	case action == "del_goal":
		return a.deleteCurrent()
	case action == "timer_pomodoro":
		a.chooseTimerTodo(a.pomodoroPlan())
		return a, nil
	case action == "timer_countdown":
		plan := model.Plan{Kind: model.TimerCountDown}
		plan.Segments = []model.Segment{{
			Name: "倒计时", Kind: "focus",
			Dur: time.Duration(a.cfg.CountdownMinutes()) * time.Minute,
		}}
		a.chooseTimerTodo(plan)
		return a, nil
	case action == "timer_countup":
		a.chooseTimerTodo(model.Plan{Kind: model.TimerCountUp})
		return a, nil
	case action == "timer_custom":
		// 让用户自己编排状态名与时长（见需求 16、20）。
		a.openCustom()
		return a, nil
	case strings.HasPrefix(action, "custom:"):
		minutes, err := strconv.Atoi(strings.TrimPrefix(action, "custom:"))
		if err != nil || minutes <= 0 {
			return a, nil
		}
		plan := model.Plan{Kind: model.TimerCustom}
		plan.Segments = []model.Segment{{Name: "自定义", Kind: "focus", Dur: time.Duration(minutes) * time.Minute}}
		a.chooseTimerTodo(plan)
		return a, nil
	case action == "start_focus":
		// 从菜单直接开始的番茄钟。
		a.beginTimer(a.pomodoroPlan(), "")
		return a, nil
	case strings.HasPrefix(action, "todo:"):
		todoID := strings.TrimPrefix(action, "todo:")
		if a.pendingPlan == nil {
			return a, nil
		}
		plan := *a.pendingPlan
		a.pendingPlan = nil
		a.beginTimer(plan, todoID)
		return a, nil
	}
	return a, nil
}

func (a *App) setToast(msg string, kind toastKind) {
	a.toast = msg
	a.toastKind = kind
}

// askCloseEditor 在随手记等内容没保存就要关闭时，问清怎么处理。
//
// 和 askQuit 的区别在于收尾动作：这里只关闭编辑器回到看板，
// 不退出程序；而 askQuit 的“保存”之后会退出。
//
// 默认选中“继续编辑”，误触回车不会丢数据。
func (a *App) askCloseEditor() {
	a.pick = &pickState{
		title: "随手记还没保存",
		items: []pickItem{
			{Label: "继续编辑", Action: "cancel"},
			{Label: "保存并关闭", Action: "close_save"},
			{Label: "不保存，关闭", Action: "close_discard"},
		},
		cursor: 0,
		small:  true,
	}
}

// askQuit 弹出退出确认，避免误触 q 直接退出（见需求 8）。
//
// 默认选中“取消”，这样误触后顺手回车也不会退出。
// 如果随手记正开着且内容没保存，会多问一句：
// 保存并退出、直接退出、取消退出（见用户反馈）。
func (a *App) askQuit() {
	if a.editor.active && a.editor.Dirty() && a.editor.multiline {
		a.pick = &pickState{
			title: "随手记还没保存",
			items: []pickItem{
				{Label: "取消退出，回去继续写", Action: "cancel"},
				{Label: "保存并退出", Action: "quit_save"},
				{Label: "直接退出，丢弃改动", Action: "quit_discard"},
			},
			cursor: 0,
			small:  true,
		}
		return
	}
	quitLabel := "退出 Kairos"
	if a.timer != nil {
		quitLabel = "结束计时并退出"
	}
	a.pick = &pickState{
		title: "确定要退出 Kairos 吗？",
		items: []pickItem{
			{Label: "取消，继续使用", Action: "cancel"},
			{Label: quitLabel, Action: "quit"},
		},
		cursor: 0,
	}
}

// ---------- 菜单 ----------

// menuItems 是中间栏的选项列表（见需求 16、12）。
var menuItems = []menuItem{
	{Label: "随手记 / Note", Action: "note"},
	{Label: "专注计时 / Focus", Action: "timer"},
	{Label: "历史记录 / History", Action: "history"},
	{Label: "设置 / Settings", Action: "settings"},
	{Label: "帮助 / Help", Action: "help"},
	{Label: "退出 / Quit", Action: "quit"},
}

type menuItem struct {
	Label  string
	Action string
}

func (a *App) activateMenuItem(idx int) (tea.Model, tea.Cmd) {
	if idx < 0 || idx >= len(menuItems) {
		return a, nil
	}
	switch menuItems[idx].Action {
	case "note":
		a.openNote()
	case "timer":
		a.startTimer()
	case "history":
		a.view = ViewHistory
	case "settings":
		a.view = ViewSettings
	case "help":
		a.view = ViewHelp
	case "quit":
		// 一律走确认流程：随手记没保存时它会多问一句“保存还是丢弃”，
		// 不能在这里直接退出绕开保护。
		a.askQuit()
	}
	return a, nil
}

// toastLine 返回当前提示文本。
func (a *App) toastLine() string {
	if a.toast == "" {
		return ""
	}
	switch a.toastKind {
	case toastWarn:
		return a.st.Warn.Render(a.toast)
	case toastErr:
		return a.st.Error.Render(a.toast)
	default:
		return a.st.Muted.Render(a.toast)
	}
}

// renderEmpty 生成占位提示。
func (a *App) renderEmpty(text string, width int) string {
	return a.st.Muted.Render(truncate(text, max(0, width)))
}

// truncate 按显示宽度截断字符串，超出部分用省略号。
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > width-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}
