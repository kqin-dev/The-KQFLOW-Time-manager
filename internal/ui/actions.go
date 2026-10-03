package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/clock"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/config"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/model"
)

// addTarget 描述“添加”要落到哪一栏。
type addTarget int

const (
	addFixed addTarget = iota
	addFloating
	addGoal
	addTaskForTodo
	addTaskForGoal
)

// startAdd 依据当前焦点决定添加固定/临时 TODO。
func (a *App) startAdd() {
	target := addFloating
	if a.focus == FocusFixed {
		target = addFixed
	}
	label := "新 TODO（临时）"
	if target == addFixed {
		label = "新 TODO（固定）"
	}
	a.editor.set(label, "")
	a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
		return a.commitAdd(target, value)
	}
}

// startAddGoal 添加一个 GOAL。
func (a *App) startAddGoal() {
	a.editor.set("新 GOAL", "")
	a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
		return a.commitAdd(addGoal, value)
	}
}

// startAddTask 给当前选中的条目添加子任务（见需求 8）。
func (a *App) startAddTask() {
	switch a.focus {
	case FocusFixed, FocusFloating:
		a.editor.set("新 TASK", "")
		a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
			return a.commitAdd(addTaskForTodo, value)
		}
	case FocusGoals:
		a.editor.set("新 TASK", "")
		a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
			return a.commitAdd(addTaskForGoal, value)
		}
	default:
		a.setToast("先在左栏或右栏选中一个条目，再按 t 添加子任务", toastWarn)
	}
}

func (a *App) commitAdd(target addTarget, value string) (tea.Model, tea.Cmd) {
	if value == "" {
		a.setToast("内容不能为空", toastWarn)
		return a, nil
	}
	now := a.clock.Now()
	switch target {
	case addFixed:
		a.data.Fixed = append(a.data.Fixed, model.NewTodo(value, model.KindFixed, a.day, now))
		a.cursors.fixed = len(a.data.Fixed) - 1
		a.focus = FocusFixed
		a.setToast("已添加固定 TODO", toastInfo)
	case addFloating:
		a.data.Floating = append(a.data.Floating, model.NewTodo(value, model.KindFloating, a.day, now))
		a.cursors.floating = len(a.data.Floating) - 1
		a.focus = FocusFloating
		a.setToast("已添加临时 TODO", toastInfo)
	case addGoal:
		a.goals = append(a.goals, *model.NewGoal(value, now))
		a.cursors.goals = len(a.goals) - 1
		a.focus = FocusGoals
		a.persistGoals()
		a.setToast("已添加 GOAL", toastInfo)
	case addTaskForTodo:
		if t := a.currentTodo(); t != nil {
			t.Tasks = append(t.Tasks, model.NewTask(value))
			a.setToast("已添加子任务", toastInfo)
		}
	case addTaskForGoal:
		if len(a.goals) > 0 {
			g := &a.goals[a.cursors.goals]
			g.Tasks = append(g.Tasks, model.NewTask(value))
			a.persistGoals()
			a.setToast("已添加子任务", toastInfo)
		}
	}
	return a, saveCmd(a.saveDay)
}

// startEdit 编辑当前选中的条目标题。
func (a *App) startEdit() {
	switch a.focus {
	case FocusFixed, FocusFloating:
		t := a.currentTodo()
		if t == nil {
			return
		}
		item := t
		a.editor.set("重命名 TODO", item.Title)
		a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
			if value == "" {
				a.setToast("标题不能为空", toastWarn)
				return a, nil
			}
			item.Title = value
			return a, saveCmd(a.saveDay)
		}
	case FocusGoals:
		if len(a.goals) == 0 {
			return
		}
		g := &a.goals[a.cursors.goals]
		a.editor.set("重命名 GOAL", g.Title)
		a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
			if value == "" {
				a.setToast("标题不能为空", toastWarn)
				return a, nil
			}
			g.Title = value
			// 标题变了，标签也要跟着更新，保证继承标签仍能对上。
			g.Tag = model.TagOf(value)
			a.persistGoals()
			return a, saveCmd(a.saveDay)
		}
	default:
		a.setToast("选中左侧 TODO 或右侧 GOAL 后再按 e 重命名", toastWarn)
	}
}

// startDelete 删除当前选中的条目，删除前先确认。
func (a *App) startDelete() {
	switch a.focus {
	case FocusFixed, FocusFloating:
		t := a.currentTodo()
		if t == nil {
			return
		}
		a.pick = &pickState{
			title: fmt.Sprintf("删除 TODO「%s」？", truncate(t.Title, 24)),
			items: []pickItem{
				{Label: "确认删除", Action: "del_todo"},
				{Label: "取消", Action: "cancel"},
			},
		}
	case FocusGoals:
		if len(a.goals) == 0 {
			return
		}
		a.pick = &pickState{
			title: fmt.Sprintf("删除 GOAL「%s」？", truncate(a.goals[a.cursors.goals].Title, 24)),
			items: []pickItem{
				{Label: "确认删除", Action: "del_goal"},
				{Label: "取消", Action: "cancel"},
			},
		}
	default:
		a.setToast("选中待删除的条目后再按 d", toastWarn)
	}
}

