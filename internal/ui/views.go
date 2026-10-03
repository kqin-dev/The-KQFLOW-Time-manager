package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/clock"
)

// ---------- 通用浮层 ----------

// IsCompact 报告当前终端是否够窄，需要收紧浮层内边距。
func (a *App) IsCompact() bool { return a.width < compactWidth }

// overlayInner 返回浮层内容区实际可用的字符数。
//
// 看板按“整个终端宽度”排版面板，而浮层是套在自己的边框里再叠上去的，
// 所以浮层内容必须再扣掉自身边框与内边距，否则会横向超出看板宽度。
// 这里再留 1 列余量，避免正好贴着终端右边缘。
func (a *App) overlayInner() int {
	frame := a.st.modalStyle(a.IsCompact()).GetHorizontalFrameSize()
	if f := a.st.pageStyle(a.IsCompact()).GetHorizontalFrameSize(); f > frame {
		frame = f
	}
	avail := a.width - frame - 1
	if avail < 8 {
		avail = 8
	}
	return avail
}

// modalInner 返回浮层内容区可用的字符数，prefer 是内容偏好的宽度。
func (a *App) modalInner(prefer int) int {
	avail := a.overlayInner()
	if prefer > avail {
		return avail
	}
	return prefer
}

// modalLine 渲染浮层的一行，超宽时截断，保证不会撑破边框。
func (a *App) modalLine(style lipgloss.Style, text string, inner int) string {
	return style.Render(truncate(text, inner))
}

// overlay 把 modal 叠在 base 之上并居中。
//
// 关键点：base 每行的“字符数”与“显示宽度”并不相等（Logo 用了 U+2001 等宽字符，
// 界面里又有中文字符），所以裁剪必须按显示宽度逐列进行，不能直接用 rune 下标。
func overlay(base, modal string, width, height int) string {
	if width <= 0 || height <= 0 {
		return modal
	}
	baseLines := strings.Split(base, "\n")
	for len(baseLines) < height {
		baseLines = append(baseLines, "")
	}
	for i, l := range baseLines {
		if w := lipgloss.Width(l); w < width {
			baseLines[i] = l + strings.Repeat(" ", width-w)
		}
	}

	modalLines := strings.Split(modal, "\n")
	modalW := 0
	for _, l := range modalLines {
		if w := lipgloss.Width(l); w > modalW {
			modalW = w
		}
	}
	startY := (len(baseLines) - len(modalLines)) / 2
	if startY < 0 {
		startY = 0
	}
	startX := (width - modalW) / 2
	if startX < 0 {
		startX = 0
	}

	for i, ml := range modalLines {
		y := startY + i
		if y < 0 || y >= len(baseLines) {
			continue
		}
		baseLines[y] = spliceCells(baseLines[y], ml, startX)
	}

	// 兜底：无论浮层怎么算，合成结果都不能超过终端宽度，
	// 否则会撑破看板边框，把界面撕成两半。
	for i, l := range baseLines {
		if w := lipgloss.Width(l); w > width {
			baseLines[i] = truncateCells(l, width)
		}
	}
	return strings.Join(baseLines, "\n")
}

// truncateCells 按显示宽度截断字符串（不追加省略号），保证结果不超过 width 列。
//
// truncate 会在末尾补一个省略号，用在这里可能让宽度再超出一列，所以单独实现。
func truncateCells(s string, width int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > width {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String()
}

// spliceCells 用 repl 覆盖 s 中从第 col 个显示列开始的区域，其余内容原样保留。
func spliceCells(s, repl string, col int) string {
	var before, after strings.Builder
	replW := lipgloss.Width(repl)
	end := col + replW
	w := 0
	placed := false
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if !placed {
			if w+rw <= col {
				before.WriteRune(r)
				w += rw
				continue
			}
			placed = true
			if w < col {
				// 插入点落在宽字符中间，先补空格对齐，避免整体错位。
				before.WriteString(strings.Repeat(" ", col-w))
				w = col
			}
		}
		if w >= end {
			after.WriteRune(r)
		}
		w += rw
	}
	return before.String() + repl + after.String()
}

