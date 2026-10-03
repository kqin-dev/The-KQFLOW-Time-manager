package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// TestGoalToggleArchives 验证 GOAL 完成后移入当日归档，取消完成则移回（见需求 10）。
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
	if app.goals[0].Tag == "" {
		t.Error("GOAL 应带标签，供继承时防混淆")
	}
	goalID := app.goals[0].ID

	// 焦点此时在 GOAL 栏，勾选后应从 goals.json 移入当日归档。
	press(t, app, "space")
	if len(app.goals) != 0 {
		t.Errorf("完成后 goals.json 不应再有该目标，实际 %d 项", len(app.goals))
	}
	if len(app.data.Archive.Goals) != 1 {
		t.Fatalf("当日归档应含 1 个 GOAL，实际 %d", len(app.data.Archive.Goals))
	}
	archived := app.data.Archive.Goals[0]
	if archived.ID != goalID {
		t.Errorf("归档的应是同一个目标，实际 %q", archived.ID)
	}
	if !archived.Done || archived.ArchivedDay != "2026-10-03" {
		t.Errorf("归档目标应标记完成与归档日: done=%v day=%q", archived.Done, archived.ArchivedDay)
	}
	// 右栏仍应能看到它（归档与进行中合并展示）。
	if !strings.Contains(app.View(), "读完三本书") {
		t.Error("看板右栏应同时展示已归档的 GOAL")
	}

	// 落盘校验：goals.json 里已没有它，当日数据里有。
	goals, err := s.Goals()
	if err != nil {
		t.Fatal(err)
	}
	if len(goals) != 0 {
		t.Errorf("goals.json 应为空，实际 %+v", goals)
	}
	savedDay, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if len(savedDay.Archive.Goals) != 1 {
		t.Errorf("当日归档应落盘 1 项，实际 %d", len(savedDay.Archive.Goals))
	}

	// 取消完成：应回到 goals.json，归档里不再有它。
	press(t, app, "space")
	if len(app.data.Archive.Goals) != 0 {
		t.Errorf("取消完成后归档应清空，实际 %d 项", len(app.data.Archive.Goals))
	}
	if len(app.goals) != 1 {
		t.Fatalf("取消完成后 goals.json 应有 1 项，实际 %d", len(app.goals))
	}
	back := app.goals[0]
	if back.ID != goalID || back.Done || back.ArchivedDay != "" {
		t.Errorf("取回的目标状态不正确: %+v", back)
	}
	goals, err = s.Goals()
	if err != nil {
		t.Fatal(err)
	}
	if len(goals) != 1 || goals[0].Done {
		t.Errorf("取回后 goals.json 应有 1 个未完成目标，实际 %+v", goals)
	}
}

// TestGoalDeleteFromArchive 验证删除归档中的 GOAL 会同时从当日数据里移除。
func TestGoalDeleteFromArchive(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)

	press(t, app, "A")
	for _, r := range "临时目标" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	press(t, app, "space") // 完成并归档

	if len(app.data.Archive.Goals) != 1 {
		t.Fatalf("归档应有 1 项，实际 %d", len(app.data.Archive.Goals))
	}

	// 选中归档中的目标并删除。
	app.focus = FocusGoals
	app.cursors.goals = 0
	press(t, app, "d")
	press(t, app, "enter") // 确认删除

	if len(app.data.Archive.Goals) != 0 {
		t.Errorf("删除后归档应为空，实际 %d", len(app.data.Archive.Goals))
	}
	if len(app.goals) != 0 {
		t.Errorf("删除后不应把它塞回 goals.json，实际 %d", len(app.goals))
	}
	savedDay, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if len(savedDay.Archive.Goals) != 0 {
		t.Errorf("删除应落盘，实际归档 %d 项", len(savedDay.Archive.Goals))
	}
}

// TestGoalEditPersists 验证重命名 GOAL 会写回真正存储它的地方。
//
// 归档中的目标存在当日数据里，未归档的存在 goals.json 里，两者都不能改到副本上。
func TestGoalEditPersists(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)

	press(t, app, "A")
	for _, r := range "原名" {
		press(t, app, string(r))
	}
	press(t, app, "enter")

	// 重命名未归档的目标。
	press(t, app, "e")
	app.editor.value = nil
	app.editor.cursor = 0
	for _, r := range "改名后" {
		press(t, app, string(r))
	}
	press(t, app, "enter")

	goals, err := s.Goals()
	if err != nil {
		t.Fatal(err)
	}
	if len(goals) != 1 || goals[0].Title != "改名后" {
		t.Fatalf("重命名未写回 goals.json: %+v", goals)
	}
	if goals[0].Tag != model.TagOf("改名后") {
		t.Errorf("标签应随标题更新，实际 %q", goals[0].Tag)
	}

	// 归档后再重命名，应写回当日数据。
	app.focus = FocusGoals
	app.cursors.goals = 0
	press(t, app, "space")
	press(t, app, "e")
	app.editor.value = nil
	app.editor.cursor = 0
	for _, r := range "归档后改名" {
		press(t, app, string(r))
	}
	press(t, app, "enter")

	savedDay, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if len(savedDay.Archive.Goals) != 1 || savedDay.Archive.Goals[0].Title != "归档后改名" {
		t.Fatalf("重命名未写回当日归档: %+v", savedDay.Archive.Goals)
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

// TestAddGoesToFocusedColumn 验证 a 添加到当前焦点所在的栏（见问题 1）。
//
// 固定栏和临时栏此前共用同一条添加逻辑，导致临时栏实际上加不进去东西。
func TestAddGoesToFocusedColumn(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)

	// 焦点在固定栏。
	app.focus = FocusFixed
	press(t, app, "a")
	for _, r := range "固定的事" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	if len(app.data.Fixed) != 1 {
		t.Fatalf("固定栏应有 1 项，实际 %d", len(app.data.Fixed))
	}
	if len(app.data.Floating) != 0 {
		t.Errorf("焦点在固定栏时不应写入临时栏，实际 %d 项", len(app.data.Floating))
	}
	if app.data.Fixed[0].Kind != model.KindFixed {
		t.Errorf("应创建固定类型，实际 %q", app.data.Fixed[0].Kind)
	}

	// 焦点在临时栏。
	app.focus = FocusFloating
	press(t, app, "a")
	for _, r := range "临时的事" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	if len(app.data.Floating) != 1 {
		t.Fatalf("临时栏应有 1 项，实际 %d", len(app.data.Floating))
	}
	if app.data.Floating[0].Kind != model.KindFloating {
		t.Errorf("应创建临时类型，实际 %q", app.data.Floating[0].Kind)
	}
}

