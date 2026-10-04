package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// customSegmentedTimer 造一个「专注 10 分钟 + 休息 5 分钟」的自定义计时。
func customSegmentedTimer(t *testing.T, app *App, at time.Time) {
	t.Helper()
	allowCarrySkip(t, app)
	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)
	plan := model.Plan{Kind: model.TimerCustom, Segments: []model.Segment{
		{Name: "深度工作", Kind: model.SegmentKindFocus, Dur: 10 * time.Minute},
		{Name: "休息", Kind: model.SegmentKindBreak, Dur: 5 * time.Minute},
	}}
	app.data.Floating = append(app.data.Floating, model.NewTodo("写论文", model.KindFloating, "2026-10-03", at))
	app.beginTimer(plan, app.data.Floating[0].ID)
	if app.timer == nil {
		t.Fatal("计时应已开始")
	}
}

// TestSegmentChangeTriggersNotification 验证跨过时段边界时触发提醒，且只触发一次。
func TestSegmentChangeTriggersNotification(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	customSegmentedTimer(t, app, at)
	app.cfg.NotifyGlow = true

	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)

	// 第一次推进：只记录当前段，不该提醒（否则刚开始就响）。
	if n := app.checkSegmentChange(now); n != nil {
		t.Error("第一次推进不该触发提醒")
	}

	// 走到第二段（10 分钟后）。
	now = at.Add(11 * time.Minute)
	n := app.checkSegmentChange(now)
	if n == nil {
		t.Fatal("跨过时段边界应触发提醒")
	}
	if n.kind != model.SegmentKindBreak {
		t.Errorf("应记录切换到的时段类型 break，实际 %q", n.kind)
	}
	if n.toName != "休息" {
		t.Errorf("应记录切换到的时段名，实际 %q", n.toName)
	}

	// 同一段内继续推进：不该重复提醒。
	now = at.Add(12 * time.Minute)
	if n := app.checkSegmentChange(now); n != nil {
		t.Error("同一段内不该重复提醒")
	}
}

// TestSegmentChangeSilentWhenAllDisabled 验证没开任何提醒时什么都不做。
func TestSegmentChangeSilentWhenAllDisabled(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	customSegmentedTimer(t, app, at)

	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)
	app.checkSegmentChange(now)
	now = at.Add(11 * time.Minute)
	if n := app.checkSegmentChange(now); n != nil {
		t.Error("全部关闭时不该触发提醒")
	}
	if app.notifyEnabled() {
		t.Error("默认应报告未启用任何提醒")
	}
}

// TestFinalSegmentAlsoNotifies 验证最后一个时段走完也会触发提醒。
//
// 用户实测指出：中间状态切换会触发，但最后一个状态结束没有——这不合直觉，
// "结束了"恰恰是最需要知道的一次。跨段检测靠 lastSeg 变化，走完最后一段时它
// 不再变化，所以必须在「计时自然结束」这条路上单独补一次。
func TestFinalSegmentAlsoNotifies(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.cfg.NotifyGlow = true
	app.cfg.NotifySound = true

	plan := model.Plan{Kind: model.TimerCountDown, Segments: []model.Segment{
		{Name: "倒计时", Kind: model.SegmentKindFocus, Dur: time.Minute},
	}}
	app.data.Floating = append(app.data.Floating, model.NewTodo("写论文", model.KindFloating, "2026-10-03", at))
	app.beginTimer(plan, app.data.Floating[0].ID)

	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)

	_, cmd := app.Update(timerDoneMsg{})
	if app.timer == nil || !app.timer.finished {
		t.Fatal("计时应标记为已完成")
	}
	if app.notifyState == nil {
		t.Fatal("最后一段结束也应触发提醒")
	}
	if !strings.Contains(app.notifyState.toName, "已结束") {
		t.Errorf("提示文案应说明已结束，实际 %q", app.notifyState.toName)
	}
	if cmd == nil {
		t.Error("应返回提示音等命令")
	}
}

// TestFinalSegmentSilentWhenDisabled 验证没开提醒时最后一段结束也不打扰。
func TestFinalSegmentSilentWhenDisabled(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	plan := model.Plan{Kind: model.TimerCountDown, Segments: []model.Segment{
		{Name: "倒计时", Kind: model.SegmentKindFocus, Dur: time.Minute},
	}}
	app.beginTimer(plan, "")
	app.Update(timerDoneMsg{})
	if app.notifyState != nil {
		t.Error("全部关闭时不该触发提醒")
	}
}

