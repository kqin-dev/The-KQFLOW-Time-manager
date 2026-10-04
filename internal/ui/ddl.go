package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// DDL 设置页（见需求 4）。
//
// 两类条目的粒度不同，这是需求明确要求的：
//   - TODO 只能设**时分**（HH:MM）：待办每天重置，18:30 天然指「今天 18:30」。
//   - GOAL 只能设**年月日**（YYYY-MM-DD）：目标不随天重置，指「到这天结束为止」。
//
// 所以这个页面按条目类型给出不同的一行快捷选项，也允许用户自己输入。
type ddlOption struct {
	Label string
	// Value 是要写入条目的 DDL 原文；空串表示「清除」。
	Value string
}

type ddlViewState struct {
	active bool
	target model.Ddl
	kind   string // TODO / GOAL
	cursor int
}

// currentDdl 返回当前焦点下可以设 DDL 的条目。
func (a *App) currentDdl() (model.Ddl, string) {
	switch a.focus {
	case FocusFixed, FocusFloating:
		if t := a.currentTodo(); t != nil {
			return t, "TODO"
		}
	case FocusGoals:
		if g := a.selectedGoal(); g != nil {
			return g, "GOAL"
		}
	}
	return nil, ""
}

// ddlOptions 返回当前条目可用的 DDL 选项。
//
// 快捷项是「按当前时间推算」的，用户大多数时候只想设个大概：
// 待办给「1 小时后 / 今天 18:00 / 今天 22:00」，目标给「今天 / 明天 / 本周日 / 本月底」。
func (a *App) ddlOptions(target model.Ddl) []ddlOption {
	now := a.clock.Now()
	loc := a.cfg.Location()
	cut := a.cfg.Cutoff()
	today := clock.LogicalDay(now, cut)
	dayStart, err := clock.DayStart(today, cut, loc)
	if err != nil {
		dayStart = now
	}

	var opts []ddlOption
	if target.DueIsTodo() {
		soon := now.Add(time.Hour).Round(time.Minute)
		opts = append(opts,
			ddlOption{Label: "1 小时后（" + clock.TimeOfDay(soon) + "）", Value: clock.TimeOfDay(soon)},
			ddlOption{Label: "今天 12:00", Value: "12:00"},
			ddlOption{Label: "今天 18:00", Value: "18:00"},
			ddlOption{Label: "今天 22:00", Value: "22:00"},
		)
		_ = dayStart
	} else {
		todayDate, err := time.ParseInLocation(clock.DateFormat, today, loc)
		if err != nil {
			todayDate = now
		}
		// 本周日：Go 里 Sunday=0。当天就是周日（差 0 天）时保留今天，
		// 否则顺推到下一个周日（最多 6 天后）。
		daysToSunday := (7 - int(todayDate.Weekday())) % 7
		sunday := todayDate.AddDate(0, 0, daysToSunday)
		// 本月底：下个月 1 号往前一天。
		monthEnd := time.Date(todayDate.Year(), todayDate.Month()+1, 1, 0, 0, 0, 0, loc).AddDate(0, 0, -1)

		opts = append(opts,
			ddlOption{Label: "今天（" + today[5:] + "）", Value: today},
			ddlOption{Label: "明天", Value: todayDate.AddDate(0, 0, 1).Format(clock.DateFormat)},
			ddlOption{Label: "本周日（" + sunday.Format(clock.DateFormat)[5:] + "）", Value: sunday.Format(clock.DateFormat)},
			ddlOption{Label: "本月底（" + monthEnd.Format(clock.DateFormat)[5:] + "）", Value: monthEnd.Format(clock.DateFormat)},
		)
	}

	if target.DueText() != "" {
		opts = append(opts, ddlOption{Label: "清除 DDL", Value: ""})
	}
	return opts
}

// openDdl 打开 DDL 设置页。
func (a *App) openDdl() {
	target, kind := a.currentDdl()
	if target == nil {
		a.setToast("先在左栏或右栏选中一个条目，再按 D 设截止时间", toastWarn)
		return
	}
	a.ddlView = ddlViewState{active: true, target: target, kind: kind}
	a.view = ViewDdl
	a.pageScroll = 0
}

// closeDdl 关闭 DDL 设置页。
func (a *App) closeDdl() {
	a.ddlView = ddlViewState{}
	a.view = ViewDashboard
}

// inputHint 返回该条目类型对应的输入提示与示例。
func (a *App) ddlInputHint() (string, string) {
	if a.ddlView.target != nil && a.ddlView.target.DueIsTodo() {
		return "截止时间（HH:MM）", "18:30"
	}
	return "截止日期（YYYY-MM-DD）", "2026-10-31"
}