// TestTabIsTheOnlyColumnSwitch 验证栏位只能通过 TAB 切换（见问题 3）。
//
// h/l 与左右方向键不再切换栏位，这样栏内才有空间给子任务等操作。
func TestTabIsTheOnlyColumnSwitch(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	start := app.focus

	for _, key := range []string{"h", "l"} {
		app.focus = start
		press(t, app, key)
		if app.focus != start {
			t.Errorf("按 %q 不应切换栏位，实际从 %v 变成 %v", key, start, app.focus)
		}
	}
	for _, k := range []tea.KeyType{tea.KeyLeft, tea.KeyRight} {
		app.focus = start
		app.Update(tea.KeyMsg{Type: k})
		if app.focus != start {
			t.Errorf("左右方向键不应切换栏位，实际变成 %v", app.focus)
		}
	}

	// TAB 仍然可以循环切换。
	app.focus = start
	seen := map[Focus]bool{}
	for i := 0; i < 4; i++ {
		press(t, app, "tab")
		seen[app.focus] = true
	}
	if len(seen) != 4 {
		t.Errorf("TAB 应能轮到全部 4 个栏位，实际只到过 %d 个", len(seen))
	}
	if app.focus != start {
		t.Errorf("切换 4 次应回到起点，实际 %v", app.focus)
	}
}

// TestTaskCanBeSelectedAndToggled 验证子任务可以被选中和勾选（见问题 3）。
func TestTaskCanBeSelectedAndToggled(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)

	// 造一条带两个子任务的临时 TODO。
	item := model.NewTodo("写论文", model.KindFloating, app.day, at)
	item.Tasks = []model.Task{model.NewTask("列提纲"), model.NewTask("写引言")}
	app.data.Floating = append(app.data.Floating, item)
	app.focus = FocusFloating
	app.cursors.floating = 0

	// enter 进入子任务模式。
	press(t, app, "enter")
	if !app.taskActive {
		t.Fatal("按 enter 应进入子任务模式")
	}
	if app.taskCursor != 0 {
		t.Fatalf("初始应选中第 1 个子任务，实际 %d", app.taskCursor)
	}

	// j 在子任务之间移动，而不是移动父条目。
	press(t, app, "j")
	if app.taskCursor != 1 {
		t.Errorf("按 j 应移到第 2 个子任务，实际 %d", app.taskCursor)
	}
	if app.cursors.floating != 0 {
		t.Errorf("子任务模式下不应移动父条目光标，实际 %d", app.cursors.floating)
	}

	// 空格勾选子任务。
	press(t, app, " ")
	if !item.Tasks[1].Done() {
		t.Error("空格应勾选选中的子任务")
	}
	if item.Done {
		t.Error("只完成一个子任务时父条目不应算完成")
	}
	if item.Tasks[1].DoneAt == nil {
		t.Error("勾选子任务应记录完成时间")
	}

	// 页面上应有选中高亮（子任务行也能被选中）。
	if !strings.Contains(app.View(), "写引言") {
		t.Error("看板应展示子任务")
	}

	// 落盘校验。
	saved, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Floating) != 1 || len(saved.Floating[0].Tasks) != 2 {
		t.Fatalf("子任务应落盘，实际 %+v", saved.Floating)
	}
	if !saved.Floating[0].Tasks[1].Done() {
		t.Error("子任务的完成状态应已落盘")
	}

	// esc 退回父条目。
	press(t, app, "esc")
	if app.taskActive {
		t.Error("按 esc 应退出子任务模式")
	}
	if app.pick != nil {
		t.Error("从子任务模式按 esc 不应弹出退出确认")
	}
}

// TestAllTasksDoneCompletesParent 验证子任务全部完成时父条目也完成。
func TestAllTasksDoneCompletesParent(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)

	item := model.NewTodo("两小步", model.KindFloating, app.day, at)
	item.Tasks = []model.Task{model.NewTask("第一步"), model.NewTask("第二步")}
	app.data.Floating = append(app.data.Floating, item)
	app.focus = FocusFloating
	app.cursors.floating = 0

	press(t, app, "enter")
	press(t, app, " ")
	press(t, app, "j")
	press(t, app, " ")
	if !item.Done {
		t.Fatal("子任务全部完成时父条目应标记为完成")
	}
	if item.DoneAt == nil {
		t.Error("父条目完成时应记录时间")
	}

	// 取消第二个子任务，父条目应回到未完成。
	// 注意：全部待办已完成会触发庆祝动画，而动画会优先吞掉按键（见需求 10），
	// 所以这里先手动结束动画，以便验证子任务回退逻辑本身。
	app.celebrate = nil
	press(t, app, " ")
	if item.Tasks[1].Done() {
		t.Fatal("再按一次空格应取消该子任务")
	}
	if item.Done {
		t.Error("取消子任务后父条目应回到未完成")
	}
	if item.DoneAt != nil {
		t.Error("父条目回到未完成时应清掉完成时间")
	}
	if item.Status != model.StatusDoing {
		t.Errorf("仍有子任务完成时父条目应为 doing，实际 %q", item.Status)
	}
}

