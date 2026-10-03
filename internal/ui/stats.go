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

// statBarWidth 是统计迷你条的可用宽度。
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

// statsLines 生成“连续 7 天统计”区块。
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

	if streak > 0 {
		line := fmt.Sprintf("连续 %d 天有记录 · 7 天里活跃 %d 天", streak, activeDays)
		out = append(out, fit(a.st.Muted, center(line, inner, lipglossWidth(line)), inner))
	}

	// 每天一格（星期 + 迷你条 + 完成比），同行并排若干天，
	// 这样 7 天只占两行左右，不会把随手记挤掉。
	segW := statBarWidth + 7
	if inner < 40 {
		segW = 5 + 4
	}
	perRow := inner / segW
	if perRow < 1 {
		perRow = 1
	}
	if perRow > len(stats) {
		perRow = len(stats)
	}
	cellW := inner / perRow

	var row strings.Builder
	cells := 0
	flush := func() {
		if cells == 0 {
			return
		}
		out = append(out, a.st.Text.Render(pad(row.String(), inner)))
		row.Reset()
		cells = 0
	}
	for _, st := range stats {
		label := weekdayLabel(st.Day, loc)
		if st.Day == a.day {
			label = "今"
		}
		barW := statBarWidth
		if inner < 40 {
			barW = 5
		}
		cell := fmt.Sprintf("%s%s", label, a.renderMiniBar(st.Done, st.Total, barW))
		row.WriteString(pad(cell, cellW))
		cells++
		if cells == perRow {
			flush()
		}
	}
	flush()
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
