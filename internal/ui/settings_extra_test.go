package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/config"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// TestSettingsDisclaimerIsComplete 验证设置页里的风险声明一个字都不少。
//
// 用户报过"风险提示显示不全"：长中文一旦超出内宽就会被终端自己折行，折行的
// 后半段看起来就像丢了内容。所以这里把设置页所有行拼起来，去掉空白后必须与
// 原始声明逐字一致，而且每一行都不能超过面板内宽。
func TestSettingsDisclaimerIsComplete(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	for _, size := range [][2]int{{140, 48}, {120, 44}, {110, 40}, {100, 36}} {
		app, _, _ := newTestApp(t, at)
		app.width, app.height = size[0], size[1]
		app.view = ViewSettings
		app.cfg.NtfyTopic = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXPJBSWY3D"
		app.cfg.NtfyEnabled = true

		styled, plain := app.settingsLines()
		inner := app.contentWidth()

		// 每行都不能超宽（超了就会被终端折行，看起来像丢字）。
		for i, l := range plain {
			if w := lipgloss.Width(l); w > inner {
				t.Errorf("%dx%d 第 %d 行宽 %d 超过内宽 %d：%q",
					size[0], size[1], i, w, inner, l)
			}
		}
		_ = styled

		// 声明必须完整：去掉空白后逐字比对。
		joined := strings.Join(plain, "")
		squash := func(s string) string {
			return strings.Join(strings.Fields(s), "")
		}
		if !strings.Contains(squash(joined), squash(config.NotifyDisclaimer)) {
			t.Errorf("%dx%d：设置页里的声明不完整\n期望包含：%s\n实际内容：%s",
				size[0], size[1], squash(config.NotifyDisclaimer), squash(joined))
		}
	}
}

// TestDisclaimerCompleteInBothViews 把**两个页面都渲染一遍**，逐个窗口尺寸
// 检查声明是否逐字完整、每行是否都不超过面板内宽。
//
// 用户连续两次报「风险提示显示不全」，说明只测一个页面/一种尺寸不够：
// 设置页直接把声明铺在列表里，说明页在 pageContent 里滚动，两条路径都要验。
// 判据用「去掉所有空白后逐字包含」，这样任何丢字、错位都会被抓到。
func TestDisclaimerCompleteInBothViews(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	squash := func(s string) string { return strings.Join(strings.Fields(s), "") }
	want := squash(config.NotifyDisclaimer)

	sizes := [][2]int{{60, 20}, {80, 24}, {90, 30}, {100, 36}, {110, 40}, {120, 44}, {140, 48}, {160, 50}}
	for _, size := range sizes {
		app, _, _ := newTestApp(t, at)
		app.width, app.height = size[0], size[1]
		app.cfg.NtfyTopic = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXPJBSWY3D"
		app.cfg.NtfyEnabled = true

		// --- 设置页：逐行检查（pageContent 的 plain 即渲染出来的行）---
		app.view = ViewSettings
		styled, plain := app.settingsLines()
		_ = styled
		inner := app.contentWidth()
		for i, l := range plain {
			if w := lipgloss.Width(l); w > inner {
				t.Errorf("%dx%d 设置页第 %d 行宽 %d 超内宽 %d：%q",
					size[0], size[1], i, w, inner, l)
			}
		}
		if got := squash(strings.Join(plain, "")); !strings.Contains(got, want) {
			t.Errorf("%dx%d 设置页的声明不完整\n期望：%s\n实际：%s", size[0], size[1], want, got)
		}

		// --- 说明页：它走 pageContent，会被滚动裁剪，所以要滚一遍找 ---
		app.view = ViewSettings
		app.ntfyHelp = true
		app.pageScroll = 0
		var all []string
		for i := 0; i < 60; i++ {
			hs, hp := app.ntfyHelpContent()
			_ = hs
			total := len(hp)
			_, _, _, bodyH := app.columnLayout()
			avail := bodyH - a4(app)
			if avail < 1 {
				avail = 1
			}
			start := app.pageScroll
			end := start + avail
			if end > total {
				end = total
			}
			if start < total {
				all = append(all, hp[start:end]...)
			}
			// 每一行都要在可见宽度内。
			for j := 0; j < total; j++ {
				if w := lipgloss.Width(hp[j]); w > app.contentWidth() {
					t.Errorf("%dx%d 说明页第 %d 行宽 %d 超内宽 %d：%q",
						size[0], size[1], j, w, app.contentWidth(), hp[j])
				}
			}
			before := app.pageScroll
			app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
			if app.pageScroll == before {
				break
			}
		}
		if got := squash(strings.Join(all, "")); !strings.Contains(got, want) {
			t.Errorf("%dx%d 说明页的声明不完整\n期望：%s\n实际：%s", size[0], size[1], want, got)
		}
		app.ntfyHelp = false
	}
}

// a4 返回面板边框占用的垂直尺寸（与 pageContent 的算法保持一致）。
func a4(a *App) int { return a.st.Panel.GetVerticalFrameSize() }