// TestPomodoroCyclesConfigurable 验证番茄钟段数可配置（见问题 4）。
func TestPomodoroCyclesConfigurable(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	// 默认 4 段：4 个专注 + 3 个休息。
	app, _, cfg := newTestApp(t, at)
	plan := app.pomodoroPlan()
	if plan.Cycle != 4 {
		t.Errorf("默认应为 4 段，实际 %d", plan.Cycle)
	}
	focus, rest := 0, 0
	for _, s := range plan.Segments {
		switch s.Kind {
		case "focus":
			focus++
		case "break":
			rest++
		}
	}
	if focus != 4 || rest != 3 {
		t.Errorf("4 段番茄钟应有 4 专注 + 3 休息，实际 %d/%d", focus, rest)
	}
	// 最后一段应是专注，避免计时结束后又跳进休息。
	if last := plan.Segments[len(plan.Segments)-1].Kind; last != "focus" {
		t.Errorf("最后一段应为专注，实际 %q", last)
	}

	// 改成 2 段。
	cfg.PomodoroCycles = 2
	plan = app.pomodoroPlan()
	if plan.Cycle != 2 {
		t.Errorf("应为 2 段，实际 %d", plan.Cycle)
	}
	if len(plan.Segments) != 3 {
		t.Errorf("2 段番茄钟应有 3 个时段，实际 %d", len(plan.Segments))
	}

	// 1 段时只有专注，没有休息。
	cfg.PomodoroCycles = 1
	plan = app.pomodoroPlan()
	if len(plan.Segments) != 1 || plan.Segments[0].Kind != "focus" {
		t.Errorf("1 段番茄钟应只有专注，实际 %+v", plan.Segments)
	}

	// 段数应通过设置页写入配置。
	cfg.PomodoroCycles = 0
	app.view = ViewSettings
	app.settingsCursor = settingIndex(t, "番茄钟段数（专注 + 休息为一轮）")
	press(t, app, "enter")
	app.editor.value = nil
	app.editor.cursor = 0
	for _, r := range "3" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	if cfg.EffectivePomodoroCycles() != 3 {
		t.Errorf("设置页应写入 3 段，实际 %d", cfg.EffectivePomodoroCycles())
	}
	// 非法输入不应写入。
	press(t, app, "enter")
	app.editor.value = nil
	app.editor.cursor = 0
	for _, r := range "99" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	if cfg.EffectivePomodoroCycles() != 3 {
		t.Errorf("非法段数不应写入，实际 %d", cfg.EffectivePomodoroCycles())
	}
}

// settingIndex 按标签找到设置项下标。
func settingIndex(t *testing.T, label string) int {
	t.Helper()
	for i, item := range settingItems {
		if item.Label == label {
			return i
		}
	}
	t.Fatalf("没有找到设置项 %q", label)
	return 0
}

// TestSettingsNavigation 验证设置页可以用 j/k 与方向键选择（见问题 5）。
func TestSettingsNavigation(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.view = ViewSettings
	app.settingsCursor = 0

	press(t, app, "j")
	if app.settingsCursor != 1 {
		t.Errorf("按 j 应下移一项，实际 %d", app.settingsCursor)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	if app.settingsCursor != 2 {
		t.Errorf("方向键下应下移一项，实际 %d", app.settingsCursor)
	}
	press(t, app, "k")
	if app.settingsCursor != 1 {
		t.Errorf("按 k 应上移一项，实际 %d", app.settingsCursor)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyUp})
	if app.settingsCursor != 0 {
		t.Errorf("方向键上应上移一项，实际 %d", app.settingsCursor)
	}
	// 从首项再上移应绕到末项。
	press(t, app, "k")
	if app.settingsCursor != len(settingItems)-1 {
		t.Errorf("越过首项应绕到末项，实际 %d", app.settingsCursor)
	}
	// G / g 跳转。
	press(t, app, "g")
	if app.settingsCursor != 0 {
		t.Errorf("按 g 应回到首项，实际 %d", app.settingsCursor)
	}
	press(t, app, "G")
	if app.settingsCursor != len(settingItems)-1 {
		t.Errorf("按 G 应到末项，实际 %d", app.settingsCursor)
	}
}

// TestCustomQuotes 验证用户可以自定义多条字条（见问题 9）。
//
// 早期输入框只能输入一行，所以“每行一条”实际只能用一条；
// 现在输入框支持多行：Enter 换行，Ctrl+S 保存。
func TestCustomQuotes(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, cfg := newTestApp(t, at)

	// 默认使用内置字条。
	if len(app.customQuotes()) != len(Quotes) {
		t.Fatalf("默认应使用内置字条，实际 %d 条", len(app.customQuotes()))
	}

	// 通过设置页写入三条自定义字条。
	app.view = ViewSettings
	app.settingsCursor = settingIndex(t, "自定义字条（每行一条）")
	press(t, app, "enter")
	if !app.editor.multiline {
		t.Fatal("自定义字条应使用多行输入框")
	}
	app.editor.value = nil
	app.editor.cursor = 0

	// 逐字输入，用 Enter 换行（多行模式下 Enter 不提交）。
	for _, r := range "第一条" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	for _, r := range "第二条" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	for _, r := range "第三条" {
		press(t, app, string(r))
	}
	// 此时仍应处于编辑状态（Enter 只换行）。
	if !app.editor.active {
		t.Fatal("多行模式下 Enter 不应提交")
	}
	if got := app.editor.lineCount(); got != 3 {
		t.Fatalf("应有 3 行，实际 %d：%q", got, string(app.editor.value))
	}

	// Ctrl+S 保存。
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if app.editor.active {
		t.Fatal("Ctrl+S 应提交并关闭输入框")
	}
	if len(cfg.Quotes) != 3 {
		t.Fatalf("应写入 3 条字条，实际 %#v", cfg.Quotes)
	}
	if cfg.Quotes[0] != "第一条" || cfg.Quotes[1] != "第二条" || cfg.Quotes[2] != "第三条" {
		t.Errorf("字条内容不正确: %#v", cfg.Quotes)
	}

	// 看板应展示自定义字条（可能因宽度不足而折行，所以先去掉空白再比对）。
	app.view = ViewDashboard
	app.quoteIdx = 0
	flat := strings.Join(strings.Fields(app.View()), "")
	if !strings.Contains(flat, "第一条") {
		t.Errorf("看板应展示自定义字条，实际输出:\n%s", app.View())
	}
	if strings.Contains(flat, Quotes[0]) {
		t.Error("设置了自定义字条后不应再显示内置字条")
	}
	// 轮换应覆盖全部三条。
	seen := map[string]bool{}
	for i := 0; i < 60; i++ {
		seen[app.CurrentQuote()] = true
		app.NextQuote()
	}
	if len(seen) != 3 {
		t.Errorf("轮换应覆盖 3 条自定义字条，实际覆盖 %d 条: %v", len(seen), seen)
	}

	// 多行解析：直接验证解析函数。
	lines := parseQuoteLines("  第一条  \n\n第二条\n   \n第三条")
	if len(lines) != 3 || lines[0] != "第一条" || lines[2] != "第三条" {
		t.Errorf("多行解析不正确: %#v", lines)
	}

	// 清空后回到内置字条。
	cfg.Quotes = nil
	if len(app.customQuotes()) != len(Quotes) {
		t.Error("清空自定义字条后应回到内置字条")
	}
	// 轮换不能越界。
	for i := 0; i < 30; i++ {
		app.NextQuote()
		if got := app.CurrentQuote(); got == "" {
			t.Fatal("轮换后字条不应为空")
		}
	}
}

