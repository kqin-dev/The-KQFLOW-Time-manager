package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/version"
)

// ---------- 尺寸与文本工具 ----------

// IsCompact 报告当前终端是否够窄，需要收紧排版。
func (a *App) IsCompact() bool { return a.width < compactWidth }

// contentWidth 返回中间栏内容区可用的字符数。
//
// 二级菜单、输入框、帮助/设置/历史都只占中间栏，所以宽度都以它为准。
func (a *App) contentWidth() int {
	_, centerW, _, _ := a.columnLayout()
	w := centerW - a.st.Panel.GetHorizontalFrameSize()
	if w < 8 {
		w = 8
	}
	return w
}

// modalLine 渲染一行内容，超宽时截断。
func (a *App) modalLine(style lipgloss.Style, text string, inner int) string {
	return style.Render(truncate(text, inner))
}

// truncateCells 按显示宽度截断字符串（不追加省略号），保证结果不超过 width 列。
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

// ---------- 中间栏内容 ----------

// scrollIntoView 保证 keepVisible 行在窗口内，必要时调整偏移。
func scrollIntoView(offset, total, height, keepVisible int) (int, int) {
	if height < 1 {
		height = 1
	}
	maxOffset := total - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	if keepVisible >= 0 {
		if keepVisible < offset {
			offset = keepVisible
		}
		if keepVisible >= offset+height {
			offset = keepVisible - height + 1
		}
		if offset > maxOffset {
			offset = maxOffset
		}
		if offset < 0 {
			offset = 0
		}
	}
	start, end := offset, offset+height
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	return start, end
}

// pageContent 把整页内容裁剪成能放进中间栏的文本块，并按需滚动。
//
// keepVisible 是要保持在可视区内的行号（例如设置页的光标），-1 表示不关心。
func (a *App) pageContent(styled, _ []string, keepVisible int) string {
	inner := a.contentWidth()
	fitted := make([]string, 0, len(styled))
	for _, l := range styled {
		fitted = append(fitted, truncateCells(l, inner))
	}

	// 可用行数 = 中间栏主体高度 - 面板边框。
	_, _, _, bodyH := a.columnLayout()
	avail := bodyH - a.st.Panel.GetVerticalFrameSize()
	if avail < 1 {
		avail = 1
	}
	start, end := scrollIntoView(a.pageScroll, len(fitted), avail, keepVisible)
	a.pageScroll = start
	return strings.Join(fitted[start:end], "\n")
}

// editorContent 渲染输入框内容。
//
// 关键点：必须实时显示用户正在输入的字符。光标用可见字符而不是“反白的空格”，
// 低色彩终端下反白背景会被降级掉，那样光标就完全看不见。
// 多行模式（自定义字条）逐行显示，每行一个条目。
func (a *App) editorContent() string {
	inner := a.contentWidth()
	var lines []string
	lines = append(lines, a.st.ModalTitle.Render(truncate(a.editor.label, inner)))

	// 可用行数：中间栏主体高度减去标题、空行与提示行。
	_, _, _, bodyH := a.columnLayout()
	avail := bodyH - a.st.Panel.GetVerticalFrameSize()
	maxValueLines := avail - 4
	if maxValueLines < 1 {
		maxValueLines = 1
	}

	lines = append(lines, "")
	if a.editor.multiline {
		lines = append(lines, a.multilineEditorLines(inner, maxValueLines)...)
	} else {
		lines = append(lines, a.singleLineEditor(inner))
	}
	lines = append(lines, "")
	if a.editor.multiline {
		lines = append(lines, a.st.Muted.Render(truncate("enter 换行 · ctrl+s 保存 · esc 取消", inner)))
	} else {
		lines = append(lines, a.st.Muted.Render(truncate("enter 确认 · esc 取消 · ctrl+u 清空", inner)))
	}
	return strings.Join(lines, "\n")
}

// singleLineEditor 渲染单行输入。
func (a *App) singleLineEditor(inner int) string {
	// 内容超宽时只显示光标附近的一段，保证光标始终可见。
	start := 0
	if a.editor.cursor > inner-2 {
		start = a.editor.cursor - (inner - 2)
	}
	if start > len(a.editor.value) {
		start = len(a.editor.value)
	}
	shown := a.editor.value[start:]
	cursorPos := a.editor.cursor - start
	runes := []rune(string(shown))
	if cursorPos < 0 {
		cursorPos = 0
	}
	if cursorPos > len(runes) {
		cursorPos = len(runes)
	}
	before := string(runes[:cursorPos])
	after := ""
	if cursorPos < len(runes) {
		after = string(runes[cursorPos:])
	}
	cursor := "▏"
	input := a.st.Text.Render(before) + a.st.ModalCursor.Render(cursor) + a.st.Text.Render(after)
	if cursorPos >= len(runes) {
		input += " "
	}
	return truncateCells(input, inner)
}