// TestBellRingsOnAnyView 验证提示音在**任何页面**都会响。
//
// 用户实测报过两件事：「测试时流光和频道都正常，但没有播放提示音」（其实实际
// 使用时能响）以及「一直呆在设置页面貌似就不会播放提示音」。根因是响铃原来只在
// 看板的渲染路径里输出 `\a`，停在设置页时那段根本不执行。已改成在 Update 的
// 动画帧里统一处理。
func TestBellRingsOnAnyView(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	for _, view := range []View{ViewDashboard, ViewSettings, ViewHistory, ViewHelp} {
		app, _, _ := newTestApp(t, at)
		app.view = view
		app.cfg.NotifySound = true

		// 走计时器状态这条路：设铃标记，动画帧应当消费它并输出响铃。
		plan := model.Plan{Kind: model.TimerCountUp, Segments: []model.Segment{
			{Name: "自由专注", Kind: model.SegmentKindFocus},
		}}
		app.beginTimer(plan, "")
		app.timer.bell = true

		_, cmd := app.Update(animMsg{})
		if app.timer.consumeBell() {
			t.Errorf("view=%v：铃标记应已被消费（否则永远不会响）", view)
		}
		if cmd == nil {
			t.Errorf("view=%v：应返回命令", view)
		}
	}

	// 没有计时器时（例如设置页里点测试）也要能响。
	app, _, _ := newTestApp(t, at)
	app.view = ViewSettings
	app.cfg.NotifySound = true
	plan := app.notifySoundCmd()
	if plan == nil {
		t.Fatal("没有计时器时也应能响")
	}
	if _, ok := plan().(bellOnceMsg); !ok {
		t.Error("应产生 bellOnceMsg")
	}
	// bellOnceMsg 只置标记，真正的响铃在下一个动画帧统一发出——
	// 这样"响铃"这件事与计时器状态彻底解耦，也不依赖任何页面的渲染。
	app.Update(bellOnceMsg{})
	if !app.bellPending {
		t.Fatal("bellOnceMsg 应置上待响标记")
	}
	_, cmd2 := app.Update(animMsg{})
	if cmd2 == nil {
		t.Error("下一个动画帧应产生响铃命令")
	}
	if app.bellPending {
		t.Error("响过之后标记应被清掉（否则会一直响）")
	}
}

// TestAutoArchiveOnFinish 验证「专注结束自动归档」这个选项的两条路径。
//
// 默认关：走完后停住等确认（时长不会被自动定成"完成"）。
// 打开后：走完即刻结束并归档。
func TestAutoArchiveOnFinish(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	newTimerApp := func(t *testing.T) (*App, *config.Config) {
		t.Helper()
		app, _, cfg := newTestApp(t, at)
		allowCarrySkip(t, app)
		now := at
		app.clock = clock.NewWith(func() time.Time { return now }, time.Local)
		plan := model.Plan{Kind: model.TimerCountDown, Segments: []model.Segment{
			{Name: "倒计时", Kind: model.SegmentKindFocus, Dur: time.Minute},
		}}
		app.data.Floating = append(app.data.Floating,
			model.NewTodo("写论文", model.KindFloating, "2026-10-03", at))
		app.beginTimer(plan, app.data.Floating[0].ID)
		if app.timer == nil {
			t.Fatal("计时应已开始")
		}
		return app, cfg
	}

	t.Run("默认关：停住等确认", func(t *testing.T) {
		app, cfg := newTimerApp(t)
		if cfg.AutoArchiveOnFinish {
			t.Fatal("默认应为关")
		}

		app.Update(timerDoneMsg{})
		if app.timer == nil {
			t.Error("默认不该自动结束计时")
		}
		if !app.timer.finished {
			t.Error("应标记为已完成")
		}
		if len(app.data.Archive.Sessions) != 0 {
			t.Errorf("确认前不该归档，实际 %d 条", len(app.data.Archive.Sessions))
		}
		if !strings.Contains(app.toast, "确认结束") {
			t.Errorf("应提示怎么结束，实际 %q", app.toast)
		}
		// 之后按 p 菜单确认，仍能正常归档。
		stopTimerViaMenu(t, app)
		saved, err := app.store.Day("2026-10-03")
		if err != nil {
			t.Fatal(err)
		}
		if len(saved.Archive.Sessions) != 1 {
			t.Errorf("确认后应归档 1 条，实际 %d", len(saved.Archive.Sessions))
		}
	})

	t.Run("打开：走完即刻归档", func(t *testing.T) {
		app, cfg := newTimerApp(t)
		cfg.AutoArchiveOnFinish = true

		_, cmd := app.Update(timerDoneMsg{})
		if app.timer != nil {
			t.Error("应已自动结束计时")
		}
		if cmd == nil {
			t.Error("应返回保存命令")
		}
		saved, err := app.store.Day("2026-10-03")
		if err != nil {
			t.Fatal(err)
		}
		if len(saved.Archive.Sessions) != 1 {
			t.Fatalf("应自动归档 1 条，实际 %d", len(saved.Archive.Sessions))
		}
		if !saved.Archive.Sessions[0].Completed {
			t.Error("自动归档的记录应算完成（不是中断）")
		}
	})

	t.Run("开关可切换并落盘", func(t *testing.T) {
		app, cfg := newTimerApp(t)
		app.toggleAutoArchive()
		if !cfg.AutoArchiveOnFinish {
			t.Error("应打开")
		}
		raw, err := readConfigFile(app)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(raw, "auto_archive_on_finish") {
			t.Errorf("应落盘，实际:\n%s", raw)
		}
		app.toggleAutoArchive()
		if cfg.AutoArchiveOnFinish {
			t.Error("应关掉")
		}
	})
}

