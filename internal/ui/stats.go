package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
)

// dayStat 是某一天的完成度与专注时长，用于连续 7 天统计。
type dayStat struct {
	Day    string
	Done   int
	Total  int
	Focus  time.Duration
	HasAny bool
}

// rollingStats 返回包含 endDay 在内的连续 n 天统计（按时间升序）。
//
// “连续 7 天”是滚动窗口，不是自然周，所以这里按天逐日回溯。
func (a *App) rollingStats(endDay string, n int) []dayStat {
	loc := a.cfg.Location()
	stats := make([]dayStat, 0, n)
	day := endDay
	// 先收集再反转，保证升序。
	var rev []dayStat
	for i := 0; i < n; i++ {
		st := dayStat{Day: day}
		if data, err := a.store.Day(day); err == nil && data != nil {
			if data.Day != "" {
				day = data.Day
			}
			st.Done, st.Total = data.Counts()
			st.Focus, _ = data.FocusTotal()
			hasNote := strings.TrimSpace(data.Note) != ""
			hasGoals := len(data.Archive.Goals) > 0
			st.HasAny = st.Total > 0 || st.Focus > 0 || hasNote || hasGoals
		}
		rev = append(rev, st)

		prev, err := clock.PrevDay(day, loc)
		if err != nil {
			break
		}
		day = prev
	}
	for i := len(rev) - 1; i >= 0; i-- {
		stats = append(stats, rev[i])
	}
	return stats
}

// currentStreak 返回以 endDay 结尾、向前连续有记录的天数。
//
// “有记录”指当天完成过至少一件事、专注过、或写过随手记。
func (a *App) currentStreak(endDay string, limit int) int {
	loc := a.cfg.Location()
	day := endDay
	streak := 0
	for i := 0; i < limit; i++ {
		data, err := a.store.Day(day)
		if err != nil || data == nil {
			break
		}
		done, _ := data.Counts()
		focus, _ := data.FocusTotal()
		active := done > 0 || focus > 0 || strings.TrimSpace(data.Note) != "" || len(data.Archive.Goals) > 0
		if !active {
			break
		}
		streak++
		prev, err := clock.PrevDay(day, loc)
		if err != nil {
			break
		}
		day = prev
	}
	return streak
}

// statBarWidth 已废弃：统计改为竖向柱状图后不再需要每格的横向宽度。
const statBarWidth = 10

// renderMiniBar 用方块画一条迷你进度条。
func (a *App) renderMiniBar(done, total, width int) string {
	if width < 3 {
		width = 3
	}
	if total <= 0 {
		return a.st.BarEmpty.Render(strings.Repeat("·", width))
	}
	filled := done * width / total
	if filled > width {
		filled = width
	}
	style := a.st.OK
	if done < total {
		style = a.st.BarFilled
	}
	return style.Render(strings.Repeat("█", filled)) + a.st.BarEmpty.Render(strings.Repeat("░", width-filled))
}

// weekdayLabel 返回某天的星期缩写。
func weekdayLabel(day string, loc *time.Location) string {
	d, err := time.ParseInLocation(clock.DateFormat, day, loc)
	if err != nil {
		return "??"
	}
	return [...]string{"日", "一", "二", "三", "四", "五", "六"}[d.Weekday()]
}

// dayValue 是柱状图里一根柱子对应的数值。
type dayValue struct {
	Label string
	Value int
	Today bool
	// Empty 表示当天完全没有记录。
	Empty bool
}

// statBarValues 把 7 天统计折算成柱状图的数值。
//
// 柱高表示“当天专注时长（分钟）”；当天没有任何记录（既没专注也没完成事项）
// 时值为 0，画成空槽，便于一眼看出断档。
func (a *App) statBarValues(stats []dayStat, loc *time.Location) []dayValue {
	out := make([]dayValue, 0, len(stats))
	for _, st := range stats {
		label := weekdayLabel(st.Day, loc)
		today := st.Day == a.day
		if today {
			label = "今"
		}
		minutes := int(st.Focus.Minutes())
		// 有专注就按专注排高；没有专注但完成了待办，给一个最小高度以示“做过事”。
		if minutes == 0 && st.Done > 0 {
			minutes = 1
		}
		out = append(out, dayValue{
			Label: label,
			Value: minutes,
			Today: today,
			Empty: minutes == 0,
		})
	}
	return out
}

