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
		if g := a.selectedGoal(); g != nil {
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
		g := a.selectedGoal()
		if g == nil {
			return
		}
		archived := g.ArchivedDay != ""
		a.editor.set("重命名 GOAL", g.Title)
		a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
			if value == "" {
				a.setToast("标题不能为空", toastWarn)
				return a, nil
			}
			g.Title = value
			// 标题变了，标签也要跟着更新，保证继承标签仍能对上。
			g.Tag = model.TagOf(value)
			if !archived {
				a.persistGoals()
			}
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
		g := a.selectedGoal()
		if g == nil {
			return
		}
		a.pick = &pickState{
			title: fmt.Sprintf("删除 GOAL「%s」？", truncate(g.Title, 24)),
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
		g := a.selectedGoal()
		if g == nil {
			break
		}
		// 归档中的目标存在当日数据里，未归档的存在 goals.json 里。
		if g.ArchivedDay != "" {
			a.removeArchivedGoal(g.ID)
			a.setToast("已从今日归档中删除 GOAL", toastInfo)
		} else {
			a.removeActiveGoal(g.ID)
			a.persistGoals()
			a.setToast("已删除 GOAL", toastInfo)
		}
	}
	a.clampCursors()
	return a, saveCmd(a.saveDay)
}

// ---------- 文本输入处理 ----------