// TestTodoAddTargets 验证两种 TODO 的添加目标互不干扰（见问题 1）。
func TestTodoAddTargets(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)

	// 焦点在中间菜单时，a 默认加到临时栏。
	app.focus = FocusMenu
	press(t, app, "a")
	for _, r := range "随手记" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	if len(app.data.Floating) != 1 {
		t.Errorf("焦点在菜单时 a 应加到临时栏，实际临时 %d 项", len(app.data.Floating))
	}

	// 从 GOAL 栏按 a 也应加到临时栏，而不是 GOAL。
	app.focus = FocusGoals
	press(t, app, "a")
	for _, r := range "另一件" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	if len(app.data.Floating) != 2 {
		t.Errorf("应加到临时栏，实际 %d 项", len(app.data.Floating))
	}
	if len(app.goals) != 0 {
		t.Errorf("按 a 不应创建 GOAL，实际 %d 个", len(app.goals))
	}
}

// TestTimerDurationsConfigurable 验证专注时长、休息时长、倒计时时长都由用户设置
// 决定，而不是写死的常量（见问题 3）。
func TestTimerDurationsConfigurable(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, cfg := newTestApp(t, at)

	// 改成一组非默认值。
	cfg.DefaultFocus = 50
	cfg.DefaultBreak = 10
	cfg.PomodoroCycles = 2
	cfg.CountdownMin = 90

	plan := app.pomodoroPlan()
	if plan.Cycle != 2 {
		t.Fatalf("段数应为 2，实际 %d", plan.Cycle)
	}
	// 2 段 = 专注 + 休息 + 专注。
	if len(plan.Segments) != 3 {
		t.Fatalf("2 段应有 3 个时段，实际 %d", len(plan.Segments))
	}
	if plan.Segments[0].Dur != 50*time.Minute {
		t.Errorf("专注时长应取自设置（50m），实际 %v", plan.Segments[0].Dur)
	}
	if plan.Segments[1].Dur != 10*time.Minute {
		t.Errorf("休息时长应取自设置（10m），实际 %v", plan.Segments[1].Dur)
	}
	if plan.Segments[2].Dur != 50*time.Minute {
		t.Errorf("最后一段专注应也是 50m，实际 %v", plan.Segments[2].Dur)
	}
	if plan.Segments[2].Kind != "focus" {
		t.Errorf("最后一段应为专注，实际 %q", plan.Segments[2].Kind)
	}

	// 自定义时段的初始方案也跟随设置。
	custom := app.defaultCustomPlan()
	if custom.Segments[0].Dur != 50*time.Minute || custom.Segments[1].Dur != 10*time.Minute {
		t.Errorf("自定义时段默认方案应跟随设置，实际 %v/%v",
			custom.Segments[0].Dur, custom.Segments[1].Dur)
	}

	// 倒计时走单独的设置项。
	if got := cfg.CountdownMinutes(); got != 90 {
		t.Errorf("倒计时时长应为 90，实际 %d", got)
	}
	cfg.CountdownMin = 0
	if got := cfg.CountdownMinutes(); got != 50 {
		t.Errorf("未单独设置倒计时时应跟随专注时长，实际 %d", got)
	}

	// 通过设置页写入专注与休息时长。
	app.view = ViewSettings
	app.settingsCursor = settingIndex(t, "专注时长（分钟）")
	press(t, app, "enter")
	app.editor.value = nil
	app.editor.cursor = 0
	for _, r := range "35" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	if cfg.FocusMinutes() != 35 {
		t.Errorf("设置页应写入专注时长 35，实际 %d", cfg.FocusMinutes())
	}

	app.settingsCursor = settingIndex(t, "休息时长（分钟）")
	press(t, app, "enter")
	app.editor.value = nil
	app.editor.cursor = 0
	for _, r := range "7" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	if cfg.BreakMinutes() != 7 {
		t.Errorf("设置页应写入休息时长 7，实际 %d", cfg.BreakMinutes())
	}
	// 非法值不写入。
	press(t, app, "enter")
	app.editor.value = nil
	app.editor.cursor = 0
	for _, r := range "999" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	if cfg.BreakMinutes() != 7 {
		t.Errorf("非法休息时长不应写入，实际 %d", cfg.BreakMinutes())
	}
}

// TestLeftPanelsRenderSeparately 验证固定与临时各自成框架，TAB 焦点可见（见问题 2）。
func TestLeftPanelsRenderSeparately(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.data.Fixed = []*model.Todo{model.NewTodo("固定项", model.KindFixed, app.day, at)}
	app.data.Floating = []*model.Todo{model.NewTodo("临时项", model.KindFloating, app.day, at)}

	render := func(focus Focus) string {
		app.focus = focus
		return app.renderLeftPanel(34, 30)
	}

	fixedView := render(FocusFixed)
	floatView := render(FocusFloating)

	// 两栏各自有边框：应该出现两组顶边。
	if n := strings.Count(fixedView, "╭"); n < 2 {
		t.Errorf("固定+临时应各有一个边框（至少两个顶边），实际 %d 个", n)
	}
	// 焦点不同时高亮不同，渲染结果应当不同。
	if fixedView == floatView {
		t.Error("切换焦点后左栏应有视觉差异")
	}
	// 两栏标题都在。
	if !strings.Contains(fixedView, "固定") || !strings.Contains(fixedView, "临时") {
		t.Error("左栏应同时显示固定与临时两栏标题")
	}
}

