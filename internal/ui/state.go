package ui

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// ---------- 随机字条 ----------

// Quotes 是看板中间栏滚动展示的句子（见需求 5）。
var Quotes = []string{
	"时机成熟时，一切都会水到渠成。",
	"把今天过好，就是对未来最好的投资。",
	"专注不是做更多，而是少做一点别的。",
	"不必完美地开始，只需开始。",
	"你不需要更多时间，你需要更少的干扰。",
	"每一次开始计时，都是一次对目标的投票。",
	"拖延的代价，是别人替你过完这一生。",
	"慢慢来，但别停。",
	"今天的一小步，抵得过明天的一大步。",
	"记录本身就是觉察，觉察本身就是改变。",
	"完成胜过完美。",
	"时间不会等人，但它会奖励尊重它的人。",
	"你专注的地方，就是你人生生长的地方。",
	"先做最重要的那件事，其余自会排队。",
}

// quoteEvery 是看板中间栏字条的轮换间隔（见需求 5）。
const quoteEvery = 12 * time.Second

// NextQuote 轮换到下一条字条。
func (a *App) NextQuote() {
	list := a.customQuotes()
	if len(list) == 0 {
		return
	}
	next := rand.Intn(len(list))
	if next == a.quoteIdx {
		next = (next + 1) % len(list)
	}
	a.quoteIdx = next
}

// CurrentQuote 返回当前要展示的字条。
func (a *App) CurrentQuote() string {
	list := a.customQuotes()
	if len(list) == 0 {
		return ""
	}
	if a.quoteIdx < 0 || a.quoteIdx >= len(list) {
		a.quoteIdx = 0
	}
	return list[a.quoteIdx]
}

// ---------- 选择框 ----------

type pickItem struct {
	Label  string
	Action string
}

type pickState struct {
	title  string
	items  []pickItem
	cursor int
	// small 表示这是个需要逐项确认的选择（两个以上的选项）。
	// 此时禁用 y/n 快捷键——它们只会被误按到第一项/最后一项。
	small bool
}

// ---------- 文本输入 ----------

type editorState struct {
	active bool
	label  string
	value  []rune
	cursor int
	// original 记录打开时的内容，用来判断有没有改动（见 Dirty）。
	original string
	// multiline 为真时允许换行（随手记、自定义字条）；
	// Enter 变成换行，用 Ctrl+S 或 Ctrl+D 提交。
	// 同时也决定 esc / q 关闭时要不要先问“保存还是丢弃”。
	multiline bool
	// onCommit 在用户确认时被调用，返回新的模型与命令。
	onCommit func(string) (tea.Model, tea.Cmd)
}

func (e *editorState) set(label, initial string) {
	e.active = true
	e.label = label
	e.value = []rune(initial)
	e.cursor = len(e.value)
	e.original = initial
	e.multiline = false
}

// setMultiline 以多行模式打开输入框。
func (e *editorState) setMultiline(label, initial string) {
	e.set(label, initial)
	e.multiline = true
}

// lineStart 返回光标所在行的起始下标。
func (e *editorState) lineStart() int {
	i := e.cursor
	for i > 0 && e.value[i-1] != '\n' {
		i--
	}
	return i
}

// lineEnd 返回光标所在行的结束下标（不含换行符）。
func (e *editorState) lineEnd() int {
	i := e.cursor
	for i < len(e.value) && e.value[i] != '\n' {
		i++
	}
	return i
}

// lineCount 返回当前内容的行数。
func (e *editorState) lineCount() int {
	if len(e.value) == 0 {
		return 1
	}
	return strings.Count(string(e.value), "\n") + 1
}

// Dirty 报告内容相对打开时是否被改过。
//
// 用于“随手记没保存就要退出”时给出提示：只有真的改过才值得打扰用户，
// 打开看一眼又原样退出的情况不该弹窗。
func (e *editorState) Dirty() bool {
	return string(e.value) != e.original
}

func (e *editorState) insert(r rune) {
	if e.cursor >= len(e.value) {
		e.value = append(e.value, r)
		e.cursor = len(e.value)
		return
	}
	e.value = append(e.value, 0)
	copy(e.value[e.cursor+1:], e.value[e.cursor:])
	e.value[e.cursor] = r
	e.cursor++
}

func (e *editorState) backspace() {
	if e.cursor == 0 || len(e.value) == 0 {
		return
	}
	e.value = append(e.value[:e.cursor-1], e.value[e.cursor:]...)
	e.cursor--
}

func (e *editorState) delete() {
	if e.cursor >= len(e.value) {
		return
	}
	e.value = append(e.value[:e.cursor], e.value[e.cursor+1:]...)
}