func (a *App) handleEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 粘贴进来的整段文本：一次性插入，不当作按键序列解释。
	// Bubble Tea v1 用 KeyMsg{Paste: true} 表示括号粘贴的内容。
	if msg.Paste {
		a.insertEditorText(string(msg.Runes))
		return a, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		a.editor.active = false
		a.editor.onCommit = nil
		return a, nil
	case tea.KeyEnter:
		// 多行模式下 Enter 是换行，提交要用 Ctrl+S / Ctrl+D。
		if a.editor.multiline {
			a.editor.insert('\n')
			return a, nil
		}
		return a.commitEditor()
	case tea.KeyCtrlS, tea.KeyCtrlD:
		// 多行模式的提交键；单行模式下也顺手支持。
		return a.commitEditor()
	case tea.KeyLeft:
		if a.editor.multiline && a.editor.cursor > 0 && a.editor.value[a.editor.cursor-1] == '\n' {
			break
		}
		a.editor.move(-1)
	case tea.KeyRight:
		if a.editor.multiline && a.editor.cursor < len(a.editor.value) && a.editor.value[a.editor.cursor] == '\n' {
			break
		}
		a.editor.move(1)
	case tea.KeyUp:
		if a.editor.multiline {
			a.editor.moveVertical(-1)
		}
	case tea.KeyDown:
		if a.editor.multiline {
			a.editor.moveVertical(1)
		}
	case tea.KeyHome, tea.KeyCtrlA:
		if a.editor.multiline {
			a.editor.cursor = a.editor.lineStart()
		} else {
			a.editor.cursor = 0
		}
	case tea.KeyEnd, tea.KeyCtrlE:
		if a.editor.multiline {
			a.editor.cursor = a.editor.lineEnd()
		} else {
			a.editor.cursor = len(a.editor.value)
		}
	case tea.KeyBackspace:
		a.editor.backspace()
	case tea.KeyDelete:
		a.editor.delete()
	case tea.KeyCtrlU:
		if a.editor.multiline {
			// 多行模式下只清掉当前行，避免一句话没打完就把全部字条删光。
			start, end := a.editor.lineStart(), a.editor.lineEnd()
			a.editor.value = append(a.editor.value[:start], a.editor.value[end:]...)
			a.editor.cursor = start
		} else {
			a.editor.value = nil
			a.editor.cursor = 0
		}
	case tea.KeyCtrlW:
		// 删除前一个单词。
		for a.editor.cursor > 0 && a.editor.value[a.editor.cursor-1] == ' ' {
			a.editor.backspace()
		}
		for a.editor.cursor > 0 && a.editor.value[a.editor.cursor-1] != ' ' &&
			a.editor.value[a.editor.cursor-1] != '\n' {
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

// commitEditor 提交当前输入框内容。
func (a *App) commitEditor() (tea.Model, tea.Cmd) {
	value := a.editor.text()
	commit := a.editor.onCommit
	a.editor.active = false
	a.editor.onCommit = nil
	a.editor.multiline = false
	if commit != nil {
		return commit(value)
	}
	return a, nil
}

// insertEditorText 把外部文本（终端粘贴）整段插入输入框。
//
// 单行输入框把换行折成空格，避免一行标题被拆断；
// 多行输入框（自定义字条）保留换行，因为“一行一条”就是它的语义。
func (a *App) insertEditorText(text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if !a.editor.multiline {
		text = strings.ReplaceAll(text, "\n", " ")
	}
	for _, r := range text {
		if r == '\t' {
			continue
		}
		a.editor.insert(r)
	}
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

// pomodoroPlan 依据配置的段数构造番茄钟方案（见需求 16）。
//
// 每一轮是“专注 + 休息”，最后一段之后不再安排休息，避免计时结束后又跳进休息段。
// 专注与休息时长都取自设置页，用户可以自己调。
func (a *App) pomodoroPlan() model.Plan {
	cycles := a.cfg.EffectivePomodoroCycles()
	focus := time.Duration(a.cfg.FocusMinutes()) * time.Minute
	rest := time.Duration(a.cfg.BreakMinutes()) * time.Minute
	plan := model.Plan{Kind: model.TimerPomodoro, Cycle: cycles}
	for i := 0; i < cycles; i++ {
		plan.Segments = append(plan.Segments,
			model.Segment{Name: fmt.Sprintf("专注 %d/%d", i+1, cycles), Kind: "focus", Dur: focus})
		if i < cycles-1 {
			plan.Segments = append(plan.Segments,
				model.Segment{Name: fmt.Sprintf("休息 %d/%d", i+1, cycles-1), Kind: "break", Dur: rest})
		}
	}
	return plan
}

// ---------- 设置 ----------

// settingItem 描述设置页的一行。
type settingItem struct {
	Label string
	// Value 返回当前值的展示文本。
	Value func(a *App) string
	// Edit 在用户选中并确认时打开输入框；为 nil 表示只读展示。
	Edit func(a *App)
}

// settingItems 是设置页的条目顺序，也是 j/k 导航的依据。
// 计时相关的四项排在最前，方便用户先调好再开始专注（见问题 3）。
var settingItems = []settingItem{
	{
		Label: "专注时长（分钟）",
		Value: func(a *App) string { return strconv.Itoa(a.cfg.FocusMinutes()) },
		Edit:  (*App).editFocusMinutes,
	},
	{
		Label: "休息时长（分钟）",
		Value: func(a *App) string { return strconv.Itoa(a.cfg.BreakMinutes()) },
		Edit:  (*App).editBreakMinutes,
	},
	{
		Label: "番茄钟段数（专注 + 休息为一轮）",
		Value: func(a *App) string { return strconv.Itoa(a.cfg.EffectivePomodoroCycles()) },
		Edit:  (*App).editPomodoroCycles,
	},
	{
		Label: "倒计时时长（分钟）",
		Value: func(a *App) string { return strconv.Itoa(a.cfg.CountdownMinutes()) },
		Edit:  (*App).editCountdownMinutes,
	},
	{
		Label: "日界线（新的一天从几点开始）",
		Value: func(a *App) string { return a.cfg.DayCutoff },
		Edit:  (*App).editCutoff,
	},
	{
		Label: "昵称（显示在问候语里）",
		Value: func(a *App) string { return orDash(a.cfg.Nickname) },
		Edit:  (*App).editNickname,
	},
	{
		Label: "自定义字条（每行一条）",
		Value: func(a *App) string { return fmt.Sprintf("%d 条", len(a.customQuotes())) },
		Edit:  (*App).editQuotes,
	},
	{
		Label: "看板展示随手记",
		Value: func(a *App) string {
			if a.cfg.ShowNote {
				return "开"
			}
			return "关"
		},
		Edit: func(a *App) { a.toggleShowNote() },
	},
	{
		Label: "数据目录",
		Value: func(a *App) string { return a.store.Root() },
	},
	{
		Label: "配置文件",
		Value: func(a *App) string { return a.pathsForSave().ConfigFile },
	},
	{
		Label: "时区",
		Value: func(a *App) string { return orDash(a.cfg.Timezone) },
	},
}

func (a *App) handleSettingsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q":
		a.view = ViewDashboard
	case "j", "down":
		a.settingsCursor = (a.settingsCursor + 1) % len(settingItems)
	case "k", "up":
		a.settingsCursor = (a.settingsCursor - 1 + len(settingItems)) % len(settingItems)
	case "g", "home":
		a.settingsCursor = 0
	case "G", "end":
		a.settingsCursor = len(settingItems) - 1
	case "enter", "e", " ":
		a.activateSetting()
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	}
	return a, nil
}

// activateSetting 打开当前选中设置的输入框。
func (a *App) activateSetting() {
	if a.settingsCursor < 0 || a.settingsCursor >= len(settingItems) {
		return
	}
	item := settingItems[a.settingsCursor]
	if item.Edit == nil {
		a.setToast("这一项是只读的", toastInfo)
		return
	}
	item.Edit(a)
}

// editCutoff 编辑日界线。
func (a *App) editCutoff() {
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
		return a, saveCmd(a.saveDay)
	}
}

