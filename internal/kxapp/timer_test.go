package kxapp

import (
	"strings"
	"testing"
	"time"

	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// setNow 把内存数据源的时钟拨到给定时刻。
//
// 计时器的正确性**只能**用可控时钟验证：真实时间里"跑了 25 分钟"
// 没法在测试里等出来，而"暂停 10 分钟不算进专注"这种事更是只有
// 拨表才能验。这也是引擎坚持把时钟注入的原因之一。
func setNow(src *memSource, t time.Time) { src.now = t }

func timerPlan(minutes int) model.Plan { return focusPlan("测试", minutes) }

// TestTimerElapsedAndPause 验证计时的核心口径：**暂停的时间不算进去**。
func TestTimerElapsedAndPause(t *testing.T) {
	base := testNow()
	var tm Timer
	if !tm.Start(timerPlan(25), nil, base) {
		t.Fatal("应当能开始计时")
	}
	if tm.Running() != true || tm.Paused() {
		t.Fatal("开始后应当是运行中且未暂停")
	}

	// 过 10 分钟。
	at10 := base.Add(10 * time.Minute)
	if got := tm.ElapsedFor(at10); got != 10*time.Minute {
		t.Fatalf("10 分钟后已过时长应为 10m，实际 %v", got)
	}
	if got := tm.Remaining(at10); got != 15*time.Minute {
		t.Fatalf("剩余应为 15m，实际 %v", got)
	}
	if tm.Done(at10) {
		t.Fatal("10 分钟时不该算完成")
	}

	// 暂停，再过 30 分钟：这段不该计入。
	if !tm.Pause(at10) {
		t.Fatal("应能暂停")
	}
	at40 := at10.Add(30 * time.Minute)
	if got := tm.ElapsedFor(at40); got != 10*time.Minute {
		t.Fatalf("暂停期间时间不该走：应仍为 10m，实际 %v", got)
	}
	if tm.Pause(at40) {
		t.Error("重复暂停应当无效")
	}

	// 恢复，再过 5 分钟：共 15 分钟。
	if !tm.Resume(at40) {
		t.Fatal("应能恢复")
	}
	at45 := at40.Add(5 * time.Minute)
	if got := tm.ElapsedFor(at45); got != 15*time.Minute {
		t.Fatalf("恢复后总时长应为 15m，实际 %v", got)
	}
	if tm.Resume(at45) {
		t.Error("未暂停时恢复应当无效")
	}

	// 走满 25 分钟即完成。
	at55 := at45.Add(10 * time.Minute)
	if !tm.Done(at55) {
		t.Fatalf("25 分钟应算完成（已过 %v）", tm.ElapsedFor(at55))
	}
	if got := tm.Remaining(at55); got != 0 {
		t.Errorf("完成后剩余应为 0，实际 %v", got)
	}
}

// TestTimerStopProducesSession 验证结束计时会生成一条口径正确的会话。
//
// 重点是 Elapsed 与 Focus 的区别：前者是挂钟时长（含休息），
// 后者只算专注时段。混淆这两者会让"今天专注了多久"直接算错。
func TestTimerStopProducesSession(t *testing.T) {
	base := testNow()
	var tm Timer
	// 一个"专注 10 分 + 休息 5 分 + 专注 10 分"的方案。
	plan := model.Plan{
		Kind: model.TimerCustom, Label: "两段",
		Segments: []model.Segment{
			{Name: "专注", Kind: "focus", Dur: 10 * time.Minute},
			{Name: "休息", Kind: "break", Dur: 5 * time.Minute},
			{Name: "专注", Kind: "focus", Dur: 10 * time.Minute},
		},
	}
	todo := &model.Todo{ID: "todo_1", Title: "写文档"}
	tm.Start(plan, todo, base)

	// 跑了 18 分钟（含 5 分钟休息）。
	session := tm.Stop(base.Add(18 * time.Minute))

	if session.Elapsed != 18*time.Minute {
		t.Errorf("Elapsed 应为 18m（挂钟），实际 %v", session.Elapsed)
	}
	// 专注时段：第一段满 10 分钟，第三段走了 3 分钟 = 13 分钟。
	if session.Focus == nil {
		t.Fatal("有休息时段时应当记录 Focus")
	}
	if *session.Focus != 13*time.Minute {
		t.Errorf("Focus 应为 13m（只算专注），实际 %v", *session.Focus)
	}
	if session.TodoRef != "todo_1" || session.TodoName != "写文档" {
		t.Errorf("会话应记住挂着的条目，实际 %+v", session)
	}
	if session.Completed {
		t.Error("没跑完整个方案时 Completed 应为假")
	}
	if session.Ended == nil {
		t.Error("会话应当有结束时刻")
	}
	// 停止之后必须复位：下一次 Start 是全新的一次。
	if tm.Running() {
		t.Error("停止后不该仍在计时")
	}
	if tm.ElapsedFor(base.Add(time.Hour)) != 0 {
		t.Error("停止后已过时长应清零")
	}
}

// TestTimerCannotStartTwice 验证重复开始不会把正在跑的计时顶掉。
func TestTimerCannotStartTwice(t *testing.T) {
	base := testNow()
	var tm Timer
	tm.Start(timerPlan(25), nil, base)

	// 10 分钟后再按"开始"：不能重新计时（否则前 10 分钟白跑）。
	if tm.Start(timerPlan(5), nil, base.Add(10*time.Minute)) {
		t.Fatal("已在计时时不该能再次开始")
	}
	if got := tm.ElapsedFor(base.Add(10 * time.Minute)); got != 10*time.Minute {
		t.Errorf("重复开始不该重置起步时刻：应仍为 10m，实际 %v", got)
	}
}

// TestTimerEndToEndThroughEngine 是计时的**端到端**验证。
//
// 它走真实按键路径（不是直接调 Timer 的方法），因此同时验证了
// "看板选项 → 视图 → 状态 → 磁贴显示"整条链路。
func TestTimerEndToEndThroughEngine(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, services, rep := l.Build()
	if len(rep.Rejected) != 0 {
		t.Fatalf("装载不应有错误：\n%s", rep.Explain())
	}
	// 计时包必须装载：它提供 focus.session 能力。
	if !containsStr(rep.Loaded, TimerPackID) {
		t.Fatalf("计时包应被装载，实际 %v", rep.Loaded)
	}
	m.Resize(120, 34)

	// 未开始时磁贴显示"未开始"。
	if !strings.Contains(m.View(), "未开始") {
		t.Errorf("未开始时磁贴应显示「未开始」：\n%s", m.View())
	}

	// 打开看板上的"开始专注"。
	opened := false
	for _, b := range m.OptionKeys() {
		if strings.Contains(b.Label, "开始专注") {
			m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: b.Key})
			opened = true
			break
		}
	}
	if !opened {
		t.Fatalf("看板上应有「开始专注」选项，实际 %v", m.OptionKeys())
	}
	if !strings.Contains(m.View(), "选择时长") {
		t.Fatalf("应打开计时的时长选择界面：\n%s", m.View())
	}

	// 回车开始第一项（默认专注时长）。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	state := l.State()
	if !state.Timer.Running() {
		t.Fatal("回车后应当开始计时")
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"}) // 退出视图

	// 磁贴应当显示剩余时间。
	out := m.View()
	if !strings.Contains(out, "专注") {
		t.Errorf("计时中磁贴应显示状态：\n%s", out)
	}

	// 让时间走过整个方案：引擎的 Tick 应当响一次铃（Effect）。
	//
	// 注意走的是 **Tick**（周期检查）而不是"按某个键"：用户那时很可能
	// 正在别的栏位干活，那才是最需要提醒的时候。
	minutes := defaultFocusMinutes(src)
	setNow(src, testNow().Add(minutesToDuration(minutes)+time.Minute))
	m.Tick(src.Now())
	if !services.Belled() {
		t.Errorf("时段走完应当响铃一次，实际副作用：%s", services.Describe())
	}
	// 铃只响一次：再 Tick 几次不该再响。
	before := len(services.Effects)
	for i := 0; i < 3; i++ {
		m.Tick(src.Now())
	}
	if len(services.Effects) != before {
		t.Errorf("完成提示只该响一次，实际 %s", services.Describe())
	}
}