// TestHelpDescriptionsNotClipped 验证帮助页不会把说明文字截掉（见用户反馈的截图）。
//
// 早期帮助页宽度算错，面板只有 20 多列，于是只剩下光秃秃的按键名，
// 说明全被截断，看起来像渲染坏掉。现在帮助页复用中间栏，内容多时滚动查看，
// 所以这里把所有滚动位置的内容拼起来检查。
func TestHelpDescriptionsNotClipped(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 30, 0, 0, time.Local)
	for _, size := range [][2]int{{144, 45}, {120, 36}, {100, 30}, {80, 24}} {
		app, _, _ := newTestApp(t, at)
		app.width, app.height = size[0], size[1]
		app.view = ViewHelp
		app.pageScroll = 0

		// 逐屏收集，模拟用户按 j 往下滚。
		var all []string
		for i := 0; i < 40; i++ {
			all = append(all, strings.Fields(app.View())...)
			before := app.pageScroll
			press(t, app, "j")
			if app.pageScroll == before {
				break
			}
		}
		flat := strings.Join(all, "")

		for _, want := range []string{
			"在当前栏内上下移动",
			"为选中条目添加子任务",
			"从昨日继承",
		} {
			if !strings.Contains(flat, want) {
				t.Errorf("%dx%d：滚动后帮助页仍缺少说明 %q", size[0], size[1], want)
			}
		}
		// 每一屏都不能超出终端。
		app.view = ViewHelp
		app.pageScroll = 0
		out := app.View()
		for i, l := range strings.Split(out, "\n") {
			if w := lipgloss.Width(l); w > size[0] {
				t.Errorf("%dx%d：帮助页第 %d 行宽 %d 超出终端", size[0], size[1], i, w)
			}
		}
		if len(strings.Split(out, "\n")) > size[1] {
			t.Errorf("%dx%d：帮助页行数超出终端", size[0], size[1])
		}
	}
}

// TestPagesUseCenterColumn 验证帮助/设置/历史复用中间栏（见用户反馈）。
//
// 这三个二级页不再单独铺满屏幕，而是和二级菜单一样只占中间栏，
// 左右两侧的 TODO 与 GOAL 面板保持可见。
func TestPagesUseCenterColumn(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 30, 0, 0, time.Local)
	for _, v := range []struct {
		name string
		view View
		key  string
	}{
		{"帮助", ViewHelp, "帮助"},
		{"设置", ViewSettings, "设置"},
		{"历史", ViewHistory, "历史"},
	} {
		app, s, _ := newTestApp(t, at)
		app.width, app.height = 120, 36
		app.data.Fixed = []*model.Todo{model.NewTodo("固定项", model.KindFixed, app.day, at)}
		app.data.Archive.Goals = []model.Goal{*model.NewGoal("归档目标", at)}
		if err := s.SaveDay(app.data); err != nil {
			t.Fatal(err)
		}
		app.reload()
		app.view = v.view

		out := app.View()
		flat := strings.Join(strings.Fields(out), "")
		if !strings.Contains(flat, v.key) {
			t.Errorf("%s：二级页应正常渲染", v.name)
		}
		// 左右面板必须仍然可见——这正是复用中间栏的意义。
		for _, want := range []string{"固定项", "GOAL"} {
			if !strings.Contains(flat, want) {
				t.Errorf("%s：二级页把 %q 盖住了，应只占中间栏", v.name, want)
			}
		}
		// 三栏边框都在。
		if n := strings.Count(out, "╭"); n < 3 {
			t.Errorf("%s：应保留三个面板边框，实际 %d 个", v.name, n)
		}
		// 每行宽度必须等于终端宽度。
		for i, l := range strings.Split(out, "\n") {
			if lw := lipgloss.Width(l); lw != app.width {
				t.Errorf("%s：第 %d 行宽 %d，应等于 %d", v.name, i, lw, app.width)
			}
		}
	}
}

// TestPasteInsertsText 验证粘贴进来的中文会整段写入输入框。
//
// 中文输入法在终端里无法可靠地逐键送字，粘贴是主要的输入路径。
func TestPasteInsertsText(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)

	app.focus = FocusFloating
	press(t, app, "a")
	if !app.editor.active {
		t.Fatal("输入框应处于激活状态")
	}

	// 模拟终端粘贴：Bubble Tea v1 用 KeyMsg{Paste: true} 表示括号粘贴。
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("买牛奶 和 面包"), Paste: true})
	if got := string(app.editor.value); got != "买牛奶 和 面包" {
		t.Fatalf("粘贴内容应进入输入框，实际 %q", got)
	}
	// 粘贴内容里的字符不应被当成快捷键（例如 a/d/q）。
	if !app.editor.active {
		t.Error("粘贴不应关闭输入框")
	}

	// 粘贴多行时应把换行折成空格，避免标题被拆断。
	app.editor.value = nil
	app.editor.cursor = 0
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("第一行\n第二行"), Paste: true})
	if got := string(app.editor.value); got != "第一行 第二行" {
		t.Errorf("多行粘贴应折成一行，实际 %q", got)
	}

	press(t, app, "enter")
	if len(app.data.Floating) != 1 {
		t.Fatalf("应新增 1 项，实际 %d", len(app.data.Floating))
	}
	if app.data.Floating[0].Title != "第一行 第二行" {
		t.Errorf("标题不正确: %q", app.data.Floating[0].Title)
	}
	saved, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Floating) != 1 || saved.Floating[0].Title != "第一行 第二行" {
		t.Errorf("中文标题应落盘: %+v", saved.Floating)
	}
}

// TestPasteIgnoredWhenNoEditor 验证没有输入框时粘贴不会造成意外操作。
func TestPasteIgnoredWhenNoEditor(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	before := len(app.data.All())
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("dq"), Paste: true})
	if app.quitting {
		t.Error("粘贴内容不应触发退出")
	}
	if app.pick != nil {
		t.Error("粘贴内容不应触发删除确认")
	}
	if len(app.data.All()) != before {
		t.Error("粘贴内容不应改动数据")
	}
}