// currentTodo 返回当前焦点下的待办。
func (a *App) currentTodo() *model.Todo {
	switch a.focus {
	case FocusFixed:
		if a.cursors.fixed < len(a.data.Fixed) {
			return a.data.Fixed[a.cursors.fixed]
		}
	case FocusFloating:
		if a.cursors.floating < len(a.data.Floating) {
			return a.data.Floating[a.cursors.floating]
		}
	}
	return nil
}

func (a *App) deleteCurrent() (tea.Model, tea.Cmd) {
	switch a.focus {
	case FocusFixed:
		if a.cursors.fixed < len(a.data.Fixed) {
			a.data.Fixed = append(a.data.Fixed[:a.cursors.fixed], a.data.Fixed[a.cursors.fixed+1:]...)
			a.setToast("已删除固定 TODO", toastInfo)
		}
	case FocusFloating:
		if a.cursors.floating < len(a.data.Floating) {
			a.data.Floating = append(a.data.Floating[:a.cursors.floating], a.data.Floating[a.cursors.floating+1:]...)
			a.setToast("已删除临时 TODO", toastInfo)
		}
	case FocusGoals:
		if a.cursors.goals < len(a.goals) {
			a.goals = append(a.goals[:a.cursors.goals], a.goals[a.cursors.goals+1:]...)
			a.persistGoals()
			a.setToast("已删除 GOAL", toastInfo)
		}
	}
	a.clampCursors()
	return a, saveCmd(a.saveDay)
}

// ---------- 文本输入处理 ----------

func (a *App) handleEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		a.editor.active = false
		a.editor.onCommit = nil
		// 取消输入时一并清理设置页的多步编辑状态。
		a.settingsEdit = nil
		return a, nil
	case tea.KeyEnter:
		value := a.editor.text()
		commit := a.editor.onCommit
		a.editor.active = false
		a.editor.onCommit = nil
		if commit != nil {
			return commit(value)
		}
		return a, nil
	case tea.KeyBackspace:
		a.editor.backspace()
	case tea.KeyDelete:
		a.editor.delete()
	case tea.KeyLeft:
		a.editor.move(-1)
	case tea.KeyRight:
		a.editor.move(1)
	case tea.KeyHome, tea.KeyCtrlA:
		a.editor.cursor = 0
	case tea.KeyEnd, tea.KeyCtrlE:
		a.editor.cursor = len(a.editor.value)
	case tea.KeyCtrlU:
		a.editor.value = nil
		a.editor.cursor = 0
	case tea.KeyCtrlW:
		// 删除前一个单词。
		for a.editor.cursor > 0 && a.editor.value[a.editor.cursor-1] == ' ' {
			a.editor.backspace()
		}
		for a.editor.cursor > 0 && a.editor.value[a.editor.cursor-1] != ' ' {
			a.editor.backspace()
		}
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			a.editor.insert(r)
		}
	case tea.KeySpace:
		a.editor.insert(' ')
	}
	return a, nil
}

// ---------- 计时 ----------

// startTimer 让用户选择计时模式与所属 TODO（见需求 16、17）。
func (a *App) startTimer() {
	if a.timer != nil {
		a.setToast("已有计时在进行，按 enter 打开计时菜单", toastWarn)
	}
	a.pick = &pickState{
		title: "选择计时方式",
		items: []pickItem{
			{Label: "番茄钟（专注 + 休息）", Action: "timer_pomodoro"},
			{Label: "倒计时", Action: "timer_countdown"},
			{Label: "正计时（不设终点）", Action: "timer_countup"},
			{Label: "自定义时段", Action: "timer_custom"},
			{Label: "取消", Action: "cancel"},
		},
	}
}

// chooseTimerTodo 让用户为本次计时选择归属的 TODO（见需求 17）。
func (a *App) chooseTimerTodo(plan model.Plan) {
	items := make([]pickItem, 0, len(a.data.All())+1)
	for _, t := range a.data.All() {
		label := t.Title
		if t.Done {
			label += "（已完成）"
		}
		items = append(items, pickItem{Label: truncate(label, 40), Action: "todo:" + t.ID})
	}
	items = append(items, pickItem{Label: "不归属任何 TODO", Action: "todo:"})
	a.pick = &pickState{
		title: fmt.Sprintf("%s · 选择归属的 TODO", describePlan(plan)),
		items: items,
	}
	a.pendingPlan = &plan
}

