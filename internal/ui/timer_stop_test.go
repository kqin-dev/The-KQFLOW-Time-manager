package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// mutableClock 让测试可以推进“当前时间”。
func mutableClock(t *testing.T, app *App, now *time.Time) {
	t.Helper()
	app.clock = clock.NewWith(func() time.Time { return *now }, time.Local)
}

// startCustomTimer 按真实的用户路径起一次自定义时段计时。
//
// 路径：打开自定义编辑器（默认“深度工作 25m + 休息 5m”）→ enter 开始 →
// 归属选“不归属任何 TODO” → 计时进行中。
func startCustomTimer(t *testing.T, app *App) {
	t.Helper()
	allowCarrySkip(t, app)
	app.openCustom()
	if app.custom == nil {
		t.Fatal("应打开自定义时段编辑器")
	}
	press(t, app, "enter")
	if app.custom != nil {
		t.Fatal("确认后应关闭自定义编辑器")
	}
	if app.pick == nil {
		t.Fatal("应弹出归属 TODO 的选择框")
	}
	app.pick.cursor = len(app.pick.items) - 1
	press(t, app, "enter")
	if app.timer == nil {
		t.Fatal("应已开始自定义计时")
	}
	if app.timer.plan.Kind != model.TimerCustom {
		t.Fatalf("计时类型应为 custom，实际 %q", app.timer.plan.Kind)
	}
}

// TestTimerStopPathsAllAskFirst 逐个走查“会结束计时”的入口，确认没有任何
// 一条路径会不打招呼就结束计时（见已知 bug 1）。
//
// 这张表就是这次的验收清单。早期 enter / esc 都会直接 stopTimer，
// 用户在计时中按回车想进入子任务，就把整段专注清掉了。
func TestTimerStopPathsAllAskFirst(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	cases := []struct {
		name string
		send func(app *App)
	}{
		{"计时菜单里的结束", func(app *App) {
			app.openTimerMenu()
			if app.pick == nil {
				t.Fatal("应弹出计时菜单")
			}
			selectAction(t, app, "timer_stop")
		}},
	}

	for _, c := range cases {
		app, s, _ := newTestApp(t, at)
		startCountUpTimer(t, app)
		if app.timer == nil {
			t.Fatalf("%s：计时未启动", c.name)
		}

		c.send(app)

		// 结束入口必须先问一句，而不是直接归档。
		if app.timer == nil {
			t.Errorf("%s：不应在未确认时就结束计时", c.name)
			continue
		}
		if !app.stopAsk {
			t.Errorf("%s：应进入“确定要结束吗”的确认状态", c.name)
			continue
		}
		// 确认框必须真的画出来，否则用户看不到这个提问。
		if out := stripStyles(app.View()); !strings.Contains(out, "确定要结束这次计时吗") {
			t.Errorf("%s：确认提示应显示在看板上，实际输出:\n%s", c.name, out)
		}
		// 计时照常推进：确认期间不该被冻结。
		if !app.timer.paused {
			now := at.Add(30 * time.Second)
			mutableClock(t, app, &now)
			if got := app.timer.elapsed(now); got != 30*time.Second {
				t.Errorf("%s：确认期间计时应继续走，实际 %v", c.name, got)
			}
		}
		// 还没有任何记录落盘。
		if saved, err := s.Day("2026-10-03"); err == nil && saved != nil && len(saved.Archive.Sessions) > 0 {
			t.Errorf("%s：未确认就写入了计时记录", c.name)
		}

		// 取消后计时必须还在，且能继续用。
		press(t, app, "esc")
		if app.stopAsk {
			t.Errorf("%s：取消后应退出确认状态", c.name)
		}
		if app.timer == nil {
			t.Errorf("%s：取消后计时不应消失", c.name)
		}

		// 再走一遍并确认，这次必须真的结束并归档——否则保护就成了
		// “计时永远结束不掉”。
		selectTimerStop(t, app)
		if !app.stopAsk {
			t.Fatalf("%s：第二次请求也应先问", c.name)
		}
		press(t, app, "enter")
		if app.timer != nil {
			t.Errorf("%s：确认后计时应结束", c.name)
		}
		saved, err := s.Day("2026-10-03")
		if err != nil {
			t.Fatal(err)
		}
		if len(saved.Archive.Sessions) != 1 {
			t.Errorf("%s：确认后应归档 1 条记录，实际 %d", c.name, len(saved.Archive.Sessions))
		}
	}
}