func (e *editorState) move(delta int) {
	e.cursor = clamp(e.cursor+delta, 0, len(e.value))
}

// moveVertical 在多行模式下上下移动光标，尽量保持列位置。
func (e *editorState) moveVertical(delta int) {
	col := e.cursor - e.lineStart()
	if delta < 0 {
		start := e.lineStart()
		if start == 0 {
			return
		}
		prevEnd := start - 1
		prevStart := prevEnd
		for prevStart > 0 && e.value[prevStart-1] != '\n' {
			prevStart--
		}
		e.cursor = clamp(prevStart+col, prevStart, prevEnd)
		return
	}
	end := e.lineEnd()
	if end >= len(e.value) {
		return
	}
	nextStart := end + 1
	nextEnd := nextStart
	for nextEnd < len(e.value) && e.value[nextEnd] != '\n' {
		nextEnd++
	}
	e.cursor = clamp(nextStart+col, nextStart, nextEnd)
}

func (e *editorState) text() string { return strings.TrimSpace(string(e.value)) }

// ---------- 计时器 ----------

// timerState 是一次进行中的计时（见需求 17、20、21）。
type timerState struct {
	plan    model.Plan
	started time.Time
	todoRef string
	todoTag string
	name    string

	paused    bool
	pausedAt  time.Time
	pausedDur time.Duration

	// finished 标记时段是否已经走完。
	finished bool
	// doneNotified 保证“完成”提示与响铃只触发一次。
	doneNotified bool
	// bell 在下一帧输出响铃字符，提醒用户时段结束。
	bell bool
	// notifiedSeg 记录已经提示过的时段序号，避免重复提示。
	notifiedSeg int
	lastSeg     int
}

// newTimer 依据计时方案创建计时器。
func newTimer(plan model.Plan, todo *model.Todo, now time.Time) *timerState {
	t := &timerState{
		plan:        plan,
		started:     now,
		notifiedSeg: -1,
		lastSeg:     -1,
		name:        "自由专注",
	}
	if todo != nil {
		t.todoRef = todo.ID
		t.todoTag = todo.GoalTag
		t.name = todo.Title
	}
	return t
}

// elapsed 返回已经过去的有效时长（不含暂停时间）。
func (t *timerState) elapsed(now time.Time) time.Duration {
	d := now.Sub(t.started) - t.pausedDur
	if t.paused {
		d -= now.Sub(t.pausedAt)
	}
	if d < 0 {
		d = 0
	}
	return d
}

// total 返回全时段总时长；正计时为 0。
func (t *timerState) total() time.Duration { return t.plan.Total() }

// remaining 返回剩余时长；正计时返回 0。
func (t *timerState) remaining(now time.Time) time.Duration {
	total := t.total()
	if total <= 0 {
		return 0
	}
	left := total - t.elapsed(now)
	if left < 0 {
		return 0
	}
	return left
}

// progress 返回 0..1 的完成比例。
func (t *timerState) progress(now time.Time) float64 {
	total := t.total()
	if total <= 0 {
		return 0
	}
	p := float64(t.elapsed(now)) / float64(total)
	if p > 1 {
		p = 1
	}
	return p
}

// segment 返回当前所处时段。
func (t *timerState) segment(now time.Time) (int, model.Segment, time.Duration) {
	return t.plan.SegmentAt(t.elapsed(now))
}

// pause 切换暂停状态。
func (t *timerState) pause(now time.Time) {
	if t.paused {
		t.pausedDur += now.Sub(t.pausedAt)
		t.paused = false
		return
	}
	t.paused = true
	t.pausedAt = now
}

// tick 推进计时；时段走完时返回提示命令。
//
// 完成通知只发送一次：动画帧每 120 毫秒推进一次，若不加这个判断，
// 用户会被同一句“已完成”反复刷屏。
func (t *timerState) tick(now time.Time) tea.Cmd {
	if t.paused || t.doneNotified {
		return nil
	}
	if t.total() <= 0 {
		return nil
	}
	idx, _, _ := t.segment(now)
	if idx != t.lastSeg {
		t.lastSeg = idx
	}
	if t.elapsed(now) >= t.total() {
		t.finished = true
		t.doneNotified = true
		t.bell = true
		return func() tea.Msg { return timerDoneMsg{} }
	}
	return nil
}

// consumeBell 取出并清除响铃标记。
func (t *timerState) consumeBell() bool {
	if !t.bell {
		return false
	}
	t.bell = false
	return true
}

// timerDoneMsg 表示计时自然结束。
type timerDoneMsg struct{}