// renderVerticalChart 画竖向柱状图（见用户草图）。
//
// 两点要求：
//   - 空白区域高度有限，柱高是**归一化相对高度**：最高的一天占满 maxHeight，
//     其余按比例缩放，而不是绝对分钟数。
//   - 柱子要有足够宽度并撑满可用横向空间，否则会缩成一根细线，看不出对比。
func (a *App) renderVerticalChart(values []dayValue, inner, maxHeight int) []string {
	n := len(values)
	if n == 0 || maxHeight < 2 {
		return nil
	}

	// 先定柱子宽度与间距，让整体尽量铺满 inner。
	// 起点留 1 列给 Y 轴；末尾再留 3 列——日期标签是宽字符（占两列），
	// 最后一根柱子的标签会向两侧各溢出 1 列，不留就会被截掉。
	usable := inner - 4
	if usable < n {
		usable = n
	}
	barW, gap := 1, 1
	for w := 3; w >= 1; w-- {
		total := n*w + (n-1)*1
		if total <= usable {
			barW, gap = w, 1
			// 还有余量就把间距摊开一些，让图更舒展。
			if n > 1 {
				extra := usable - total
				gap = 1 + extra/(n-1)
			}
			break
		}
	}
	step := barW + gap
	if last := 1 + (n-1)*step + barW - 1; last > inner-2 {
		// 兜底：整体右端不得越过标签所需的位置。
		step = max(1, (inner-4-barW)/max(1, n-1))
	}
	chartW := n*barW + (n-1)*gap
	if chartW > inner-1 {
		chartW = max(1, inner-1)
	}
	_ = chartW

	// 归一化到 maxHeight 行，用半格（maxHeight*2 个单位）保证小数不被压成 0。
	maxVal := 0
	for _, v := range values {
		if v.Value > maxVal {
			maxVal = v.Value
		}
	}
	halfUnits := maxHeight * 2
	heights := make([]int, n)
	for i, v := range values {
		if maxVal <= 0 || v.Value <= 0 {
			continue
		}
		h := v.Value * halfUnits / maxVal
		if h < 1 {
			h = 1
		}
		heights[i] = h
	}

	var out []string
	for row := maxHeight - 1; row >= 0; row-- {
		line := make([]rune, max(1, inner))
		for i := range line {
			line[i] = ' '
		}
		// 每根柱子画 barW 宽的实心块。
		for i := range values {
			cellBottom := row * 2
			level := heights[i]
			var ch rune
			switch {
			case level >= cellBottom+2:
				ch = '█'
			case level == cellBottom+1:
				ch = '▄'
			default:
				continue
			}
			startCol := 1 + i*step
			for k := 0; k < barW; k++ {
				col := startCol + k
				if col >= 0 && col < len(line) {
					line[col] = ch
				}
			}
		}
		out = append(out, a.st.BarFilled.Render(strings.TrimRight(string(line), " ")))
	}

	// 坐标轴：与柱子同宽同位置，像草图那样一条横线。
	axis := make([]rune, max(1, inner))
	for i := range axis {
		axis[i] = ' '
	}
	axis[0] = '└'
	for i := range values {
		startCol := 1 + i*step
		for k := 0; k < barW; k++ {
			col := startCol + k
			if col >= 0 && col < len(axis) {
				axis[col] = '─'
			}
		}
	}
	out = append(out, a.st.BarEmpty.Render(strings.TrimRight(string(axis), " ")))

	// 日期标签：居中放在各自柱子下方。
	//
	// 标签是宽字符（占两列），所以必须按“显示列”摆放，
	// 不能像柱子那样用 rune 下标——否则宽字符会让后面的标签整体错位。
	out = append(out, a.st.Muted.Render(a.renderChartLabels(values, inner, step, barW)))
	return out
}

// renderChartLabels 按显示列摆放日期标签，保证每个标签居中在柱子下方。
func (a *App) renderChartLabels(values []dayValue, inner, step, barW int) string {
	var b strings.Builder
	col := 0
	writePad := func(to int) {
		for col < to {
			b.WriteByte(' ')
			col++
		}
	}
	for i, v := range values {
		label := firstCell(v.Label)
		lw := lipgloss.Width(label)
		// 柱子中心列；宽字符向左挪半格，视觉上正好居中。
		center := 1 + i*step + (barW-1)/2
		start := center - (lw-1)/2
		if start < col {
			start = col
		}
		if start+lw > inner {
			break
		}
		writePad(start)
		b.WriteString(label)
		col += lw
	}
	return strings.TrimRight(b.String(), " ")
}