// selectTimerStop 走一次“请求结束计时”的动作（菜单路径）。
func selectTimerStop(t *testing.T, app *App) {
	t.Helper()
	if app.pick != nil {
		selectAction(t, app, "cancel")
	}
	app.openTimerMenu()
	if app.pick == nil {
		t.Fatal("应弹出计时菜单")
	}
	selectAction(t, app, "timer_stop")
}

// TestTimerKeysAreNotHijacked 验证计时中不再借用空格与回车。
//
// 这两个键在看板上本来另有用处（空格勾选完成、回车进入子任务），而且
// 在终端里最容易误触。计时中它们必须维持看板语义，否则用户在计时期间
// 根本没法勾选任务（见已知 bug 1 引申出的回归）。
func TestTimerKeysAreNotHijacked(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	// 回车：进入子任务，而不是弹结束确认。
	t.Run("enter 进入子任务", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		todo := model.NewTodo("写论文", model.KindFloating, "2026-10-03", at)
		todo.Tasks = append(todo.Tasks, model.NewTask("查资料"))
		app.data.Floating = append(app.data.Floating, todo)
		app.focus = FocusFloating
		startCountUpTimer(t, app)
		app.focus = FocusFloating

		press(t, app, "enter")
		if app.stopAsk {
			t.Error("计时中按回车不应弹出结束确认")
		}
		if !app.taskActive {
			t.Error("计时中按回车应进入子任务")
		}
		if app.timer == nil {
			t.Error("计时中按回车不应结束计时")
		}
	})

	// 空格：勾选任务，而不是暂停。
	t.Run("space 勾选任务", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		todo := model.NewTodo("写论文", model.KindFloating, "2026-10-03", at)
		app.data.Floating = append(app.data.Floating, todo)
		app.focus = FocusFloating
		startCountUpTimer(t, app)
		app.focus = FocusFloating

		press(t, app, "space")
		if app.timer.paused {
			t.Error("计时中按空格不应暂停")
		}
		if !app.data.Floating[0].Done {
			t.Error("计时中按空格应勾选完成")
		}
		if app.timer == nil {
			t.Error("计时中按空格不应结束计时")
		}
	})

	// 其它看板按键照常生效。
	t.Run("其它按键照常可用", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		startCountUpTimer(t, app)
		for _, key := range []string{"j", "k", "tab", "?", "N"} {
			app.focus = FocusMenu
			press(t, app, key)
			if app.stopAsk {
				t.Errorf("按 %s 不应弹出结束确认", key)
			}
			if app.timer == nil {
				t.Errorf("按 %s 不应结束计时", key)
			}
			if app.editor.active {
				app.closeEditor()
			}
			app.view = ViewDashboard
		}
	})
}

// TestTimerKeysDoNotStopWhenTypingOtherKeys 验证确认状态里按其它键一律
// 当作“继续计时”，贴着键盘随手一按不会清掉专注时段。
func TestTimerKeysDoNotStopWhenTypingOtherKeys(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	for _, key := range []string{"q", "j", "k", "a", "n", "y", "space", "p"} {
		app, _, _ := newTestApp(t, at)
		startCountUpTimer(t, app)
		app.askStopTimer()
		if !app.stopAsk {
			t.Fatalf("%s：应进入确认状态", key)
		}
		press(t, app, key)
		if app.stopAsk {
			t.Errorf("%s：应退出确认状态", key)
		}
		if app.timer == nil {
			t.Errorf("%s：按 %s 不应结束计时", key, key)
		}
	}
}

// TestTimerPauseStillWorksThroughMenu 验证暂停/继续仍然可用（走 p 菜单）。
func TestTimerPauseStillWorksThroughMenu(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	startCountUpTimer(t, app)

	press(t, app, "p")
	selectAction(t, app, "timer_pause")
	if !app.timer.paused {
		t.Error("菜单里选暂停后应处于暂停状态")
	}
	press(t, app, "p")
	selectAction(t, app, "timer_resume")
	if app.timer.paused {
		t.Error("菜单里选继续后应恢复计时")
	}
	// 暂停也不该产生归档记录。
	if len(app.data.Archive.Sessions) != 0 {
		t.Errorf("暂停不应归档，实际 %d 条", len(app.data.Archive.Sessions))
	}
}