// stop 结束计时并生成归档记录。
func (a *App) stopTimer(interrupted bool) {
	if a.timer == nil {
		return
	}
	now := a.clock.Now()
	elapsed := a.timer.elapsed(now)
	_, seg, _ := a.timer.segment(now)

	session := model.Session{
		ID:          model.NewID("sess"),
		Plan:        a.timer.plan,
		TodoRef:     a.timer.todoRef,
		TodoName:    a.timer.name,
		Started:     a.timer.started,
		Ended:       &now,
		Elapsed:     elapsed,
		Completed:   !interrupted,
		SegmentName: seg.Name,
		SegmentKind: seg.Kind,
	}
	a.data.Archive.Sessions = append(a.data.Archive.Sessions, session)

	// 累计到条目活动时长，便于历史统计（见需求 11）。
	if elapsed > time.Second {
		key := session.TodoName
		act, ok := a.data.Activity[key]
		if !ok {
			act = &model.Activity{Name: key, First: session.Started}
			a.data.Activity[key] = act
		}
		act.Last = now
		act.Total += elapsed
		act.Sessions++
	}

	a.timer = nil
	if err := a.store.SaveDay(a.data); err != nil {
		a.setToast("计时记录保存失败："+err.Error(), toastErr)
		return
	}
	if interrupted {
		a.setToast(fmt.Sprintf("计时已中断，记录 %s 到「%s」", clock.HumanDuration(elapsed), session.TodoName), toastWarn)
	} else {
		a.setToast(fmt.Sprintf("计时完成，记录 %s 到「%s」", clock.HumanDuration(elapsed), session.TodoName), toastInfo)
	}
}

// ---------- 庆祝特效 ----------

const celebrateFor = 4 * time.Second

type celebrateState struct {
	started time.Time
	seed    int64
}

// celebrateFrame 渲染一次庆祝动画；t 为 0..1 的进度。
func (a *App) celebrateFrame(t float64, width, height int) string {
	if width < 8 || height < 3 {
		return a.st.OK.Render("全部完成！")
	}
	seed := a.celebrate.seed
	if seed == 0 {
		seed = time.Now().UnixNano()
		a.celebrate.seed = seed
	}
	rng := rand.New(rand.NewSource(seed))

	// 预生成粒子，保证同一场庆祝的粒子位置稳定。
	type particle struct {
		x, y  float64
		glyph rune
		col   lipgloss.Color
	}
	glyphs := []rune{'✦', '✧', '★', '✺', '❋', '·', '•'}
	colors := []lipgloss.Color{
		a.st.Theme.Primary, a.st.Theme.Secondary, a.st.Theme.Success,
		a.st.Theme.Warning, a.st.Theme.Danger,
	}
	n := 46
	parts := make([]particle, n)
	for i := range parts {
		parts[i] = particle{
			x:     rng.Float64(),
			y:     rng.Float64(),
			glyph: glyphs[rng.Intn(len(glyphs))],
			col:   colors[rng.Intn(len(colors))],
		}
	}

	grid := make([][]rune, height)
	styleGrid := make([][]lipgloss.Color, height)
	for y := range grid {
		grid[y] = make([]rune, width)
		styleGrid[y] = make([]lipgloss.Color, width)
		for x := range grid[y] {
			grid[y][x] = ' '
		}
	}

	for _, p := range parts {
		// 粒子从中心向外扩散并下落。
		px := 0.5 + (p.x-0.5)*(0.4+t*1.6)
		py := p.y + t*t*0.6
		x := int(px * float64(width-1))
		y := int(py * float64(height-1))
		if x < 0 || y < 0 || x >= width || y >= height {
			continue
		}
		grid[y][x] = p.glyph
		styleGrid[y][x] = p.col
	}

	var b strings.Builder
	for y := range grid {
		for x := range grid[y] {
			if grid[y][x] == ' ' {
				b.WriteRune(' ')
				continue
			}
			b.WriteString(lipgloss.NewStyle().Foreground(styleGrid[y][x]).Render(string(grid[y][x])))
		}
		if y < len(grid)-1 {
			b.WriteByte('\n')
		}
	}
	banner := a.st.OK.Bold(true).Render("🎉 今日 TODO 全部完成！")
	return overlayCenter(b.String(), banner, width, height)
}

// overlayCenter 把一段文字叠在画面正中。
func overlayCenter(base, text string, width, height int) string {
	lines := strings.Split(base, "\n")
	if len(lines) == 0 {
		return text
	}
	mid := len(lines) / 2
	textLines := strings.Split(text, "\n")
	start := mid - len(textLines)/2
	for i, tl := range textLines {
		y := start + i
		if y < 0 || y >= len(lines) {
			continue
		}
		pad := (width - lipgloss.Width(tl)) / 2
		if pad < 0 {
			pad = 0
		}
		lines[y] = strings.Repeat(" ", pad) + tl
	}
	return strings.Join(lines, "\n")
}