// firstCell 取文本的第一个显示单元，保证标签恰好占一列，和柱子对齐。
func firstCell(s string) string {
	for _, r := range s {
		return string(r)
	}
	return " "
}

// statsLines 生成“连续 7 天统计”区块（竖向柱状图）。
//
// maxLines 是整个区块可用的行数。空间紧张时先压缩表头（两行并一行），
// 且只画一天没记录的日期标签，把省下的行留给柱状图——
// 用户的诉求是“要能看到柱状图”，标题被压掉一点无妨。
func (a *App) statsLines(inner, maxLines int) []string {
	stats := a.rollingStats(a.day, 7)
	if len(stats) == 0 || maxLines < 2 {
		return nil
	}
	var totalDone, totalTodos int
	var totalFocus time.Duration
	activeDays := 0
	for _, st := range stats {
		totalDone += st.Done
		totalTodos += st.Total
		totalFocus += st.Focus
		if st.Done > 0 || st.Focus > 0 {
			activeDays++
		}
	}
	streak := a.currentStreak(a.day, 365)
	loc := a.cfg.Location()

	// 表头：宽裕时两行（总数 + 连续天数），紧张时压成一行。
	compact := maxLines < 9
	var head []string
	title := fmt.Sprintf("连续 7 天 · 完成 %d/%d · 专注 %s",
		totalDone, totalTodos, clock.HumanDuration(totalFocus))
	if compact {
		title = fmt.Sprintf("7 天 · %d/%d · %s · 连续 %d 天",
			totalDone, totalTodos, clock.HumanDuration(totalFocus), streak)
		head = append(head, fit(a.st.PanelTitle, center(title, inner, lipglossWidth(title)), inner))
	} else {
		head = append(head, fit(a.st.PanelTitle, center(title, inner, lipglossWidth(title)), inner))
		sub := fmt.Sprintf("连续 %d 天有记录 · 活跃 %d 天", streak, activeDays)
		head = append(head, fit(a.st.Muted, center(sub, inner, lipglossWidth(sub)), inner))
	}

	// 柱状图拿到剩余行数，并以“每根柱子 1 行 + 轴 + 标签行中一天无标签”为目标。
	rest := maxLines - len(head)
	labelRows := 2 // 轴 + 日期标签
	chartHeight := rest - labelRows
	if chartHeight > 6 {
		chartHeight = 6
	}
	if chartHeight < 1 {
		// 连柱状图都放不下：只留表头，宁可不画也不要半截图。
		return head
	}
	out := head
	out = append(out, a.renderVerticalChart(a.statBarValues(stats, loc), inner, chartHeight)...)
	return out
}

// notePreviewLines 生成看板上的随手记预览（只显示前几行）。
func (a *App) notePreviewLines(inner, maxLines int) []string {
	note := strings.TrimRight(a.data.Note, "\n")
	if strings.TrimSpace(note) == "" || maxLines < 1 {
		return nil
	}
	var out []string
	head := fmt.Sprintf("随手记 · %d 字", len([]rune(strings.TrimSpace(note))))
	out = append(out, fit(a.st.PanelTitle, center(head, inner, lipglossWidth(head)), inner))

	shown := 0
	for _, raw := range strings.Split(note, "\n") {
		if shown >= maxLines {
			break
		}
		for _, wl := range wrapBalanced(raw, inner-2) {
			if shown >= maxLines {
				break
			}
			out = append(out, a.st.Muted.Render(truncate("  "+wl, inner)))
			shown++
		}
	}
	// 还有更多内容时给个提示。
	if strings.Count(note, "\n")+1 > shown {
		out = append(out, a.st.DoneTag.Render(truncate("  …", inner)))
	}
	return out
}

// lipglossWidth 是 lipgloss.Width 的短包装，便于在 fmt 表达式里使用。
func lipglossWidth(s string) int { return lipgloss.Width(s) }