// TestNotifySoundIsOnOffBell 验证提示音只剩下"开/关"，开着就是系统响铃。
//
// 用户实测：代码合成的三种音频在本机都放不出声，系统响铃好听且可用。
// 所以这个开关不再有预设列表。
func TestNotifySoundIsOnOffBell(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	app, _, _ := newTestApp(t, at)
	if cmd := app.notifySoundCmd(); cmd != nil {
		t.Error("默认关闭时不该有响铃命令")
	}

	app.cfg.NotifySound = true
	cmd := app.notifySoundCmd()
	if cmd == nil {
		t.Fatal("开启后应有响铃命令")
	}
	if _, ok := cmd().(bellMsg); !ok {
		t.Error("提示音应为终端响铃（bellMsg）")
	}

	// 关掉就没了。
	app.cfg.NotifySound = false
	if cmd := app.notifySoundCmd(); cmd != nil {
		t.Error("关掉后不该有响铃命令")
	}
}

// TestNotifyText 验证推送文案按时段类型区分。
func TestNotifyText(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	customSegmentedTimer(t, app, at)

	title, body := app.notifyText(model.Segment{Name: "休息", Kind: model.SegmentKindBreak})
	if !strings.Contains(title, "休息") {
		t.Errorf("标题应含时段名，实际 %q", title)
	}
	if !strings.Contains(body, "休息时间到了") {
		t.Errorf("休息段的正文应说明休息，实际 %q", body)
	}

	title, body = app.notifyText(model.Segment{Name: "深度工作", Kind: model.SegmentKindFocus})
	if !strings.Contains(title, "深度工作") {
		t.Errorf("标题应含时段名，实际 %q", title)
	}
	if !strings.Contains(body, "深度工作") {
		t.Errorf("专注段的正文应含时段名，实际 %q", body)
	}
}

// TestNotifyGlowOnlyTouchesLogo 验证流光只影响 LOGO 那几行，不再铺满中间栏。
//
// 用户先后两次反馈：先是"太微弱"（因为铺不满），后来是"直接占据了整个中间栏
// 不过确实很丑"。最终定案：只做在 LOGO 上。
func TestNotifyGlowOnlyTouchesLogo(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)
	app.width, app.height = 120, 40
	seedForViews(t, app, s, at)
	customSegmentedTimer(t, app, at)
	app.cfg.NotifyGlow = true

	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)

	before := stripANSI(app.View())
	if !app.notifyActive() {
		// 没有提醒状态时不该有流光。
		if strings.Contains(before, "◈") {
			t.Error("没有提醒状态时不该出现流光记号")
		}
	}

	// 进入提醒状态后，LOGO 那几行应出现提醒记号，而中间栏底部不该被铺满字符。
	app.notifyState = &notifyState{started: now, kind: model.SegmentKindFocus, toName: "深度工作"}
	if !app.notifyActive() {
		t.Fatal("应处于提醒状态")
	}
	out := stripANSI(app.View())
	if !strings.Contains(out, "◈") {
		t.Error("LOGO 上应出现提醒记号")
	}
	// 底纹字符不该再出现（那是上一版"铺满整栏"的做法）。
	if strings.ContainsAny(out, "░▒▓") {
		t.Error("不该再铺满中间栏底纹，流光应只限于 LOGO")
	}
	// 左右面板的边框仍要完整。
	lines := strings.Split(out, "\n")
	if len(lines) > app.height {
		t.Errorf("行数 %d 超限", len(lines))
	}
	for i, l := range lines {
		if w := len([]rune(l)); w > app.width {
			t.Errorf("第 %d 行宽 %d 超限", i, w)
		}
	}
}

// TestNotifyGlowIsRemovedAfterDuration 验证流光播完就收掉。
func TestNotifyGlowIsRemovedAfterDuration(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	customSegmentedTimer(t, app, at)
	app.cfg.NotifyGlow = true

	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)
	app.notifyState = &notifyState{started: now, kind: model.SegmentKindFocus, toName: "深度工作"}

	if app.notifyDone(now) {
		t.Error("刚开始不该算播完")
	}
	if !app.notifyActive() {
		t.Error("刚开始应处于提醒状态")
	}
	now = at.Add(notifyDuration + time.Millisecond)
	if !app.notifyDone(now) {
		t.Error("超过时长应算播完")
	}
	if app.notifyActive() {
		t.Error("播完后不该还算在提醒")
	}

	// 动画帧推进后状态被清掉。
	app.Update(animMsg{})
	if app.notifyState != nil {
		t.Error("播完后应清掉提醒状态")
	}
	if strings.Contains(stripANSI(app.View()), "◈") {
		t.Error("播完后不该还有流光记号")
	}
}