// TestEditorVisibleOnSettingsPage 验证在设置页编辑时能看到输入框与输入内容。
//
// 早期输入框只在看板那一页才渲染，所以在设置页按 enter 之后完全看不到
// 自己在输入什么，直到回车确认才生效——用户以为没反应。
func TestEditorVisibleOnSettingsPage(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, cfg := newTestApp(t, at)
	app.width, app.height = 100, 30
	app.view = ViewSettings
	app.settingsCursor = settingIndex(t, "昵称（显示在问候语里）")

	press(t, app, "enter")
	if !app.editor.active {
		t.Fatal("按 enter 应打开输入框")
	}
	app.insertEditorText("kevin")

	out := app.View()
	flat := strings.Join(strings.Fields(out), "")
	// 输入框标题与刚输入的字符都必须出现在屏幕上。
	if !strings.Contains(flat, "昵称") {
		t.Errorf("设置页编辑时应显示输入框标题，实际输出:\n%s", out)
	}
	if !strings.Contains(flat, "kevin") {
		t.Errorf("设置页编辑时应能看到刚输入的字符，实际输出:\n%s", out)
	}
	// 输入过程中设置尚未写入。
	if cfg.Nickname != "" {
		t.Errorf("回车确认前不应写入配置，实际 %q", cfg.Nickname)
	}
	// 回车确认后才写入。
	press(t, app, "enter")
	if cfg.Nickname != "kevin" {
		t.Errorf("确认后应写入昵称，实际 %q", cfg.Nickname)
	}
}

// TestEditorVisibleWhenAddingTodo 验证添加 TODO 时输入框只占中间栏。
//
// 早期输入框被拼到整个看板上，会盖住左右面板的边框，看起来像渲染坏了。
func TestEditorVisibleWhenAddingTodo(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.width, app.height = 100, 30
	app.focus = FocusFixed
	app.startAdd()
	app.insertEditorText("买牛奶")

	out := app.View()
	flat := strings.Join(strings.Fields(out), "")
	if !strings.Contains(flat, "买牛奶") {
		t.Errorf("输入内容应可见，实际输出:\n%s", out)
	}
	// 左右面板的边框必须保留：输入框只占中间栏。
	for i, l := range strings.Split(out, "\n") {
		if lw := lipgloss.Width(l); lw != app.width {
			t.Errorf("第 %d 行宽 %d，应等于终端宽度 %d", i, lw, app.width)
		}
	}
	if strings.Count(out, "╭") < 3 {
		t.Errorf("应保留三个面板的边框，实际 %d 个", strings.Count(out, "╭"))
	}
}

// TestRollingStats 验证“连续 7 天统计”取的是滚动窗口（见用户反馈）。
func TestRollingStats(t *testing.T) {
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)
	app.day = "2026-10-07"

	// 造历史：10-01 空，10-02 完成 1/2，10-05 完成 2/2，10-07（今天）0/1。
	mk := func(day string, done, total int, focus time.Duration) {
		d := model.NewDayData(day, at)
		for i := 0; i < total; i++ {
			item := model.NewTodo(fmt.Sprintf("%s-%d", day, i), model.KindFloating, day, at)
			if i < done {
				item.Toggle(at)
			}
			d.Floating = append(d.Floating, item)
		}
		if focus > 0 {
			end := at
			d.Archive.Sessions = append(d.Archive.Sessions,
				model.Session{Elapsed: focus, SegmentKind: "focus", Ended: &end})
		}
		if err := s.SaveDay(d); err != nil {
			t.Fatal(err)
		}
	}
	mk("2026-10-02", 1, 2, 30*time.Minute)
	mk("2026-10-05", 2, 2, time.Hour)
	mk("2026-10-07", 0, 1, 0)

	stats := app.rollingStats("2026-10-07", 7)
	if len(stats) != 7 {
		t.Fatalf("应返回 7 天，实际 %d", len(stats))
	}
	if stats[0].Day != "2026-10-01" || stats[6].Day != "2026-10-07" {
		t.Errorf("窗口应为 10-01..10-07，实际 %s..%s", stats[0].Day, stats[6].Day)
	}
	byDay := map[string]dayStat{}
	for _, st := range stats {
		byDay[st.Day] = st
	}
	if got := byDay["2026-10-02"]; got.Done != 1 || got.Total != 2 || got.Focus != 30*time.Minute {
		t.Errorf("10-02 统计不正确: %+v", got)
	}
	if got := byDay["2026-10-05"]; got.Done != 2 || got.Total != 2 {
		t.Errorf("10-05 统计不正确: %+v", got)
	}
	// 没有数据的日子标记为无记录。
	if byDay["2026-10-03"].HasAny {
		t.Error("10-03 没有数据，不应标记为有记录")
	}
	if !byDay["2026-10-02"].HasAny {
		t.Error("10-02 有数据，应标记为有记录")
	}
	// 渲染出来要包含统计标题与今天的标记。
	lines := app.statsLines(app.contentWidth(), 12)
	flat := strings.Join(strings.Fields(strings.Join(lines, "")), "")
	if !strings.Contains(flat, "连续7天") {
		t.Errorf("统计区应包含标题，实际: %v", flat)
	}
	if !strings.Contains(flat, "今") {
		t.Error("统计区应标出今天")
	}
}

// TestCurrentStreak 验证连续天数统计。
func TestCurrentStreak(t *testing.T) {
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)

	// 10-05、10-06、10-07 都有记录，10-04 空 → 连续 3 天。
	for _, day := range []string{"2026-10-05", "2026-10-06", "2026-10-07"} {
		d := model.NewDayData(day, at)
		item := model.NewTodo("完成的事", model.KindFloating, day, at)
		item.Toggle(at)
		d.Floating = append(d.Floating, item)
		if err := s.SaveDay(d); err != nil {
			t.Fatal(err)
		}
	}
	if got := app.currentStreak("2026-10-07", 30); got != 3 {
		t.Errorf("连续天数应为 3，实际 %d", got)
	}

	// 今天没有记录时，从今天起就断了。
	empty := model.NewDayData("2026-10-08", at)
	if err := s.SaveDay(empty); err != nil {
		t.Fatal(err)
	}
	if got := app.currentStreak("2026-10-08", 30); got != 0 {
		t.Errorf("当天无记录时连续天数应为 0，实际 %d", got)
	}
}

