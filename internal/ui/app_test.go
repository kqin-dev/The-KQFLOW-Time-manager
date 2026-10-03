package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/clock"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/config"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/model"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/store"
)

// newTestApp 构造一个隔离的 App，固定时间与数据目录。
func newTestApp(t *testing.T, at time.Time) (*App, *store.Store, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("打开数据层失败: %v", err)
	}
	cfg := config.Default()
	cfg.DayCutoff = "00:00"
	paths := &config.Paths{Root: dir, ConfigFile: dir + "/config.json"}

	app, err := NewApp(Options{
		Store:  s,
		Config: cfg,
		Clock:  clock.NewWith(func() time.Time { return at }, time.Local),
		Paths:  paths,
	})
	if err != nil {
		t.Fatalf("创建 App 失败: %v", err)
	}
	app.width, app.height = 120, 36
	return app, s, cfg
}

// press 依次按下若干按键，并同步执行返回的命令。
func press(t *testing.T, app *App, keys ...string) {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "space":
			msg = tea.KeyMsg{Type: tea.KeySpace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		if _, cmd := app.Update(msg); cmd != nil {
			// 立刻执行命令，让保存等副作用在测试中同步发生。
			if m := cmd(); m != nil {
				app.Update(m)
			}
		}
	}
}

// TestDashboardRendersLogoAndPanels 验证看板包含 Logo 与三栏结构（见需求 2、5）。
func TestDashboardRendersLogoAndPanels(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 30, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	out := app.View()

	for _, want := range []string{"TODAY", "GOAL", "Focus", "History", "Settings"} {
		if !strings.Contains(out, want) {
			t.Errorf("看板缺少 %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, "Good morning") {
		t.Errorf("看板应显示问候语，实际输出:\n%s", out)
	}
	if !strings.Contains(out, "2026-10-03") {
		t.Errorf("看板应显示当前逻辑日")
	}
	if !strings.Contains(out, "█") {
		t.Errorf("看板缺少 ASCII 艺术字 Logo")
	}
}

// TestAddAndToggleTodo 验证添加与勾选（见需求 9、12）。
func TestAddAndToggleTodo(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)

	// 切到临时栏并添加两项。
	press(t, app, "tab", "tab")
	if app.focus != FocusFloating {
		t.Fatalf("焦点应为临时 TODO 栏，实际 %v", app.focus)
	}
	press(t, app, "a")
	for _, r := range "写周报" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	if len(app.data.Floating) != 1 {
		t.Fatalf("应有 1 项临时 TODO，实际 %d", len(app.data.Floating))
	}

	press(t, app, "a")
	for _, r := range "整理桌面" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	if len(app.data.Floating) != 2 {
		t.Fatalf("应有 2 项临时 TODO，实际 %d", len(app.data.Floating))
	}

	// 勾选第一项。
	app.cursors.floating = 0
	press(t, app, "space")
	if !app.data.Floating[0].Done {
		t.Error("第一项应被勾选为完成")
	}
	if app.data.Floating[0].DoneAt == nil {
		t.Error("完成时应记录完成时间")
	}
	out := app.View()
	if !strings.Contains(out, "1/2") {
		t.Errorf("看板应显示 (n/m) 形式的完成情况，实际输出:\n%s", out)
	}

	// 数据应已落盘。
	saved, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Floating) != 2 {
		t.Errorf("落盘数据应包含 2 项，实际 %d", len(saved.Floating))
	}

	// 全部勾选后触发庆祝（见需求 12）。
	app.cursors.floating = 1
	press(t, app, "space")
	if app.celebrate == nil {
		t.Error("全部完成时应触发庆祝特效")
	}
}