// multilineEditorLines 渲染多行输入，只显示光标附近的若干行。
//
// 光标位置按“显示宽度”计算，不能按 rune 个数：中文一个字占两列，
// 用 rune 下标定位会让画出来的光标跑到文字前面去（用户报过这个问题）。
func (a *App) multilineEditorLines(inner, maxLines int) []string {
	all := strings.Split(string(a.editor.value), "\n")

	// 光标所在行号 = 光标之前的换行数。
	// 关键：这里必须按 rune 切片，不能直接对字符串做 text[:cursor]——
	// 字符串切片走的是字节偏移，而 cursor 是 rune 下标，
	// 一旦有中文就会切在半个字符上，行号与列位全错（这正是用户报的 bug）。
	runes := a.editor.value
	cur := clamp(a.editor.cursor, 0, len(runes))
	line := 0
	lineStartRune := 0
	for i := 0; i < cur; i++ {
		if runes[i] == '\n' {
			line++
			lineStartRune = i + 1
		}
	}
	if line >= len(all) {
		line = len(all) - 1
	}
	if line < 0 {
		line = 0
	}
	// 光标在其所在行内的 rune 偏移。
	colRune := cur - lineStartRune

	start := 0
	if line >= maxLines {
		start = line - maxLines + 1
	}
	end := start + maxLines
	if end > len(all) {
		end = len(all)
	}

	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		marker := "   "
		if i == line {
			marker = " ▸ "
		}
		body := all[i]
		if i == line {
			lineRunes := []rune(body)
			if colRune < 0 {
				colRune = 0
			}
			if colRune > len(lineRunes) {
				colRune = len(lineRunes)
			}
			// 光标列 = 光标前内容的显示宽度（中文占两列，不能用 rune 个数）。
			wantCol := lipgloss.Width(string(lineRunes[:colRune]))

			avail := max(1, inner-3)
			// 先把整行裁到可用宽度。
			visible := truncateCells(body, avail)
			// 光标在可见范围之外时，从左侧裁掉多余部分，保证光标始终可见。
			if over := wantCol - lipgloss.Width(visible); over > 0 {
				visible = truncateCellsFromEnd(visible, max(0, lipgloss.Width(visible)-over))
			}
			// 按显示列切开可见文本，在切口处画光标；全部用宽度算。
			col := min(wantCol, lipgloss.Width(visible))
			left, cue, right := splitAtColumn(visible, col)
			rendered := a.st.Text.Render(left) + a.st.ModalCursor.Render(cue) + a.st.Text.Render(right)
			out = append(out, a.st.ModalCursor.Render(marker)+truncateCells(rendered, avail))
			continue
		}
		out = append(out, a.st.Muted.Render(marker+truncateCells(body, max(1, inner-3))))
	}
	// 补足空行，避免输入框高度跳动。
	for len(out) < maxLines {
		out = append(out, "")
	}
	return out
}

// splitAtColumn 在显示列 col 处切分字符串，返回左侧、该列上的一个单元、右侧。
//
// 全部按显示宽度推进，不依赖 rune 下标——中文占两列，混排时用 rune 下标必然错位。
// 若 col 正好落在宽字符的中间，则把该字符归到左侧，光标落在它后面。
func splitAtColumn(s string, col int) (left, cur, right string) {
	if col < 0 {
		col = 0
	}
	var l, r strings.Builder
	w := 0
	placed := false
	for _, ch := range s {
		cw := lipgloss.Width(string(ch))
		switch {
		case placed:
			r.WriteRune(ch)
		case w+cw <= col:
			l.WriteRune(ch)
			w += cw
		case w >= col:
			// 光标正好可以落在这里，把当前字符交给右侧，光标单独占位。
			placed = true
			cur = "▏"
			r.WriteRune(ch)
		default:
			// 光标落在宽字符中间：该字符归左侧，光标跟在它后面。
			l.WriteRune(ch)
			w += cw
			placed = true
			cur = "▏"
		}
	}
	if !placed {
		cur = "▏"
	}
	return l.String(), cur, r.String()
}

