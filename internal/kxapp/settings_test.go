package kxapp

import (
	"strings"
	"testing"

	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"
)

// TestSettingsPageEditsConfig 验证设置页能真的改到配置上。
//
// 这是 M5 交互类功能里最要紧的一条：设置项改的是**配置文件**，
// 与日数据是两个文件（混在一起会让"改个昵称"也触发日数据备份）。
func TestSettingsPageEditsConfig(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, services, _ := l.Build()
	m.Resize(120, 40)

	// 从菜单进入设置页（它是看板选项，不需要选中条目）。
	if !openMenuOptionOK(t, m, "设置") {
		t.Fatalf("菜单里应有「设置」，实际 %v", m.Options())
	}
	if !strings.Contains(m.View(), "专注时长") {
		t.Fatalf("应显示设置页：\n%s", m.View())
	}

	// 光标停在第一项"专注时长"，回车进入编辑。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	if !strings.Contains(m.View(), "当前值") {
		t.Fatalf("应进入编辑界面：\n%s", m.View())
	}

	// 输入 45 并确认。
	for _, r := range []rune("45") {
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: string(r), Runes: []rune{r}})
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	if got := src.Config().FocusMinutes(); got != 45 {
		t.Fatalf("专注时长应改成 45，实际 %d", got)
	}
	if src.configSaved == 0 {
		t.Error("改设置应当写配置文件")
	}
	if services.Saves == 0 {
		t.Error("改设置应当触发一次落盘")
	}
	// 编辑界面应当已关闭（回到设置页）。
	if !strings.Contains(m.View(), "专注时长") {
		t.Errorf("确认后应回到设置页：\n%s", m.View())
	}
}

// TestSettingsRejectsInvalidInput 验证非法输入被拒绝、且**保留用户输入**。
//
// 敲了半天的值被静默清空是最让人恼火的一种交互，因此错误必须
// 显示出来、输入必须留着让用户改。
func TestSettingsRejectsInvalidInput(t *testing.T) {
	src := newMemSource(t, testNow())
	before := src.Config().FocusMinutes()

	l := NewLoader(src, src.Config())
	m, services, _ := l.Build()
	m.Resize(120, 40)
	openMenuOption(t, m, "设置")
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"}) // 编辑专注时长

	for _, r := range []rune("999") {
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: string(r), Runes: []rune{r}})
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})

	out := m.View()
	if !strings.Contains(out, "600") && !strings.Contains(out, "之间") {
		t.Errorf("非法值应给出范围提示：\n%s", out)
	}
	if got := src.Config().FocusMinutes(); got != before {
		t.Errorf("非法输入不该改动配置：%d → %d", before, got)
	}
	if services.Saves != 0 {
		t.Error("非法输入不该落盘")
	}
	// 输入必须还在（999 三个字符）。
	if !strings.Contains(out, "999") {
		t.Errorf("非法输入时应当保留用户输入：\n%s", out)
	}
}

// TestSettingsCutoffValidatesFormat 验证日界线格式校验。
//
// 日界线解析错了会让"今天是哪一天"整片错位，而那种错误在界面上
// 极难看出来，因此这里必须拦住。
func TestSettingsCutoffValidatesFormat(t *testing.T) {
	src := newMemSource(t, testNow())
	original := src.Config().DayCutoff

	for _, bad := range []string{"25:00", "4", "abc", "04:99"} {
		if err := parseCutoffSetting(src, bad); err == nil {
			t.Errorf("非法日界线 %q 应当被拒绝", bad)
		}
	}
	if src.Config().DayCutoff != original {
		t.Errorf("非法输入不该改动日界线：%q → %q", original, src.Config().DayCutoff)
	}
	// 合法输入应当被规范化（"4:00" → "04:00"）。
	if err := parseCutoffSetting(src, "4:00"); err != nil {
		t.Fatalf("合法日界线不该报错：%v", err)
	}
	if got := src.Config().DayCutoff; got != "04:00" {
		t.Errorf("日界线应被规范化为 04:00，实际 %q", got)
	}
}

// TestSettingsSnapshotHasNoInvisibleRunes 验证设置页展示的值不带控制字符。
func TestSettingsSnapshotHasNoInvisibleRunes(t *testing.T) {
	src := newMemSource(t, testNow())
	// 昵称里塞一个 NUL（模拟老数据/粘贴带进来的情况）。
	src.Config().Nickname = "张\x00三"

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	openMenuOption(t, m, "设置")

	out := m.View()
	if strings.ContainsRune(out, 0) {
		t.Errorf("设置页不该出现 NUL：%q", out)
	}
}

