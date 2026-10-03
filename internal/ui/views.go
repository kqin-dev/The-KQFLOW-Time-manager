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

// minMargin 是浮层两侧最少保留的空白列数，让二级菜单居中而不是贴着终端边缘。
const minMargin = 2

// overlayInner 返回浮层内容区实际可用的字符数。
//
// 看板按“整个终端宽度”排版面板，而浮层是套在自己的边框里再叠上去的，
// 所以浮层内容必须再扣掉自身边框与两侧留白，否则会横向超出看板宽度、
// 或者铺满整屏显得贴边。
func (a *App) overlayInner() int {
	frame := a.st.modalStyle(a.IsCompact()).GetHorizontalFrameSize()
	if f := a.st.pageStyle(a.IsCompact()).GetHorizontalFrameSize(); f > frame {
		frame = f
	}
	avail := a.width - frame - minMargin*2
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

	// 兜底：无论浮层怎么算，合成结果都不能超过终端尺寸，
	// 否则会撑破看板边框或顶掉整屏，把界面撕坏、内容跑出可视区域。
	for i, l := range baseLines {
		if w := lipgloss.Width(l); w > width {
			baseLines[i] = truncateCells(l, width)
		}
	}
	if len(baseLines) > height {
		baseLines = baseLines[:height]
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

// pageWidth 返回整页浮层的内容宽度：以纯文本行的最大宽度为准。
//
// 不能拿已上色的字符串去量：ANSI 转义序列会让宽度计算偏大，
// 结果面板被撑到终端宽度、说明文字被迫折成两行。
func (a *App) pageWidth(plainLines []string, prefer int) int {
	natural := 0
	for _, l := range plainLines {
		if w := lipgloss.Width(l); w > natural {
			natural = w
		}
	}
	if prefer > 0 && natural > prefer {
		natural = prefer
	}
	avail := a.overlayInner()
	// 永远不要在终端里左右顶到边：留出空白既好看，也能避免某些终端
	// 在最后一列自动换行而多出一行。
	if avail > a.width-4 {
		avail = a.width - 4
	}
	if natural > avail {
		natural = avail
	}
	if natural < 8 {
		natural = 8
	}
	return natural
}

// renderPage 渲染整页浮层。
//
// 帮助、设置、历史是“整页”而不是浮层：直接把面板居中铺在空白背景上，
// 不再叠在看板之上。早期用 overlay 拼接时，看板的面板边框会从浮层两侧露出来，
// 看起来像界面被撕开，也让人误以为必须全屏才能看清。
//
// sized 与 plain 一一对应：sized[i] 是上色后的行，plain[i] 是同一行的纯文本，
// 宽度只按 plain 计算。
func (a *App) renderPage(sized, plain []string, prefer int) string {
	inner := a.pageWidth(plain, prefer)
	fitted := make([]string, 0, len(sized))
	for _, l := range sized {
		fitted = append(fitted, truncateCells(l, inner))
	}
	panel := a.st.pageStyle(a.IsCompact()).Width(inner).Render(strings.Join(fitted, "\n"))
	return centerBlock(panel, a.width, a.height)
}

// centerBlock 把一段渲染好的内容居中放在 width×height 的空白画布上。
func centerBlock(block string, width, height int) string {
	if width <= 0 || height <= 0 {
		return block
	}
	lines := strings.Split(block, "\n")
	blockW := 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > blockW {
			blockW = w
		}
	}
	if blockW > width {
		blockW = width
	}
	left := (width - blockW) / 2
	if left < 0 {
		left = 0
	}
	top := (height - len(lines)) / 2
	if top < 0 {
		top = 0
	}

	out := make([]string, 0, height)
	for i := 0; i < top && len(out) < height; i++ {
		out = append(out, strings.Repeat(" ", width))
	}
	for _, l := range lines {
		if len(out) >= height {
			break
		}
		l = truncateCells(l, blockW)
		line := strings.Repeat(" ", left) + l
		if w := lipgloss.Width(line); w < width {
			line += strings.Repeat(" ", width-w)
		}
		out = append(out, line)
	}
	for len(out) < height {
		out = append(out, strings.Repeat(" ", width))
	}
	return strings.Join(out, "\n")
}

// ---------- 设置页 ----------

func (a *App) renderSettings() string {
	inner := a.overlayInner()
	if inner > 90 {
		inner = 90
	}
	// 标签列宽度：给值留出至少 12 列。
	labelW := inner - 16
	if labelW > 38 {
		labelW = 38
	}
	if labelW < 10 {
		labelW = 10
	}

	var styled, plain []string
	add := func(s, p string) {
		styled = append(styled, s)
		plain = append(plain, p)
	}
	add(a.st.Title.Render("设置 / Settings"), "设置 / Settings")
	sub := "熬夜用户可以把日界线设为 04:00，凌晨 2 点仍算前一天。"
	add(a.st.Muted.Render(sub), sub)
	add("", "")

	for i, item := range settingItems {
		value := item.Value(a)
		marker := "  "
		if i == a.settingsCursor {
			marker = "▸ "
		}
		// 长值（例如数据目录、配置文件路径）一律单独一行并缩进显示，
		// 内联会被迫在词中间折行，读起来像是两条不同内容。
		inline := lipgloss.Width(marker)+labelW+2+lipgloss.Width(value) <= inner
		longValue := lipgloss.Width(value) > 40
		if !inline || longValue {
			label := truncate(item.Label, max(4, inner-2))
			text := truncate(marker+label, inner)
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
	add(a.st.Muted.Render(hint), hint)

	return a.renderPage(styled, plain, 90)
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
	var styled, plain []string
	add := func(s, p string) {
		styled = append(styled, s)
		plain = append(plain, p)
	}
	add(a.st.Title.Render("历史 / History"), "历史 / History")
	if err != nil {
		msg := "读取历史失败：" + err.Error()
		add(a.st.Error.Render(msg), msg)
	}
	if len(stats) == 0 {
		msg := "还没有历史数据，完成一些 TODO 或专注一段时间后再来看。"
		add(a.st.Muted.Render(msg), msg)
	}
	add("", "")

	// 先按终端宽度决定各列宽度，窄终端下自动收窄“最投入的条目”。
	inner := a.overlayInner() - 4
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
		row := fmt.Sprintf("  %-12s %-8s %-10s %-6d %s",
			st.Day, ratio, clock.HumanDuration(st.Focus), st.Goals, top)
		row = truncate(row, inner)
		add(a.st.Text.Render(row), row)
	}

	total := fmt.Sprintf("  合计：完成 %d 项 TODO，专注 %s", totalDone, clock.HumanDuration(totalFocus))
	add("", "")
	add(a.st.OK.Render(total), total)
	add("", "")
	add(a.st.Muted.Render("  esc / q 返回看板"), "  esc / q 返回看板")

	return a.renderPage(styled, plain, 110)
}

// ---------- 帮助页 ----------

// helpRow 是一行帮助内容；Key 为空表示标题或说明行。
type helpRow struct {
	Key  string
	Desc string
}

// helpRows 返回扁平的帮助条目。窄终端下会跳过说明性文字，只留按键。
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
		// 窄终端只保留按键，避免说明把版面挤爆。
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

// buildHelpLines 依据可用宽度排版帮助内容；空间不足时自动折行而不是被截断。
//
// 返回两套行：styled 用于显示，plain 用于测量宽度。两者一一对应。
func (a *App) buildHelpLines(inner int, compact bool) (styled, plain []string) {
	rows := helpRows(compact)
	// 先算按键列宽度。
	keyW := 0
	for _, r := range rows {
		if w := lipgloss.Width(r.Key); w > keyW {
			keyW = w
		}
	}
	if keyW > 20 {
		keyW = 20
	}
	// 说明列至少要留 12 列，否则改为“按键独占一行”。
	descW := inner - keyW - 4
	wrapDesc := descW < 12

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
			add(a.st.Accent.Bold(true).Render("  "+r.Key), "  "+r.Key)
			continue
		}
		keyText := pad(r.Key, keyW)
		if wrapDesc {
			add("  "+a.st.HintKey.Render(keyText), "  "+keyText)
			for _, wl := range wrapBalanced(r.Desc, inner-4) {
				add("  "+strings.Repeat(" ", keyW)+a.st.Text.Render(wl), "  "+strings.Repeat(" ", keyW)+wl)
			}
			continue
		}
		if lipgloss.Width(r.Desc) <= descW {
			add("  "+a.st.HintKey.Render(keyText)+"  "+a.st.Text.Render(r.Desc),
				"  "+keyText+"  "+r.Desc)
			continue
		}
		for i, dl := range wrapBalanced(r.Desc, descW) {
			if i == 0 {
				add("  "+a.st.HintKey.Render(keyText)+"  "+a.st.Text.Render(dl), "  "+keyText+"  "+dl)
			} else {
				add("  "+strings.Repeat(" ", keyW)+"  "+a.st.Text.Render(dl), "  "+strings.Repeat(" ", keyW)+"  "+dl)
			}
		}
	}
	add("", "")
	add(a.st.Muted.Render("  esc / q / ? 返回看板 · j/k 滚动"), "  esc / q / ? 返回看板 · j/k 滚动")
	return styled, plain
}

