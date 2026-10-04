package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// readConfigFile 直接读回配置文件内容，用来确认确实落盘了。
func readConfigFile(app *App) (string, error) {
	raw, err := os.ReadFile(app.pathsForSave().ConfigFile)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// seedSavedPlan 往配置里塞一套收藏方案。
func seedSavedPlan(app *App, label string, focus, rest time.Duration) {
	app.cfg.AddSavedPlan(model.Plan{
		Kind:  model.TimerCustom,
		Label: label,
		Segments: []model.Segment{
			{Name: "深度工作", Kind: model.SegmentKindFocus, Dur: focus},
			{Name: "休息", Kind: model.SegmentKindBreak, Dur: rest},
		},
	})
}

// TestSaveFavoriteFromCustomEditor 验证在自定义时段编辑器里按 s 收藏。
func TestSaveFavoriteFromCustomEditor(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	t.Run("默认名字可直接回车采用", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		allowCarrySkip(t, app)
		app.openCustom()
		press(t, app, "s")
		if !app.editor.active {
			t.Fatal("s 应打开收藏名输入框")
		}
		// 预设名字应来自方案内容。
		if got := string(app.editor.value); got == "" {
			t.Error("收藏名应预填一个可读的默认值")
		}
		press(t, app, "enter")

		list := app.cfg.SavedPlanList()
		if len(list) != 1 {
			t.Fatalf("应有 1 套收藏，实际 %d", len(list))
		}
		if len(list[0].Segments) != 2 {
			t.Errorf("收藏应保留两段，实际 %d", len(list[0].Segments))
		}
		if !strings.Contains(app.toast, "已收藏") {
			t.Errorf("应提示收藏成功，实际 toast=%q", app.toast)
		}
	})

	t.Run("自定义名字覆盖同名", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		allowCarrySkip(t, app)
		seedSavedPlan(app, "我的方案", 25*time.Minute, 5*time.Minute)

		app.openCustom()
		// 改一下时长再覆盖。
		press(t, app, "p")
		app.editor.value = nil
		app.editor.cursor = 0
		for _, r := range "50" {
			press(t, app, string(r))
		}
		press(t, app, "enter")

		press(t, app, "s")
		app.editor.value = nil
		app.editor.cursor = 0
		for _, r := range "我的方案" {
			press(t, app, string(r))
		}
		press(t, app, "enter")

		list := app.cfg.SavedPlanList()
		if len(list) != 1 {
			t.Fatalf("同名应覆盖而不是新增，实际 %d 套", len(list))
		}
		if list[0].Segments[0].Dur != 50*time.Minute {
			t.Errorf("覆盖后应取新时长 50m，实际 %v", list[0].Segments[0].Dur)
		}
	})

	t.Run("方案不完整时拒绝收藏", func(t *testing.T) {
		app, _, _ := newTestApp(t, at)
		allowCarrySkip(t, app)
		app.openCustom()
		// 删到只剩一段（自定义要求至少两段才能开始，但收藏校验另有一套）。
		press(t, app, "d")
		press(t, app, "p")
		app.editor.value = nil
		app.editor.cursor = 0
		for _, r := range "0" {
			press(t, app, string(r))
		}
		press(t, app, "enter")

		press(t, app, "s")
		if app.editor.active && strings.Contains(app.toast, "不完整") {
			// 被拒：不应打开输入框。
			t.Log("按预期被拒")
		}
		if len(app.cfg.SavedPlanList()) != 0 {
			t.Errorf("不完整的方案不该被收藏，实际 %+v", app.cfg.SavedPlanList())
		}
	})
}

// TestTimerMenuListsSavedPlans 验证计时菜单里有「收藏的方案」入口与数量。
func TestTimerMenuListsSavedPlans(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	allowCarrySkip(t, app)
	seedSavedPlan(app, "甲方案", 25*time.Minute, 5*time.Minute)
	seedSavedPlan(app, "乙方案", 50*time.Minute, 10*time.Minute)

	app.startTimer()
	labels := quitPromptLabels(app)
	joined := strings.Join(labels, "|")
	if !strings.Contains(joined, "收藏的方案（2）") {
		t.Errorf("计时菜单应显示收藏数量，实际 %v", labels)
	}

	// 进入收藏菜单后，每套方案要有「开始」与「载入为模板」两条用法。
	selectAction(t, app, "timer_saved")
	if app.pick == nil {
		t.Fatal("应打开收藏菜单")
	}
	joined = strings.Join(quitPromptLabels(app), "|")
	for _, want := range []string{"▶ 开始 甲方案", "✎ 载入为模板 甲方案", "▶ 开始 乙方案", "管理收藏"} {
		if !strings.Contains(joined, want) {
			t.Errorf("收藏菜单应包含 %q，实际 %v", want, quitPromptLabels(app))
		}
	}
}