// TestTimerStopRecordsSession 验证结束计时会**真的写进当天记录**。
func TestTimerStopRecordsSession(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, services, _ := l.Build()
	m.Resize(120, 34)

	// 直接开始一次计时（走 Timer 的公开方法，避免依赖界面文案）。
	plan := focusPlan("测试", 25)
	if !l.State().Timer.Start(plan, nil, testNow()) {
		t.Fatal("应当能开始计时")
	}
	// 过 12 分钟，然后通过看板选项进入计时界面并结束。
	setNow(src, testNow().Add(12*time.Minute))

	opened := false
	for _, b := range m.OptionKeys() {
		if strings.Contains(b.Label, "计时中") {
			m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: b.Key})
			opened = true
			break
		}
	}
	if !opened {
		t.Fatalf("计时中时看板选项应变成「计时中…」，实际 %v", m.OptionKeys())
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "s"}) // 结束并记录

	if l.State().Timer.Running() {
		t.Error("结束后不该仍在计时")
	}
	data := src.Day()
	if got := len(data.Archive.Sessions); got != 1 {
		t.Fatalf("应当记录 1 条会话，实际 %d", got)
	}
	s := data.Archive.Sessions[0]
	if s.Elapsed != 12*time.Minute {
		t.Errorf("会话时长应为 12m，实际 %v", s.Elapsed)
	}
	if services.Saves == 0 {
		t.Error("记录会话后应当落盘")
	}
	// 活动表也要更新，否则统计页会漏掉这一次。
	if data.Activity["自由专注"] == nil {
		t.Errorf("活动表应记下「自由专注」，实际 %v", data.Activity)
	} else if data.Activity["自由专注"].Sessions != 1 {
		t.Errorf("活动次数应为 1，实际 %d", data.Activity["自由专注"].Sessions)
	}
}