// TestTimerMenuOffersPauseAndStop 验证 p 菜单是计时中唯一的控制入口。
func TestTimerMenuOffersPauseAndStop(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	startCountUpTimer(t, app)

	press(t, app, "p")
	if app.pick == nil {
		t.Fatal("p 应打开计时菜单")
	}
	labels := quitPromptLabels(app)
	joined := strings.Join(labels, "|")
	if !strings.Contains(joined, "暂停") {
		t.Errorf("计时菜单应包含暂停，实际 %v", labels)
	}
	if !strings.Contains(joined, "结束") {
		t.Errorf("计时菜单应包含结束，实际 %v", labels)
	}
	// 默认项不能是“结束”，误触回车不该顺手结束计时。
	if strings.Contains(app.pick.items[app.pick.cursor].Label, "结束") {
		t.Errorf("默认选中项不应是结束，实际 %q", app.pick.items[app.pick.cursor].Label)
	}
	app.pick = nil
}

// TestTimerHintsMatchReality 验证计时中的提示行只列出真正可用的键。
//
// 计时中不再借用空格与回车，提示行如果还教用户按它们，就会误导。
func TestTimerHintsMatchReality(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	startCountUpTimer(t, app)

	hints := stripStyles(app.renderHints())
	if !strings.Contains(hints, "p:计时菜单") {
		t.Errorf("计时中的提示行应包含 p:计时菜单，实际 %q", hints)
	}
	// 空的与回车不再被计时借用，提示行要如实说明它们仍是看板语义。
	if !strings.Contains(hints, "space:勾选") {
		t.Errorf("计时中的提示行应说明 space 仍是勾选，实际 %q", hints)
	}
	if !strings.Contains(hints, "enter:子任务") {
		t.Errorf("计时中的提示行应说明 enter 仍是子任务，实际 %q", hints)
	}
	if strings.Contains(hints, "暂停") || strings.Contains(hints, "添加") {
		t.Errorf("计时中的提示行不应出现未绑定的动作，实际 %q", hints)
	}

	// 确认框里的提示仍要说明 enter 确认、其它键继续。
	app.askStopTimer()
	confirm := stripStyles(app.renderHints())
	if !strings.Contains(confirm, "enter:结束并归档") {
		t.Errorf("确认状态的提示行应说明 enter 结束，实际 %q", confirm)
	}
}

// TestTimerKeysReachOtherFeatures 验证计时中仍能打开随手记等其它功能。
//
// 已知 bug 1 的另一半：结束计时的按键与其它操作重合，导致计时中
// 一些功能根本用不了。
func TestTimerKeysReachOtherFeatures(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	startCountUpTimer(t, app)

	press(t, app, "N")
	if !app.editor.active {
		t.Error("计时中应能打开随手记")
	}
	if app.timer == nil {
		t.Error("打开随手记不应结束计时")
	}
	press(t, app, "esc")
	if app.timer == nil {
		t.Error("关掉随手记后计时应还在")
	}
	if app.stopAsk {
		t.Error("关掉随手记不应触发结束确认")
	}

	press(t, app, "?")
	if app.view != ViewHelp {
		t.Errorf("计时中应能打开帮助，实际 %v", app.view)
	}
	press(t, app, "?")
	if app.timer == nil {
		t.Error("查看帮助不应结束计时")
	}
}

// TestCanStartNewTimerAfterEnding 验证拒绝第二次计时不会把用户永久锁死：
// 结束当前计时之后，必须能正常开始新的一次。
func TestCanStartNewTimerAfterEnding(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)
	startCountUpTimer(t, app)

	now := at
	mutableClock(t, app, &now)
	now = at.Add(15 * time.Minute)

	// 计时中被拒绝。
	app.startTimer()
	if app.pick != nil {
		t.Fatal("计时中不应打开计时方式菜单")
	}
	if app.timer == nil {
		t.Fatal("计时不应消失")
	}

	// 结束计时后必须能开始新的。
	stopTimerViaMenu(t, app)
	if app.timer != nil {
		t.Fatal("结束后计时应已清空")
	}
	startCountUpTimer(t, app)
	if app.timer == nil {
		t.Fatal("结束后应能开始新的计时")
	}
	if app.timer == nil || app.timer.plan.Kind != model.TimerCountUp {
		t.Errorf("新计时应为正计时，实际 %v", app.timer.plan.Kind)
	}
	// 旧计时已归档，新计时尚未归档。
	saved, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Archive.Sessions) != 1 {
		t.Fatalf("应只有旧计时被归档，实际 %d 条", len(saved.Archive.Sessions))
	}
	if got := saved.Archive.Sessions[0].Elapsed; got != 15*time.Minute {
		t.Errorf("旧计时归档时长应为 15m，实际 %v", got)
	}
}

