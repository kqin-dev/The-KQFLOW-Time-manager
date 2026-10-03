package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/clock"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/model"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/version"
)

// 看板可用的最小终端尺寸；更小的时候只显示一行提示。
const (
	minWidth  = 60
	minHeight = 16
)

// View 渲染当前界面。
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "正在启动 Kairos…"
	}
	if a.width < minWidth || a.height < minHeight {
		return a.viewTooSmall()
	}

	// 选择框优先级最高：它常常是“在编辑器之上”弹出的确认（例如
	// 随手记没保存就问“保存还是丢弃”），必须盖住下面的输入框，
	// 否则用户看不到这个提问。
	if a.pick != nil {
		return clipBlock(a.renderCenterBox(a.pickContent()), a.width, a.height)
	}
	// 输入框次之：无论当前在哪一页，编辑中都要能看见自己在输入什么。
	if a.editor.active {
		return clipBlock(a.renderCenterBox(a.editorContent()), a.width, a.height)
	}
	// 自定义时段编辑器同样占用中间栏。
	if a.custom != nil {
		return clipBlock(a.renderCenterBox(a.customContent()), a.width, a.height)
	}

	switch a.view {
	case ViewHistory:
		styled, plain := a.historyLines()
		return clipBlock(a.renderCenterBox(a.pageContent(styled, plain, -1)), a.width, a.height)
	case ViewSettings:
		styled, plain := a.settingsLines()
		// 设置页让光标所在行始终可见。
		return clipBlock(a.renderCenterBox(a.pageContent(styled, plain, a.settingsCursorRow(styled, plain))), a.width, a.height)
	case ViewHelp:
		styled, plain := a.helpLines()
		return clipBlock(a.renderCenterBox(a.pageContent(styled, plain, -1)), a.width, a.height)
	case ViewCarry:
		return clipBlock(a.renderCenterBox(a.carryContent()), a.width, a.height)
	default:
		base := a.renderDashboard()
		// 计时结束时响一声，提醒正在别处工作的用户。
		if a.timer != nil && a.timer.consumeBell() {
			base = "\a" + base
		}
		return base
	}
}

// settingsCursorRow 返回设置页当前选中项在内容行里的下标。
func (a *App) settingsCursorRow(styled, plain []string) int {
	// 行数一一对应，直接按 marker 找更稳：选中行以 ▸ 开头。
	for i, p := range plain {
		if strings.HasPrefix(p, "▸") {
			return i
		}
	}
	return -1
}

