package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/clock"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/model"
)

// View 渲染当前界面。
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "正在启动 Kairos…"
	}
	if a.width < 60 || a.height < 16 {
		return a.viewTooSmall()
	}

	switch a.view {
	case ViewHistory:
		return a.renderHistory()
	case ViewSettings:
		return a.renderSettings()
	case ViewHelp:
		return a.renderHelp()
	case ViewCarry:
		return a.renderCarry()
	default:
		base := a.renderDashboard()
		// 计时结束时响一声，提醒正在别处工作的用户。
		if a.timer != nil && a.timer.consumeBell() {
			base = "\a" + base
		}
		// 模态浮层叠在看板之上，保留背景上下文。
		if a.pick != nil {
			return overlay(base, a.renderPick(), a.width, a.height)
		}
		if a.editor.active {
			return overlay(base, a.renderEditor(), a.width, a.height)
		}
		return base
	}
}

func (a *App) viewTooSmall() string {
	return lipgloss.JoinVertical(lipgloss.Center,
		"",
		a.st.Warn.Render("终端窗口太小"),
		a.st.Muted.Render(fmt.Sprintf("当前 %d×%d，Kairos 至少需要 60×16", a.width, a.height)),
		"",
		a.st.Muted.Render("放大窗口，或按 ctrl+c 退出"),
	)
}

// renderDashboard 组装主看板：左 TODO、中选项、右 GOAL、下进度条（见需求 5）。
func (a *App) renderDashboard() string {
	header := a.renderHeader()
	footer := a.renderFooter()

	bodyHeight := a.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyHeight < 6 {
		bodyHeight = 6
	}

	leftWidth := 34
	rightWidth := 30
	if a.width < 110 {
		leftWidth, rightWidth = 28, 24
	}
	if a.width < 90 {
		leftWidth, rightWidth = 24, 22
	}
	centerWidth := a.width - leftWidth - rightWidth
	if centerWidth < 24 {
		// 窄屏时收窄两侧，保证中间栏可用。
		shrink := 24 - centerWidth
		leftWidth -= shrink / 2
		rightWidth -= shrink - shrink/2
		centerWidth = a.width - leftWidth - rightWidth
	}

	left := a.renderLeftPanel(leftWidth, bodyHeight)
	right := a.renderGoalPanel(rightWidth, bodyHeight)
	center := a.renderCenterPanel(centerWidth, bodyHeight)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
	out := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)

	if a.celebrate != nil {
		elapsed := time.Since(a.celebrate.started)
		t := float64(elapsed) / float64(celebrateFor)
		if t > 1 {
			t = 1
		}
		overlay := a.celebrateFrame(t, a.width, bodyHeight)
		out = lipgloss.JoinVertical(lipgloss.Left, header, overlay, footer)
	}
	return out
}