// truncateCellsFromEnd 从左侧裁剪，保留字符串末尾 width 列。
//
// 光标在长行末尾时用它，保证光标仍然可见。
func truncateCellsFromEnd(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	total := 0
	i := len(runes)
	for i > 0 {
		w := lipgloss.Width(string(runes[i-1]))
		if total+w > width {
			break
		}
		total += w
		i--
	}
	return string(runes[i:])
}

// pickContent 渲染选择框内容。
func (a *App) pickContent() string {
	inner := a.contentWidth()
	var lines []string
	lines = append(lines, a.st.ModalTitle.Render(truncate(a.pick.title, inner)))
	lines = append(lines, "")
	for i, it := range a.pick.items {
		label := truncate("  "+it.Label, inner)
		if i == a.pick.cursor {
			lines = append(lines, a.st.ModalCursor.Render(padTo(label, highlightWidth(label, inner))))
		} else {
			lines = append(lines, a.st.Text.Render(label))
		}
	}
	lines = append(lines, "")
	lines = append(lines, a.st.Muted.Render(truncate("j/k 选择 · enter 确定 · esc 取消", inner)))
	return strings.Join(lines, "\n")
}

// carryContent 渲染继承确认页。
func (a *App) carryContent() string {
	inner := a.contentWidth()
	var lines []string
	lines = append(lines, a.st.Title.Render(truncate("新的一天开始了", inner)))
	lines = append(lines, "")
	lines = append(lines, a.st.Text.Render(truncate(
		fmt.Sprintf("今天是 %s，昨天是 %s。", a.day, a.prevDay), inner)))

	prev, err := a.store.Day(a.prevDay)
	if err != nil {
		lines = append(lines, a.st.Error.Render(truncate("读取昨日数据失败："+err.Error(), inner)))
	}
	if prev != nil {
		unfinished := 0
		for _, t := range prev.Floating {
			if !t.Done {
				unfinished++
			}
		}
		lines = append(lines, "")
		lines = append(lines, a.st.Muted.Render(truncate(
			fmt.Sprintf("昨日固定 TODO %d 项，未完成的临时 TODO %d 项。", len(prev.Fixed), unfinished), inner)))
	}
	lines = append(lines, "")
	lines = append(lines, a.st.Text.Render(truncate("y  两者都继承（推荐）", inner)))
	lines = append(lines, a.st.Text.Render(truncate("f  只拉取昨日固定 TODO", inner)))
	lines = append(lines, a.st.Text.Render(truncate("x  只继承昨日未完成的 TODO", inner)))
	lines = append(lines, a.st.Text.Render(truncate("n  都不继承，从空白开始", inner)))
	return strings.Join(lines, "\n")
}

// ---------- 设置页 ----------

func (a *App) settingsLines() (styled, plain []string) {
	inner := a.contentWidth()
	labelW := inner - 16
	if labelW > 30 {
		labelW = 30
	}
	if labelW < 10 {
		labelW = 10
	}

	add := func(s, p string) {
		styled = append(styled, s)
		plain = append(plain, p)
	}
	add(a.st.Title.Render("设置 / Settings"), "设置 / Settings")
	sub := "日界线决定“今天”从几点开始，熬夜可设为 04:00。"
	add(a.st.Muted.Render(truncate(sub, inner)), sub)
	add("", "")

	for i, item := range settingItems {
		value := item.Value(a)
		marker := "  "
		if i == a.settingsCursor {
			marker = "▸ "
		}
		// 长值（路径等）单独一行，避免在词中间被折断。
		if lipgloss.Width(marker)+labelW+2+lipgloss.Width(value) > inner || lipgloss.Width(value) > 40 {
			text := truncate(marker+item.Label, inner)
			addSettingRow(&styled, &plain, a, i, item, text, inner)
			if value != "" {
				sub := truncate("    "+value, inner)
				add(a.st.Muted.Render(sub), sub)
			}
			continue
		}
		label := pad(truncate(item.Label, labelW), labelW)
		text := truncate(marker+label+"  "+value, inner)
		addSettingRow(&styled, &plain, a, i, item, text, inner)
	}

	hint := "  j/k 或 ↑/↓ 选择 · enter/e 编辑 · esc 返回看板"
	add("", "")
	add(a.st.Muted.Render(truncate(hint, inner)), hint)
	return styled, plain
}