// viewTooSmall 在终端过小时给出提示。
//
// 这段输出同样必须装进终端：早期它固定输出 5 行、每行 30 多列，
// 在一个 10×3 的窗口里会溢出并糊掉整屏。
func (a *App) viewTooSmall() string {
	if a.width <= 0 || a.height <= 0 {
		return "Kairos"
	}
	full := fmt.Sprintf("当前 %d×%d，至少需要 %d×%d", a.width, a.height, minWidth, minHeight)
	candidates := []string{
		a.st.Warn.Render(truncateCells("窗口太小", a.width)),
		a.st.Muted.Render(truncateCells(full, a.width)),
		a.st.Muted.Render(truncateCells("ctrl+c 退出", a.width)),
	}
	var lines []string
	for _, l := range candidates {
		if len(lines) >= a.height {
			break
		}
		if lipgloss.Width(l) == 0 {
			continue
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		return truncateCells("Kairos", a.width)
	}
	return clipBlock(strings.Join(lines, "\n"), a.width, a.height)
}

// columnLayout 计算三栏宽度与看板主体高度。
//
// 看板与中间栏内容（二级菜单、输入框、二级页）共用这一份计算，
// 保证它们永远落在中间栏里，不会盖住左右两侧的边框。
func (a *App) columnLayout() (leftW, centerW, rightW, bodyH int) {
	header := a.renderHeader()
	footer := a.renderFooter()
	bodyH = a.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyH < 6 {
		bodyH = 6
	}

	leftW, rightW = 34, 30
	if a.width < 110 {
		leftW, rightW = 28, 24
	}
	if a.width < 90 {
		leftW, rightW = 24, 22
	}
	// 中间栏至少要能完整放下完整版 Logo（44 列），否则字会被截断并折行。
	// 中间栏内容宽 = centerW - 4（边框 2 + 内边距 2）。
	const minCenterWidth = 48
	centerW = a.width - leftW - rightW
	if centerW < minCenterWidth {
		shrink := minCenterWidth - centerW
		takeLeft := shrink / 2
		takeRight := shrink - takeLeft
		if leftW-takeLeft < 18 {
			takeLeft = leftW - 18
			takeRight = shrink - takeLeft
		}
		if rightW-takeRight < 16 {
			takeRight = rightW - 16
			takeLeft = shrink - takeRight
		}
		leftW -= takeLeft
		rightW -= takeRight
		centerW = a.width - leftW - rightW
	}
	if centerW < 20 {
		centerW = 20
		leftW = max(18, a.width-centerW-rightW)
		rightW = max(0, a.width-leftW-centerW)
	}
	return leftW, centerW, rightW, bodyH
}

// renderDashboard 组装主看板：左 TODO、中选项、右 GOAL、下进度条（见需求 5）。
func (a *App) renderDashboard() string {
	header := a.renderHeader()
	footer := a.renderFooter()
	leftWidth, centerWidth, rightWidth, bodyHeight := a.columnLayout()

	left := a.renderLeftPanel(leftWidth, bodyHeight)
	right := a.renderGoalPanel(rightWidth, bodyHeight)
	center := a.renderCenterPanel(centerWidth, bodyHeight)

	if a.celebrate != nil {
		elapsed := time.Since(a.celebrate.started)
		t := float64(elapsed) / float64(celebrateFor)
		if t > 1 {
			t = 1
		}
		center = overlayBox(center, a.celebrateFrame(t, centerWidth, bodyHeight), centerWidth, bodyHeight)
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
	out := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	// 最后一道防线：整个看板必须正好装进终端。
	return clipBlock(out, a.width, a.height)
}

// renderCenterBox 把一段内容放进中间栏，两侧保留原来的 TODO 与 GOAL 面板。
//
// 二级菜单、输入框、帮助/设置/历史都走这条路：只占用中间栏，
// 既不会盖住左右面板的边框（那会把界面画花），渲染量也小得多。
func (a *App) renderCenterBox(content string) string {
	header := a.renderHeader()
	footer := a.renderFooter()
	leftWidth, centerWidth, rightWidth, bodyHeight := a.columnLayout()

	left := a.renderLeftPanel(leftWidth, bodyHeight)
	right := a.renderGoalPanel(rightWidth, bodyHeight)
	center := a.panel(false, centerWidth, bodyHeight, content)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
	return clipBlock(lipgloss.JoinVertical(lipgloss.Left, header, body, footer), a.width, a.height)
}

// overlayBox 在已经渲染好的面板文本上叠加另一段内容（用于庆祝特效）。
func overlayBox(base, top string, width, height int) string {
	baseLines := strings.Split(clipBlock(base, width, height), "\n")
	topLines := strings.Split(top, "\n")
	offset := (len(baseLines) - len(topLines)) / 2
	if offset < 0 {
		offset = 0
	}
	for i, l := range topLines {
		y := offset + i
		if y < 0 || y >= len(baseLines) {
			continue
		}
		baseLines[y] = truncateCells(l, width)
	}
	return strings.Join(baseLines, "\n")
}

// renderHeader 显示问候语、日期与今日专注时长（见需求 18）。
//
// 右侧的“今日专注”是用户最关心的信息，因此空间不够时优先压缩左侧的
// 问候语与日期，而不是让整行溢出去撑破下面的面板。
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

	greetText := greeting + "！"
	dateText := fmt.Sprintf("%s %s · %s · 日界线 %s",
		a.day, weekCN, now.Format("15:04:05"), clock.WallClock(cut))

	rightPlain := "今日专注 " + clock.HumanDuration(focus)
	right := a.st.Muted.Render("今日专注 ") + a.st.OK.Bold(true).Render(clock.HumanDuration(focus))

	// 先按完整内容排版；放不下时依次退让：先缩短日期，再截断问候语。
	leftPlainW := lipgloss.Width(greetText) + 2 + lipgloss.Width(dateText)
	avail := a.width - lipgloss.Width(rightPlain) - 1

	var left string
	switch {
	case leftPlainW <= avail:
		left = a.st.Title.Render(greetText) + "  " + a.st.Muted.Render(dateText)
	default:
		greetW := lipgloss.Width(greetText)
		dateW := avail - greetW - 2
		if dateW >= 12 {
			left = a.st.Title.Render(greetText) + "  " + a.st.Muted.Render(truncate(dateText, dateW))
		} else if avail > 6 {
			left = a.st.Title.Render(truncate(greetText, avail-1)) + " "
		} else {
			left = ""
		}
	}

	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	if w := lipgloss.Width(line); w > a.width {
		line = truncate(line, a.width)
	}

	if a.toast != "" {
		line += "\n" + a.toastLine()
	}
	return line
}

// renderLeftPanel 渲染 TODAY TODO 的上下两栏（见需求 13）。
//
// 固定与临时各自是一个带边框的子面板，这样 TAB 切换时能一眼看出焦点在哪一半。
// 早期两栏共用一个边框，切过去只有字色变化，看起来像没反应。
func (a *App) renderLeftPanel(width, height int) string {
	// 底部留一行显示总体完成度。
	stack := height - 1
	if stack < 6 {
		stack = 6
	}
	topH := stack / 2
	bottomH := stack - topH

	done, total := a.data.Counts()
	summary := a.st.Muted.Render(truncate(fmt.Sprintf("完成 %d/%d", done, total), width-2))
	if total > 0 && done == total {
		summary = a.st.OK.Bold(true).Render(truncate("✔ 全部完成 "+fmt.Sprintf("%d/%d", done, total), width-2))
	}

	fixedTitle := fmt.Sprintf("TODAY · 固定 (%d)", len(a.data.Fixed))
	floatTitle := fmt.Sprintf("TODAY · 临时 (%d)", len(a.data.Floating))

	fixed := a.renderTodoPanel(a.data.Fixed, FocusFixed, fixedTitle, width, topH)
	floating := a.renderTodoPanel(a.data.Floating, FocusFloating, floatTitle, width, bottomH)

	return lipgloss.JoinVertical(lipgloss.Left, fixed, floating, summary)
}

// renderTodoPanel 渲染一个带边框的待办子面板（固定或临时）。
func (a *App) renderTodoPanel(items []*model.Todo, which Focus, title string, width, height int) string {
	focused := a.focus == which
	innerW, innerH := a.panelInner(width, height)
	if innerW < 8 {
		innerW = 8
	}
	if innerH < 1 {
		innerH = 1
	}

	cursor := a.cursors.fixed
	if which == FocusFloating {
		cursor = a.cursors.floating
	}

	var lines []string
	// 焦点标记用字符而不是只靠边框颜色：低色彩终端会把两种边框色渲染成同一个，
	// 那样 TAB 切过去就完全看不出变化。
	marker := "  "
	if focused {
		marker = "▌ "
	}
	titleText := truncate(title, max(2, innerW-2))
	if focused {
		lines = append(lines, a.st.Title.Render(marker+titleText))
	} else {
		lines = append(lines, a.st.PanelTitle.Render(marker+titleText))
	}

	listRows := innerH - 1
	if listRows < 1 {
		listRows = 1
	}
	if len(items) == 0 {
		lines = append(lines, a.renderEmpty("按 a 添加", innerW))
	}
	offset := scrollOffset(cursor, len(items), listRows)

	for i := offset; i < len(items) && len(lines) < innerH; i++ {
		item := items[i]
		selected := focused && i == cursor
		lines = append(lines, a.renderTodoRow(item, selected, innerW))
		// 展开选中项的子任务，并支持在子任务里移动（见需求 8）。
		if selected && !a.collapsed[item.ID] {
			for ti, task := range item.Tasks {
				if len(lines) >= innerH {
					break
				}
				taskSelected := a.taskActive && ti == a.taskCursor
				mark := MarkTodo
				style := a.st.Muted
				if task.Done() {
					mark, style = MarkDone, a.st.DoneTag
				}
				branch := "   ├"
				if ti == len(item.Tasks)-1 {
					branch = "   └"
				}
				label := fmt.Sprintf("%s %s %s", branch, mark, task.Title)
				if taskSelected {
					lines = append(lines, a.st.RowCursor.Render(pad(truncate(label, innerW), innerW)))
					continue
				}
				lines = append(lines, style.Render(truncate(label, innerW)))
			}
		}
	}
	return a.panel(focused, width, height, strings.Join(lines, "\n"))
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
	title := fmt.Sprintf("GOAL (%d 进行中 · %d 今日完成)", active, len(a.data.Archive.Goals))

	list := a.goalList()
	var lines []string
	lines = append(lines, a.st.PanelTitle.Render(truncate(title, inner)))
	if len(list) == 0 {
		lines = append(lines, a.renderEmpty("按 A 添加目标", inner))
	}

	listHeight := height - 1
	if listHeight < 1 {
		listHeight = 1
	}
	cursor := a.cursors.goals
	offset := scrollOffset(cursor, len(list), listHeight)

	for i := offset; i < len(list) && len(lines) < listHeight; i++ {
		g := list[i]
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
		// 标记出归档到当天的目标，它们已经不在 goals.json 里。
		if g.ArchivedDay != "" {
			label += " ⌂"
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
				meta += " · 已归档到 " + g.ArchivedDay
			}
			if len(lines) < listHeight {
				lines = append(lines, a.st.Muted.Render(truncate(meta, inner)))
			}
			if !a.collapsed[g.ID] {
				for ti, task := range g.Tasks {
					if len(lines) >= listHeight {
						break
					}
					tmark := MarkTodo
					tstyle := a.st.Muted
					if task.Done() {
						tmark, tstyle = MarkDone, a.st.DoneTag
					}
					branch := "   ├"
					if ti == len(g.Tasks)-1 {
						branch = "   └"
					}
					label := fmt.Sprintf("%s %s %s", branch, tmark, task.Title)
					if a.taskActive && ti == a.taskCursor {
						lines = append(lines, a.st.RowCursor.Render(pad(truncate(label, inner), inner)))
						continue
					}
					lines = append(lines, tstyle.Render(truncate(label, inner)))
				}
			}
		}
	}
	return a.panel(a.focus == FocusGoals, width, height, strings.Join(lines, "\n"))
}