// modalBox 按当前终端宽度渲染浮层内容，宽度跟随实际内边距。
func (a *App) modalBox(inner int, lines []string) string {
	style := a.st.modalStyle(a.IsCompact())
	return style.Width(inner + style.GetHorizontalFrameSize()).Render(strings.Join(lines, "\n"))
}

// renderPick 渲染选择框。
func (a *App) renderPick() string {
	inner := 0
	for _, it := range a.pick.items {
		if w := lipgloss.Width(it.Label); w > inner {
			inner = w
		}
	}
	if w := lipgloss.Width(a.pick.title); w > inner {
		inner = w
	}
	inner = a.modalInner(inner)

	var lines []string
	lines = append(lines, a.modalLine(a.st.ModalTitle, a.pick.title, inner))
	lines = append(lines, "")
	for i, it := range a.pick.items {
		if i == a.pick.cursor {
			lines = append(lines, a.st.ModalCursor.Render(pad("  "+truncate(it.Label, inner), inner)))
		} else {
			lines = append(lines, a.st.Text.Render(truncate("  "+it.Label, inner)))
		}
	}
	lines = append(lines, "")
	lines = append(lines, a.modalLine(a.st.Muted, "j/k 选择 · enter 确认 · esc 取消", inner))
	return a.modalBox(inner, lines)
}

// renderEditor 渲染文本输入框。
func (a *App) renderEditor() string {
	inner := a.modalInner(44)
	value := string(a.editor.value)
	var shown string
	// 输入内容较长时，保持光标可见。
	if a.editor.cursor > inner-2 {
		start := a.editor.cursor - (inner - 2)
		runes := a.editor.value
		if start > len(runes) {
			start = len(runes)
		}
		shown = string(runes[start:])
	} else {
		shown = value
	}
	cursorPos := a.editor.cursor
	if cursorPos > inner-2 {
		cursorPos = inner - 2
	}
	runes := []rune(shown)
	before := string(runes[:min(cursorPos, len(runes))])
	after := ""
	if cursorPos < len(runes) {
		after = string(runes[cursorPos:])
	}
	input := before + a.st.BarCursor.Render("▏") + after

	lines := []string{
		a.modalLine(a.st.ModalTitle, a.editor.label, inner),
		"",
		a.st.Text.Render(truncate(input, inner)),
		"",
		a.modalLine(a.st.Muted, "enter 确认 · esc 取消 · ctrl+u 清空", inner),
	}
	return a.modalBox(inner, lines)
}

// pageWidth 返回整页浮层可用的内容宽度：不超过终端，也不比内容更宽。
func (a *App) pageWidth(lines []string, prefer int) int {
	natural := 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > natural {
			natural = w
		}
	}
	if prefer > 0 && natural > prefer {
		natural = prefer
	}
	avail := a.overlayInner()
	if natural > avail {
		natural = avail
	}
	if natural < 8 {
		natural = 8
	}
	return natural
}

// renderPage 渲染整页浮层，超宽的行会被截断，保证不会撑破面板。
func (a *App) renderPage(lines []string, prefer int) string {
	inner := a.pageWidth(lines, prefer)
	fitted := make([]string, 0, len(lines))
	for _, l := range lines {
		fitted = append(fitted, truncate(l, inner))
	}
	body := a.st.pageStyle(a.IsCompact()).Width(inner).Render(strings.Join(fitted, "\n"))
	return overlay(a.renderDashboard(), body, a.width, a.height)
}

// ---------- 设置页 ----------