// TestSecondTimerIsRefusedWhileRunning 逐个走查“启动计时”的入口，确认计时
// 进行中没有任何一条路能悄悄顶掉正在进行的计时。
//
// 早期 startTimer() 只弹了一句 toast 就继续打开菜单，用户选完方式与归属后
// a.timer 被直接覆盖——旧计时既没归档也没提示，时长静默丢失。
func TestSecondTimerIsRefusedWhileRunning(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	cases := []struct {
		name string
		send func(app *App)
	}{
		{"中间栏计时菜单", func(app *App) { app.startTimer() }},
		{"菜单项 timer", func(app *App) { app.activateMenuItem(1) }},
		{"计时方式：番茄钟", func(app *App) { app.runAction("timer_pomodoro") }},
		{"计时方式：倒计时", func(app *App) { app.runAction("timer_countdown") }},
		{"计时方式：正计时", func(app *App) { app.runAction("timer_countup") }},
		{"计时方式：自定义", func(app *App) { app.runAction("timer_custom") }},
		{"菜单直接开始专注", func(app *App) { app.runAction("start_focus") }},
		{"beginTimer 直接调用", func(app *App) {
			app.beginTimer(model.Plan{Kind: model.TimerCountDown}, "")
		}},
	}

	for _, c := range cases {
		app, s, _ := newTestApp(t, at)
		startCountUpTimer(t, app)
		first := app.timer
		if first == nil {
			t.Fatalf("%s：第一个计时未启动", c.name)
		}

		now := at
		mutableClock(t, app, &now)
		now = at.Add(15 * time.Minute)

		c.send(app)

		// 正在进行的计时必须原封不动地留着。
		if app.timer == nil {
			t.Errorf("%s：正在进行的计时不应消失", c.name)
			continue
		}
		if app.timer != first {
			t.Errorf("%s：计时被新方案顶掉了（旧计时会静默丢时长）", c.name)
		}
		// 不能留下半开的启动流程。
		if app.pick != nil {
			t.Errorf("%s：不应打开计时方式/归属的选择框", c.name)
		}
		if app.pendingPlan != nil {
			t.Errorf("%s：不应留下待确认的计时方案", c.name)
		}
		if app.custom != nil {
			t.Errorf("%s：不应打开自定义时段编辑器", c.name)
		}
		// 必须告诉用户为什么没开始，以及怎么结束当前的计时。
		if app.toast == "" {
			t.Errorf("%s：应给出提示说明当前已有计时", c.name)
		}
		// 归档里不该凭空多出记录。
		if saved, err := s.Day("2026-10-03"); err == nil && saved != nil && len(saved.Archive.Sessions) > 0 {
			t.Errorf("%s：不应产生归档记录，实际 %d 条", c.name, len(saved.Archive.Sessions))
		}
	}
}

// stopTimerViaMenu 走完整的用户路径结束计时：p → 结束计时并归档 → 确认。
//
// 计时中不再有直接的结束键（空格与回车都还给看板），所以测试也必须
// 走这条真实路径，否则测的就不是用户能走通的流程。
func stopTimerViaMenu(t *testing.T, app *App) {
	t.Helper()
	press(t, app, "p")
	if app.pick == nil {
		t.Fatal("p 应打开计时菜单")
	}
	selectAction(t, app, "timer_stop")
	if !app.stopAsk {
		t.Fatal("选“结束”后应先问一句")
	}
	press(t, app, "enter")
	if app.timer != nil {
		t.Fatal("确认后计时应结束")
	}
}

