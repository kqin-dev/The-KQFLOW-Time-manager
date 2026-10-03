package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/clock"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/model"
)

// 时段类型，决定进度条上用什么颜色区分（见需求 20）。
var segmentKinds = []struct {
	Key   string
	Label string
}{
	{"focus", "专注"},
	{"break", "休息"},
	{"other", "自定义"},
}

// customState 是“自定义时段”编辑器的状态。
//
// 需求 20 要求自定义状态下显示用户自定义的状态名，因此这里让用户自己
// 编排若干段，每段可以改名、改时长、改类型，再据此渲染分段进度条。
type customState struct {
	plan   model.Plan
	cursor int
}

// defaultCustomPlan 依据设置页里的专注/休息时长生成一份初始方案。
func (a *App) defaultCustomPlan() model.Plan {
	return model.Plan{
		Kind: model.TimerCustom,
		Segments: []model.Segment{
			{Name: "深度工作", Kind: "focus", Dur: time.Duration(a.cfg.FocusMinutes()) * time.Minute},
			{Name: "休息", Kind: "break", Dur: time.Duration(a.cfg.BreakMinutes()) * time.Minute},
		},
	}
}

// openCustom 打开自定义时段编辑器。
func (a *App) openCustom() {
	a.custom = &customState{plan: a.defaultCustomPlan()}
}

// moveCustom 移动时段光标。
func (a *App) moveCustom(delta int) {
	if a.custom == nil || len(a.custom.plan.Segments) == 0 {
		return
	}
	n := len(a.custom.plan.Segments)
	a.custom.cursor = (a.custom.cursor + delta + n) % n
}

// addCustomSegment 在当前段之后插入一段。
func (a *App) addCustomSegment() {
	if a.custom == nil {
		return
	}
	seg := model.Segment{Name: "新时段", Kind: "focus", Dur: 15 * time.Minute}
	at := a.custom.cursor + 1
	segs := a.custom.plan.Segments
	if at >= len(segs) {
		a.custom.plan.Segments = append(segs, seg)
		a.custom.cursor = len(a.custom.plan.Segments) - 1
		return
	}
	segs = append(segs[:at], append([]model.Segment{seg}, segs[at:]...)...)
	a.custom.plan.Segments = segs
	a.custom.cursor = at
}

// deleteCustomSegment 删除当前时段，至少保留一段。
func (a *App) deleteCustomSegment() {
	if a.custom == nil || len(a.custom.plan.Segments) <= 1 {
		a.setToast("至少保留一个时段", toastWarn)
		return
	}
	segs := a.custom.plan.Segments
	at := a.custom.cursor
	a.custom.plan.Segments = append(segs[:at], segs[at+1:]...)
	if a.custom.cursor >= len(a.custom.plan.Segments) {
		a.custom.cursor = len(a.custom.plan.Segments) - 1
	}
}

// cycleCustomKind 轮换当前时段的类型。
func (a *App) cycleCustomKind() {
	if a.custom == nil {
		return
	}
	segs := a.custom.plan.Segments
	if a.custom.cursor >= len(segs) {
		return
	}
	cur := segs[a.custom.cursor].Kind
	next := "focus"
	for i, k := range segmentKinds {
		if k.Key == cur {
			next = segmentKinds[(i+1)%len(segmentKinds)].Key
			break
		}
	}
	segs[a.custom.cursor].Kind = next
}

// handleCustomKey 处理自定义时段编辑器的按键。
func (a *App) handleCustomKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "j", "down":
		a.moveCustom(1)
	case "k", "up":
		a.moveCustom(-1)
	case "n", "a":
		a.addCustomSegment()
	case "d":
		a.deleteCustomSegment()
	case "t":
		a.cycleCustomKind()
	case "e":
		a.editCustomName()
	case "p":
		a.editCustomDuration()
	case "r":
		a.custom = &customState{plan: a.defaultCustomPlan()}
		a.setToast("已恢复默认时段", toastInfo)
	case "enter":
		return a.commitCustom()
	case "esc", "q":
		a.custom = nil
		a.setToast("已取消自定义时段", toastInfo)
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	}
	return a, nil
}

// editCustomName 让用户给当前时段起名。
func (a *App) editCustomName() {
	if a.custom == nil {
		return
	}
	idx := a.custom.cursor
	segs := a.custom.plan.Segments
	if idx >= len(segs) {
		return
	}
	nameCommit := a.makeNameCommit(idx, segs)
	a.editor.set("时段名称", segs[idx].Name)
	a.editor.onCommit = nameCommit
}