// TestNoteEditor 验证随手记可以写多行、按日保存并在看板上按需展示。
func TestNoteEditor(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, cfg := newTestApp(t, at)

	// 从菜单打开随手记。
	app.focus = FocusMenu
	app.cursors.menu = 0
	press(t, app, "enter")
	if !app.editor.active || !app.editor.multiline {
		t.Fatal("菜单第一项应打开多行随手记编辑器")
	}

	// 输入三行：Enter 必须是换行而不是提交。
	for _, r := range "第一行" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	for _, r := range "第二行" {
		press(t, app, string(r))
	}
	press(t, app, "enter")
	for _, r := range "第三行" {
		press(t, app, string(r))
	}
	if !app.editor.active {
		t.Fatal("Enter 应换行而不是提交")
	}
	if got := app.editor.lineCount(); got != 3 {
		t.Fatalf("应有 3 行，实际 %d", got)
	}

	// Ctrl+S 保存。
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if app.editor.active {
		t.Fatal("Ctrl+S 应保存并关闭")
	}
	if want := "第一行\n第二行\n第三行"; app.data.Note != want {
		t.Fatalf("随手记内容不正确: %q", app.data.Note)
	}
	// 落盘校验。
	saved, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Note != "第一行\n第二行\n第三行" {
		t.Errorf("随手记应落盘，实际 %q", saved.Note)
	}

	// 默认不在看板展示。
	if cfg.ShowNote {
		t.Fatal("默认不应在看板展示随手记")
	}
	app.view = ViewDashboard
	flat := strings.Join(strings.Fields(app.View()), "")
	if strings.Contains(flat, "随手记·") {
		t.Error("未开启展示时看板不应出现随手记")
	}

	// 开启展示后，看板出现随手记的前几行。
	cfg.ShowNote = true
	flat = strings.Join(strings.Fields(app.View()), "")
	if !strings.Contains(flat, "随手记") {
		t.Errorf("开启后看板应展示随手记，实际输出:\n%s", app.View())
	}
	if !strings.Contains(flat, "第一行") {
		t.Error("看板应展示随手记的开头内容")
	}

	// Esc 取消不应改动内容。
	app.openNote()
	if app.data.Note != "第一行\n第二行\n第三行" {
		t.Fatal("打开编辑器不应改动内容")
	}
	app.editor.value = []rune("改坏了")
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.data.Note != "第一行\n第二行\n第三行" {
		t.Errorf("Esc 取消不应写回，实际 %q", app.data.Note)
	}

	// 第二天是全新的一份，不会带上前一天的随手记。
	app2, s2, _ := newTestApp(t, time.Date(2026, 10, 4, 9, 0, 0, 0, time.Local))
	if app2.data.Note != "" {
		t.Errorf("第二天随手记应为空，实际 %q", app2.data.Note)
	}
	_ = s2
}

// TestNoteNotShownWhenEmpty 验证没有随手记时看板不会留出空区块。
func TestNoteNotShownWhenEmpty(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, cfg := newTestApp(t, at)
	cfg.ShowNote = true
	app.view = ViewDashboard
	if got := app.notePreviewLines(40, 4); len(got) != 0 {
		t.Errorf("随手记为空时不应生成预览，实际 %d 行", len(got))
	}
}

// TestCJKEditorCursorAlignsWithRenderedContent 验证含中文时画出的光标落在真实位置。
//
// 中文一个字占两列，早期用 rune 下标定位光标，一旦输入法粘进中文，
// 画出来的光标就会跑到文字前面（用户报过这个 bug）。
func TestCJKEditorCursorAlignsWithRenderedContent(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.width, app.height = 120, 40

	cases := []struct {
		name string
		text string
	}{
		{"纯英文", "hello world"},
		{"纯中文", "中文内容测试"},
		{"中英混排", "今天 write 周报"},
		{"以中文结尾", "abc中文"},
		{"以英文结尾", "中文abc"},
	}
	for _, c := range cases {
		app.openNote()
		app.editor.value = []rune(c.text)
		app.editor.cursor = len(app.editor.value)

		lines := app.multilineEditorLines(app.contentWidth(), 4)
		// 找到带光标的那一行。
		var target string
		for _, l := range lines {
			if strings.Contains(l, "▏") {
				target = l
				break
			}
		}
		if target == "" {
			t.Fatalf("%s：没找到光标", c.name)
		}
		// 光标左边应当是完整输入的内容（去掉 marker 与样式）。
		plain := stripStyles(target)
		idx := strings.Index(plain, "▏")
		left := plain[len(" ▸ "):idx]
		if strings.TrimSpace(left) != c.text {
			t.Errorf("%s：光标左侧应为完整内容 %q，实际 %q（整行 %q）", c.name, c.text, left, plain)
		}
	}
}