// TestCustomTimerCountsFocusIntoDailyTotal 验证自定义时段的专注时长
// 会计入当日专注总计（见已知 bug 2）。
//
// 用户报告：自定义专注“计时结果存在，但没计入当日专注时长”。
// 根因是归档时只按结束时所处的那一段判断类型——自定义方案跨段，
// 结束时若正好在休息段，整段专注都被算成了休息。
func TestCustomTimerCountsFocusIntoDailyTotal(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)
	startCustomTimer(t, app)

	// 自定义默认方案：深度工作 25m（专注）+ 休息 5m。
	total := app.timer.plan.Total()
	if total != 30*time.Minute {
		t.Fatalf("默认自定义方案应为 30m，实际 %v", total)
	}

	// 走到中途（第 10 分钟）结束：全部落在专注段。
	now := at
	mutableClock(t, app, &now)
	now = at.Add(10 * time.Minute)
	stopTimerViaMenu(t, app)

	focus, rest := app.data.FocusTotal()
	if focus != 10*time.Minute {
		t.Errorf("自定义计时的专注应计入 10m，实际 %v", focus)
	}
	if rest != 0 {
		t.Errorf("休息应为 0，实际 %v", rest)
	}
	if !strings.Contains(stripStyles(app.View()), "今日专注") {
		t.Error("看板应展示今日专注时长")
	}

	// 关键回归：整段走完（结束时落在休息段），专注部分仍要算进去。
	app2, _, _ := newTestApp(t, at)
	startCustomTimer(t, app2)
	now2 := at
	mutableClock(t, app2, &now2)
	now2 = at.Add(total)
	stopTimerViaMenu(t, app2)

	focus2, rest2 := app2.data.FocusTotal()
	if focus2 != 25*time.Minute {
		t.Errorf("整段走完后专注应为 25m，实际 %v", focus2)
	}
	if rest2 != 5*time.Minute {
		t.Errorf("整段走完后休息应为 5m，实际 %v", rest2)
	}

	// 落盘的数据也要带分段统计。
	saved, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Archive.Sessions) != 1 {
		t.Fatalf("应有 1 条计时记录，实际 %d", len(saved.Archive.Sessions))
	}
	if saved.Archive.Sessions[0].Focus == nil {
		t.Error("新记录应带有分时段专注时长")
	} else if got := *saved.Archive.Sessions[0].Focus; got != 10*time.Minute {
		t.Errorf("记录里的专注时长应为 10m，实际 %v", got)
	}
}

// TestOldSessionsKeepOriginalTotals 验证老数据（没有 focus 字段）的统计
// 口径不变，升级不会改动用户已有的历史数字。
func TestOldSessionsKeepOriginalTotals(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	end := at.Add(25 * time.Minute)
	app.data.Archive.Sessions = []model.Session{
		// 老数据：只有 segment_kind。
		{Elapsed: 25 * time.Minute, SegmentKind: "focus", Ended: &end},
		{Elapsed: 5 * time.Minute, SegmentKind: "break", Ended: &end},
		// 老数据里 segment_kind 为空或自定义：维持原口径，算作专注。
		{Elapsed: 40 * time.Minute, SegmentKind: "other", Ended: &end},
	}
	focus, rest := app.data.FocusTotal()
	if focus != 65*time.Minute {
		t.Errorf("老数据专注应为 65m，实际 %v", focus)
	}
	if rest != 5*time.Minute {
		t.Errorf("老数据休息应为 5m，实际 %v", rest)
	}
}

// TestFocusUpToSplitsAcrossSegments 验证分段累计专注时长。
func TestFocusUpToSplitsAcrossSegments(t *testing.T) {
	plan := model.Plan{Kind: model.TimerCustom, Segments: []model.Segment{
		{Name: "深度工作", Kind: "focus", Dur: 25 * time.Minute},
		{Name: "休息", Kind: "break", Dur: 5 * time.Minute},
		{Name: "复盘", Kind: "focus", Dur: 10 * time.Minute},
	}}

	cases := []struct {
		elapsed time.Duration
		want    time.Duration
	}{
		{0, 0},
		{10 * time.Minute, 10 * time.Minute},
		{25 * time.Minute, 25 * time.Minute},
		{30 * time.Minute, 25 * time.Minute}, // 整段休息不算专注
		{35 * time.Minute, 30 * time.Minute}, // 休息 5m + 复盘 5m
		{40 * time.Minute, 35 * time.Minute}, // 方案刚好走完
		{60 * time.Minute, 55 * time.Minute}, // 超出末尾，余下归最后一段
	}
	for _, c := range cases {
		if got := plan.FocusUpTo(c.elapsed); got != c.want {
			t.Errorf("FocusUpTo(%v) = %v，期望 %v", c.elapsed, got, c.want)
		}
	}

	// 正计时（总长为 0）也要能算出专注时长。
	countUp := model.Plan{Kind: model.TimerCountUp, Segments: []model.Segment{
		{Name: "自由专注", Kind: "focus"},
	}}
	if got := countUp.FocusUpTo(45 * time.Minute); got != 45*time.Minute {
		t.Errorf("正计时 FocusUpTo 应为 45m，实际 %v", got)
	}

	// 自定义里的“其它”段既不算专注也不算休息。
	other := model.Plan{Kind: model.TimerCustom, Segments: []model.Segment{
		{Name: "整理", Kind: "other", Dur: 10 * time.Minute},
	}}
	if got := other.FocusUpTo(10 * time.Minute); got != 0 {
		t.Errorf("自定义“其它”段不应计入专注，实际 %v", got)
	}
}