func describePlan(p model.Plan) string {
	if len(p.Segments) == 0 {
		return "计时"
	}
	switch p.Kind {
	case model.TimerPomodoro:
		return fmt.Sprintf("番茄钟 %s/%s ×%d",
			clock.ClockString(p.Segments[0].Dur), clock.ClockString(segDur(p, 1)), p.Cycle)
	case model.TimerCountDown:
		return "倒计时 " + clock.ClockString(p.Total())
	case model.TimerCountUp:
		return "正计时"
	default:
		return "自定义 " + clock.ClockString(p.Total())
	}
}

func segDur(p model.Plan, idx int) time.Duration {
	if idx < len(p.Segments) {
		return p.Segments[idx].Dur
	}
	return 0
}

// beginTimer 真正开始计时。
func (a *App) beginTimer(plan model.Plan, todoID string) {
	var todo *model.Todo
	if todoID != "" {
		todo = a.data.Find(todoID)
	}
	a.timer = newTimer(plan, todo, a.clock.Now())
	a.setToast(fmt.Sprintf("开始%s", describePlan(plan)), toastInfo)
}

// ---------- 设置 ----------

// settingsEdit 记录正在进行的多步设置编辑。
type settingsEdit struct {
	field string
	step  int
	buf   string
}

func (a *App) handleSettingsKey(key string) (tea.Model, tea.Cmd) {
	if a.settingsEdit != nil {
		return a.handleSettingsInput(key)
	}
	switch key {
	case "esc", "q", "enter":
		a.view = ViewDashboard
	case "c":
		a.settingsEdit = &settingsEdit{field: "cutoff"}
		a.editor.set("日界线（HH:MM，例如 04:00）", a.cfg.DayCutoff)
		a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
			d, err := config.ParseClock(value)
			if err != nil {
				a.setToast(err.Error(), toastErr)
				return a, nil
			}
			a.cfg.DayCutoff = config.FormatClock(d)
			a.saveConfig()
			a.setToast("日界线已设为 "+a.cfg.DayCutoff, toastInfo)
			a.settingsEdit = nil
			return a, saveCmd(a.saveDay)
		}
	case "f":
		a.settingsEdit = &settingsEdit{field: "focus"}
		a.editor.set("默认专注时长（分钟）", strconv.Itoa(a.cfg.DefaultFocus))
		a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n <= 0 || n > 600 {
				a.setToast("请输入 1-600 之间的分钟数", toastErr)
				return a, nil
			}
			a.cfg.DefaultFocus = n
			a.saveConfig()
			a.setToast(fmt.Sprintf("默认专注时长已设为 %d 分钟", n), toastInfo)
			a.settingsEdit = nil
			return a, nil
		}
	case "b":
		a.settingsEdit = &settingsEdit{field: "break"}
		a.editor.set("默认休息时长（分钟）", strconv.Itoa(a.cfg.DefaultBreak))
		a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n <= 0 || n > 120 {
				a.setToast("请输入 1-120 之间的分钟数", toastErr)
				return a, nil
			}
			a.cfg.DefaultBreak = n
			a.saveConfig()
			a.setToast(fmt.Sprintf("默认休息时长已设为 %d 分钟", n), toastInfo)
			a.settingsEdit = nil
			return a, nil
		}
	case "n":
		a.settingsEdit = &settingsEdit{field: "nickname"}
		a.editor.set("昵称（留空则不显示）", a.cfg.Nickname)
		a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
			a.cfg.Nickname = strings.TrimSpace(value)
			a.saveConfig()
			a.setToast("昵称已更新", toastInfo)
			a.settingsEdit = nil
			return a, nil
		}
	}
	return a, nil
}

// handleSettingsInput 在设置页打开输入框时把按键交给编辑器。
func (a *App) handleSettingsInput(key string) (tea.Model, tea.Cmd) {
	// 输入框由 handleEditorKey 统一处理；这里只负责在输入框关闭后清理状态。
	if !a.editor.active {
		a.settingsEdit = nil
	}
	return a, nil
}

// saveConfig 保存配置并让界面按新配置重新载入数据。
//
// 日界线改变时“今天是第几日”可能随之改变，因此必须重新载入。
func (a *App) saveConfig() {
	if err := config.Save(a.pathsForSave(), a.cfg); err != nil {
		a.setToast("配置保存失败："+err.Error(), toastErr)
		return
	}
	if err := a.reload(); err != nil {
		a.setToast("按新配置载入失败："+err.Error(), toastErr)
	}
}

// pathsForSave 返回用于写配置的路径集合。
func (a *App) pathsForSave() *config.Paths {
	if a.paths != nil {
		return a.paths
	}
	p, err := config.Resolve(a.cfg)
	if err != nil {
		return &config.Paths{}
	}
	a.paths = p
	return p
}