// TestDemoNotify 验证「测试」功能：三项提醒各自独立触发，并如实报告跑了哪几项。
func TestDemoNotify(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	t.Run("全都没开时给出可操作提示", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		app.demoNotify()
		if app.notifyState != nil {
			t.Error("没开任何提醒时不该产生提醒状态")
		}
		if app.hearingCmd != nil {
			t.Error("没开任何提醒时不该产生命令")
		}
		if !strings.Contains(app.toast, "先在设置里打开") {
			t.Errorf("应提示先去打开，实际 %q", app.toast)
		}
	})

	t.Run("只开流光：报告另外两项未开", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		app.cfg.NotifyGlow = true
		app.view = ViewSettings
		app.demoNotify()
		if app.notifyState == nil {
			t.Fatal("应产生提醒状态（流光）")
		}
		if app.notifyState.toName != "测试提醒" {
			t.Errorf("提示文案应为测试提醒，实际 %q", app.notifyState.toName)
		}
		if !strings.Contains(app.toast, "流光") {
			t.Errorf("提示里应说明流光跑了，实际 %q", app.toast)
		}
		if !strings.Contains(app.toast, "提示音（未开）") {
			t.Errorf("应说明提示音未开，实际 %q", app.toast)
		}
		if !strings.Contains(app.toast, "手机推送（未开）") {
			t.Errorf("应说明手机推送未开，实际 %q", app.toast)
		}
		// 关键：必须切回看板，否则流光（只做在 LOGO 上）根本看不见。
		if app.view != ViewDashboard {
			t.Errorf("测试后应切回看板，实际 view=%v", app.view)
		}
		if app.ntfyHelp {
			t.Error("测试后不该还停在说明页")
		}
		// 切回看板后 LOGO 上应真的出现流光记号。
		if !strings.Contains(stripANSI(app.View()), "◈") {
			t.Error("切回看板后应能看到 LOGO 流光")
		}
	})

	t.Run("全开：三项都跑并附带命令", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		app.cfg.NotifyGlow = true
		app.cfg.NotifySound = true
		app.cfg.NtfyTopic = "testtopicabcdefghijklmnopqrst"
		app.cfg.NtfyEnabled = true

		app.demoNotify()
		toast := app.toast
		for _, want := range []string{"流光", "提示音", "手机推送"} {
			if !strings.Contains(toast, want) {
				t.Errorf("提示里应包含 %q，实际 %q", want, toast)
			}
		}
		if strings.Contains(toast, "未开") {
			t.Errorf("全开时不该说未开，实际 %q", toast)
		}
		if app.hearingCmd == nil {
			t.Error("应产生命令（动画 + 响铃 + 推送）")
		}
		if app.notifyState == nil || app.notifyState.pushCmd == nil {
			t.Error("应产生推送命令")
		}
	})

	t.Run("通过设置页的 enter 也能触发", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		app.cfg.NotifyGlow = true
		app.view = ViewSettings

		// 找到「测试」那一项并把光标移过去。
		idx := settingIndex(t, "提醒 · 测试（流光 + 提示音 + 推送）")
		app.settingsCursor = idx
		_, cmd := app.activateSetting()
		if app.notifyState == nil {
			t.Error("设置页里按 enter 也应触发测试")
		}
		if cmd == nil {
			t.Error("应回传命令")
		}
	})
}

// TestAutoArchiveKeepsNotification 验证自动归档时该发的提醒照发。
//
// 两个功能不能互相吃掉：走完那一刻既要提醒，又要归档。
func TestAutoArchiveKeepsNotification(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, cfg := newTestApp(t, at)
	allowCarrySkip(t, app)
	app.cfg.NotifyGlow = true
	app.cfg.NotifySound = true
	cfg.AutoArchiveOnFinish = true

	plan := model.Plan{Kind: model.TimerCountDown, Segments: []model.Segment{
		{Name: "倒计时", Kind: model.SegmentKindFocus, Dur: time.Minute},
	}}
	app.beginTimer(plan, "")

	_, cmd := app.Update(timerDoneMsg{})
	if app.notifyState == nil {
		t.Fatal("自动归档时也应触发提醒")
	}
	if app.timer != nil {
		t.Error("应已自动归档")
	}
	if cmd == nil {
		t.Error("应返回命令（提醒 + 保存）")
	}
}

// 让本文件对 tea 的引用不至于未使用；同时确保 keyMsg 类型可用。
var _ = tea.KeyMsg{}