// TestGoalToggleArchives 验证 GOAL 勾选后归档到当日（见需求 10）。
func TestGoalToggleArchives(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)

	press(t, app, "A")
	for _, r := range "读完三本书" {
		press(t, app, string(r))
	}
	press(t, app, "enter")

	if len(app.goals) != 1 {
		t.Fatalf("应有 1 个 GOAL，实际 %d", len(app.goals))
	}
	goal := app.goals[0]
	if goal.Tag == "" {
		t.Error("GOAL 应带标签，供继承时防混淆")
	}

	// 焦点此时在 GOAL 栏，直接勾选。
	press(t, app, "space")
	if !app.goals[0].Done {
		t.Fatal("GOAL 应被勾选为完成")
	}
	if app.goals[0].ArchivedDay != "2026-10-03" {
		t.Errorf("归档日应为 2026-10-03，实际 %q", app.goals[0].ArchivedDay)
	}
	if len(app.data.Archive.Goals) != 1 {
		t.Fatalf("当日归档应含 1 个 GOAL，实际 %d", len(app.data.Archive.Goals))
	}
	if !strings.Contains(app.View(), "今日已归档") {
		t.Error("看板右栏应展示今日已归档的 GOAL")
	}

	goals, err := s.Goals()
	if err != nil {
		t.Fatal(err)
	}
	if len(goals) != 1 || !goals[0].Done {
		t.Errorf("GOAL 落盘不正确: %+v", goals)
	}
}