// stripStyles 去掉 ANSI 转义，便于按显示内容做断言。
func stripStyles(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// TestVerticalChartBars 验证竖向柱状图：归一化高度 + 柱子与标签对齐 + 铺满可用宽度。
func TestVerticalChartBars(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)

	vals := []dayValue{
		{Label: "日", Value: 0}, {Label: "一", Value: 20}, {Label: "二", Value: 35},
		{Label: "三", Value: 10}, {Label: "四", Value: 0}, {Label: "五", Value: 50},
		{Label: "今", Value: 25, Today: true},
	}
	inner := 72
	maxH := 4
	lines := app.renderVerticalChart(vals, inner, maxH)
	if len(lines) != maxH+2 {
		t.Fatalf("应有 %d 行柱 + 轴 + 标签 = %d 行，实际 %d", maxH, maxH+2, len(lines))
	}

	// 推算出柱子的几何位置：与实现同样的规则。
	// 这里只断言“柱子和标签成对出现且间隔均匀”，不重复实现细节。
	plot := lines[:maxH]
	axis := stripStyles(lines[maxH])
	labels := stripStyles(lines[maxH+1])

	// 轴必须以 └ 开头，且长度与柱子覆盖范围一致。
	if !strings.HasPrefix(axis, "└") {
		t.Errorf("坐标轴应以 └ 开头，实际 %q", axis)
	}
	// 找出轴上的横线段，作为每根柱子的列范围。
	var segs [][2]int
	inSeg := false
	start := 0
	for i, r := range []rune(axis) {
		if r == '─' && !inSeg {
			inSeg, start = true, i
		} else if r != '─' && inSeg {
			inSeg = false
			segs = append(segs, [2]int{start, i - 1})
		}
	}
	if inSeg {
		segs = append(segs, [2]int{start, len([]rune(axis)) - 1})
	}
	if len(segs) != len(vals) {
		t.Fatalf("轴上应有 %d 段横线（每根柱子一段），实际 %d：%q", len(vals), len(segs), axis)
	}
	// 柱子要有实际宽度（不是一根细线）。
	for i, sg := range segs {
		if w := sg[1] - sg[0] + 1; w < 1 {
			t.Errorf("第 %d 根柱子宽度 %d 过小", i, w)
		}
	}
	// 每根有值的柱子都应在自己的列范围内出现方块字符。
	// 注意：这里按**显示列**取字符，不能按 rune 下标（标签是宽字符）。
	for i, v := range vals {
		if v.Value == 0 {
			continue
		}
		found := false
		for _, l := range plot {
			p := stripStyles(l)
			for col := segs[i][0]; col <= segs[i][1]; col++ {
				if ch := cellAt(p, col); ch == "█" || ch == "▄" {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Errorf("第 %d 根柱子（值 %d）应在列 %v 出现方块字符", i, v.Value, segs[i])
		}
	}
	// 标签必须落在各自柱子的列范围内。
	for i, v := range vals {
		want := string([]rune(v.Label)[0])
		ok := false
		for col := segs[i][0]; col <= segs[i][1]+1; col++ {
			if cellAt(labels, col) == want {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("第 %d 个标签 %q 应落在柱子列范围 %v，实际标签行 %q",
				i, v.Label, segs[i], labels)
		}
	}
	// 归一化：最高的一天应占满 maxH 行。
	for i, v := range vals {
		if v.Value != 50 {
			continue
		}
		filled := 0
		for _, l := range plot {
			p := stripStyles(l)
			hit := false
			for col := segs[i][0]; col <= segs[i][1]; col++ {
				if ch := cellAt(p, col); ch == "█" || ch == "▄" {
					hit = true
					break
				}
			}
			if hit {
				filled++
			}
		}
		if filled != maxH {
			t.Errorf("最高的一天应占满 %d 行，实际 %d 行", maxH, filled)
		}
	}
	// 图表整体不应超出内容宽度。
	for i, l := range lines {
		if w := lipgloss.Width(stripStyles(l)); w > inner {
			t.Errorf("第 %d 行宽 %d 超出内容宽度 %d", i, w, inner)
		}
	}
}

// TestStatBarValuesUseFocus 验证柱状图数值取自专注时长。
func TestStatBarValuesUseFocus(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)
	d := model.NewDayData("2026-10-02", at)
	e := at
	d.Archive.Sessions = append(d.Archive.Sessions,
		model.Session{Elapsed: 45 * time.Minute, SegmentKind: "focus", Ended: &e})
	if err := s.SaveDay(d); err != nil {
		t.Fatal(err)
	}
	// 今天只完成了一件事、没有专注 → 给最小高度而不是 0。
	today := model.NewDayData("2026-10-03", at)
	item := model.NewTodo("事", model.KindFloating, "2026-10-03", at)
	item.Toggle(at)
	today.Floating = append(today.Floating, item)
	if err := s.SaveDay(today); err != nil {
		t.Fatal(err)
	}

	vals := app.statBarValues(app.rollingStats("2026-10-03", 7), time.Local)
	byLabel := map[string]dayValue{}
	for _, v := range vals {
		byLabel[v.Label] = v
	}
	if got := byLabel["五"]; got.Value != 45 {
		t.Errorf("10-02 是周五，柱值应为 45 分钟，实际 %d", got.Value)
	}
	if got := byLabel["今"]; got.Value != 1 {
		t.Errorf("今天只完成了待办，柱值应为 1，实际 %d", got.Value)
	}
}

// TestQuitRequiresConfirmation 验证按 q 先弹确认而不是直接退出（见需求 8）。
func TestQuitRequiresConfirmation(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	for _, key := range []string{"q", "Q", "esc"} {
		app, _, _ := newTestApp(t, at)
		_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if cmd != nil {
			t.Errorf("按 %q 不应直接退出", key)
		}
		if app.quitting {
			t.Errorf("按 %q 不应立刻标记为退出", key)
		}
		if app.pick == nil {
			t.Fatalf("按 %q 应弹出退出确认", key)
		}
		if app.pick.cursor != 0 {
			t.Errorf("退出确认默认应选中“取消”，实际下标 %d", app.pick.cursor)
		}
		// 直接回车 = 取消，不应退出。
		press(t, app, "enter")
		if app.quitting {
			t.Errorf("确认框里直接回车应取消退出")
		}
		if app.pick != nil {
			t.Errorf("取消后确认框应关闭")
		}
	}
}

// TestQuitConfirmedExits 验证确认后才真正退出。
func TestQuitConfirmedExits(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)

	press(t, app, "q")
	if app.pick == nil {
		t.Fatal("应弹出退出确认")
	}
	// 选到“退出”并确认。
	press(t, app, "j")
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !app.quitting {
		t.Error("确认后应标记为退出")
	}
	if cmd == nil {
		t.Error("确认后应返回退出命令")
	}
}

// TestCelebrateInterruptsOnAnyKey 验证庆祝动画期间任意按键先中断动画（见需求 10）。
func TestCelebrateInterruptsOnAnyKey(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)

	// 造一个已完成全部待办的场景，触发庆祝。
	app.data.Floating = []*model.Todo{model.NewTodo("唯一的事", model.KindFloating, app.day, at)}
	app.cursors.floating = 0
	app.focus = FocusFloating
	press(t, app, "space")
	if app.celebrate == nil {
		t.Fatal("全部完成应触发庆祝")
	}
	before := len(app.data.Floating)

	// 动画期间的按键只应中断动画，不应作用到看板上。
	press(t, app, "d")
	if app.celebrate != nil {
		t.Error("按键应中断庆祝动画")
	}
	if len(app.data.Floating) != before {
		t.Error("中断动画的按键不应触发删除等操作")
	}
	// 动画已结束，再按 d 才应该真的走删除确认。
	press(t, app, "d")
	if app.pick == nil {
		t.Error("动画结束后的按键应正常生效")
	}
}