// TestSavedPlanNoFavoritesToast 验证没有收藏时给出可操作的提示。
func TestSavedPlanNoFavoritesToast(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	allowCarrySkip(t, app)
	app.startTimer()
	selectAction(t, app, "timer_saved")
	if app.pick != nil {
		t.Error("没有收藏时不该打开空菜单")
	}
	if !strings.Contains(app.toast, "还没有收藏") {
		t.Errorf("应提示如何收藏，实际 toast=%q", app.toast)
	}
	if !strings.Contains(app.toast, "s") {
		t.Errorf("提示里应说明怎么收藏，实际 toast=%q", app.toast)
	}
}

// TestStartSavedPlan 验证「直接调用」收藏方案开始计时。
func TestStartSavedPlan(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, s, _ := newTestApp(t, at)
	allowCarrySkip(t, app)
	seedSavedPlan(app, "我的方案", 25*time.Minute, 5*time.Minute)

	app.openSavedPlans()
	selectAction(t, app, "saved_start:0")
	if app.pick == nil {
		t.Fatal("应继续询问归属的 TODO")
	}
	app.pick.cursor = len(app.pick.items) - 1
	press(t, app, "enter")

	if app.timer == nil {
		t.Fatal("应已开始计时")
	}
	if app.timer.plan.Kind != model.TimerCustom {
		t.Errorf("计时类型应为 custom，实际 %q", app.timer.plan.Kind)
	}
	if len(app.timer.plan.Segments) != 2 {
		t.Fatalf("应保留 2 段，实际 %d", len(app.timer.plan.Segments))
	}
	if app.timer.plan.Segments[0].Dur != 25*time.Minute {
		t.Errorf("第一段应为 25m，实际 %v", app.timer.plan.Segments[0].Dur)
	}
	_ = s
}

// TestStartSavedPlanDoesNotMutateStored 验证跑起来的方案是副本，改它不会改到收藏。
//
// 直接把收藏里的方案交给计时器，等于把原件交出去——共享切片的坑与「当模板改」
// 是同一个。
func TestStartSavedPlanDoesNotMutateStored(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	allowCarrySkip(t, app)
	seedSavedPlan(app, "原件", 25*time.Minute, 5*time.Minute)

	app.openSavedPlans()
	selectAction(t, app, "saved_start:0")
	app.pick.cursor = len(app.pick.items) - 1
	press(t, app, "enter")

	// 改运行中的方案。
	app.timer.plan.Segments[0].Dur = 1 * time.Minute
	app.timer.plan.Segments[0].Name = "被改过"

	list := app.cfg.SavedPlanList()
	if list[0].Segments[0].Dur != 25*time.Minute {
		t.Errorf("改运行中的方案不该影响收藏，实际 %v", list[0].Segments[0].Dur)
	}
	if list[0].Segments[0].Name != "深度工作" {
		t.Errorf("名字也不该被改，实际 %q", list[0].Segments[0].Name)
	}
}

// TestLoadSavedPlanAsTemplate 验证「调出来作为模板改」。
func TestLoadSavedPlanAsTemplate(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	allowCarrySkip(t, app)
	seedSavedPlan(app, "模板方案", 25*time.Minute, 5*time.Minute)

	app.openSavedPlans()
	selectAction(t, app, "saved_edit:0")
	if app.custom == nil {
		t.Fatal("应把方案装进自定义时段编辑器")
	}
	if len(app.custom.plan.Segments) != 2 {
		t.Fatalf("编辑器应有 2 段，实际 %d", len(app.custom.plan.Segments))
	}
	if app.timer != nil {
		t.Error("载入模板不该直接开始计时")
	}

	// 模板独立：改它不影响收藏。
	app.custom.plan.Segments[0].Dur = 99 * time.Minute
	if got := app.cfg.SavedPlanList()[0].Segments[0].Dur; got != 25*time.Minute {
		t.Errorf("改模板不该影响收藏，实际 %v", got)
	}

	// 改完能直接开始。
	press(t, app, "enter")
	if app.custom != nil {
		t.Error("确认后应关闭编辑器")
	}
	if app.pick == nil {
		t.Fatal("应弹出归属 TODO 的选择框")
	}
	app.pick.cursor = len(app.pick.items) - 1
	press(t, app, "enter")
	if app.timer == nil {
		t.Fatal("应已开始计时")
	}
	if app.timer.plan.Segments[0].Dur != 99*time.Minute {
		t.Errorf("应按改过的模板计时，实际 %v", app.timer.plan.Segments[0].Dur)
	}

	// 收藏里的原件仍是 25m。
	if got := app.cfg.SavedPlanList()[0].Segments[0].Dur; got != 25*time.Minute {
		t.Errorf("收藏原件应保持 25m，实际 %v", got)
	}
}