// handleDdlKey 处理 DDL 设置页的按键。
func (a *App) handleDdlKey(key string) (tea.Model, tea.Cmd) {
	dv := &a.ddlView
	opts := a.ddlOptions(dv.target)

	switch key {
	case "j", "down":
		if n := len(opts); n > 0 {
			dv.cursor = (dv.cursor + 1) % n
		}
	case "k", "up":
		if n := len(opts); n > 0 {
			dv.cursor = (dv.cursor - 1 + n) % n
		}
	case "g", "home":
		dv.cursor = 0
	case "G", "end":
		if n := len(opts); n > 0 {
			dv.cursor = n - 1
		}
	case "enter", " ":
		if dv.cursor < 0 || dv.cursor >= len(opts) {
			return a, nil
		}
		return a, a.applyDdl(opts[dv.cursor].Value)
	case "e":
		// 自己输入。输入框里预填当前值，方便微调；校验失败时会保留用户输入，
		// 不会把已有 DDL 弄丢。
		label, example := a.ddlInputHint()
		a.editor.set(label+"，例如 "+example, dv.target.DueText())
		a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
			return a, a.commitDdlInput(value)
		}
	case "d":
		// 直接清除，不再问一次：清除 DDL 不丢用户内容，且随时可以重设。
		if dv.target.DueText() == "" {
			a.setToast("这个条目本来就没有 DDL", toastWarn)
			return a, nil
		}
		return a, a.applyDdl("")
	case "esc", "q":
		a.closeDdl()
		return a, saveCmd(a.saveDay)
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	}
	return a, nil
}

// commitDdlInput 处理自定义输入：校验后写入，非法时留在输入框让用户改。
func (a *App) commitDdlInput(value string) tea.Cmd {
	dv := &a.ddlView
	if dv.target == nil {
		return nil
	}
	parsed, err := a.parseDdlInput(value)
	if err != nil {
		// 校验失败：保留输入框，用户接着改，不关闭页面。
		label, example := a.ddlInputHint()
		a.editor.set(label+"，例如 "+example, value)
		a.editor.onCommit = func(v string) (tea.Model, tea.Cmd) {
			return a, a.commitDdlInput(v)
		}
		a.setToast(err.Error(), toastErr)
		return nil
	}
	return a.applyDdl(parsed)
}

// parseDdlInput 按条目类型校验输入。
func (a *App) parseDdlInput(value string) (string, error) {
	if a.ddlView.target != nil && a.ddlView.target.DueIsTodo() {
		return model.ParseDueTime(value)
	}
	return model.ParseDueDate(value)
}

// applyDdl 写入 DDL 并落盘。
func (a *App) applyDdl(value string) tea.Cmd {
	dv := &a.ddlView
	if dv.target == nil {
		return nil
	}
	// 先校验再写：非法值绝不入库。
	parsed, err := a.parseDdlInput(value)
	if err != nil {
		a.setToast(err.Error(), toastErr)
		return nil
	}
	dv.target.SetDue(parsed)
	if parsed == "" {
		a.setToast("已清除 DDL", toastInfo)
	} else {
		a.setToast(fmt.Sprintf("「%s」的 DDL 已设为 %s", dv.target.ItemTitle(), parsed), toastInfo)
	}
	return saveCmd(a.saveDay)
}

// ddlContent 渲染 DDL 设置页的内容，只占中间栏。
func (a *App) ddlContent() string {
	dv := &a.ddlView
	inner := a.contentWidth()
	now := a.clock.Now()

	var lines []string
	lines = append(lines, a.modalLine(a.st.Title, "截止时间 / DDL", inner))
	lines = append(lines, "")
	lines = append(lines, a.modalLine(a.st.Text, truncate(fmt.Sprintf("%s：%s", dv.kind, dv.target.ItemTitle()), inner), inner))

	// 当前 DDL 与倒计时。
	current := "（未设置）"
	if due := dv.target.DueText(); due != "" {
		state, left := model.DueStatus(due, dv.target.DueIsTodo(), now, a.cfg.Cutoff(), a.cfg.Location())
		if state == model.DueNone {
			current = due + "（格式无法识别，建议重新设置）"
		} else {
			current = fmt.Sprintf("%s · 剩余 %s", due, model.DueCountdown(left))
		}
	}
	lines = append(lines, a.modalLine(a.st.Accent, "当前："+current, inner))
	lines = append(lines, "")

	// 粒度说明：需求明确要求 TODO 只到分、GOAL 只到日。
	if dv.target.DueIsTodo() {
		lines = append(lines, a.modalLine(a.st.Muted, "TODO 的 DDL 只到时分（每天重置）", inner))
	} else {
		lines = append(lines, a.modalLine(a.st.Muted, "GOAL 的 DDL 只到年月日（到当天结束）", inner))
	}
	lines = append(lines, "")

	opts := a.ddlOptions(dv.target)
	for i, opt := range opts {
		row := "  " + opt.Label
		if i == dv.cursor {
			lines = append(lines, a.st.ModalCursor.Render(padTo("▸ "+opt.Label, highlightWidth("▸ "+opt.Label, inner))))
			continue
		}
		lines = append(lines, a.st.Text.Render(truncate(row, inner)))
	}

	lines = append(lines, "")
	lines = append(lines, a.modalLine(a.st.Muted, "enter 应用 · e 自己输入 · d 清除", inner))
	lines = append(lines, a.modalLine(a.st.Muted, "j/k 移动 · esc 完成", inner))
	return strings.Join(lines, "\n")
}