// TestCarryFromYesterday 验证继承昨日数据（见需求 14）。
func TestCarryFromYesterday(t *testing.T) {
	at := time.Date(2026, 10, 4, 9, 0, 0, 0, time.Local)
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()

	// 造一份昨日数据：一个固定 TODO、一个未完成临时 TODO、一个已完成临时 TODO。
	prev := model.NewDayData("2026-10-03", at.Add(-24*time.Hour))
	prev.Fixed = append(prev.Fixed, model.NewTodo("每日阅读", model.KindFixed, "2026-10-03", at))
	prev.Floating = append(prev.Floating, model.NewTodo("未完成的事", model.KindFloating, "2026-10-03", at))
	doneItem := model.NewTodo("已完成的事", model.KindFloating, "2026-10-03", at)
	doneItem.Toggle(at)
	prev.Floating = append(prev.Floating, doneItem)
	if err := s.SaveDay(prev); err != nil {
		t.Fatal(err)
	}

	// 现在才启动 App，让它像真实启动那样载入今日并询问是否继承。
	app, err := NewApp(Options{
		Store:  s,
		Config: cfg,
		Clock:  clock.NewWith(func() time.Time { return at }, time.Local),
		Paths:  &config.Paths{Root: dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	app.width, app.height = 120, 36

	if app.day != "2026-10-04" {
		t.Fatalf("逻辑日应为 2026-10-04，实际 %s", app.day)
	}
	if app.prevDay != "2026-10-03" {
		t.Fatalf("前一日应为 2026-10-03，实际 %s", app.prevDay)
	}
	// 存在未完成的昨日事项时应弹出继承确认页。
	if app.view != ViewCarry {
		t.Fatalf("应显示继承确认页，实际 %v", app.view)
	}

	press(t, app, "y")
	today, err := s.Day("2026-10-04")
	if err != nil {
		t.Fatal(err)
	}
	if len(today.Fixed) != 1 || today.Fixed[0].Title != "每日阅读" {
		t.Errorf("固定 TODO 应被继承: %+v", today.Fixed)
	}
	if len(today.Floating) != 1 || today.Floating[0].Title != "未完成的事" {
		t.Errorf("只应继承未完成的临时 TODO: %+v", today.Floating)
	}
	if !today.CarryAsked {
		t.Error("继承后应标记 CarryAsked，避免重复询问")
	}
}

// TestNoCarryPromptOnFirstRun 验证空数据首次启动不会弹出继承页。
func TestNoCarryPromptOnFirstRun(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	if app.view != ViewDashboard {
		t.Errorf("没有历史数据时不应弹出继承页，实际 view=%v", app.view)
	}
}

// TestDayRollover 验证跨过日界线后自动切换到新的一天（见需求 7）。
func TestDayRollover(t *testing.T) {
	now := time.Date(2026, 10, 3, 23, 59, 0, 0, time.Local)
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	// 日界线设为 04:00。
	cfg.DayCutoff = "04:00"
	app, err := NewApp(Options{
		Store:  s,
		Config: cfg,
		Clock:  clock.NewWith(func() time.Time { return now }, time.Local),
		Paths:  &config.Paths{Root: dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	app.width, app.height = 120, 36

	if app.day != "2026-10-03" {
		t.Fatalf("23:59 应属 2026-10-03，实际 %s", app.day)
	}

	// 时间推进到次日 03:00，仍未跨过 04:00 日界线。
	now = time.Date(2026, 10, 4, 3, 0, 0, 0, time.Local)
	app.Update(tickMsg{})
	if app.day != "2026-10-03" {
		t.Errorf("04:00 日界线内，03:00 仍应属 2026-10-03，实际 %s", app.day)
	}

	// 推进到 04:30，进入新的一天。
	now = time.Date(2026, 10, 4, 4, 30, 0, 0, time.Local)
	app.Update(tickMsg{})
	if app.day != "2026-10-04" {
		t.Errorf("跨过日界线后应为 2026-10-04，实际 %s", app.day)
	}
}

// TestTimerRecordsToTodo 验证计时归档到所属 TODO（见需求 17、21）。
func TestTimerRecordsToTodo(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	app, err := NewApp(Options{
		Store:  s,
		Config: cfg,
		Clock:  clock.NewWith(func() time.Time { return now }, time.Local),
		Paths:  &config.Paths{Root: dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	app.width, app.height = 120, 36

	todo := model.NewTodo("写论文", model.KindFloating, "2026-10-03", now)
	app.data.Floating = append(app.data.Floating, todo)

	plan := model.Plan{Kind: model.TimerCountDown, Segments: []model.Segment{
		{Name: "专注", Kind: "focus", Dur: 25 * time.Minute},
	}}
	app.beginTimer(plan, todo.ID)
	if app.timer == nil {
		t.Fatal("计时器应已启动")
	}

	// 推进 10 分钟后中断。
	now = now.Add(10 * time.Minute)
	app.stopTimer(true)

	if app.timer != nil {
		t.Error("中断后计时器应被清空")
	}
	if len(app.data.Archive.Sessions) != 1 {
		t.Fatalf("应记录 1 次计时，实际 %d", len(app.data.Archive.Sessions))
	}
	sess := app.data.Archive.Sessions[0]
	if sess.TodoRef != todo.ID {
		t.Errorf("计时记录应归属 TODO %s，实际 %q", todo.ID, sess.TodoRef)
	}
	if sess.Elapsed != 10*time.Minute {
		t.Errorf("已用时长应为 10m，实际 %v", sess.Elapsed)
	}
	// 中断的计时也要落盘，防止意外关闭丢数据。
	saved, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Archive.Sessions) != 1 {
		t.Errorf("中断的计时应已落盘，实际 %d 条", len(saved.Archive.Sessions))
	}
	// 专注时长应出现在看板问候语旁（见需求 18）。
	if !strings.Contains(app.View(), "今日专注") {
		t.Error("看板应展示今日专注时长")
	}
}

// TestFocusTotalCountsOnlyEndedSessions 验证未结束的计时不计入当日总计。
func TestFocusTotalCountsOnlyEndedSessions(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	end := at.Add(25 * time.Minute)
	app.data.Archive.Sessions = []model.Session{
		{Elapsed: 25 * time.Minute, SegmentKind: "focus", Ended: &end},
		{Elapsed: 5 * time.Minute, SegmentKind: "break", Ended: &end},
		{Elapsed: 90 * time.Minute, SegmentKind: "focus"}, // 进行中，不应计入
	}
	focus, rest := app.data.FocusTotal()
	if focus != 25*time.Minute {
		t.Errorf("专注应为 25m，实际 %v", focus)
	}
	if rest != 5*time.Minute {
		t.Errorf("休息应为 5m，实际 %v", rest)
	}
}

// TestPlanBarRendersSegments 验证进度条按状态段渲染（见需求 20）。
func TestPlanBarRendersSegments(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)

	plan := model.Plan{Kind: model.TimerPomodoro, Segments: []model.Segment{
		{Name: "专注", Kind: "focus", Dur: 25 * time.Minute},
		{Name: "休息", Kind: "break", Dur: 5 * time.Minute},
	}}
	bar := app.renderPlanBar(plan, 0, 40)
	if bar == "" {
		t.Fatal("进度条不应为空")
	}
	if n := strings.Count(bar, "─"); n == 0 {
		t.Errorf("尚未开始的时段应渲染为空槽，实际: %q", bar)
	}
	if n := strings.Count(bar, "━"); n != 0 {
		t.Errorf("尚未开始的时段不应有实心格，实际: %q", bar)
	}

	// 走完前 20 分钟，前段应出现实心格。
	bar2 := app.renderPlanBar(plan, 20*time.Minute, 40)
	if strings.Count(bar2, "━") == 0 {
		t.Errorf("已走过的时段应渲染为实心，实际: %q", bar2)
	}
	if bar2 == bar {
		t.Error("推进时间后进度条应有变化")
	}
}

// TestDayBarShowsReadableRange 验证当日进度条显示可读的起止时间。
func TestDayBarShowsReadableRange(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	bar := app.renderDayBar(120)
	// 午夜换日时不应显示成 “00:00 → 00:00”。
	if strings.Contains(bar, "00:00 → 00:00") {
		t.Errorf("当日进度条应显示可读的起止时间，实际: %q", bar)
	}
	if !strings.Contains(bar, "24:00") {
		t.Errorf("当日结束应显示为 24:00，实际: %q", bar)
	}
}

// TestPlanSegmentAt 验证时段定位。
func TestPlanSegmentAt(t *testing.T) {
	plan := model.Plan{Segments: []model.Segment{
		{Name: "专注", Dur: 25 * time.Minute},
		{Name: "休息", Dur: 5 * time.Minute},
	}}
	idx, seg, within := plan.SegmentAt(10 * time.Minute)
	if idx != 0 || seg.Name != "专注" || within != 10*time.Minute {
		t.Errorf("10 分钟应落在第 1 段内 10 分钟处，实际 idx=%d seg=%s within=%v", idx, seg.Name, within)
	}
	idx, seg, within = plan.SegmentAt(26 * time.Minute)
	if idx != 1 || seg.Name != "休息" || within != time.Minute {
		t.Errorf("26 分钟应落在休息段 1 分钟处，实际 idx=%d seg=%s within=%v", idx, seg.Name, within)
	}
	idx, seg, _ = plan.SegmentAt(90 * time.Minute)
	if idx != 1 || seg.Name != "休息" {
		t.Errorf("超出总时长应停在最后一段，实际 idx=%d seg=%s", idx, seg.Name)
	}
}

// TestSmallTerminal 验证极小窗口不会崩溃。
func TestSmallTerminal(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	for _, size := range [][2]int{{1, 1}, {20, 5}, {60, 16}, {120, 36}, {200, 60}} {
		app.width, app.height = size[0], size[1]
		out := app.View()
		if out == "" {
			t.Errorf("尺寸 %v 时渲染为空", size)
		}
	}
}

// TestEditorCancel 验证输入框可以取消。
func TestEditorCancel(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	press(t, app, "a")
	if !app.editor.active {
		t.Fatal("按 a 后输入框应激活")
	}
	press(t, app, "esc")
	if app.editor.active {
		t.Error("按 esc 后输入框应关闭")
	}
	if len(app.data.All()) != 0 {
		t.Error("取消后不应添加任何条目")
	}
}

// TestHistoryView 验证历史页可以渲染（见需求 11）。
func TestHistoryView(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)

	prev := model.NewDayData("2026-10-02", at.Add(-24*time.Hour))
	prev.Floating = append(prev.Floating, model.NewTodo("昨天的事", model.KindFloating, "2026-10-02", at))
	if err := s.SaveDay(prev); err != nil {
		t.Fatal(err)
	}

	app.view = ViewHistory
	out := app.View()
	if !strings.Contains(out, "历史") {
		t.Errorf("历史页应显示标题，实际:\n%s", out)
	}
	if !strings.Contains(out, "2026-10-02") {
		t.Errorf("历史页应列出已有数据的日期，实际:\n%s", out)
	}
}

// TestCarryKeyCommitsFixed 验证按 f 只继承固定 TODO。
func TestCarryKeyCommitsFixed(t *testing.T) {
	at := time.Date(2026, 10, 4, 9, 0, 0, 0, time.Local)
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	prev := model.NewDayData("2026-10-03", at.Add(-24*time.Hour))
	prev.Fixed = append(prev.Fixed, model.NewTodo("冥想 10 分钟", model.KindFixed, "2026-10-03", at))
	prev.Floating = append(prev.Floating, model.NewTodo("临时事项", model.KindFloating, "2026-10-03", at))
	if err := s.SaveDay(prev); err != nil {
		t.Fatal(err)
	}

	app, err := NewApp(Options{
		Store:  s,
		Config: config.Default(),
		Clock:  clock.NewWith(func() time.Time { return at }, time.Local),
		Paths:  &config.Paths{Root: dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	app.width, app.height = 120, 36
	press(t, app, "f")

	today, err := s.Day("2026-10-04")
	if err != nil {
		t.Fatal(err)
	}
	if len(today.Fixed) != 1 || today.Fixed[0].Title != "冥想 10 分钟" {
		t.Errorf("固定 TODO 应被继承: %+v", today.Fixed)
	}
	if len(today.Floating) != 0 {
		t.Errorf("只继承固定项时不应带入临时 TODO: %+v", today.Floating)
	}
}

// TestTimerCompletionNotifiesOnce 验证时段走完只提示一次。
//
// 进度靠每 120 毫秒的动画帧推进，若不做去重，用户会被同一句提示反复刷屏。
func TestTimerCompletionNotifiesOnce(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Options{
		Store:  s,
		Config: config.Default(),
		Clock:  clock.NewWith(func() time.Time { return now }, time.Local),
		Paths:  &config.Paths{Root: dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	app.width, app.height = 120, 36

	plan := model.Plan{Kind: model.TimerCountDown, Segments: []model.Segment{
		{Name: "专注", Kind: "focus", Dur: time.Minute},
	}}
	app.beginTimer(plan, "")

	// 时间推到时段结束之后。
	now = now.Add(2 * time.Minute)

	// 第一帧应返回完成命令。
	cmd := app.timer.tick(now)
	if cmd == nil {
		t.Fatal("时段结束后应返回完成命令")
	}
	app.Update(cmd())
	if !app.timer.finished {
		t.Error("时段结束后应标记为已完成")
	}

	// 后续帧不应再返回命令，否则提示会不断重复。
	for i := 0; i < 10; i++ {
		if again := app.timer.tick(now); again != nil {
			t.Fatalf("第 %d 帧不应重复发送完成通知", i+2)
		}
	}

	// 响铃标记也应只消费一次。
	if !app.timer.consumeBell() {
		t.Error("完成时应设置响铃标记")
	}
	if app.timer.consumeBell() {
		t.Error("响铃标记只应被消费一次")
	}
}

// TestTimerPauseAndResume 验证暂停不会累计时间。
func TestTimerPauseAndResume(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Options{
		Store:  s,
		Config: config.Default(),
		Clock:  clock.NewWith(func() time.Time { return now }, time.Local),
		Paths:  &config.Paths{Root: dir},
	})
	if err != nil {
		t.Fatal(err)
	}

	plan := model.Plan{Kind: model.TimerCountUp}
	app.beginTimer(plan, "")
	timer := app.timer

	// 走 5 分钟。
	now = now.Add(5 * time.Minute)
	if got := timer.elapsed(now); got != 5*time.Minute {
		t.Fatalf("已用应为 5m，实际 %v", got)
	}

	// 暂停 3 分钟，这段时间不应计入。
	timer.pause(now)
	now = now.Add(3 * time.Minute)
	if got := timer.elapsed(now); got != 5*time.Minute {
		t.Errorf("暂停期间不应计时，实际 %v", got)
	}

	// 继续后再走 2 分钟，总计 7 分钟。
	timer.pause(now)
	now = now.Add(2 * time.Minute)
	if got := timer.elapsed(now); got != 7*time.Minute {
		t.Errorf("恢复后总计应为 7m，实际 %v", got)
	}
}

// TestQuoteRotates 验证中间栏字条会随时间轮换（见需求 5）。
func TestQuoteRotates(t *testing.T) {
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Options{
		Store:  s,
		Config: config.Default(),
		Clock:  clock.NewWith(func() time.Time { return now }, time.Local),
		Paths:  &config.Paths{Root: dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	app.width, app.height = 120, 36

	first := app.quoteIdx
	// 未到轮换间隔时不应变化。
	now = now.Add(quoteEvery - time.Second)
	app.Update(animMsg{})
	if app.quoteIdx != first {
		t.Error("未到轮换间隔时字条不应变化")
	}

	// 跨过间隔后应换一条。
	now = now.Add(2 * time.Second)
	app.Update(animMsg{})
	if app.quoteIdx == first {
		t.Error("跨过轮换间隔后字条应换新")
	}
	if app.quoteIdx < 0 || app.quoteIdx >= len(Quotes) {
		t.Errorf("字条下标越界: %d", app.quoteIdx)
	}
}

// TestNextQuoteAlwaysChanges 验证连续轮换不会原地打转。
func TestNextQuoteAlwaysChanges(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	for i := 0; i < 50; i++ {
		before := app.quoteIdx
		app.NextQuote()
		if app.quoteIdx == before {
			t.Fatalf("第 %d 次轮换后下标未变化: %d", i, before)
		}
	}
}

// TestCustomEditorBuildsPlan 验证自定义时段编辑器能编排多段状态（见需求 16、20）。
func TestCustomEditorBuildsPlan(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)

	allowCarrySkip(t, app)
	app.openCustom()
	if app.custom == nil {
		t.Fatal("应打开自定义时段编辑器")
	}
	if len(app.custom.plan.Segments) != 2 {
		t.Fatalf("默认应有 2 段，实际 %d", len(app.custom.plan.Segments))
	}

	// 新增一段并改名。
	press(t, app, "n")
	if len(app.custom.plan.Segments) != 3 {
		t.Fatalf("新增后应有 3 段，实际 %d", len(app.custom.plan.Segments))
	}
	press(t, app, "e")
	// 输入框会带上原名，先清空再输入。
	app.editor.value = nil
	app.editor.cursor = 0
	for _, r := range "复盘" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	idx := app.custom.cursor
	if app.custom.plan.Segments[idx].Name != "复盘" {
		t.Errorf("改名失败，实际 %q", app.custom.plan.Segments[idx].Name)
	}

	// 改类型。
	before := app.custom.plan.Segments[idx].Kind
	press(t, app, "t")
	if app.custom.plan.Segments[idx].Kind == before {
		t.Error("按 t 后类型应轮换")
	}

	// 改时长。
	press(t, app, "p")
	app.editor.value = nil
	app.editor.cursor = 0
	for _, r := range "45" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	if got := app.custom.plan.Segments[idx].Dur; got != 45*time.Minute {
		t.Errorf("时长应为 45m，实际 %v", got)
	}

	// 删除一段。
	press(t, app, "d")
	if len(app.custom.plan.Segments) != 2 {
		t.Errorf("删除后应有 2 段，实际 %d", len(app.custom.plan.Segments))
	}

	// 开始计时会先要求选择归属 TODO。
	press(t, app, "enter")
	if app.custom != nil {
		t.Error("确认后应关闭编辑器")
	}
	if app.pick == nil {
		t.Fatal("应弹出归属 TODO 的选择框")
	}
	// 选择“不归属任何 TODO”（最后一项）。
	app.pick.cursor = len(app.pick.items) - 1
	press(t, app, "enter")
	if app.timer == nil {
		t.Fatal("应已开始自定义计时")
	}
	if app.timer.plan.Kind != model.TimerCustom {
		t.Errorf("计时类型应为 custom，实际 %q", app.timer.plan.Kind)
	}
	if len(app.timer.plan.Segments) != 2 {
		t.Errorf("方案应保留 2 段，实际 %d", len(app.timer.plan.Segments))
	}
}

// TestCustomEditorRejectsSingleSegment 验证只有一段时不允许开始。
func TestCustomEditorRejectsSingleSegment(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	allowCarrySkip(t, app)
	app.openCustom()

	press(t, app, "d")
	if len(app.custom.plan.Segments) != 1 {
		t.Fatalf("应剩 1 段，实际 %d", len(app.custom.plan.Segments))
	}
	// 再删应被拒。
	press(t, app, "d")
	if len(app.custom.plan.Segments) != 1 {
		t.Error("不应允许删到零段")
	}
	press(t, app, "enter")
	if app.custom == nil {
		t.Error("只有一段时不应开始计时")
	}
	if app.timer != nil {
		t.Error("只有一段时不应创建计时器")
	}
}

// TestCustomSegmentsColorByKind 验证自定义状态的进度条按类型着色（见需求 20）。
func TestCustomSegmentsColorByKind(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)

	plan := model.Plan{Kind: model.TimerCustom, Segments: []model.Segment{
		{Name: "深度工作", Kind: "focus", Dur: 50 * time.Minute},
		{Name: "散步", Kind: "break", Dur: 10 * time.Minute},
	}}
	bar := app.renderPlanBar(plan, 0, 60)
	if bar == "" {
		t.Fatal("自定义方案的进度条不应为空")
	}
	// 两段都应渲染出槽位；未开始时都是空槽。
	if n := strings.Count(bar, "─"); n == 0 {
		t.Errorf("应渲染出时段槽位，实际 %q", bar)
	}
	// 走到第二段时，应出现休息色与专注色的实心格。
	barMid := app.renderPlanBar(plan, 55*time.Minute, 60)
	if strings.Count(barMid, "━") == 0 {
		t.Errorf("走过的时段应渲染为实心，实际 %q", barMid)
	}
}

// TestParseMinutes 验证分钟输入解析。
func TestParseMinutes(t *testing.T) {
	ok := map[string]time.Duration{
		"25":  25 * time.Minute,
		" 5 ": 5 * time.Minute,
		"600": 600 * time.Minute,
	}
	for in, want := range ok {
		got, err := parseMinutes(in)
		if err != nil {
			t.Errorf("parseMinutes(%q) 不应报错: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseMinutes(%q) = %v，期望 %v", in, got, want)
		}
	}
	for _, in := range []string{"0", "-5", "601", "abc", ""} {
		if _, err := parseMinutes(in); err == nil {
			t.Errorf("parseMinutes(%q) 应报错", in)
		}
	}
}

// allowCarrySkip 关掉可能出现的继承确认页，便于测试其他界面。
func allowCarrySkip(t *testing.T, app *App) {
	t.Helper()
	if app.view == ViewCarry {
		press(t, app, "n")
	}
}

// TestQuitKeys 验证退出快捷键。
func TestQuitKeys(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	for _, key := range []string{"q", "Q"} {
		app, _, _ := newTestApp(t, at)
		_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if cmd == nil {
			t.Errorf("按 %q 应触发退出命令", key)
		}
		if !app.quitting {
			t.Errorf("按 %q 应标记为正在退出", key)
		}
	}
}