// TestStatsDependsOnTimerCapability 验证"提供者被关闭"的警告在真实包上成立。
//
// 统计包 Requires(focus.session)，而该能力由计时包提供。
// 关掉计时包之后，统计包应当进 Inactive 并给出**可操作**的指引。
func TestStatsDependsOnTimerCapability(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())

	var packs []plugin.Pack
	for _, p := range l.Packs() {
		if p.ID() == TimerPackID {
			packs = append(packs, disabledPack{p})
			continue
		}
		packs = append(packs, p)
	}
	// 让统计包真的依赖计时能力。
	for i, p := range packs {
		if p.ID() == StatsPackID {
			packs[i] = requiresCap{p, CapFocusSession}
		}
	}

	m := buildWithPacks(t, src, NewServices(src), packs)
	rep := m.Report()
	if len(rep.Rejected) != 0 {
		t.Fatalf("不该有错误：\n%s", rep.Explain())
	}
	if !containsStr(rep.Inactive, StatsPackID) {
		t.Fatalf("统计包应进 Inactive，实际 %v", rep.Inactive)
	}
	ws := rep.WarningsOf(plugin.WarnProviderDisabled)
	if len(ws) == 0 {
		t.Fatalf("应给出「提供者被关闭」的警告：\n%s", rep.Explain())
	}
	if !strings.Contains(ws[0].Detail, TimerPackID) {
		t.Errorf("指引里要点出去开哪个包，实际 %q", ws[0].Detail)
	}
}

// requiresCap 给任意包附加一条能力依赖（测试辅助）。
type requiresCap struct {
	plugin.Pack
	cap string
}

func (r requiresCap) Requires() []string {
	return append(append([]string{}, r.Pack.Requires()...), r.cap)
}

// 让 geometry 与 svc 的引用保持有效（本文件只用到部分符号）。
var _ = geometry.AnchorLeftBottom
