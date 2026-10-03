package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/clock"
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
// 空白区域高度有限，所以柱高是**归一化相对高度**：最高的一天占满 maxHeight，
// 其余按比例缩放，而不是绝对分钟数。
func (a *App) renderVerticalChart(values []dayValue, inner, maxHeight int) []string {
	if len(values) == 0 || maxHeight < 2 {
		return nil
	}
	// 每天两列（柱 + 间隔），最少也要留出这个宽度。
	if inner < len(values)*2 {
		maxHeight = min(maxHeight, 3)
	}
	maxVal := 0
	for _, v := range values {
		if v.Value > maxVal {
			maxVal = v.Value
		}
	}

	// 归一化到 maxHeight 行；每行用半高块与整高块拼出更平滑的柱。
	// 用“半格”为单位，避免小数值全部塌成 0 或 1。
	halfUnits := maxHeight * 2
	heights := make([]int, len(values))
	for i, v := range values {
		if maxVal <= 0 {
			heights[i] = 0
			continue
		}
		h := v.Value * halfUnits / maxVal
		if v.Value > 0 && h < 1 {
			// 有值就至少给半格，否则看起来和没记录一样。
			h = 1
		}
		heights[i] = h
	}

	var out []string
	for row := maxHeight - 1; row >= 0; row-- {
		var b strings.Builder
		b.WriteString(" ")
		for i, v := range values {
			cellBottom := row * 2
			level := heights[i]
			var ch string
			style := a.st.BarFilled
			if v.Today {
				style = a.st.OK
			}
			switch {
			case level >= cellBottom+2:
				ch = "█"
			case level == cellBottom+1:
				ch = "▄"
			default:
				ch = " "
				style = a.st.BarEmpty
			}
			b.WriteString(style.Render(ch))
			if i < len(values)-1 {
				b.WriteString(" ")
			}
		}
		out = append(out, b.String())
	}

	// 坐标轴：一条横线加日期标签。
	// 每根柱子占 1 列、间隔 1 列，所以第 i 根柱子在显示列 2i 上；
	// 标签用同样间距逐字给出，才能正好落在柱子正下方（首列留空对齐 "└"）。
	axis := a.st.BarEmpty.Render("└" + strings.Repeat("─", max(1, len(values)*2-1)))
	out = append(out, axis)

	var labels strings.Builder
	labels.WriteString(" ")
	for i, v := range values {
		style := a.st.Muted
		if v.Today {
			style = a.st.Accent
		}
		labels.WriteString(style.Render(firstCell(v.Label)))
		if i < len(values)-1 {
			labels.WriteString(" ")
		}
	}
	out = append(out, labels.String())
	return out
}

// firstCell 取文本的第一个显示单元，保证标签恰好占一列，和柱子对齐。
func firstCell(s string) string {
	for _, r := range s {
		return string(r)
	}
	return " "
}

// statsLines 生成“连续 7 天统计”区块（竖向柱状图）。
func (a *App) statsLines(inner int) []string {
	stats := a.rollingStats(a.day, 7)
	if len(stats) == 0 {
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

	var out []string
	title := fmt.Sprintf("连续 7 天 · 完成 %d/%d · 专注 %s",
		totalDone, totalTodos, clock.HumanDuration(totalFocus))
	out = append(out, fit(a.st.PanelTitle, center(title, inner, lipglossWidth(title)), inner))
	sub := fmt.Sprintf("连续 %d 天有记录 · 活跃 %d 天", streak, activeDays)
	out = append(out, fit(a.st.Muted, center(sub, inner, lipglossWidth(sub)), inner))

	// 柱状图高度随可用空间伸缩，最多 5 行，保证轴与标签放得下。
	_, _, _, bodyH := a.columnLayout()
	avail := bodyH - a.st.Panel.GetVerticalFrameSize()
	maxChart := avail - 4
	chartHeight := min(5, max(2, maxChart-8))
	chart := a.renderVerticalChart(a.statBarValues(stats, loc), inner, chartHeight)
	out = append(out, chart...)
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
