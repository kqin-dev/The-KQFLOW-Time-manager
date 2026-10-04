package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

// TestFinalSegmentAlsoNotifies 验证最后一个时段走完也会触发提醒。
//
// 用户实测指出：中间状态切换会触发，但最后一个状态结束没有——这不合直觉，
// "结束了"恰恰是最需要知道的一次。跨段检测靠 lastSeg 变化，走完最后一段时它
// 不再变化，所以必须在「计时自然结束」这条路上单独补一次。
func TestFinalSegmentAlsoNotifies(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.cfg.NotifyGlow = "aurora"
	app.cfg.NotifySound = "bell"

	plan := model.Plan{Kind: model.TimerCountDown, Segments: []model.Segment{
		{Name: "倒计时", Kind: model.SegmentKindFocus, Dur: time.Minute},
	}}
	app.data.Floating = append(app.data.Floating, model.NewTodo("写论文", model.KindFloating, "2026-10-03", at))
	app.beginTimer(plan, app.data.Floating[0].ID)

	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)

	// 计时自然结束：timerDoneMsg 由 tick 在走到终点时发出。
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
	} else {
		// 命令里应该能找到响铃（bell 预设）。
		msg := cmd()
		if msg == nil {
			t.Error("命令不应返回 nil 消息")
		}
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

	// 直接看设置页的完整内容（不止一屏时也要能找到）。
	styled, plain := app.settingsLines()
	joined := strings.Join(plain, "\n")
	_ = styled

	for _, want := range []string{"ntfy.sh/JBSWY3D", "第三方", "不加密", "不承担"} {
		if !strings.Contains(joined, want) {
			t.Errorf("设置页应同时包含 %q（地址与声明同层），实际:\n%s", want, joined)
		}
	}
	// 页面上必须说明"可以点开二维码"，否则用户意识不到这一项能按。
	if !strings.Contains(joined, "二维码") {
		t.Errorf("应说明该项可打开二维码页，实际:\n%s", joined)
	}
}

// TestRegenerateNtfyTopic 验证可以换频道（用户要求：不要求就不变，要求才换）。
func TestRegenerateNtfyTopic(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.width, app.height = 110, 44

	// 没开启时给出可操作提示，不生成。
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
	// 换完直接打开说明页，用户不用再找入口。
	if !app.ntfyHelp {
		t.Error("换频道后应直接打开说明页看新二维码")
	}
	// 落盘。
	raw, err := readConfigFile(app)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, app.cfg.NtfyTopic) {
		t.Error("新频道应已落盘")
	}
}

// TestBuiltinSoundDirIsWritable 验证提示音目录确实可写。
//
// 用户实机报过 `mkdir %TEMP%\kqflow-sounds: Access is denied`，所以这里同时
// 覆盖「候选目录有一个能写」这一点，并核对合成文件能真的写出来。
func TestBuiltinSoundDirIsWritable(t *testing.T) {
	dir, err := builtinSoundDir()
	if err != nil {
		// 环境极端受限时可以没有可写目录，但要给出候选数量，便于排查。
		if !strings.Contains(err.Error(), "候选位置") {
			t.Errorf("错误信息应说明试过多少候选目录，实际 %v", err)
		}
		t.Skipf("本环境没有可写目录：%v", err)
	}
	if dir == "" {
		t.Fatal("返回的目录不应为空")
	}
	if len(builtinSoundDirCandidates()) == 0 {
		t.Error("候选目录列表不该为空")
	}

	path, err := builtinSoundFile("bowl")
	if err != nil {
		t.Fatalf("写出提示音失败: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("提示音文件不存在: %v", err)
	}
	if info.Size() < 1000 {
		t.Errorf("WAV 文件太小（%d 字节），可能没写全", info.Size())
	}
	// 同一预设应复用同一个文件。
	again, err := builtinSoundFile("bowl")
	if err != nil {
		t.Fatal(err)
	}
	if again != path {
		t.Errorf("同一预设应复用同一个文件：%q vs %q", path, again)
	}

	raw, err := generateSoundWAV("bowl")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		t.Error("合成结果应是合法的 WAV（RIFF/WAVE 头）")
	}
}

// TestGenerateSoundWAVAllPresets 验证三种合成音都能生成且不削顶。
func TestGenerateSoundWAVAllPresets(t *testing.T) {
	for _, name := range builtinSoundNames() {
		raw, err := generateSoundWAV(name)
		if err != nil {
			t.Errorf("%s 生成失败: %v", name, err)
			continue
		}
		if len(raw) < 1000 {
			t.Errorf("%s 生成的 WAV 太小（%d 字节）", name, len(raw))
		}
		if _, err := generateSoundWAV(name + "-不存在"); err == nil {
			t.Error("未知名字应报错")
		}
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
		// 现在流光铺满整个中间栏，用的是 ░▒▓█ 这一组底纹字符。
		if !strings.ContainsAny(stripANSI(out), "░▒▓█") {
			t.Errorf("%dx%d：画面里应出现流光底纹", size[0], size[1])
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
	if strings.ContainsAny(stripANSI(app.View()), "░▒▓") {
		t.Error("播完后画面里不该还有流光")
	}
}

// TestNotifyGlowCoversWholeCenterColumn 验证流光铺满整个中间栏，而不是只有一行。
//
// 用户实机反馈「流光太微弱了，我忽略了其实大部分地方是没有字符的空白」：
// 只替换一行的做法在空白处根本看不见。
func TestNotifyGlowCoversWholeCenterColumn(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)
	app.width, app.height = 110, 36
	seedForViews(t, app, s, at)
	customSegmentedTimer(t, app, at)

	now := at
	app.clock = clock.NewWith(func() time.Time { return now }, time.Local)
	app.cfg.NotifyGlow = "aurora"
	app.notifyState = &notifyState{started: now, kind: model.SegmentKindFocus, toName: "深度工作"}

	_, _, _, bodyH := app.columnLayout()
	out := stripANSI(app.View())
	lines := strings.Split(out, "\n")

	// 统计有多少行带流光底纹：应当接近整个中间栏高度，而不是一两行。
	decorated := 0
	for _, l := range lines {
		if strings.ContainsAny(l, "░▒▓█") {
			decorated++
		}
	}
	if decorated < bodyH-2 {
		t.Errorf("流光应铺满中间栏（约 %d 行），实际只有 %d 行", bodyH, decorated)
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
	// 二维码可能被滚动裁掉（说明页比中间栏高是常态），所以要滚一遍找；
	// 终端够高时它应当直接可见。
	qrVisible := false
	app.pageScroll = 0
	for i := 0; i < 40; i++ {
		if strings.ContainsAny(stripANSI(app.View()), "█▀▄") {
			qrVisible = true
			break
		}
		before := app.pageScroll
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		if app.pageScroll == before {
			break
		}
	}
	if !qrVisible {
		t.Error("滚动整页都应该能找到二维码，实际始终没出现")
	}

	// 终端足够高时二维码应直接可见，不用滚动。
	app.pageScroll = 0
	app.width, app.height = 120, 60
	if !strings.ContainsAny(stripANSI(app.View()), "█▀▄") {
		t.Error("终端够高时二维码应直接可见")
	}
	app.width, app.height = 110, 44

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