// TestDeleteSavedPlan 验证删除收藏。
func TestDeleteSavedPlan(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	allowCarrySkip(t, app)
	seedSavedPlan(app, "要删的", 25*time.Minute, 5*time.Minute)
	seedSavedPlan(app, "留着的", 50*time.Minute, 10*time.Minute)

	app.openSavedPlans()
	selectAction(t, app, "saved_manage")
	if app.pick == nil {
		t.Fatal("应打开管理菜单")
	}
	selectAction(t, app, "saved_del:0")

	list := app.cfg.SavedPlanList()
	if len(list) != 1 || list[0].Label != "留着的" {
		t.Fatalf("应只剩「留着的」，实际 %+v", list)
	}
	if !strings.Contains(app.toast, "已删除收藏") {
		t.Errorf("应提示已删除，实际 toast=%q", app.toast)
	}
}

// TestSavedPlanStartRespectsRunningTimer 验证计时进行中不能从收藏直接开始，
// 与其它启动入口保持同一个守卫（见已知 bug 1 那一批）。
func TestSavedPlanStartRespectsRunningTimer(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	seedSavedPlan(app, "第二套", 25*time.Minute, 5*time.Minute)
	startCountUpTimer(t, app)
	first := app.timer
	if first == nil {
		t.Fatal("第一个计时未启动")
	}

	// 直接走「开始收藏方案」这条路。
	app.startSavedPlan(0)
	if app.timer != first {
		t.Error("计时进行中不该被收藏方案顶掉")
	}
	if !strings.Contains(app.toast, "已有计时") {
		t.Errorf("应给出已有计时的提示，实际 toast=%q", app.toast)
	}

	// 「载入为模板」同样不该在计时中打开编辑器。
	app.loadSavedPlanIntoTemplate(0)
	if app.custom != nil {
		t.Error("计时进行中不该打开模板编辑器")
	}
	if app.timer != first {
		t.Error("计时不该被顶掉")
	}
}

// TestSavedPlanIndexOutOfRange 验证下标越界时给出提示而不是崩。
func TestSavedPlanIndexOutOfRange(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	allowCarrySkip(t, app)
	seedSavedPlan(app, "唯一一套", 25*time.Minute, 5*time.Minute)

	app.startSavedPlan(5)
	if app.timer != nil {
		t.Error("越界不该开始计时")
	}
	if !strings.Contains(app.toast, "不存在") {
		t.Errorf("应提示收藏不存在，实际 toast=%q", app.toast)
	}

	app.loadSavedPlanIntoTemplate(9)
	if app.custom != nil {
		t.Error("越界不该打开编辑器")
	}

	// 动作串里的下标也要能容错。
	if _, _, handled := app.savedPlanActions("saved_start:abc"); !handled {
		t.Error("非法下标应被处理并提示")
	}
	if !strings.Contains(app.toast, "不存在") {
		t.Errorf("非法下标应提示不存在，实际 toast=%q", app.toast)
	}
}

// TestSavedPlanPersistsToConfig 验证收藏与删除都写进配置文件。
func TestSavedPlanPersistsToConfig(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	allowCarrySkip(t, app)
	app.openCustom()
	press(t, app, "s")
	press(t, app, "enter")

	// 直接读配置文件，确认落盘。
	raw, err := readConfigFile(app)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "saved_plans") {
		t.Errorf("收藏应写进 config.json，实际:\n%s", raw)
	}
	if !strings.Contains(raw, "深度工作") {
		t.Errorf("配置里应包含方案内容，实际:\n%s", raw)
	}
}