func (a *App) renderSettings() string {
	rows := [][2]string{
		{"日界线（新的一天从几点开始）", a.cfg.DayCutoff},
		{"默认专注时长", fmt.Sprintf("%d 分钟", a.cfg.DefaultFocus)},
		{"默认休息时长", fmt.Sprintf("%d 分钟", a.cfg.DefaultBreak)},
		{"昵称", orDash(a.cfg.Nickname)},
		{"数据目录", a.store.Root()},
		{"配置文件", a.pathsForSave().ConfigFile},
		{"时区", orDash(a.cfg.Timezone)},
	}

	var lines []string
	lines = append(lines, a.st.Title.Render("设置 / Settings"))
	lines = append(lines, a.st.Muted.Render("熬夜用户可以把日界线设为 04:00，凌晨 2 点仍算前一天。"))
	lines = append(lines, "")
	for _, r := range rows {
		lines = append(lines, a.st.Text.Render("  "+r[0]+"  ")+a.st.Accent.Render(r[1]))
	}
	lines = append(lines, "")
	lines = append(lines, a.st.Muted.Render("  c 改日界线 · f 改专注时长 · b 改休息时长 · n 改昵称"))
	lines = append(lines, a.st.Muted.Render("  esc / q 返回看板"))

	return a.renderPage(lines, 90)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ---------- 历史页 ----------

func (a *App) handleHistoryKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q", "enter":
		a.view = ViewDashboard
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	}
	return a, nil
}

// historyStat 是一天的汇总统计。
type historyStat struct {
	Day       string
	Done      int
	Total     int
	Focus     time.Duration
	Rest      time.Duration
	Goals     int
	Sessions  int
	TopItem   string
	TopAmount time.Duration
}

// collectHistory 汇总最近若干天的数据（见需求 11）。
func (a *App) collectHistory(limit int) ([]historyStat, error) {
	days, err := a.store.RecentDays(limit)
	if err != nil {
		return nil, err
	}
	stats := make([]historyStat, 0, len(days))
	for _, day := range days {
		data, err := a.store.Day(day)
		if err != nil || data == nil {
			continue
		}
		done, total := data.Counts()
		focus, rest := data.FocusTotal()
		st := historyStat{
			Day:      day,
			Done:     done,
			Total:    total,
			Focus:    focus,
			Rest:     rest,
			Goals:    len(data.Archive.Goals),
			Sessions: len(data.Archive.Sessions),
		}
		// 找出当日投入最多的条目。
		type kv struct {
			name string
			dur  time.Duration
		}
		var all []kv
		for name, act := range data.Activity {
			all = append(all, kv{name, act.Total})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].dur > all[j].dur })
		if len(all) > 0 {
			st.TopItem, st.TopAmount = all[0].name, all[0].dur
		}
		stats = append(stats, st)
	}
	return stats, nil
}

func (a *App) renderHistory() string {
	stats, err := a.collectHistory(14)
	var lines []string
	lines = append(lines, a.st.Title.Render("历史 / History"))
	if err != nil {
		lines = append(lines, a.st.Error.Render("读取历史失败："+err.Error()))
	}
	if len(stats) == 0 {
		lines = append(lines, a.st.Muted.Render("还没有历史数据，完成一些 TODO 或专注一段时间后再来看。"))
	}
	lines = append(lines, "")

	// 先按终端宽度决定各列宽度，窄终端下自动收窄“最投入的条目”。
	inner := a.width - a.st.PanelFocused.GetHorizontalFrameSize() - 4
	if inner > 104 {
		inner = 104
	}
	if inner < 30 {
		inner = 30
	}
	topW := inner - 48
	if topW > 34 {
		topW = 34
	}
	if topW < 6 {
		topW = 6
	}

	header := fmt.Sprintf("  %-12s %-8s %-10s %-6s %s", "日期", "TODO", "专注", "GOAL", "最投入的条目")
	lines = append(lines, a.st.PanelTitle.Render(truncate(header, inner)))

	var totalFocus time.Duration
	var totalDone int
	for i := len(stats) - 1; i >= 0; i-- {
		st := stats[i]
		totalFocus += st.Focus
		totalDone += st.Done
		ratio := "—"
		if st.Total > 0 {
			ratio = fmt.Sprintf("%d/%d", st.Done, st.Total)
		}
		top := "—"
		if st.TopItem != "" {
			top = fmt.Sprintf("%s（%s）", truncate(st.TopItem, topW), clock.HumanDuration(st.TopAmount))
		}
		row := fmt.Sprintf("  %-12s %-8s %-10s %-6d %s",
			st.Day, ratio, clock.HumanDuration(st.Focus), st.Goals, top)
		lines = append(lines, a.st.Text.Render(truncate(row, inner)))
	}

	lines = append(lines, "")
	lines = append(lines, a.st.OK.Render(truncate(
		fmt.Sprintf("  合计：完成 %d 项 TODO，专注 %s", totalDone, clock.HumanDuration(totalFocus)), inner)))
	lines = append(lines, "")
	lines = append(lines, a.st.Muted.Render("  esc / q 返回看板"))

	return a.renderPage(lines, 110)
}

