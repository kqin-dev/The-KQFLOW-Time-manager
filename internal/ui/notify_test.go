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

// TestSegmentChangeTriggersNotification 验证跨过时段边界时触发提醒，
// 并且只触发一次。
func TestSegmentChangeTriggersNotification(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	customSegmentedTimer(t, app, at)
	app.cfg.NotifyGlow = "aurora"

	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)

	// 第一次推进：只是记录当前段，不该提醒（否则刚开始就响）。
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

// TestNotifySoundCmdMapping 验证提示音预设到命令的映射。
func TestNotifySoundCmdMapping(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	cases := []struct {
		preset  string
		wantNil bool
		wantMsg string
	}{
		{"", true, ""},
		{"none", true, ""},
		{"bell", false, "bellMsg"},
		{"bowl", false, "soundPlayedMsg"},
	}
	for _, c := range cases {
		app, _, _ := newTestApp(t, at)
		app.cfg.NotifySound = c.preset
		cmd := app.notifySoundCmd()
		if c.wantNil {
			if cmd != nil {
				t.Errorf("预设 %q 应不产生命令", c.preset)
			}
			continue
		}
		if cmd == nil {
			t.Errorf("预设 %q 应产生命令", c.preset)
			continue
		}
		msg := cmd()
		if got := typeName(msg); got != c.wantMsg {
			t.Errorf("预设 %q 应产生 %s，实际 %s", c.preset, c.wantMsg, got)
		}
	}
}