// TestNotifyGlowDisabledMeansNoGlow 验证关掉流光后不动 LOGO。
func TestNotifyGlowDisabledMeansNoGlow(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	customSegmentedTimer(t, app, at)
	app.cfg.NotifyGlow = false
	app.notifyState = &notifyState{started: at, kind: model.SegmentKindFocus, toName: "深度工作"}

	if app.notifyActive() {
		t.Error("关掉流光时不该处于提醒状态")
	}
	if strings.Contains(stripANSI(app.View()), "◈") {
		t.Error("关掉流光时不该出现提醒记号")
	}
}

// TestNtfyHelpShowsAddressAndDisclaimer 验证说明页给出可复制的地址与风险提示。
//
// **不再有二维码**：用户实测自研二维码始终扫不出来（两个版本都试过），
// 改为直接给地址让用户复制。
func TestNtfyHelpShowsAddressAndDisclaimer(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.width, app.height = 120, 60
	app.view = ViewSettings
	app.cfg.NtfyTopic = "testtopicabcdefghijklmnopqrstuvwx"
	app.cfg.NtfyEnabled = true

	app.showNtfyHelp()
	if !app.ntfyHelp {
		t.Fatal("应打开说明页")
	}

	styled, plain := app.ntfyHelpContent()
	_ = styled
	joined := strings.Join(plain, "\n")
	for _, want := range []string{
		"ntfy", "testtopic", "订阅地址", "topic", "风险提示", "第三方", "不加密", "不承担",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("说明页应包含 %q，实际:\n%s", want, joined)
		}
	}
	// 不该再有二维码相关的说法与字符。
	if strings.Contains(joined, "扫码") || strings.ContainsAny(joined, "█▀▄") {
		t.Errorf("不该再出现二维码，实际:\n%s", joined)
	}

	// esc 返回设置页。
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.ntfyHelp {
		t.Error("esc 应关闭说明页")
	}
	if app.view != ViewSettings {
		t.Errorf("应回到设置页，实际 view=%v", app.view)
	}
}

// TestNtfyHelpRequiresEnabled 验证没开推送时说明页给出可操作提示。
func TestNtfyHelpRequiresEnabled(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.showNtfyHelp()
	if app.ntfyHelp {
		t.Error("没开推送不该打开说明页")
	}
	if !strings.Contains(app.toast, "打开手机推送") {
		t.Errorf("应提示先打开推送，实际 %q", app.toast)
	}
}

// TestSettingsShowsAddressAndDisclaimerTogether 验证设置页里地址与风险声明同层可见。
//
// 用户要求：看到地址就要看到声明，不能等用户自己点进下一级才发现。
func TestSettingsShowsAddressAndDisclaimerTogether(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.width, app.height = 110, 44
	app.view = ViewSettings
	app.cfg.NtfyTopic = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXPJBSWY3D"
	app.cfg.NtfyEnabled = true

	styled, plain := app.settingsLines()
	_ = styled
	joined := strings.Join(plain, "\n")

	for _, want := range []string{"ntfy.sh/JBSWY3D", "第三方", "不加密", "不承担"} {
		if !strings.Contains(joined, want) {
			t.Errorf("设置页应同时包含 %q（地址与声明同层），实际:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "扫码") {
		t.Errorf("不该再出现扫码说法，实际:\n%s", joined)
	}
}

// TestSettingsNotificationsAreOnOff 验证三项提醒在设置页都是开/关。
func TestSettingsNotificationsAreOnOff(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)

	app.cycleNotifyGlow()
	if !app.cfg.NotifyGlow {
		t.Error("应打开流光")
	}
	app.cycleNotifyGlow()
	if app.cfg.NotifyGlow {
		t.Error("应关掉流光")
	}

	app.cycleNotifySound()
	if !app.cfg.NotifySound {
		t.Error("应打开提示音")
	}
	if cmd := app.notifySoundCmd(); cmd == nil {
		t.Error("打开提示音后应能响")
	}

	app.toggleNtfy()
	if !app.cfg.NtfyEnabled {
		t.Fatal("应打开手机推送")
	}
	if len(app.cfg.NtfyTopic) < 40 {
		t.Errorf("应生成高熵频道，实际 %q", app.cfg.NtfyTopic)
	}
	app.toggleNtfy()
	if app.cfg.NtfyEnabled {
		t.Error("应关掉手机推送")
	}
	if app.cfg.NtfyTopic == "" {
		t.Error("关闭不该清掉频道名")
	}

	// 落盘：布尔开关用 omitempty，只断言当前为真的那些（关着不写进 JSON）。
	raw, err := readConfigFile(app)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"notify_sound", "ntfy_topic"} {
		if !strings.Contains(raw, want) {
			t.Errorf("配置里应留下 %q，实际:\n%s", want, raw)
		}
	}
	if !strings.Contains(raw, app.cfg.NtfyTopic) {
		t.Error("频道名应落盘")
	}
}