// addSettingRow 追加一行设置项，按是否选中与是否可编辑选择合适的样式。
//
// 选中项除底色外还加一个指针字符，低色彩终端下也能看出光标在哪。
func addSettingRow(styled, plain *[]string, a *App, i int, item settingItem, text string, inner int) {
	switch {
	case i == a.settingsCursor:
		*styled = append(*styled, a.st.ModalCursor.Render(pad(text, inner)))
	case item.Edit != nil:
		*styled = append(*styled, a.st.Text.Render(text))
	default:
		*styled = append(*styled, a.st.Muted.Render(text))
	}
	*plain = append(*plain, text)
}

// renderHelp 渲染帮助页。内容放不下时按可用高度滚动，而不是要求用户全屏。
func (a *App) renderHelp() string {
	compact := a.IsCompact()
	inner := a.overlayInner()
	if inner > 92 {
		inner = 92
	}
	styled, plain := a.buildHelpLines(inner, compact)

	// 可用高度：终端高度减去面板边框（上下各一行）。
	// renderPage 会给内容套一层边框，所以这里只需扣掉边框本身，
	// 多扣会让帮助/设置页出现大片空白，少扣则底部越界。
	frameV := a.st.pageStyle(compact).GetVerticalFrameSize()
	avail := a.height - frameV
	if avail < 3 {
		avail = 3
	}
	if len(styled) > avail {
		// 内容超出时按 a.helpScroll 滚动。
		maxScroll := len(styled) - avail
		if a.helpScroll > maxScroll {
			a.helpScroll = maxScroll
		}
		if a.helpScroll < 0 {
			a.helpScroll = 0
		}
		styled = styled[a.helpScroll : a.helpScroll+avail]
		plain = plain[a.helpScroll : a.helpScroll+avail]
	}
	return a.renderPage(styled, plain, 92)
}

// handleHelpKey 处理帮助页按键（支持滚动）。
func (a *App) handleHelpKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q", "?":
		a.view = ViewDashboard
		a.helpScroll = 0
	case "j", "down":
		a.helpScroll++
	case "k", "up":
		if a.helpScroll > 0 {
			a.helpScroll--
		}
	case "g", "home":
		a.helpScroll = 0
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	}
	return a, nil
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