// renderCenterPanel 渲染中间栏：Logo、状态、选项、字条。
func (a *App) renderCenterPanel(width, height int) string {
	return a.panel(false, width, height, a.centerContent(width, height))
}

// centerContent 生成中间栏的内容行。
//
// 行数绝不会超过面板能装下的行数，而且结尾的字条永远保留：
// 之前是“先拼内容再补空行、最后放字条”，终端一矮字条就被裁掉，
// 而它恰好是需求 5 明确要求常驻的元素。
func (a *App) centerContent(width, height int) string {
	inner := width - 4
	if inner < 10 {
		inner = 10
	}
	// 面板内部可用行数：扣掉上下边框。
	bodyRows := height - a.st.Panel.GetVerticalFrameSize()
	if bodyRows < 1 {
		bodyRows = 1
	}

	// Logo：按可用宽度自动选字形，绝不让它折行。
	logo := GradientLogo(a.st.Theme.Primary, a.st.Theme.Secondary, a.animPhase, inner)
	var logoLines []string
	if logo != "" {
		logoLines = strings.Split(logo, "\n")
	}

	quote := "「" + a.CurrentQuote() + "」"
	quoteLines := wrapBalanced(quote, inner)
	// 字条是必须保留的尾部块。
	if len(quoteLines) > bodyRows {
		quoteLines = quoteLines[:bodyRows]
	}
	budget := bodyRows - len(quoteLines)

	// 先组织可选内容（Logo、状态、菜单、计时摘要），空行只作为分隔。
	var body []string
	maxTop := 2
	if budget < len(logoLines)+10 {
		maxTop = 0
	}
	for i := 0; i < maxTop; i++ {
		body = append(body, "")
	}
	for _, l := range logoLines {
		body = append(body, center(fitText(l, inner), inner, lipgloss.Width(fitText(l, inner))))
	}
	body = append(body, "")

	done, total := a.data.Counts()
	status := fmt.Sprintf("今日待办 %d/%d", done, total)
	if total > 0 && done == total {
		status += " · 已全部完成"
	}
	body = append(body, fit(a.st.Muted, center(status, inner, lipgloss.Width(status)), inner))
	body = append(body, "")

	for i, item := range menuItems {
		label := "  " + item.Label
		if a.focus == FocusMenu && i == a.cursors.menu {
			body = append(body, pad(fit(a.st.MenuCursor, label, inner), inner))
		} else {
			body = append(body, fit(a.st.Menu, label, inner))
		}
	}
	body = append(body, "")

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
		body = append(body, fit(a.st.Accent, center(info, inner, lipgloss.Width(info)), inner))
	} else {
		info := "按 enter 开始专注"
		body = append(body, fit(a.st.Muted, center(info, inner, lipgloss.Width(info)), inner))
	}

	// 剩余空间放“连续 7 天统计”和随手记预览。
	//
	// 两者都想要，但用户的诉求是“要看得到柱状图”，所以按下面的优先级分配：
	//   1. 先给柱状图留够最小高度（表头 + 柱 + 轴 + 标签）；
	//   2. 再给随手记预览，行数随剩余空间伸缩（3 → 2 → 1 行）；
	//   3. 两者都放不下时保柱状图，随手记只留标题。
	var optional []string
	room := budget - len(body)
	noteLines := 0
	if a.showNoteOnBoard() && room > 0 {
		// 柱状图至少要 minStats 行才画得出形状。
		const minStats = 5
		for n := 3; n >= 1; n-- {
			note := a.notePreviewLines(inner, n)
			if len(note) == 0 {
				break
			}
			if room-(len(note)+1) >= minStats {
				optional = append(optional, note...)
				optional = append(optional, "")
				noteLines = len(note) + 1
				break
			}
			if n == 1 {
				// 空间实在不够：只留随手记标题（1 行），其余给统计。
				optional = append(optional, note[:1]...)
				optional = append(optional, "")
				noteLines = 2
			}
		}
	}
	if room > 0 {
		if s := a.statsLines(inner, room-noteLines); len(s) > 0 {
			optional = append(optional, s...)
			optional = append(optional, "")
		}
	}
	if room > 0 && len(optional) > 0 {
		if len(optional) <= room {
			body = append(body, optional...)
		} else {
			body = append(body, optional[:room]...)
		}
	}

	// 超高时先丢空行（只丢多余的分隔，不丢有内容的行），再丢末尾内容行。
	body = trimBlankLines(body, budget)

	// 剩余空间全部用来把字条推到底部，不多不少正好铺满面板。
	pad := bodyRows - len(body) - len(quoteLines)
	if pad < 0 {
		pad = 0
	}
	lines := make([]string, 0, bodyRows)
	lines = append(lines, body...)
	for i := 0; i < pad; i++ {
		lines = append(lines, "")
	}
	for _, ql := range quoteLines {
		lines = append(lines, fit(a.st.Muted, center(ql, inner, lipgloss.Width(ql)), inner))
	}
	return strings.Join(lines, "\n")
}