// ---------- 帮助页 ----------

func (a *App) renderHelp() string {
	groups := []struct {
		title string
		keys  [][2]string
	}{
		{"导航", [][2]string{
			{"tab / shift+tab", "在中间、左固定、左临时、右 GOAL 之间切换"},
			{"h / l", "左右切换栏"},
			{"j / k", "上下移动"},
			{"g / G", "跳到首项 / 末项"},
			{"1 / 2 / 3", "直接进入中间菜单 / 固定 TODO / 临时 TODO / GOAL"},
		}},
		{"编辑", [][2]string{
			{"space / enter", "勾选完成（GOAL 勾选后自动归档到本日）"},
			{"a", "添加 TODO（焦点在固定栏则添加固定 TODO）"},
			{"A", "添加 GOAL"},
			{"e", "重命名选中条目"},
			{"t", "为选中条目添加子任务"},
			{"d", "删除选中条目（会先确认）"},
			{"r", "从昨日继承（固定 / 未完成 / 两者）"},
		}},
		{"计时", [][2]string{
			{"enter（中间栏）", "打开计时菜单：番茄钟 / 倒计时 / 正计时 / 自定义"},
			{"space", "计时中暂停或继续"},
			{"enter", "计时中结束并归档到所属 TODO"},
			{"esc", "计时中中断，已用时长仍会记录"},
		}},
		{"其它", [][2]string{
			{"?", "打开 / 关闭本帮助"},
			{"q / esc", "返回看板，或退出"},
			{"ctrl+c", "随时退出（进行中的计时会先记录再退出）"},
		}},
	}

	var lines []string
	lines = append(lines, a.st.Title.Render("帮助 / Help"))
	lines = append(lines, "")
	for _, g := range groups {
		lines = append(lines, a.st.Accent.Bold(true).Render("  "+g.title))
		for _, kv := range g.keys {
			lines = append(lines, "  "+a.st.HintKey.Render(pad(kv[0], 22))+a.st.Text.Render(kv[1]))
		}
		lines = append(lines, "")
	}
	lines = append(lines, a.st.Muted.Render("  esc / q / ? 返回看板"))

	return a.renderPage(lines, 92)
}

// ---------- 继承确认页 ----------

func (a *App) renderCarry() string {
	inner := a.modalInner(56)
	var lines []string
	lines = append(lines, a.modalLine(a.st.Title, "新的一天开始了", inner))
	lines = append(lines, "")
	lines = append(lines, a.modalLine(a.st.Text,
		fmt.Sprintf("今天是 %s，昨天是 %s。", a.day, a.prevDay), inner))

	prev, err := a.store.Day(a.prevDay)
	if err != nil {
		lines = append(lines, a.modalLine(a.st.Error, "读取昨日数据失败："+err.Error(), inner))
	}
	if prev != nil {
		unfinished := 0
		for _, t := range prev.Floating {
			if !t.Done {
				unfinished++
			}
		}
		lines = append(lines, "")
		lines = append(lines, a.modalLine(a.st.Muted,
			fmt.Sprintf("昨日固定 TODO %d 项，未完成的临时 TODO %d 项。", len(prev.Fixed), unfinished), inner))
	}
	lines = append(lines, "")
	lines = append(lines, a.modalLine(a.st.Text, "y  两者都继承（推荐）", inner))
	lines = append(lines, a.modalLine(a.st.Text, "f  只拉取昨日固定 TODO", inner))
	lines = append(lines, a.modalLine(a.st.Text, "x  只继承昨日未完成的 TODO", inner))
	lines = append(lines, a.modalLine(a.st.Text, "n  都不继承，从空白开始", inner))
	lines = append(lines, "")

	body := a.modalBox(inner, lines)
	return overlay(a.renderDashboard(), body, a.width, a.height)
}