// typeName 返回消息类型的短名，便于断言。
func typeName(msg interface{}) string {
	switch msg.(type) {
	case bellMsg:
		return "bellMsg"
	case soundPlayedMsg:
		return "soundPlayedMsg"
	case ntfyPushedMsg:
		return "ntfyPushedMsg"
	default:
		return "unknown"
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

// TestNtfyPushActuallySends 验证推送真的发了出去（用一个假的 ntfy 服务接住）。
func TestNtfyPushActuallySends(t *testing.T) {
	type captured struct {
		path   string
		title  string
		body   string
		method string
	}
	var (
		mu  sync.Mutex
		got []captured
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, captured{
			path:   r.URL.Path,
			title:  r.Header.Get("X-Title"),
			body:   string(body),
			method: r.Method,
		})
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

	// 执行推送命令。
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
	if !strings.Contains(got[0].body, "休息") {
		t.Errorf("正文应说明进入休息，实际 %q", got[0].body)
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

	// 网络不可达（端口关掉了）。
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	if err := pushNtfy(deadURL, "topic", "标题", "正文"); err == nil {
		t.Error("地址不可达时应返回错误")
	}

	// 参数不全。
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

	app.cfg.NotifyGlow = "aurora" // 只开流光
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

// TestNotifyGlowRendersEveryPreset 验证每种流光预设都能渲染出内容，
// 且宽度不超过中间栏内容宽度。
func TestNotifyGlowRendersEveryPreset(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.width, app.height = 100, 30
	customSegmentedTimer(t, app, at)

	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)
	inner := app.contentWidth()

	app.notifyState = &notifyState{started: now, kind: model.SegmentKindBreak, toName: "休息"}
	for _, preset := range notifyGlowPresets {
		if preset.Key == "none" {
			continue
		}
		app.cfg.NotifyGlow = preset.Key
		for _, progress := range []float64{0, 0.25, 0.5, 0.75, 1} {
			line := app.notifyFrame(progress, inner)
			if strings.TrimSpace(stripANSI(line)) == "" {
				t.Errorf("预设 %s 在进度 %.2f 时渲染为空", preset.Key, progress)
			}
			if w := displayWidth(line); w > inner {
				t.Errorf("预设 %s 在进度 %.2f 时宽 %d 超过内容宽 %d", preset.Key, progress, w, inner)
			}
		}
	}
}

// displayWidth 按显示宽度计算，忽略 ANSI 转义。
func displayWidth(s string) int {
	return len([]rune(stripANSI(s)))
}

// TestNotifyGlowOverlayKeepsLayout 验证流光叠加后三栏宽度与总行数都不变。
//
// 这是本项目踩过的坑：浮层一旦改变行宽，三栏边框就会被挤歪。
func TestNotifyGlowOverlayKeepsLayout(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	for _, size := range [][2]int{{100, 30}, {120, 36}, {80, 24}} {
		app, s, _ := newTestApp(t, at)
		app.width, app.height = size[0], size[1]
		seedForViews(t, app, s, at)
		customSegmentedTimer(t, app, at)
		app.cfg.NotifyGlow = "aurora"

		now := at
		app.clock = clock.NewWith(func() time.Time { return now }, time.Local)
		app.notifyState = &notifyState{started: now, kind: model.SegmentKindFocus, toName: "深度工作"}

		out := app.View()
		lines := strings.Split(out, "\n")
		if len(lines) > size[1] {
			t.Errorf("%dx%d：行数 %d 超限", size[0], size[1], len(lines))
		}
		for i, l := range lines {
			if w := len([]rune(stripANSI(l))); w > size[0] {
				t.Errorf("%dx%d 第 %d 行宽 %d 超限", size[0], size[1], i, w)
			}
		}
		// 流光应该真的出现在画面里（否则这条测试没验到东西）。
		if !strings.Contains(stripANSI(out), "即将开始") {
			t.Errorf("%dx%d：画面里应出现流光提示", size[0], size[1])
		}
	}
}

// TestNotifyGlowIsRemovedAfterDuration 验证流光播完就收掉。
func TestNotifyGlowIsRemovedAfterDuration(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	customSegmentedTimer(t, app, at)
	app.cfg.NotifyGlow = "aurora"

	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)
	app.notifyState = &notifyState{started: now, kind: model.SegmentKindFocus, toName: "深度工作"}

	if app.notifyDone(now) {
		t.Error("刚开始不该算播完")
	}
	now = at.Add(notifyDuration / 2)
	if app.notifyDone(now) {
		t.Error("播到一半不该算播完")
	}
	now = at.Add(notifyDuration + time.Millisecond)
	if !app.notifyDone(now) {
		t.Error("超过时长应算播完")
	}

	// 动画帧推进后状态被清掉，画面里不再有流光。
	app.Update(animMsg{})
	if app.notifyState != nil {
		t.Error("播完后应清掉提醒状态")
	}
	if strings.Contains(stripANSI(app.View()), "即将开始") {
		t.Error("播完后画面里不该还有流光")
	}
}

// TestNtfyHelpView 验证说明页包含订阅地址、风险提示与免责声明。
func TestNtfyHelpView(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.width, app.height = 110, 44
	app.view = ViewSettings
	app.cfg.NtfyTopic = "testtopicabcdefghijklmnopqrstuvwx"
	app.cfg.NtfyEnabled = true

	app.showNtfyHelp()
	if !app.ntfyHelp {
		t.Fatal("应打开说明页")
	}
	out := stripANSI(app.View())
	for _, want := range []string{"ntfy", "Subscribe", "testtopic", "风险提示", "第三方", "不加密"} {
		if !strings.Contains(out, want) {
			t.Errorf("说明页应包含 %q，实际:\n%s", want, out)
		}
	}
	// 二维码应该画出来了（有半块字符）。
	if !strings.ContainsAny(out, "█▀▄") {
		t.Error("说明页应画出二维码")
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

// TestSettingsNotificationsPersist 验证设置页里能轮换提醒预设并落盘。
func TestSettingsNotificationsPersist(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)

	// 预设列表第一项是「关闭」，先轮换到具体预设再比较。
	for i := 0; i < len(notifyGlowPresets); i++ {
		app.cycleNotifyGlow()
		if app.cfg.NotifyGlow != "" && app.cfg.NotifyGlow != "none" {
			break
		}
	}
	if app.cfg.NotifyGlow == "" || app.cfg.NotifyGlow == "none" {
		t.Fatalf("轮换后应是具体预设，实际 %q", app.cfg.NotifyGlow)
	}
	first := app.cfg.NotifyGlow
	app.cycleNotifyGlow()
	if app.cfg.NotifyGlow == first {
		t.Error("再轮换一次应该换到下一个预设")
	}

	app.cycleNotifySound()
	if app.cfg.NotifySound == "" {
		t.Fatal("提示音应被设置")
	}
	// 配置文件里应该留下了记录（说明重新载入后还在）。
	raw, err := readConfigFile(app)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "notify_sound") {
		t.Errorf("提示音设置应落盘，实际:\n%s", raw)
	}
}

// TestToggleNtfyGeneratesTopic 验证打开推送会生成高熵频道名并落盘。
func TestToggleNtfyGeneratesTopic(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)

	app.toggleNtfy()
	if !app.cfg.NtfyEnabled {
		t.Fatal("应已开启")
	}
	if app.cfg.NtfyTopic == "" {
		t.Fatal("应生成频道名")
	}
	if len(app.cfg.NtfyTopic) < 40 {
		t.Errorf("频道名应该有足够熵（长度 >= 40），实际 %q", app.cfg.NtfyTopic)
	}
	if !app.cfg.NtfyReady() {
		t.Error("开启后应就绪")
	}

	// 再关一次。
	app.toggleNtfy()
	if app.cfg.NtfyEnabled {
		t.Error("应已关闭")
	}
	// 关掉后频道名要留着：下次打开不该换频道（否则手机订阅失效）。
	if app.cfg.NtfyTopic == "" {
		t.Error("关闭不该清掉频道名")
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