// renderHeader 显示问候语、日期与今日专注时长（见需求 18）。
func (a *App) renderHeader() string {
	now := a.clock.Now()
	cut := a.cfg.Cutoff()

	greeting := clock.Greeting(now, cut)
	if a.cfg.Nickname != "" {
		greeting += "，" + a.cfg.Nickname
	}
	focus, _ := a.data.FocusTotal()

	loc := a.cfg.Location()
	weekday, err := clock.Weekday(a.day, loc)
	if err != nil {
		weekday = now.Weekday()
	}
	weekCN := [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[weekday]

	left := a.st.Title.Render(greeting+"！") + "  " +
		a.st.Muted.Render(fmt.Sprintf("%s %s · %s · 日界线 %s",
			a.day, weekCN, now.Format("15:04:05"), clock.WallClock(cut)))

	right := a.st.Muted.Render("今日专注 ") + a.st.OK.Bold(true).Render(clock.HumanDuration(focus))

	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right

	if a.toast != "" {
		line += "\n" + a.toastLine()
	}
	return line
}

// renderLeftPanel 渲染 TODAY TODO 的上下两栏（见需求 13）。
func (a *App) renderLeftPanel(width, height int) string {
	half := (height - 3) / 2
	if half < 3 {
		half = 3
	}

	done, total := a.data.Counts()
	titleFixed := fmt.Sprintf("TODAY · 固定 (%d)", len(a.data.Fixed))
	titleFloating := fmt.Sprintf("TODAY · 临时 (%d)", len(a.data.Floating))
	progress := fmt.Sprintf("完成 %d/%d", done, total)

	fixed := a.renderTodoList(a.data.Fixed, FocusFixed, titleFixed, width, half)
	floating := a.renderTodoList(a.data.Floating, FocusFloating, titleFloating, width, half)
	summary := a.st.Muted.Render(progress)
	if total > 0 && done == total {
		summary = a.st.OK.Bold(true).Render("✔ 全部完成 " + progress)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, fixed, floating, summary)
	return a.panel(a.focus == FocusFixed || a.focus == FocusFloating, width, height, content)
}

// renderTodoList 渲染一栏待办。
func (a *App) renderTodoList(items []*model.Todo, which Focus, title string, width, height int) string {
	inner := width - 4
	if inner < 8 {
		inner = 8
	}
	var lines []string
	lines = append(lines, a.st.PanelTitle.Render(truncate(title, inner)))
	if len(items) == 0 {
		lines = append(lines, a.renderEmpty("按 a 添加", inner))
	}

	cursor := a.cursors.fixed
	if which == FocusFloating {
		cursor = a.cursors.floating
	}
	// 列表区域减去标题行。
	listHeight := height - 1
	if listHeight < 1 {
		listHeight = 1
	}
	offset := scrollOffset(cursor, len(items), listHeight)

	for i := offset; i < len(items) && len(lines) < listHeight; i++ {
		item := items[i]
		selected := a.focus == which && i == cursor
		lines = append(lines, a.renderTodoRow(item, selected, inner))
		// 展开选中项的子任务（见需求 8）。
		if selected {
			for ti, task := range item.Tasks {
				if len(lines) >= listHeight {
					break
				}
				mark := MarkTodo
				style := a.st.Muted
				if task.Done() {
					mark, style = MarkDone, a.st.DoneTag
				}
				label := fmt.Sprintf("   %s %s", mark, task.Title)
				if ti == len(item.Tasks)-1 {
					label = fmt.Sprintf("   └ %s %s", mark, task.Title)
				}
				lines = append(lines, style.Render(truncate(label, inner)))
			}
		}
	}
	return strings.Join(lines, "\n")
}

// renderTodoRow 渲染单条待办。
func (a *App) renderTodoRow(item *model.Todo, selected bool, width int) string {
	mark := MarkTodo
	switch {
	case item.Done:
		mark = MarkDone
	case item.Status == model.StatusDoing:
		mark = MarkDoing
	}

	label := fmt.Sprintf("%s %s", mark, item.Title)
	done, total := item.Progress()
	if total > 0 {
		label += fmt.Sprintf(" (%d/%d)", done, total)
	}
	if item.GoalTag != "" {
		label += " " + item.GoalTag
	}
	if item.CarriedFrom != "" {
		label += " ↺"
	}
	label = truncate(label, width)

	style := a.st.Row
	switch {
	case selected && item.Done:
		style = a.st.RowDoneCur
	case selected:
		style = a.st.RowCursor
	case item.Done:
		style = a.st.RowDone
	}
	return style.Render(label)
}

// renderGoalPanel 渲染右栏 GOAL（见需求 6、10）。
func (a *App) renderGoalPanel(width, height int) string {
	inner := width - 4
	if inner < 8 {
		inner = 8
	}
	active := 0
	for _, g := range a.goals {
		if !g.Done {
			active++
		}
	}
	title := fmt.Sprintf("GOAL (%d 进行中)", active)

	var lines []string
	lines = append(lines, a.st.PanelTitle.Render(truncate(title, inner)))
	if len(a.goals) == 0 {
		lines = append(lines, a.renderEmpty("按 A 添加目标", inner))
	}

	listHeight := height - 1
	if listHeight < 1 {
		listHeight = 1
	}
	cursor := a.cursors.goals
	offset := scrollOffset(cursor, len(a.goals), listHeight)

	for i := offset; i < len(a.goals) && len(lines) < listHeight; i++ {
		g := a.goals[i]
		selected := a.focus == FocusGoals && i == cursor

		mark := MarkTodo
		if g.Done {
			mark = MarkDone
		}
		label := fmt.Sprintf("%s %s", mark, g.Title)
		done, total := g.Progress()
		if total > 0 {
			label += fmt.Sprintf(" (%d/%d)", done, total)
		}
		label = truncate(label, inner)

		style := a.st.Row
		switch {
		case selected && g.Done:
			style = a.st.RowDoneCur
		case selected:
			style = a.st.RowCursor
		case g.Done:
			style = a.st.RowDone
		}
		lines = append(lines, style.Render(label))

		if selected {
			// 展示标签与归档日，标签用于继承时避免同名混淆（见需求 15）。
			meta := "   " + g.Tag
			if g.ArchivedDay != "" {
				meta += " · 归档于 " + g.ArchivedDay
			}
			if len(lines) < listHeight {
				lines = append(lines, a.st.Muted.Render(truncate(meta, inner)))
			}
			for _, task := range g.Tasks {
				if len(lines) >= listHeight {
					break
				}
				tmark := MarkTodo
				tstyle := a.st.Muted
				if task.Done() {
					tmark, tstyle = MarkDone, a.st.DoneTag
				}
				lines = append(lines, tstyle.Render(truncate("   └ "+tmark+" "+task.Title, inner)))
			}
		}
	}

	// 本日已归档的目标（见需求 10）。
	if len(a.data.Archive.Goals) > 0 {
		if len(lines) < listHeight {
			lines = append(lines, "")
		}
		if len(lines) < listHeight {
			lines = append(lines, a.st.PanelTitle.Render("今日已归档"))
		}
		for _, g := range a.data.Archive.Goals {
			if len(lines) >= listHeight {
				break
			}
			lines = append(lines, a.st.DoneTag.Render(truncate("✔ "+g.Title, inner)))
		}
	}
	return a.panel(a.focus == FocusGoals, width, height, strings.Join(lines, "\n"))
}

// renderCenterPanel 渲染中间栏：Logo、状态、选项、字条。
func (a *App) renderCenterPanel(width, height int) string {
	inner := width - 4
	if inner < 10 {
		inner = 10
	}

	// Logo：窄栏时用紧凑版本。
	compact := inner < LogoWidth()
	logo := GradientLogo(a.st.Theme.Primary, a.st.Theme.Secondary, a.animPhase, compact)
	logoLines := strings.Split(logo, "\n")

	var lines []string
	// 垂直留白，让 Logo 大致居中在中间栏上部。
	for len(lines) < 1 {
		lines = append(lines, "")
	}
	for _, l := range logoLines {
		lines = append(lines, center(l, inner, lipgloss.Width(l)))
	}
	lines = append(lines, "")

	// 状态行：今日完成度与专注统计。
	done, total := a.data.Counts()
	status := fmt.Sprintf("今日待办 %d/%d", done, total)
	if total > 0 && done == total {
		status += " · 已全部完成 🎉"
	}
	lines = append(lines, a.st.Muted.Render(center(status, inner, lipgloss.Width(status))))
	lines = append(lines, "")

	// 选项列表。
	for i, item := range menuItems {
		label := truncate("  "+item.Label, inner)
		if a.focus == FocusMenu && i == a.cursors.menu {
			lines = append(lines, a.st.MenuCursor.Render(pad(label, inner)))
		} else {
			lines = append(lines, a.st.Menu.Render(label))
		}
	}
	lines = append(lines, "")

	// 计时状态摘要。
	if a.timer != nil {
		now := a.clock.Now()
		_, seg, _ := a.timer.segment(now)
		state := "专注中"
		if a.timer.paused {
			state = "已暂停"
		}
		if a.timer.finished {
			state = "已完成"
		}
		info := fmt.Sprintf("%s · %s · %s", state, clock.ClockString(a.timer.elapsed(now)), seg.Name)
		lines = append(lines, a.st.Accent.Render(center(info, inner, lipgloss.Width(info))))
	} else {
		info := "按 enter 开始专注"
		lines = append(lines, a.st.Muted.Render(center(info, inner, lipgloss.Width(info))))
	}

	// 随机字条固定在底部（见需求 5）。
	quote := "「" + Quotes[a.quoteIdx%len(Quotes)] + "」"
	quoteLines := wrap(quote, inner)
	needed := len(lines) + 1 + len(quoteLines)
	if needed < height-2 {
		for i := 0; i < height-2-needed; i++ {
			lines = append(lines, "")
		}
	}
	lines = append(lines, "")
	for _, ql := range quoteLines {
		lines = append(lines, a.st.Muted.Render(center(ql, inner, lipgloss.Width(ql))))
	}

	return a.panel(false, width, height, strings.Join(lines, "\n"))
}

// renderFooter 渲染底部时段看条（见需求 20）。
func (a *App) renderFooter() string {
	bar := a.renderTimerBar(a.width)
	hints := a.renderHints()
	if bar == "" {
		return hints
	}
	return lipgloss.JoinVertical(lipgloss.Left, bar, hints)
}

// renderTimerBar 渲染进度条；没有计时时显示当日时间进度。
func (a *App) renderTimerBar(width int) string {
	if a.timer == nil {
		return a.renderDayBar(width)
	}
	now := a.clock.Now()
	label := a.timer.name
	segIdx, seg, within := a.timer.segment(now)

	state := "专注中"
	if a.timer.paused {
		state = "已暂停"
	}
	if a.timer.finished {
		state = "已完成"
	}

	total := a.timer.total()
	elapsed := a.timer.elapsed(now)

	var head string
	if total <= 0 {
		// 正计时没有终点，用一条流动的填充条表示仍在推进。
		head = fmt.Sprintf("%s · %s · 已用 %s · 第 %d 段 %s",
			state, label, clock.ClockString(elapsed), segIdx+1, seg.Name)
	} else {
		head = fmt.Sprintf("%s · %s · 剩余 %s / 共 %s · 第 %d 段 %s（本段 %s/%s）",
			state, label, clock.ClockString(a.timer.remaining(now)), clock.ClockString(total),
			segIdx+1, seg.Name, clock.ClockString(within), clock.ClockString(seg.Dur))
	}

	barWidth := width - 4
	if barWidth < 10 {
		barWidth = 10
	}
	bar := a.renderPlanBar(a.timer.plan, elapsed, barWidth)

	return lipgloss.JoinVertical(lipgloss.Left,
		a.st.Muted.Render(truncate(head, width)),
		bar,
	)
}

// renderPlanBar 按方案的时段切分渲染进度条，不同状态用不同颜色（见需求 20）。
func (a *App) renderPlanBar(plan model.Plan, elapsed time.Duration, width int) string {
	total := plan.Total()
	if total <= 0 {
		// 正计时：用 1/3 宽度的流动光带表示无限推进。
		pos := int(elapsed.Seconds()*2) % width
		cells := make([]string, 0, width)
		for i := 0; i < width; i++ {
			d := i - pos
			if d < 0 {
				d = -d
			}
			if d <= 2 {
				cells = append(cells, a.st.BarFilled.Render("━"))
			} else {
				cells = append(cells, a.st.BarEmpty.Render("─"))
			}
		}
		return strings.Join(cells, "")
	}

	acc := time.Duration(0)
	var b strings.Builder
	for _, seg := range plan.Segments {
		n := int(float64(seg.Dur) / float64(total) * float64(width))
		if n < 1 {
			n = 1
		}
		style := a.st.BarFocus
		if seg.Kind == "break" {
			style = a.st.BarBreak
		}
		for i := 0; i < n; i++ {
			// 段的起止处按已流逝时间切成实心与空心的部分。
			cellStart := acc + time.Duration(float64(seg.Dur)*float64(i)/float64(n))
			if elapsed >= cellStart+time.Duration(float64(seg.Dur)/float64(n)) {
				b.WriteString(style.Render("━"))
			} else if elapsed >= cellStart {
				// 当前正在走的这一段，用醒目的光标字符。
				b.WriteString(a.st.BarCursor.Render("╸"))
			} else {
				b.WriteString(a.st.BarEmpty.Render("─"))
			}
		}
		acc += seg.Dur
	}
	return b.String()
}

// renderDayBar 在没有计时时显示当日时间进度。
func (a *App) renderDayBar(width int) string {
	cut := a.cfg.Cutoff()
	loc := a.cfg.Location()
	start, err1 := clock.DayStart(a.day, cut, loc)
	end, err2 := clock.DayEnd(a.day, cut, loc)
	if err1 != nil || err2 != nil {
		return ""
	}
	now := a.clock.Now()
	total := end.Sub(start)
	if total <= 0 {
		return ""
	}
	p := float64(now.Sub(start)) / float64(total)
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	barWidth := width - 4
	if barWidth < 10 {
		barWidth = 10
	}
	filled := int(p * float64(barWidth))
	var b strings.Builder
	for i := 0; i < barWidth; i++ {
		if i < filled {
			b.WriteString(a.st.BarFilled.Render("━"))
		} else {
			b.WriteString(a.st.BarEmpty.Render("─"))
		}
	}
	head := fmt.Sprintf("今日进度 %d%% · %s → %s · 按 enter 开始专注",
		int(p*100), clock.TimeOfDay(start), clock.DayEndLabel(end))
	return lipgloss.JoinVertical(lipgloss.Left, a.st.Muted.Render(truncate(head, width)), b.String())
}

// renderHints 渲染按键提示。
func (a *App) renderHints() string {
	pairs := [][2]string{
		{"tab", "切换栏"},
		{"j/k", "移动"},
		{"space", "勾选"},
		{"a", "添加"},
		{"t", "子任务"},
		{"r", "继承昨日"},
		{"?", "帮助"},
		{"q", "退出"},
	}
	var parts []string
	for _, p := range pairs {
		parts = append(parts, a.st.HintKey.Render(p[0])+a.st.Hint.Render(":"+p[1]))
	}
	line := strings.Join(parts, a.st.Hint.Render("  "))
	return truncate(line, a.width)
}

// ---------- 布局辅助 ----------

// panel 给内容套上面板边框，聚焦时高亮。
func (a *App) panel(focused bool, width, height int, content string) string {
	style := a.st.Panel
	if focused {
		style = a.st.PanelFocused
	}
	innerW := width - style.GetHorizontalFrameSize()
	innerH := height - style.GetVerticalFrameSize()
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	return style.Width(innerW).Height(innerH).Render(content)
}

// scrollOffset 计算列表滚动偏移，保证光标可见。
func scrollOffset(cursor, n, height int) int {
	if n <= height {
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

// center 在给定宽度内居中一段文本。
func center(s string, width, textWidth int) string {
	if textWidth >= width {
		return s
	}
	left := (width - textWidth) / 2
	return strings.Repeat(" ", left) + s
}

// pad 把文本右侧补空格到指定宽度。
func pad(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// wrap 按显示宽度折行。
func wrap(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > width {
			out = append(out, cur.String())
			cur.Reset()
			w = 0
		}
		cur.WriteRune(r)
		w += rw
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

// 确保 tea 包被使用（Update 的返回值类型依赖它）。
var _ tea.Model = (*App)(nil)