// editFocusMinutes 编辑专注时长，它同时决定番茄钟与倒计时的默认真实时长。
func (a *App) editFocusMinutes() {
	a.editor.set("专注时长（分钟，1-600）", strconv.Itoa(a.cfg.FocusMinutes()))
	a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
		mins, err := parseMinutes(value)
		if err != nil {
			a.setToast(err.Error(), toastErr)
			return a, nil
		}
		a.cfg.DefaultFocus = int(mins.Minutes())
		a.saveConfig()
		a.setToast(fmt.Sprintf("专注时长已设为 %d 分钟", a.cfg.FocusMinutes()), toastInfo)
		return a, nil
	}
}

// editBreakMinutes 编辑休息时长。
func (a *App) editBreakMinutes() {
	a.editor.set("休息时长（分钟，1-120）", strconv.Itoa(a.cfg.BreakMinutes()))
	a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 1 || n > 120 {
			a.setToast("请输入 1-120 之间的分钟数", toastErr)
			return a, nil
		}
		a.cfg.DefaultBreak = n
		a.saveConfig()
		a.setToast(fmt.Sprintf("休息时长已设为 %d 分钟", n), toastInfo)
		return a, nil
	}
}

// editCountdownMinutes 编辑倒计时时长。
func (a *App) editCountdownMinutes() {
	a.editor.set("倒计时时长（分钟，1-600）", strconv.Itoa(a.cfg.CountdownMinutes()))
	a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
		mins, err := parseMinutes(value)
		if err != nil {
			a.setToast(err.Error(), toastErr)
			return a, nil
		}
		a.cfg.CountdownMin = int(mins.Minutes())
		a.saveConfig()
		a.setToast(fmt.Sprintf("倒计时时长已设为 %d 分钟", a.cfg.CountdownMinutes()), toastInfo)
		return a, nil
	}
}

// editPomodoroCycles 编辑番茄钟段数（见需求 16）。
func (a *App) editPomodoroCycles() {
	a.editor.set("番茄钟段数（1-12）", strconv.Itoa(a.cfg.EffectivePomodoroCycles()))
	a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 1 || n > 12 {
			a.setToast("请输入 1-12 之间的段数", toastErr)
			return a, nil
		}
		a.cfg.PomodoroCycles = n
		a.saveConfig()
		a.setToast(fmt.Sprintf("番茄钟已设为 %d 段", n), toastInfo)
		return a, nil
	}
}

// editNickname 编辑昵称。
func (a *App) editNickname() {
	a.editor.set("昵称（留空则不显示）", a.cfg.Nickname)
	a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
		a.cfg.Nickname = strings.TrimSpace(value)
		a.saveConfig()
		a.setToast("昵称已更新", toastInfo)
		return a, nil
	}
}

// editQuotes 编辑自定义字条（见需求 9）。
//
// 多行输入：一行一条字条。Enter 换行，Ctrl+S 保存，Esc 取消；
// 留空表示清空自定义字条，回到内置字条。
func (a *App) editQuotes() {
	a.editor.setMultiline("自定义字条（每行一条，ctrl+s 保存）", a.cfg.QuotesText())
	a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
		a.cfg.Quotes = parseQuoteLines(value)
		a.saveConfig()
		if n := len(a.cfg.Quotes); n > 0 {
			a.setToast(fmt.Sprintf("已保存 %d 条自定义字条", n), toastInfo)
		} else {
			a.setToast("已清空自定义字条，回到内置字条", toastInfo)
		}
		a.quoteIdx = 0
		a.quoteAt = a.clock.Now()
		return a, nil
	}
}

// parseQuoteLines 把多行文本拆成字条列表，去掉空行与首尾空白。
func parseQuoteLines(text string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// customQuotes 返回当前生效的字条列表。
func (a *App) customQuotes() []string {
	if len(a.cfg.Quotes) > 0 {
		return a.cfg.Quotes
	}
	return Quotes
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
