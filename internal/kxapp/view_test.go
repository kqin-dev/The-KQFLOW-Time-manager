package kxapp

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"
)

// TestViewSettingsShowsSlotsAndTiles 验证界面布局页列出了槽位与磁贴。
//
// 用户点名："还没有 view 设置这种功能。"
// req.md 把视图列为独立能力：**包开关**决定功能在不在，
// **视图配置**决定磁贴摆哪儿、显不显示——两者粒度不同。
func TestViewSettingsShowsSlotsAndTiles(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	if !openMenuOptionOK(t, m, "界面布局") {
		t.Fatalf("菜单里应有界面布局，实际 %v", m.Options())
	}
	out := m.View()
	for _, want := range []string{"界面布局", "中栏停靠区", "槽位", "磁贴"} {
		if !strings.Contains(out, want) {
			t.Errorf("布局页应有 %q：\n%s", want, out)
		}
	}
	// 至少要列出四个槽位（左上/左下/右上/右下）。
	for _, a := range geometry.AllAnchors {
		if !strings.Contains(out, a.String()) {
			t.Errorf("布局页应列出槽位 %q：\n%s", a, out)
		}
	}
}

// TestViewSettingsHidesAndShowsTile 验证隐藏/显示磁贴真的生效。
//
// 这是本条功能的**核心语义**：隐藏后磁贴不再占槽位、不再渲染；
// 恢复后它回到自己声明的槽位。
func TestViewSettingsHidesAndShowsTile(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, services, _ := l.Build()
	m.Resize(120, 40)

	before := m.View()
	if !strings.Contains(before, "随手记") {
		t.Fatalf("初始应能看到随手记磁贴：\n%s", before)
	}

	// 进布局页，把光标移到"磁贴 随手记"那一行并回车隐藏。
	if !openMenuOptionOK(t, m, "界面布局") {
		t.Fatal("应能进入界面布局")
	}
	if !moveCursorToRow(t, m, "磁贴 随手记") {
		t.Fatalf("布局页里应有随手记这一行：\n%s", m.View())
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	// 回到看板看效果。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	after := m.View()
	// 注意要按内容判断：磁贴标题与页脚的"设置"字样都可能撞车。
	if strings.Contains(after, "（今天还没写）") {
		t.Errorf("隐藏后不该再渲染随手记磁贴：\n%s", after)
	}
	if services.Saves == 0 {
		t.Error("改布局应当落盘")
	}
	if !m.CanvasClean() {
		t.Errorf("隐藏后画布应当干净：%s", m.Diagnostics())
	}

	// 再进布局页恢复它。
	if !openMenuOptionOK(t, m, "界面布局") {
		t.Fatal("应能再次进入界面布局")
	}
	if !moveCursorToRow(t, m, "磁贴 随手记") {
		t.Fatalf("布局页里应有随手记这一行：\n%s", m.View())
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if got := m.View(); !strings.Contains(got, "（今天还没写）") {
		t.Errorf("恢复后应当重新渲染随手记磁贴：\n%s", got)
	}
}

// TestViewSettingsMovesTileToSlot 验证"把磁贴搬到指定槽位"真的生效。
func TestViewSettingsMovesTileToSlot(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	// 随手记默认在左栏（dock 的左格）。把它搬到右上槽位。
	if !openMenuOptionOK(t, m, "界面布局") {
		t.Fatal("应能进入界面布局")
	}
	if !moveCursorToRow(t, m, "槽位 "+geometry.AnchorRightTop.String()) {
		t.Fatalf("应能找到右上槽位：\n%s", m.View())
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"}) // 进入槽位选择

	// 在选择界面里找"随手记"并确认。
	picker := m.View()
	if !strings.Contains(picker, "放哪个磁贴") {
		t.Fatalf("应进入槽位选择界面：\n%s", picker)
	}
	if !moveCursorToRow(t, m, "随手记") {
		t.Fatalf("候选里应有随手记：\n%s", m.View())
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	// 应当回到布局页，且右上槽位显示随手记。
	layout := m.View()
	if !strings.Contains(layout, "界面布局") {
		t.Fatalf("确认后应回到布局页：\n%s", layout)
	}
	// 直接问引擎：右上槽位是否已是随手记。
	placement := m.Placements()
	found := false
	for _, p := range placement {
		if p.Anchor == geometry.AnchorRightTop && strings.Contains(p.PluginID, "note") {
			found = true
		}
	}
	if !found {
		t.Errorf("随手记应当被搬到右上，实际落位 %+v", placement)
	}
}

// TestViewSettingsTogglesDock 验证停靠区整块开关。
func TestViewSettingsTogglesDock(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	before := m.View()
	if !strings.Contains(before, "（今天还没写）") {
		t.Fatal("初始应能看到停靠区里的磁贴")
	}

	if !openMenuOptionOK(t, m, "界面布局") {
		t.Fatal("应能进入界面布局")
	}
	if !moveCursorToRow(t, m, "中栏停靠区") {
		t.Fatalf("布局页应有停靠区一行：\n%s", m.View())
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	// 关掉停靠区：那一块整个消失。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if got := m.View(); strings.Contains(got, "（今天还没写）") {
		t.Errorf("关掉停靠区后不该再渲染那里的磁贴：\n%s", got)
	}
}

// TestViewConfigRoundTrip 验证视图配置能存能读（这是它能持久化的前提）。
//
// 存档用**锚点名字**而不是数字枚举：数字枚举一改，老配置里的槽位
// 就会集体错位——而那种错位没有任何报错，只是东西放错了地方。
func TestViewConfigRoundTrip(t *testing.T) {
	src := newMemSource(t, testNow())
	cfg := src.Config()

	vc := plugin.NewViewConfig().
		WithSlot(geometry.AnchorRightTop, "kqflow.note.tile").
		WithSlot(geometry.AnchorLeftTop, "kqflow.todo.fixed").
		WithHidden("kqflow.stats.tile")
	vc.DockVisible = false
	SaveViewConfig(cfg, vc)

	// 存档里应当是**名字**，不是数字。
	slots, ok := cfg.View["slots"].(map[string]any)
	if !ok {
		t.Fatalf("存档应有 slots，实际 %#v", cfg.View)
	}
	if _, ok := slots[geometry.AnchorRightTop.String()]; !ok {
		t.Errorf("槽位键应当是锚点名字 %q，实际 %#v", geometry.AnchorRightTop.String(), slots)
	}

	// 读回来必须与写进去的一致。
	got := NewLoader(src, cfg).viewConfig()
	if pid := got.SlotOf(geometry.AnchorRightTop); pid != "kqflow.note.tile" {
		t.Errorf("右上槽位读回应为 note，实际 %q", pid)
	}
	if pid := got.SlotOf(geometry.AnchorLeftTop); pid != "kqflow.todo.fixed" {
		t.Errorf("左上槽位读回应为 todo.fixed，实际 %q", pid)
	}
	if !got.IsHidden("kqflow.stats.tile") {
		t.Error("隐藏项读回后应当仍然隐藏")
	}
	if got.DockVisible {
		t.Error("停靠区应当是关的")
	}
}

// TestViewConfigIgnoresGarbage 验证存档里有脏数据/未知锚点时**不崩、只忽略**。
//
// 配置文件是给用户手改的，因此"改错了"必须比"崩了"更可能发生。
func TestViewConfigIgnoresGarbage(t *testing.T) {
	src := newMemSource(t, testNow())
	cfg := src.Config()
	cfg.View = map[string]any{
		"slots":  map[string]any{"不存在的锚点": "kqflow.x", "左上": 12345},
		"hidden": []any{"kqflow.y", 42, nil},
		"dock":   "yes", // 类型不对
	}
	vc := NewLoader(src, cfg).viewConfig()
	if pid := vc.SlotOf(geometry.AnchorLeftTop); pid != "" {
		t.Errorf("非字符串的槽位值应当被忽略，实际 %q", pid)
	}
	if vc.IsHidden("kqflow.y") == false {
		t.Error("合法的隐藏项仍应生效")
	}
	if !vc.DockVisible {
		t.Error("dock 值类型不对时应保留默认（可见）")
	}
}

// moveCursorToRow 在设置/布局类页面里，把光标移到**包含 want 的那一行**。
func moveCursorToRow(t *testing.T, m interface {
	View() string
	Dispatch(plugin.Event) bool
}, want string) bool {
	t.Helper()
	for i := 0; i < 40; i++ {
		if lineIsSelected(m.View(), want) {
			return true
		}
		before := m.View()
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "j"})
		if m.View() == before {
			return false // 到底了，没找到
		}
	}
	return false
}

// lineIsSelected 报告 want 那一行当前是否带选中标记。
func lineIsSelected(view, want string) bool {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, want) && strings.Contains(line, "▸") {
			return true
		}
	}
	return false
}