// TestSettingPackHasNoTile 验证设置包**不含磁贴**（它不该占看板空间）。
//
// 这条同时也是"包可以只含选项"的第二个例子（第一个是 kqflow.labels）。
func TestSettingPackHasNoTile(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, rep := l.Build()
	if !containsStr(rep.Loaded, SettingPackID) {
		t.Fatalf("设置包应被装载，实际 %v", rep.Loaded)
	}
	// 它的选项应当在菜单里。
	found := false
	for _, b := range m.Options() {
		if strings.Contains(b.Label, "设置") {
			found = true
		}
	}
	if !found {
		t.Fatalf("菜单里应有设置，实际 %v", m.Options())
	}
	// 设置包不该占任何槽位：它的成员只有看板选项。
	for _, a := range m.Registry().Anchors() {
		slot, _ := m.Registry().At(a)
		if slot.PackID == SettingPackID {
			t.Errorf("设置包不该占槽位，实际占了 %v", a)
		}
	}
}

// TestSettingEditorEscKeepsChanges 验证编辑界面里 esc 不落盘。
//
// "取消了却把值改掉了"是最难查的一类问题，必须钉住。
//
// ⚠️ 这里顺便把"用 l 进功能要爬几层"记下来，因为它是**真实的观感代价**：
//
//	层 1 菜单 → 层 2 设置页 → 层 3 编辑界面
//
// 因此从编辑界面一路退回看板要按 **3 次 esc**。这是"把菜单做成事务"
// 换来的（好处是按键数量不随功能增长、不用给每个功能发一个字母键）。
// 这条层数是**契约**，写进测试免得日后改乱了没人发现。
func TestSettingEditorEscKeepsChanges(t *testing.T) {
	src := newMemSource(t, testNow())
	before := src.Config().FocusMinutes()

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	// 2 层：菜单 + 设置页。
	if got := openMenuOption(t, m, "设置"); got != 2 {
		t.Fatalf("从菜单进设置页应压 2 层，实际 %d", got)
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"}) // 第 3 层：编辑
	if got := m.Stage().Depth(); got != 3 {
		t.Fatalf("进入编辑后栈深应为 3，实际 %d", got)
	}

	for _, r := range []rune("45") {
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: string(r), Runes: []rune{r}})
	}
	// esc 不确认：应当丢弃这次编辑。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})

	if got := src.Config().FocusMinutes(); got != before {
		t.Errorf("esc 取消后配置不该变：%d → %d", before, got)
	}
	if src.configSaved != 0 {
		t.Error("esc 取消不该写配置文件")
	}
	// 退回设置页（还有两层栈）。
	if got := m.Stage().Depth(); got != 2 {
		t.Fatalf("第一次 esc 后栈深应为 2，实际 %d", got)
	}
	if !strings.Contains(m.View(), "专注时长") {
		t.Errorf("esc 应退回设置页：\n%s", m.View())
	}
	// 第二次 esc：关设置页，回到菜单。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if got := m.Stage().Depth(); got != 1 {
		t.Fatalf("第二次 esc 后应只剩菜单，实际栈深 %d", got)
	}
	// 第三次 esc：关菜单，焦点回到某个磁贴。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	if m.Stage().Borrowing() {
		t.Error("第三次 esc 应关闭菜单")
	}
	if !m.Focus().IsSlot() {
		t.Errorf("关闭整条事务后焦点应回到某个磁贴，实际 %v", m.Focus())
	}
}

// TestSettingFocusHints 验证设置页与编辑界面各自申报提示。
//
// 它们的按键集不同（设置页有 j/k，编辑界面只有 enter/esc），
// 因此下栏必须跟着**当前这一层**变——这正是 KeyHinter 的用途。
func TestSettingFocusHints(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	openMenuOption(t, m, "设置")

	page := m.Hints()
	if !hasHint(page, "j/k") || !hasHint(page, "enter") {
		t.Errorf("设置页应提示 j/k 与 enter，实际 %v", page)
	}
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "enter"})
	editor := m.Hints()
	if hasHint(editor, "j/k") {
		t.Errorf("编辑界面不该提示 j/k（它不吃这个键），实际 %v", editor)
	}
	if !hasHint(editor, "esc") {
		t.Errorf("编辑界面应提示 esc 取消，实际 %v", editor)
	}
	_ = geometry.AnchorLeftTop
}