// TestRegenerateNtfyTopic 验证可以换频道（用户要求：不要求就不变，要求才换）。
func TestRegenerateNtfyTopic(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.width, app.height = 110, 44

	app.regenerateNtfyTopic()
	if app.cfg.NtfyTopic != "" {
		t.Error("没开启推送时不该生成频道")
	}
	if !strings.Contains(app.toast, "先打开") {
		t.Errorf("应提示先打开推送，实际 %q", app.toast)
	}

	app.cfg.NtfyTopic = "oldtopicabcdefghijklmnopqrstuvwx"
	app.cfg.NtfyEnabled = true
	app.regenerateNtfyTopic()

	if app.cfg.NtfyTopic == "oldtopicabcdefghijklmnopqrstuvwx" {
		t.Error("应换成新频道")
	}
	if len(app.cfg.NtfyTopic) < 40 {
		t.Errorf("新频道应有足够熵，实际 %q", app.cfg.NtfyTopic)
	}
	if !strings.Contains(app.toast, "重新订阅") {
		t.Errorf("应提醒手机需要重新订阅，实际 %q", app.toast)
	}
	if !app.ntfyHelp {
		t.Error("换频道后应直接打开说明页看新地址")
	}
}

// TestNtfyPushActuallySends 验证推送真的发了出去（用一个假的 ntfy 服务接住）。
func TestNtfyPushActuallySends(t *testing.T) {
	type captured struct {
		path, title, body, method string
	}
	var (
		mu  sync.Mutex
		got []captured
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, captured{r.URL.Path, r.Header.Get("X-Title"), string(body), r.Method})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	customSegmentedTimer(t, app, at)
	app.cfg.NtfyServer = srv.URL
	app.cfg.NtfyTopic = "testtopicabcdefghijklmnop"
	app.cfg.NtfyEnabled = true

	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)
	app.checkSegmentChange(now)
	now = at.Add(11 * time.Minute)
	n := app.checkSegmentChange(now)
	if n == nil || n.pushCmd == nil {
		t.Fatal("启用推送后跨段应产生推送命令")
	}
	if msg := n.pushCmd(); msg != nil {
		if m, ok := msg.(ntfyPushedMsg); ok && m.err != nil {
			t.Fatalf("推送失败: %v", m.err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("假服务应收到 1 次推送，实际 %d 次", len(got))
	}
	if got[0].method != http.MethodPost {
		t.Errorf("应用 POST 发布，实际 %s", got[0].method)
	}
	if got[0].path != "/testtopicabcdefghijklmnop" {
		t.Errorf("路径应为 /<topic>，实际 %q", got[0].path)
	}
	if !strings.Contains(got[0].title, "休息") {
		t.Errorf("X-Title 应含时段名（中文也要能带），实际 %q", got[0].title)
	}
}

// TestNtfyPushFailureIsSilent 验证推送失败只报错、不重试、不影响计时。
func TestNtfyPushFailureIsSilent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "topic is too short", http.StatusBadRequest)
	}))
	defer srv.Close()

	err := pushNtfy(srv.URL, "short", "标题", "正文")
	if err == nil {
		t.Fatal("被拒绝时应返回错误")
	}
	if !strings.Contains(err.Error(), "topic is too short") {
		t.Errorf("错误信息应带上服务端原因，实际 %v", err)
	}

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	if err := pushNtfy(deadURL, "topic", "标题", "正文"); err == nil {
		t.Error("地址不可达时应返回错误")
	}

	if err := pushNtfy("", "topic", "t", "b"); err == nil {
		t.Error("服务地址为空应返回错误")
	}
	if err := pushNtfy("https://ntfy.sh", "  ", "t", "b"); err == nil {
		t.Error("频道名为空应返回错误")
	}
}

// TestNtfyDisabledMeansNoPush 验证关掉开关就不推送（需求：可以填了但是关掉）。
func TestNtfyDisabledMeansNoPush(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	customSegmentedTimer(t, app, at)

	app.cfg.NotifyGlow = true // 只开流光
	app.cfg.NtfyTopic = "abcdefghijklmnopqrstuvwx"
	app.cfg.NtfyEnabled = false

	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)
	app.checkSegmentChange(now)
	now = at.Add(11 * time.Minute)
	n := app.checkSegmentChange(now)
	if n == nil {
		t.Fatal("开了流光就该有提醒状态")
	}
	if n.pushCmd != nil {
		t.Error("ntfy 关掉时不该产生推送命令")
	}
}
