package kxapp

import (
	"fmt"
	"time"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/semver"
	"github.com/kqin-dev/kxflow/svc"
	"github.com/kqin-dev/kxflow/tile"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// CapFocusSession 是"有专注会话记录"这个能力标记。
//
// 统计包（柱状图）依赖它：没有计时就没有专注时长可画。
const CapFocusSession = "focus.session"

// Timer 是专注计时器的进程内状态。
//
// 设计取向与 v2.1.0 一致的地方：**不把"已经跑了多久"存成一个计数器**，
// 而是存"什么时候开始的"和"暂停了多久"，用时钟算差值。
// 理由很实在：TUI 每秒才重绘一次，靠累加计数会慢慢漂移，
// 而"开始时刻 + 暂停累计"在任何时刻都能精确还原，也扛得住丢帧。
type Timer struct {
	// running 为真表示正在计时（可能处于暂停）。
	running bool
	// paused 为真表示暂停中。
	paused bool
	// plan 是计时方案（各时段的名称与时长）。
	plan model.Plan
	// started 是本次计时的开始时刻。
	started time.Time
	// pausedTotal 是累计暂停时长；pausedAt 是本次暂停的开始时刻。
	pausedTotal time.Duration
	pausedAt    time.Time
	// todoRef / todoName 记录这次专注是挂在哪个条目上的（可为空）。
	todoRef  string
	todoName string
	// announced 记录"完成提示"是否已经发过，保证只响一次。
	announced bool
}

// init 把计时器复位到"未开始"。
func (t *Timer) init() { *t = Timer{} }

// Running 报告是否正在计时。
func (t Timer) Running() bool { return t.running }

// Paused 报告是否处于暂停。
func (t Timer) Paused() bool { return t.paused }

// Plan 返回当前方案。
func (t Timer) Plan() model.Plan { return t.plan }

// TodoName 返回这次专注挂着的条目名（可为空）。
func (t Timer) TodoName() string { return t.todoName }

// elapsed 返回"已经跑了多久"（扣除暂停时间）。
//
// 它是唯一的时间计算入口：渲染、判断是否完成、写记录都用它，
// 因此不可能出现"界面显示 10 分钟、记录里写 9 分钟"这种不一致。
func (t Timer) elapsed(now time.Time) time.Duration {
	if !t.running {
		return 0
	}
	end := now
	if t.paused {
		// 暂停期间时间不走：以暂停那一刻为准。
		end = t.pausedAt
	}
	d := end.Sub(t.started) - t.pausedTotal
	if d < 0 {
		return 0
	}
	return d
}

// ElapsedFor 暴露"已经跑了多久"给同包的展示代码。
//
// 加这个方法而不是把 elapsed 导出到包外：时间口径只能有一处实现，
// 但同包的展示代码需要一个入口（它不该自己再算一遍）。
func (t Timer) ElapsedFor(now time.Time) time.Duration { return t.elapsed(now) }

// Remaining 返回距离方案结束还剩多久（未在计时则为 0）。
func (t Timer) Remaining(now time.Time) time.Duration {
	if !t.running {
		return 0
	}
	total := t.plan.Total()
	if total <= 0 {
		return 0
	}
	left := total - t.elapsed(now)
	if left < 0 {
		return 0
	}
	return left
}

// Done 报告方案是否已经走完。
func (t Timer) Done(now time.Time) bool {
	if !t.running {
		return false
	}
	total := t.plan.Total()
	return total > 0 && t.elapsed(now) >= total
}

// CurrentSegment 返回当前所在时段（下标、时段、该时段内已过的时长）。
//
// 未在计时时返回空时段。用 model.Plan.SegmentAt —— 时段切分规则
// 只能有一处实现（它同时也是"计时记录里 focus/break 怎么分"的依据）。
func (t Timer) CurrentSegment(now time.Time) (int, model.Segment, time.Duration) {
	if !t.running {
		return -1, model.Segment{}, 0
	}
	return t.plan.SegmentAt(t.elapsed(now))
}

// ---------- 操作 ----------

// Start 开始一次计时。
//
// 已在计时时不重复开始：那样会把 started 覆盖成现在，等于白跑一段。
func (t *Timer) Start(plan model.Plan, todo *model.Todo, now time.Time) bool {
	if t.running {
		return false
	}
	*t = Timer{running: true, plan: plan, started: now}
	if todo != nil {
		t.todoRef = todo.ID
		t.todoName = todo.Title
	}
	return true
}

// Pause 暂停计时；返回是否真的发生了变化。
func (t *Timer) Pause(now time.Time) bool {
	if !t.running || t.paused {
		return false
	}
	t.paused = true
	t.pausedAt = now
	return true
}

// Resume 继续计时。
func (t *Timer) Resume(now time.Time) bool {
	if !t.running || !t.paused {
		return false
	}
	// 把这一段的暂停时长累加进去，然后清掉暂停标记。
	t.pausedTotal += now.Sub(t.pausedAt)
	t.paused = false
	t.pausedAt = time.Time{}
	return true
}

// Stop 结束计时并返回要写入记录的会话。
//
// 停止之后计时器复位：下一次 Start 是全新的一次。
func (t *Timer) Stop(now time.Time) model.Session {
	if !t.running {
		return model.Session{}
	}
	s := t.session(now)
	t.init()
	return s
}

// session 把当前计时状态整理成一条可落盘的会话记录。
//
// 字段口径与 v2.1.0 一致：Elapsed 是挂钟时长（含休息），
// Focus 是本方案里**专注时段**的长度（不含休息）——
// 后者才是用户关心的"今天专注了多久"。
func (t Timer) session(now time.Time) model.Session {
	elapsed := t.elapsed(now)
	focus := t.plan.FocusUpTo(elapsed)
	s := model.Session{
		ID:        newSessionID(now),
		Plan:      t.plan,
		TodoRef:   t.todoRef,
		TodoName:  t.todoName,
		Started:   t.started,
		Elapsed:   elapsed,
		Completed: elapsed >= t.plan.Total(),
	}
	end := now
	s.Ended = &end
	if focus != elapsed {
		f := focus
		s.Focus = &f
	}
	if idx, seg, _ := t.plan.SegmentAt(elapsed); idx >= 0 {
		s.SegmentName = seg.Name
		s.SegmentKind = seg.Kind
	}
	return s
}

// newSessionID 生成会话 ID。
//
// 用时间戳而不是随机数：会话记录是给人看的，"哪一次"只要唯一即可，
// 而时间戳在排查问题时能直接对上"几点开始的"。
func newSessionID(now time.Time) string {
	return "sess_" + now.Format("20060102150405")
}

// TimerPackID / TimerTileID 是计时包与它的磁贴。
const (
	TimerPackID = "kqflow.timer"
	TimerTileID = "kqflow.timer.tile"
	// TimerOptionID 是看板选项：不选中任何条目也能开始一次"自由专注"。
	TimerOptionID = "kqflow.timer.board"
)

// timerPack 是计时整合包：**磁贴 + 看板选项**。
//
// 这是"整合包"的第二个好例子（design §5.1）：计时天然横跨两种插件类型——
//
//	磁贴：看板上正在跑的倒计时（不占焦点也能看见）
//	看板选项：开始一次"自由专注"（不需要先选中某个待办）
//
// 两半拆开都不成立（只能看不能开始、或只能开始看不到在跑），
// 因此它们必须同属一个包。这与 kqflow.ddl（磁贴 + **联动**选项）
// 的区别在于触发条件：自由专注不依赖选中，所以它是**看板**选项。
type timerPack struct {
	src   Source
	state *HostState
}

// NewTimerPack 创建计时包。
func NewTimerPack(src Source, st *HostState) plugin.Pack {
	return &timerPack{src: src, state: st}
}

func (p *timerPack) ID() string              { return TimerPackID }
func (p *timerPack) Name() string            { return "专注计时" }
func (p *timerPack) Version() semver.Version { return semver.MustParse("0.1.0") }
func (p *timerPack) EngineAPI() semver.Range { return engineRange }
func (p *timerPack) Enabled() bool           { return true }
func (p *timerPack) Requires() []string      { return nil }
func (p *timerPack) Conflicts() []string     { return nil }

// Provides 声明本包提供"专注会话记录"。
//
// 统计包据此判定"有没有东西可统计"：没有计时包时它会进 Inactive
// 并给一条警告（不是错误）——这正是 design §4.5 的机制。
func (p *timerPack) Provides() []string { return []string{CapFocusSession} }

func (p *timerPack) Members() []plugin.Plugin {
	return []plugin.Plugin{
		&tilePluginSpec{
			mf: plugin.Manifest{
				ID: TimerTileID, Name: "专注计时", Kind: plugin.KindTile,
				Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
				// 放在中栏停靠区：计时是个"看着就行"的小状态，
				// 不该挤掉侧栏的列表。
				//
				// 这不是随手选的——它原本声明 AnchorLeftBottom，
				// 于是与"临时待办"抢同一个槽位，后者被挤成未安置，
				// **整份临时列表从界面上消失了**（测试当场抓到）。
				// 磁贴的槽位偏好是会互相挤掉的，声明前要想清楚它在版面上的角色。
				Slots: plugin.SlotPreference{Anchor: geometry.AnchorCenterDockRight, Priority: 20},
			},
			newComp: func(s svc.Services) plugin.Component {
				return &timerTile{src: p.src, state: p.state, svc: s}
			},
		},
		&ctxOptionSpec{
			mf: plugin.Manifest{
				ID: TimerContextOptionID, Name: "开始专注", Kind: plugin.KindContextOption,
				Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
			},
			opt: &timerContextOption{src: p.src, state: p.state},
		},
		&boardOptionSpec{
			mf: plugin.Manifest{
				ID: TimerOptionID, Name: "开始专注", Kind: plugin.KindBoardOption,
				Version: semver.MustParse("0.1.0"), EngineAPI: engineRange,
			},
			opt: &timerBoardOption{src: p.src, state: p.state},
		},
	}
}

func (p *timerPack) Assemble(s svc.Services) (plugin.Assembled, error) {
	a := &timerAssembled{p: p}
	for _, m := range p.Members() {
		switch spec := m.(type) {
		case *tilePluginSpec:
			comp, err := spec.New(s)
			if err != nil {
				return nil, err
			}
			a.tiles = append(a.tiles, comp)
		case *boardOptionSpec:
			a.board = append(a.board, spec.opt)
		case *ctxOptionSpec:
			a.ctx = append(a.ctx, spec.opt)
		}
	}
	return a, nil
}

type timerAssembled struct {
	p     *timerPack
	tiles []plugin.Component
	board []plugin.BoardOption
	ctx   []plugin.ContextOption
}

func (a *timerAssembled) Pack() plugin.Pack                  { return a.p }
func (a *timerAssembled) Kernel() plugin.Kernel              { return nil }
func (a *timerAssembled) Tiles() []plugin.Component          { return a.tiles }
func (a *timerAssembled) BoardOptions() []plugin.BoardOption { return a.board }
func (a *timerAssembled) ContextOptions() []plugin.ContextOption {
	return a.ctx
}
func (a *timerAssembled) Services() []plugin.Service { return nil }
func (a *timerAssembled) Dispose()                   {}

// boardOptionSpec 是"声明 + 看板选项实现"组成的插件。
type boardOptionSpec struct {
	mf  plugin.Manifest
	opt plugin.BoardOption
}

func (p *boardOptionSpec) Manifest() plugin.Manifest { return p.mf }
func (p *boardOptionSpec) New(svc.Services) (plugin.Component, error) {
	return nil, nil // 选项不渲染组件
}

// ---------- 磁贴：正在跑的倒计时 ----------

// timerTile 显示当前计时状态。
//
// 它**只读** HostState.Timer 并展示；所有会改变计时状态的按键都在
// 联动视图里（见 timerView）。理由：计时是可变的全局状态，
// 让磁贴在按键里改它，就会出现"焦点在别的磁贴上时也能改计时"这种怪事。
// 但"暂停/继续"是高频动作，放在磁贴上最顺手——因此那一个键例外，
// 且明确写在这里。
type timerTile struct {
	src   Source
	state *HostState
	svc   svc.Services
}

func (t *timerTile) Title() string { return "专注" }

func (t *timerTile) Render(ctx plugin.RenderCtx) {
	now := ctx.Svc.Clock()
	tm := t.state.Timer

	if !tm.Running() {
		y := ctx.Rect.Y
		y = drawWrapped(ctx, y, ctx.Rect, "未开始", tile.StyleMuted)
		if y < ctx.Rect.Y1() {
			drawWrapped(ctx, y, ctx.Rect, "按 l 选预设，或看板上开始自由专注", tile.StyleHint)
		}
		return
	}

	y := ctx.Rect.Y
	// 第一行：大字号的剩余时间（计时磁贴的核心信息）。
	left := tm.Remaining(now)
	line := clock.HumanDuration(left)
	if tm.Done(now) {
		line = "已完成"
	}
	style := tile.StyleAccent
	if tm.Paused() {
		line += "  已暂停"
		style = tile.StyleWarn
	}
	y = drawWrapped(ctx, y, ctx.Rect, line, style)

	// 第二行：当前时段。
	if _, seg, within := tm.CurrentSegment(now); seg.Name != "" {
		y = drawWrapped(ctx, y, ctx.Rect,
			fmt.Sprintf("%s %s / %s", seg.Name,
				clock.HumanDuration(within), clock.HumanDuration(seg.Dur)), tile.StyleStatus)
	}
	// 第三行：挂着的条目 + 已专注时长。
	if tm.TodoName() != "" {
		y = drawWrapped(ctx, y, ctx.Rect, "· "+tm.TodoName(), tile.StyleMuted)
	}
	if y < ctx.Rect.Y1() {
		focus := tm.Plan().FocusUpTo(tm.ElapsedFor(now))
		drawWrapped(ctx, y, ctx.Rect, "已专注 "+clock.HumanDuration(focus), tile.StyleMuted)
	}
}

// Update 处理时钟推进与空格键。
//
// 两件事分得很清楚：
//
//	EventTick  → 检查"时段是否走完"，走完就响一次铃
//	            （由引擎的 Model.Tick 每秒送达，**与焦点无关**）
//	" "        → 暂停 / 继续（只在拿到焦点时才有意义）
//
// 为什么"走完"必须走 Tick 而不是按键：用户很可能正在别的栏位里干活，
// 而那时才最需要提醒。挂在按键上就只有"恰好盯着这个磁贴按键"才会响。
func (t *timerTile) Update(ctx plugin.EventCtx, ev plugin.Event) plugin.Action {
	if ev.Kind == plugin.EventTick {
		return t.checkDone(ctx.Now)
	}

	if ev.Key != " " {
		return plugin.None()
	}
	if !t.state.Timer.Running() {
		return plugin.None()
	}
	if t.state.Timer.Paused() {
		t.state.Timer.Resume(ctx.Now)
		return plugin.Toast("已继续")
	}
	t.state.Timer.Pause(ctx.Now)
	return plugin.Toast("已暂停")
}

// FooterProgress 让专注进度显示在**下栏的进度条**上。
//
// 用户反馈："专注还没有进度条。"下栏本来就有进度条能力
// （chrome.FooterBar.HasProgress），只是没人往上填——这件事只有
// 正在跑的计时磁贴知道，因此由它申报。
//
// 文字用"专注 12m / 25m"：进度条本身只表达比例，
// 没有数字就说不清"还剩多久"，而后者才是用户真正要看的。
func (t *timerTile) FooterProgress(ctx plugin.RenderCtx) (float64, string, bool) {
	if !t.state.Timer.Running() {
		return 0, "", false
	}
	now := ctx.Svc.Clock()
	total := t.state.Timer.Plan().Total()
	if total <= 0 {
		return 0, "", false
	}
	elapsed := t.state.Timer.ElapsedFor(now)
	progress := float64(elapsed) / float64(total)

	label := "专注 " + clock.HumanDuration(elapsed) + " / " + clock.HumanDuration(total)
	if t.state.Timer.Paused() {
		label = "已暂停 · " + label
	}
	if t.state.Timer.Done(now) {
		label = "已完成 · " + label
	}
	return progress, label, true
}

// FocusSelection 回报：计时磁贴没有"当前条目"（它管的是时间），
// 返回零值把上一个磁贴的选中清掉。
func (t *timerTile) FocusSelection(plugin.RenderCtx) plugin.Selection {
	return plugin.Selection{}
}

// KeyHints 申报计时磁贴上的可用按键——**随计时状态变化**。
//
// 这正是"提示必须由插件包提供"的最好例子：没在计时时只有"开始"，
// 计时中才有"暂停"；这些只有计时包自己知道，引擎无从推断。
func (t *timerTile) KeyHints(plugin.RenderCtx) []plugin.KeyHint {
	if !t.state.Timer.Running() {
		return []plugin.KeyHint{{Key: "l", Desc: "开始专注"}}
	}
	stop := "暂停"
	if t.state.Timer.Paused() {
		stop = "继续"
	}
	return []plugin.KeyHint{
		{Key: "space", Desc: stop},
		{Key: "l", Desc: "计时操作"},
	}
}

// checkDone 检查方案是否走完；走完只响一次铃。
//
// announced 标记由引擎侧的共享状态持有，因此"响铃"与"界面显示已完成"
// 用的是同一个事实来源，不会出现"铃响了但界面还说在跑"。
func (t *timerTile) checkDone(now time.Time) plugin.Action {
	if !t.state.Timer.Done(now) || t.state.Timer.announced {
		return plugin.None()
	}
	t.state.Timer.announced = true
	return plugin.EffectOf(svc.Effect{Kind: svc.EffectBell, Text: "专注时段结束"})
}

// ---------- 看板选项：开始一次自由专注 ----------

// timerBoardOption 是"开始专注"看板选项。
type timerBoardOption struct {
	src   Source
	state *HostState
}

func (o *timerBoardOption) Label() string {
	if o.state.Timer.Running() {
		return "专注计时中…"
	}
	return "开始专注 / Focus"
}
func (o *timerBoardOption) Order() int { return 40 }

// Activate 打开计时控制界面。
//
// 已经在计时时它退化成"看一眼 + 暂停/结束"，而不是重新开一个——
// 那会把正在跑的计时顶掉。
func (o *timerBoardOption) Activate(svc.Services) (plugin.View, error) {
	return newTimerView(o.src, o.state, nil), nil
}

// newTimerView 是计时控制界面（借调舞台）。
//
// todo 为 nil 表示"自由专注"（不挂条目）；否则这是一次针对某条待办的专注
// （由联动选项进入，见 timerContextOption）。
func newTimerView(src Source, st *HostState, todo *model.Todo) plugin.View {
	// 预设：从配置里取默认时长；自定义分钟数由用户输入。
	minutes := defaultFocusMinutes(src)
	cursor := 0
	var status string

	presets := func() []model.Plan {
		return []model.Plan{
			focusPlan("专注 "+itoa(minutes)+" 分", minutes),
			focusPlan("专注 "+itoa(minutes*2)+" 分", minutes*2),
			focusPlan("番茄钟（专注 + 休息）", minutes),
			focusPlan("自定义…", 0),
		}
	}

	return &plugin.ViewFunc{
		ViewName: "专注",
		RenderFn: func(ctx plugin.RenderCtx) {
			y := ctx.Rect.Y
			put := func(s string, style canvas.StyleID) {
				y = drawWrapped(ctx, y, ctx.Rect, s, style)
			}
			put("专注计时", tile.StyleTitle)
			put("", tile.StyleMuted)
			if todo != nil {
				put("目标："+todo.Title, tile.StyleStatus)
			} else {
				put("自由专注（不挂具体条目）", tile.StyleStatus)
			}
			put("", tile.StyleMuted)

			if st.Timer.Running() {
				// 已在计时：给出暂停/继续与结束，而不是重开。
				now := ctx.Svc.Clock()
				put("正在计时 · 剩余 "+clock.HumanDuration(st.Timer.Remaining(now)), tile.StyleAccent)
				put("", tile.StyleMuted)
				put("  p  暂停 / 继续", tile.StyleMuted)
				put("  s  结束并记录", tile.StyleMuted)
				put("  esc 返回（计时继续）", tile.StyleHint)
				if status != "" {
					put("", tile.StyleMuted)
					put(status, tile.StyleWarn)
				}
				return
			}

			put("选择时长：", tile.StyleMuted)
			for i, p := range presets() {
				mark, style := "  ", tile.StyleMuted
				if i == cursor {
					mark, style = "▸ ", tile.StyleTitleFocused
				}
				put(mark+p.Label, style)
			}
			put("", tile.StyleMuted)
			put("↑↓ 选择 · enter 开始 · esc 取消", tile.StyleHint)
			if status != "" {
				put(status, tile.StyleWarn)
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			// 计时进行中：只认暂停/结束。
			if st.Timer.Running() {
				switch ev.Key {
				case "esc", "q":
					return plugin.None(), true
				case "p":
					if st.Timer.Paused() {
						st.Timer.Resume(ec.Now)
						status = "已继续"
					} else {
						st.Timer.Pause(ec.Now)
						status = "已暂停"
					}
					return plugin.None(), false
				case "s":
					return stopAndRecord(src, st, ec.Now), true
				}
				return plugin.None(), false
			}

			switch ev.Key {
			case "esc", "q":
				return plugin.None(), true
			case "j", "down":
				cursor = (cursor + 1) % len(presets())
				return plugin.None(), false
			case "k", "up":
				cursor = (cursor - 1 + len(presets())) % len(presets())
				return plugin.None(), false
			case "enter", " ":
				chosen := presets()[cursor]
				if chosen.Label == "自定义…" {
					status = "自定义时长还没做，先选一个预设"
					return plugin.None(), false
				}
				if !st.Timer.Start(chosen, todo, ec.Now) {
					status = "已经在计时了"
					return plugin.None(), false
				}
				st.Timer.announced = false
				return plugin.Toast("开始专注：" + chosen.Label), false
			}
			return plugin.None(), false
		},
	}
}

// stopAndRecord 结束计时并把会话写进当天记录。
//
// 会话进的是 DayData.Archive.Sessions（与 v2.1.0 同一个位置），
// 并同步累计 Activity —— 两处都要写，否则"活动统计"会漏掉这一次。
func stopAndRecord(src Source, st *HostState, now time.Time) plugin.Action {
	data := src.Day()
	if data == nil {
		st.Timer.init()
		return plugin.Toast("没有当天数据，已丢弃这次计时")
	}
	session := st.Timer.Stop(now)
	if session.Elapsed <= 0 {
		return plugin.Toast("计时太短，没有记录")
	}
	data.Archive.Sessions = append(data.Archive.Sessions, session)
	recordActivity(data, session)
	return plugin.Persist(TimerPackID, "day", data)
}

// recordActivity 把一次会话累计进当天的活动表。
//
// 活动按**条目名**聚合（自由专注固定叫"自由专注"），与 v2.1.0 一致：
// 统计页要看的是"哪件事占了多少时间"，而条目名就是用户认得的那个名字。
func recordActivity(data *model.DayData, s model.Session) {
	name := s.TodoName
	if name == "" {
		name = "自由专注"
	}
	if data.Activity == nil {
		data.Activity = map[string]*model.Activity{}
	}
	a := data.Activity[name]
	if a == nil {
		a = &model.Activity{Name: name, First: s.Started}
		data.Activity[name] = a
	}
	a.Last = s.Started.Add(s.Elapsed)
	a.Total += s.Elapsed
	a.Sessions++
}

// ---------- 联动选项：对选中的待办开始专注 ----------

// timerContextOption 是"对这条待办开始专注"的联动选项。
//
// 它体现 design §5.2 的那条约定：**它不认识任何磁贴**——谁被选中由引擎
// 广播过来，它只声明"我要 todo 类选中、需要 item.due 能力"。
type timerContextOption struct {
	src   Source
	state *HostState
}

// TimerContextOptionID 是联动选项的插件 ID。
const TimerContextOptionID = "kqflow.timer.ctx"

func (o *timerContextOption) AppliesTo() []string { return []string{"todo", "goal"} }

// Requires 要求"可选中条目"这个能力：没有可挂的条目时它没有意义。
func (o *timerContextOption) Requires() []string { return []string{CapItemSelection} }

func (o *timerContextOption) Order() int { return 30 }

func (o *timerContextOption) Label(s plugin.Selection) string {
	if o.state.Timer.Running() {
		return "查看计时"
	}
	if s.Title == "" {
		return "开始专注"
	}
	return "专注「" + s.Title + "」"
}

// Activate 打开计时界面，并把选中条目带进去当作专注目标。
func (o *timerContextOption) Activate(sel plugin.Selection, services svc.Services) (plugin.View, error) {
	return newTimerView(o.src, o.state, findTodoByID(o.src, sel.ID)), nil
}

// findTodoByID 在当天的两类列表里按 ID 找待办。
func findTodoByID(src Source, id string) *model.Todo {
	if id == "" {
		return nil
	}
	for _, kind := range []model.Kind{model.KindFixed, model.KindFloating} {
		for _, t := range TodoList(src, kind) {
			if t.ID == id {
				return t
			}
		}
	}
	return nil
}

// ---------- 小工具 ----------

// defaultFocusMinutes 返回默认专注时长（分钟）。
func defaultFocusMinutes(src Source) int {
	if cfg := src.Config(); cfg != nil {
		if n := cfg.FocusMinutes(); n > 0 {
			return n
		}
	}
	return 25
}

// focusPlan 造一个"专注 N 分钟"的方案。
func focusPlan(label string, minutes int) model.Plan {
	return model.Plan{
		Kind:     model.TimerCountDown,
		Label:    label,
		Segments: []model.Segment{{Name: "专注", Kind: "focus", Dur: minutesToDuration(minutes)}},
	}
}

// minutesToDuration 把分钟数换成时长（负数按 0 处理）。
func minutesToDuration(m int) time.Duration {
	if m < 0 {
		m = 0
	}
	return time.Duration(m) * time.Minute
}

// itoa 是小整数转字符串（避免为一处格式化引入 strconv）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		return "-" + string(buf)
	}
	return string(buf)
}