// showNoteOnBoard 报告用户是否选择在看板上展示随手记。
func (a *App) showNoteOnBoard() bool { return a.cfg.ShowNote }

// fitText 按显示宽度截断纯文本。
func fitText(s string, width int) string { return truncate(s, width) }

// trimBlankLines 把行数压到最多 n 行：先去掉空行，再从末尾截断。
func trimBlankLines(lines []string, n int) []string {
	if n < 0 {
		n = 0
	}
	if len(lines) <= n {
		return lines
	}
	kept := make([]string, 0, n)
	for i, l := range lines {
		if l == "" && i != len(lines)-1 {
			continue
		}
		kept = append(kept, l)
		if len(kept) == n {
			break
		}
	}
	if len(kept) > n {
		kept = kept[:n]
	}
	return kept
}

// renderFooter 渲染底部时段看条（见需求 20）。
func (a *App) renderFooter() string {
	var rows []string
	if bar := a.renderTimerBar(a.width); bar != "" {
		rows = append(rows, bar)
	}
	rows = append(rows, a.versionedHints())
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// versionedHints 渲染按键提示行，并在右端放上版本号。
//
// 版本号随每次发布更新（见 internal/version），放在这里既随时可见，
// 又不会占用中间栏的内容空间。窄终端上提示行已经占满，版本号就单独
// 占一行——它不该因为屏幕小就消失（排查问题时最先要问的就是版本）。
func (a *App) versionedHints() string {
	hints := a.renderHints()
	ver := version.String()
	gap := a.width - lipgloss.Width(hints) - lipgloss.Width(ver)
	if hints == "" {
		return a.st.Muted.Render(ver)
	}
	if gap < 1 {
		// 放不下就换一行，而不是不显示。
		return lipgloss.JoinVertical(lipgloss.Left, hints, a.st.Muted.Render(ver))
	}
	return hints + strings.Repeat(" ", gap) + a.st.Muted.Render(ver)
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
	head := fmt.Sprintf("今日已过 %d%% · %s · 按 enter 开始专注",
		int(p*100), dayRangeLabel(start, end, cut))
	return lipgloss.JoinVertical(lipgloss.Left, a.st.Muted.Render(truncate(head, width)), b.String())
}

// dayRangeLabel 返回逻辑日的起止展示文本。
//
// 逻辑日通常跨到次日，起止时刻看起来一样（例如 04:00 → 04:00），
// 容易让人以为跨度为零，所以这种情况下标出“次日”。
// 日界线为 00:00 时它就是一个自然日（00:00 → 24:00），不加“次日”；
// 注意此时 DayEnd 落在次日 00:00，单看日历日是“下一天”，
// 所以不能只按日历日判断，否则会出现“次日 24:00”这种写法。
func dayRangeLabel(start, end time.Time, cut time.Duration) string {
	if cut > 0 && start.Day() != end.Day() {
		return clock.TimeOfDay(start) + " → 次日 " + clock.DayEndLabel(end)
	}
	return clock.TimeOfDay(start) + " → " + clock.DayEndLabel(end)
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

// clipLines 把多行文本截断到最多 n 行。
//
// lipgloss 的 Height 只保证“至少这么高”，内容更多时面板会被撑高，
// 于是一个小窗口也能渲染出超过终端高度的界面，把画面顶出可视区域。
func clipLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n")
}

// clipBlock 把渲染结果裁剪到指定宽高，作为整个界面的最后一道防线。
func clipBlock(s string, width, height int) string {
	if width <= 0 || height <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, l := range lines {
		if lipgloss.Width(l) > width {
			lines[i] = truncateCells(l, width)
		}
	}
	return strings.Join(lines, "\n")
}

// panel 给内容套上面板边框，聚焦时高亮，并保证外层尺寸正好是 width×height。
//
// lipgloss 的 Width 语义在不同版本里并不直观（它既影响内容区又影响外框），
// 这里改为渲染后实测一次外框宽度，必要时再校正一次，确保三个栏位的边界对齐。
func (a *App) panel(focused bool, width, height int, content string) string {
	style := a.st.Panel
	if focused {
		style = a.st.PanelFocused
	}
	innerH := height - style.GetVerticalFrameSize()
	if innerH < 1 {
		innerH = 1
	}
	content = padOrClipLines(content, innerH)

	// 内容区目标宽度：总数减去边框与内边距，再留给 Width 自行处理。
	contentW := width - style.GetHorizontalFrameSize()
	if contentW < 1 {
		contentW = 1
	}
	out := style.Width(contentW).Render(content)

	// 实测外框宽度，偏差时按差值校正一次。
	const maxFix = 4
	for i := 0; i < maxFix; i++ {
		got := lipgloss.Width(out)
		if got == width {
			break
		}
		delta := width - got
		next := contentW + delta
		if next < 1 {
			next = 1
		}
		if next == contentW {
			break
		}
		contentW = next
		out = style.Width(contentW).Render(content)
	}
	return out
}

// padOrClipLines 把内容调整为正好 n 行：多了从末尾截断，少了用空行补齐。
func padOrClipLines(s string, n int) string {
	if n < 1 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// fit 把纯文本截断到指定显示宽度，再交给样式渲染。
//
// 必须先截断纯文本再上色：上色后带有 ANSI 序列，再按显示宽度裁会破坏转义，
// 而留着超宽的行会被 lipgloss 折行，把面板撑高。
func fit(style lipgloss.Style, text string, width int) string {
	return style.Render(truncate(text, width))
}

// panelInner 返回面板内容区可用的宽高。
func (a *App) panelInner(width, height int) (int, int) {
	frameH := a.st.Panel.GetHorizontalFrameSize()
	frameV := a.st.Panel.GetVerticalFrameSize()
	w := width - frameH
	h := height - frameV
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
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
			out = append(out, strings.TrimRight(cur.String(), " "))
			cur.Reset()
			w = 0
		}
		cur.WriteRune(r)
		w += rw
	}
	if cur.Len() > 0 {
		out = append(out, strings.TrimRight(cur.String(), " "))
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

// wrapBalanced 尽量把文本折成宽度均衡的几行。
//
// 中文没有空格可依，按最大宽度贪心折行很容易出现“最后一行只剩一个句号”，
// 所以这里尝试多个目标宽度，取行数最少、且各行长度最接近的方案。
// 行数少优先，避免把一句话拆得七零八落。
func wrapBalanced(s string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{s}
	}
	if lipgloss.Width(s) <= maxWidth {
		return []string{s}
	}
	best := wrap(s, maxWidth)
	bestScore := lineSpread(best)
	for target := maxWidth - 1; target >= maxWidth/2; target-- {
		candidate := wrap(s, target)
		score := lineSpread(candidate)
		if len(candidate) < len(best) || (len(candidate) == len(best) && score < bestScore) {
			best, bestScore = candidate, score
		}
	}
	return best
}

// lineSpread 返回各行显示宽度中最大值与最小值的差，用来衡量折行是否均衡。
func lineSpread(lines []string) int {
	if len(lines) == 0 {
		return 0
	}
	lo, hi := -1, 0
	for _, l := range lines {
		w := lipgloss.Width(l)
		if lo < 0 || w < lo {
			lo = w
		}
		if w > hi {
			hi = w
		}
	}
	return hi - lo
}

// 确保 tea 包被使用（Update 的返回值类型依赖它）。
var _ tea.Model = (*App)(nil)
