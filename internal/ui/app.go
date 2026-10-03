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
	scroll struct{ fixed, floating, goals int }

	editor editorState

	// picks 是等待用户选择的模态框状态。
	pick *pickState

	// pendingPlan 保存已经选好、等待选择归属 TODO 的计时方案。
	pendingPlan *model.Plan

	// settingsEdit 记录设置页正在进行的多步编辑。
	settingsEdit *settingsEdit

	// paths 是配置文件与数据目录的定位信息。
	paths *config.Paths

	timer *timerState

	celebrate *celebrateState

	quoteIdx  int
	animPhase float64
	frame     int

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
		store: opts.Store,
		cfg:   opts.Config,
		clock: clk,
		st:    st,
		paths: opts.Paths,
		view:  ViewDashboard,
		focus: FocusMenu,
	}
	a.quoteIdx = rand.Intn(len(Quotes))
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

	// 模态框优先处理按键。
	if a.pick != nil {
		return a.handlePickKey(key)
	}
	if a.editor.active {
		return a.handleEditorKey(msg)
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
		switch key {
		case "esc", "q", "?":
			a.view = ViewDashboard
		case "ctrl+c":
			a.quitting = true
			return a, tea.Quit
		}
		return a, nil
	}

	switch key {
	case "ctrl+c", "Q":
		a.quitting = true
		return a, tea.Quit
	case "q", "esc":
		a.quitting = true
		return a, tea.Quit
	case "?":
		a.view = ViewHelp
	case "tab":
		a.focus = (a.focus + 1) % 4
	case "shift+tab":
		a.focus = (a.focus + 3) % 4
	case "h", "left":
		a.focus = (a.focus + 3) % 4
	case "l", "right":
		a.focus = (a.focus + 1) % 4
	case "j", "down":
		a.moveCursor(1)
	case "k", "up":
		a.moveCursor(-1)
	case "g", "home":
		a.jumpCursor(true)
	case "G", "end":
		a.jumpCursor(false)
	case " ":
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
		if a.focus == FocusMenu {
			return a.activateMenuItem(a.cursors.menu)
		}
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
		return len(a.goals)
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
	a.cursors.goals = clamp(a.cursors.goals, 0, max(0, len(a.goals)-1))
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
		if len(a.goals) > 0 {
			g := &a.goals[a.cursors.goals]
			g.Toggle(now, a.day)
			a.persistGoals()
			if g.Done {
				a.archiveGoal(*g)
			}
		}
	}
	return a, saveCmd(a.saveDay)
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
		action := a.pick.items[0].Action
		a.pick = nil
		return a.runAction(action)
	case "n":
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
		a.quitting = true
		return a, tea.Quit
	case action == "del_todo":
		return a.deleteCurrent()
	case action == "del_goal":
		return a.deleteCurrent()
	case action == "timer_pomodoro":
		plan := model.Plan{Kind: model.TimerPomodoro, Cycle: 1}
		plan.Segments = []model.Segment{
			{Name: "专注", Kind: "focus", Dur: a.cfg.FocusDuration()},
			{Name: "休息", Kind: "break", Dur: a.cfg.BreakDuration()},
		}
		a.chooseTimerTodo(plan)
		return a, nil
	case action == "timer_countdown":
		plan := model.Plan{Kind: model.TimerCountDown}
		plan.Segments = []model.Segment{{Name: "倒计时", Kind: "focus", Dur: a.cfg.FocusDuration()}}
		a.chooseTimerTodo(plan)
		return a, nil
	case action == "timer_countup":
		a.chooseTimerTodo(model.Plan{Kind: model.TimerCountUp})
		return a, nil
	case action == "timer_custom":
		a.pick = &pickState{
			title: "自定义时段总长",
			items: []pickItem{
				{Label: "15 分钟", Action: "custom:15"},
				{Label: "30 分钟", Action: "custom:30"},
				{Label: "45 分钟", Action: "custom:45"},
				{Label: "60 分钟", Action: "custom:60"},
				{Label: "90 分钟", Action: "custom:90"},
				{Label: "取消", Action: "cancel"},
			},
		}
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
		plan := model.Plan{Kind: model.TimerPomodoro, Cycle: 1}
		plan.Segments = []model.Segment{
			{Name: "专注", Kind: "focus", Dur: a.cfg.FocusDuration()},
			{Name: "休息", Kind: "break", Dur: a.cfg.BreakDuration()},
		}
		a.beginTimer(plan, "")
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

// ---------- 菜单 ----------

// menuItems 是中间栏的选项列表（见需求 16、12）。
var menuItems = []menuItem{
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
	case "timer":
		a.startTimer()
	case "history":
		a.view = ViewHistory
	case "settings":
		a.view = ViewSettings
	case "help":
		a.view = ViewHelp
	case "quit":
		a.quitting = true
		return a, tea.Quit
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