// addSettingRow 追加一行设置项，按是否选中与是否可编辑选择合适的样式。
func addSettingRow(styled, plain *[]string, a *App, i int, item settingItem, text string, inner int) {
	switch {
	case i == a.settingsCursor:
		// 选中行的底色只铺到文字末尾再加一点余量，不要铺满整行。
		// 早期这里 pad 到整个内容宽度，于是短短一行设置项会拖出一条
		// 上百列的蓝条，看起来像“光标有一行半那么长”。
		*styled = append(*styled, a.st.ModalCursor.Render(padTo(text, highlightWidth(text, inner))))
	case item.Edit != nil:
		*styled = append(*styled, a.st.Text.Render(text))
	default:
		*styled = append(*styled, a.st.Muted.Render(text))
	}
	*plain = append(*plain, text)
}

// highlightWidth 返回选中行底色该铺多宽：文字宽度 + 少量余量，且不超过内容宽度。
func highlightWidth(text string, inner int) int {
	w := lipgloss.Width(text) + 2
	if w > inner {
		w = inner
	}
	return w
}

// padTo 把文本按显示宽度补空格到 width 列（已足够宽则原样返回）。
func padTo(s string, width int) string {
	if w := lipgloss.Width(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
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

func (a *App) historyLines() (styled, plain []string) {
	inner := a.contentWidth()
	stats, err := a.collectHistory(14)

	add := func(s, p string) {
		styled = append(styled, s)
		plain = append(plain, p)
	}
	add(a.st.Title.Render("历史 / History"), "历史 / History")
	if err != nil {
		msg := "读取历史失败：" + err.Error()
		add(a.st.Error.Render(truncate(msg, inner)), msg)
	}
	if len(stats) == 0 {
		msg := "还没有历史数据，完成一些 TODO 或专注一段时间后再来看。"
		add(a.st.Muted.Render(truncate(msg, inner)), msg)
	}
	add("", "")

	// 列宽按可用宽度分配；窄栏时收窄“最投入的条目”。
	topW := inner - 44
	if topW > 30 {
		topW = 30
	}
	if topW < 4 {
		topW = 4
	}
	header := truncate(fmt.Sprintf("  %-12s %-8s %-9s %-6s %s", "日期", "TODO", "专注", "GOAL", "最投入"), inner)
	add(a.st.PanelTitle.Render(header), header)

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
		row := truncate(fmt.Sprintf("  %-12s %-8s %-9s %-6d %s",
			st.Day, ratio, clock.HumanDuration(st.Focus), st.Goals, top), inner)
		add(a.st.Text.Render(row), row)
	}

	total := truncate(fmt.Sprintf("  合计：完成 %d 项 TODO，专注 %s", totalDone, clock.HumanDuration(totalFocus)), inner)
	add("", "")
	add(a.st.OK.Render(total), total)
	add("", "")
	add(a.st.Muted.Render("  esc / q 返回看板"), "  esc / q 返回看板")
	return styled, plain
}

// ---------- 帮助页 ----------

// helpRow 是一行帮助内容；Desc 为空表示标题行。
type helpRow struct {
	Key  string
	Desc string
}

// helpRows 返回帮助条目。窄终端下会压缩说明文字。
func helpRows(compact bool) []helpRow {
	rows := []helpRow{
		{Key: "导航"},
		{Key: "tab / shift+tab", Desc: "循环切换栏位（中间 / 固定 / 临时 / GOAL）"},
		{Key: "j / k ↑ / ↓", Desc: "在当前栏内上下移动"},
		{Key: "g / G", Desc: "跳到首项 / 末项"},
		{Key: "enter", Desc: "进入选中条目的子任务；再按 enter 勾选子任务"},
		{Key: "esc", Desc: "从子任务退回父条目"},
		{Key: "1 / 2 / 3", Desc: "直接跳到固定 TODO / 临时 TODO / GOAL"},
		{Key: "编辑"},
		{Key: "space", Desc: "勾选选中项；在子任务模式下勾选子任务"},
		{Key: "a", Desc: "添加 TODO（固定栏加固定项，临时栏加临时项）"},
		{Key: "A", Desc: "添加 GOAL"},
		{Key: "e", Desc: "重命名选中条目"},
		{Key: "t", Desc: "为选中条目添加子任务"},
		{Key: "d", Desc: "删除选中条目（会先确认）"},
		{Key: "r", Desc: "从昨日继承（固定 / 未完成 / 两者）"},
		{Key: "N", Desc: "打开随手记（多行编辑器，按日保存）"},
		{Key: "随手记 / 日记"},
		{Key: "enter", Desc: "换行（不是确认，避免写日记时误提交）"},
		{Key: "ctrl+s", Desc: "保存随手记"},
		{Key: "esc / q", Desc: "关闭编辑器（有未保存改动会先问）"},
		{Key: "计时"},
		{Key: "enter", Desc: "在中间栏打开计时菜单"},
		{Key: "space", Desc: "计时中暂停或继续"},
		{Key: "enter", Desc: "计时中结束并归档到所属 TODO"},
		{Key: "esc", Desc: "计时中中断，已用时长仍会记录"},
		{Key: "设置"},
		{Key: "j / k", Desc: "在设置项之间移动"},
		{Key: "enter / e", Desc: "编辑选中的设置项"},
		{Key: "其它"},
		{Key: "?", Desc: "打开 / 关闭本帮助"},
		{Key: "j / k", Desc: "帮助页内容滚动"},
		{Key: "q", Desc: "退出（会先确认）"},
		{Key: "ctrl+c", Desc: "立即退出"},
	}
	if compact {
		out := rows[:0:0]
		for _, r := range rows {
			if r.Desc == "" {
				out = append(out, r)
				continue
			}
			out = append(out, helpRow{Key: r.Key, Desc: shortDesc(r.Desc)})
		}
		return out
	}
	return rows
}

// shortDesc 截短说明文字，供窄终端使用。
func shortDesc(s string) string {
	if i := strings.IndexAny(s, "（("); i > 0 {
		s = s[:i]
	}
	return s
}

// helpLines 依据中间栏宽度排版帮助内容。
//
// 说明文字宁可换到下一行也不截断：早期宽度算错导致只剩光秃秃的按键名。
func (a *App) helpLines() (styled, plain []string) {
	inner := a.contentWidth()
	rows := helpRows(a.IsCompact())
	keyW := 0
	for _, r := range rows {
		if w := lipgloss.Width(r.Key); w > keyW {
			keyW = w
		}
	}
	if keyW > 20 {
		keyW = 20
	}
	descW := inner - keyW - 4
	stacked := descW < 10

	add := func(s, p string) {
		styled = append(styled, s)
		plain = append(plain, p)
	}
	add(a.st.Title.Render("帮助 / Help"), "帮助 / Help")
	add("", "")
	for _, r := range rows {
		if r.Desc == "" {
			if len(styled) > 2 {
				add("", "")
			}
			add(a.st.Accent.Bold(true).Render("  "+truncate(r.Key, inner)), "  "+r.Key)
			continue
		}
		keyText := pad(r.Key, keyW)
		if stacked {
			add("  "+a.st.HintKey.Render(keyText), "  "+keyText)
			for _, wl := range wrapBalanced(r.Desc, max(8, inner-4)) {
				add("  "+strings.Repeat(" ", keyW)+a.st.Text.Render(wl), "  "+strings.Repeat(" ", keyW)+wl)
			}
			continue
		}
		// 折行时每行再收 1 列：折行算法只需满足“不超过 descW”，
		// 但续行会多一层缩进，留出余量才不会顶到边框。
		for i, dl := range wrapBalanced(r.Desc, descW-1) {
			if i == 0 {
				add("  "+a.st.HintKey.Render(keyText)+"  "+a.st.Text.Render(dl), "  "+keyText+"  "+dl)
			} else {
				// 续行缩进到说明列下方，读起来明显是同一项的后半句。
				add("  "+strings.Repeat(" ", keyW+2)+a.st.Text.Render(dl), "  "+strings.Repeat(" ", keyW+2)+dl)
			}
		}
	}
	add("", "")
	add(a.st.Muted.Render("  esc / q / ? 返回看板 · j/k 滚动"), "  esc / q / ? 返回看板 · j/k 滚动")
	add(a.st.Muted.Render(truncate("  KQFLOW "+version.Version, inner)), "  KQFLOW "+version.Version)
	return styled, plain
}

// handleHelpKey 处理帮助页按键（支持滚动）。
func (a *App) handleHelpKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q", "?":
		a.view = ViewDashboard
		a.pageScroll = 0
	case "j", "down":
		a.pageScroll++
	case "k", "up":
		if a.pageScroll > 0 {
			a.pageScroll--
		}
	case "g", "home":
		a.pageScroll = 0
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	}
	return a, nil
}