// makeNameCommit 生成“改名”的确认回调；输入非法时保留输入框让用户重试。
func (a *App) makeNameCommit(idx int, segs []model.Segment) func(string) (tea.Model, tea.Cmd) {
	return func(value string) (tea.Model, tea.Cmd) {
		if value == "" {
			a.setToast("名称不能为空", toastWarn)
			a.editor.set("时段名称", segs[idx].Name)
			a.editor.onCommit = a.makeNameCommit(idx, segs)
			return a, nil
		}
		segs[idx].Name = value
		return a, nil
	}
}

// editCustomDuration 让用户给当前时段设时长。
func (a *App) editCustomDuration() {
	if a.custom == nil {
		return
	}
	idx := a.custom.cursor
	segs := a.custom.plan.Segments
	if idx >= len(segs) {
		return
	}
	cs := a.custom
	a.editor.set(fmt.Sprintf("「%s」时长（分钟）", segs[idx].Name),
		fmt.Sprintf("%d", int(segs[idx].Dur.Minutes())))
	a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
		mins, err := parseMinutes(value)
		if err != nil {
			a.setToast(err.Error(), toastErr)
			return a, nil
		}
		cs.plan.Segments[idx].Dur = mins
		return a, nil
	}
}

// commitCustom 校验并开始这次自定义计时。
func (a *App) commitCustom() (tea.Model, tea.Cmd) {
	if a.custom == nil {
		return a, nil
	}
	if len(a.custom.plan.Segments) < 2 {
		a.setToast("自定义时段至少需要两段，单段请直接用倒计时", toastWarn)
		return a, nil
	}
	for _, seg := range a.custom.plan.Segments {
		if seg.Dur <= 0 {
			a.setToast("每段时长都必须大于 0", toastErr)
			return a, nil
		}
		if strings.TrimSpace(seg.Name) == "" {
			a.setToast("每段都需要名称", toastErr)
			return a, nil
		}
	}
	plan := a.custom.plan
	plan.Kind = model.TimerCustom
	a.custom = nil
	a.chooseTimerTodo(plan)
	return a, nil
}

// renderCustomEditor 渲染自定义时段编辑器。
func (a *App) renderCustomEditor() string {
	// 内容按可用宽度自适应，窄终端下自动收窄名字列。
	inner := a.modalInner(60)
	nameW := inner - 22
	if nameW > 16 {
		nameW = 16
	}
	if nameW < 4 {
		nameW = 4
	}

	var lines []string
	lines = append(lines, a.modalLine(a.st.Title, "自定义时段", inner))

	total := a.custom.plan.Total()
	for i, seg := range a.custom.plan.Segments {
		marker := "  "
		if i == a.custom.cursor {
			marker = "▸ "
		}
		line := fmt.Sprintf("%s%d. %s %s %s",
			marker, i+1, pad(truncate(seg.Name, nameW), nameW),
			segKindLabel(seg.Kind), clock.ClockString(seg.Dur))
		line = truncate(line, inner)
		if i == a.custom.cursor {
			lines = append(lines, a.st.ModalCursor.Render(pad(line, inner)))
		} else {
			lines = append(lines, a.st.Text.Render(line))
		}
	}
	lines = append(lines, a.modalLine(a.st.Accent,
		fmt.Sprintf("合计 %s（%d 段）", clock.ClockString(total), len(a.custom.plan.Segments)), inner))
	lines = append(lines, "")
	// 说明与帮助在窄终端下会挤占看板，压到两行。
	if a.IsCompact() {
		lines = append(lines, a.modalLine(a.st.Muted, "e 改名 · p 时长 · t 类型 · n 新增 · d 删除", inner))
		lines = append(lines, a.modalLine(a.st.Muted, "enter 开始 · esc 取消 · r 恢复默认", inner))
	} else {
		lines = append(lines, a.modalLine(a.st.Muted, "按顺序依次进行，进度条会按类型着色。", inner))
		lines = append(lines, "")
		lines = append(lines, a.modalLine(a.st.Muted, "j/k 选择 · e 改名 · p 改时长 · t 改类型", inner))
		lines = append(lines, a.modalLine(a.st.Muted, "n 新增一段 · d 删除 · r 恢复默认", inner))
		lines = append(lines, a.modalLine(a.st.Muted, "enter 开始 · esc 取消", inner))
	}

	body := a.modalBox(inner, lines)
	return overlay(a.renderDashboard(), body, a.width, a.height)
}

// segKindLabel 返回时段类型的中文名。
func segKindLabel(kind string) string {
	for _, k := range segmentKinds {
		if k.Key == kind {
			return k.Label
		}
	}
	return "自定义"
}

// parseMinutes 解析用户输入的分钟数。
func parseMinutes(value string) (time.Duration, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("请输入 1-600 之间的分钟数")
	}
	if n <= 0 || n > 600 {
		return 0, fmt.Errorf("分钟数需在 1-600 之间")
	}
	return time.Duration(n) * time.Minute, nil
}
