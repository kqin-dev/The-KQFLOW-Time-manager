package model

import (
	"strings"
	"testing"
	"time"
)

// TestLabelNameSanitizes 验证标签名的清洗：控制字符、空白、超长。
func TestLabelNameSanitizes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"星星", "星星"},
		{"  紧急  ", "紧急"},
		{"有\x00控制\x07字符", "有控制字符"},
		{"多   个   空   白", "多 个 空 白"},
		// 制表与换行属于控制字符，Sanitize 直接**删掉**而不是当分隔符——
		// 拆分多个标签是 ParseLabelsText 那一层的职责（见下面的用例）。
		{"带\t制表\n换行", "带制表换行"},
		{"", ""},
		{"   ", ""},
		{"\x00\x01", ""},
	}
	for _, c := range cases {
		if got := LabelName(c.in); got != c.want {
			t.Errorf("LabelName(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}

	// 超长截断到上限，而不是报错。
	long := strings.Repeat("长", MaxLabelRunes+5)
	got := LabelName(long)
	if n := len([]rune(got)); n != MaxLabelRunes {
		t.Errorf("超长标签应截断到 %d 个字符，实际 %d", MaxLabelRunes, n)
	}
}

// TestNormalizeLabelsDedupesAndCaps 验证去空、去重、保序与数量上限。
func TestNormalizeLabelsDedupesAndCaps(t *testing.T) {
	got := NormalizeLabels([]string{"星星", "", "  ", "紧急", "星星", "爱心"})
	want := []string{"星星", "紧急", "爱心"}
	if len(got) != len(want) {
		t.Fatalf("应为 %v，实际 %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("应为 %v，实际 %v", want, got)
		}
	}

	// 全空返回 nil，方便 omitempty 不写进 JSON。
	if got := NormalizeLabels([]string{"", "  "}); got != nil {
		t.Errorf("全空应返回 nil，实际 %#v", got)
	}
	if got := NormalizeLabels(nil); got != nil {
		t.Errorf("nil 输入应返回 nil，实际 %#v", got)
	}

	// 数量上限。
	many := make([]string, 0, MaxLabelsPerItem+4)
	for i := 0; i < MaxLabelsPerItem+4; i++ {
		many = append(many, string(rune('a'+i)))
	}
	if got := NormalizeLabels(many); len(got) != MaxLabelsPerItem {
		t.Errorf("应截到 %d 个，实际 %d", MaxLabelsPerItem, len(got))
	}
}

// TestParseLabelsText 验证把一行文本解析成标签：空格与中英文逗号都当分隔符。
func TestParseLabelsText(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"星星 紧急", 2},
		{"星星,紧急", 2},
		{"星星，紧急", 2},
		{"星星、紧急、爱心", 3},
		{"星星;紧急", 2},
		{"星星；紧急", 2},
		{"  星星   紧急  ", 2},
		{"星星 星星", 1},
		{"", 0},
	}
	for _, c := range cases {
		if got := ParseLabelsText(c.in); len(got) != c.want {
			t.Errorf("ParseLabelsText(%q) = %v（%d 个），期望 %d 个", c.in, got, len(got), c.want)
		}
	}

	// 多行输入要在这一层被拆成多个标签，而不是被 Sanitize 拼成一团。
	got := ParseLabelsText("星星\n紧急\n\t爱心")
	if len(got) != 3 {
		t.Fatalf("换行/制表分隔应拆出 3 个标签，实际 %v", got)
	}
	for i, want := range []string{"星星", "紧急", "爱心"} {
		if got[i] != want {
			t.Errorf("第 %d 个应为 %q，实际 %q", i, want, got[i])
		}
	}
}

// TestTodoAndGoalToggleLabel 验证两个条目类型都能打开/关闭标签。
func TestTodoAndGoalToggleLabel(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	// 用接口调用，确保两边行为一致（界面层就是靠这个接口统一处理的）。
	items := map[string]Labeled{
		"todo": NewTodo("写论文", KindFloating, "2026-10-03", at),
		"goal": NewGoal("跑完半程马拉松", at),
	}

	for name, item := range items {
		t.Run(name, func(t *testing.T) {
			if on := item.ToggleItemLabel("星星"); !on {
				t.Error("第一次切换应打开标签")
			}
			if got := item.ItemLabels(); len(got) != 1 || got[0] != "星星" {
				t.Fatalf("应有 [星星]，实际 %v", got)
			}
			if !HasLabel(item.ItemLabels(), "星星") {
				t.Error("HasLabel 应报告存在")
			}
			if on := item.ToggleItemLabel("星星"); on {
				t.Error("第二次切换应关闭标签")
			}
			if len(item.ItemLabels()) != 0 {
				t.Errorf("关闭后应为空，实际 %v", item.ItemLabels())
			}

			// 空名与只有控制字符的名字不该被记上。
			item.ToggleItemLabel("   ")
			item.ToggleItemLabel("\x00")
			if len(item.ItemLabels()) != 0 {
				t.Errorf("无效标签不该被记上，实际 %v", item.ItemLabels())
			}

			// 顺序保留、不重复。
			item.ToggleItemLabel("紧急")
			item.ToggleItemLabel("星星")
			item.ToggleItemLabel("紧急")
			got := item.ItemLabels()
			if len(got) != 1 || got[0] != "星星" {
				t.Errorf("开关顺序应留下 [星星]，实际 %v", got)
			}

			// 整份替换。
			item.SetItemLabels([]string{"爱心", "爱心", "  ", "忽略"})
			got = item.ItemLabels()
			if len(got) != 2 || got[0] != "爱心" || got[1] != "忽略" {
				t.Errorf("整份替换应为 [爱心 忽略]，实际 %v", got)
			}
		})
	}
}

// TestToggleLabelRespectsCap 验证达到上限后不再新增，但已存在的仍能取下。
func TestToggleLabelRespectsCap(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	todo := NewTodo("写论文", KindFloating, "2026-10-03", at)

	full := make([]string, 0, MaxLabelsPerItem)
	for i := 0; i < MaxLabelsPerItem; i++ {
		full = append(full, string(rune('a'+i)))
	}
	todo.SetItemLabels(full)
	if len(todo.ItemLabels()) != MaxLabelsPerItem {
		t.Fatalf("应装满 %d 个，实际 %d", MaxLabelsPerItem, len(todo.ItemLabels()))
	}

	// 新增应被拒（返回 false 且列表不变）。
	if on := todo.ToggleItemLabel("再来一个"); on {
		t.Error("超过上限时不该新增成功")
	}
	if len(todo.ItemLabels()) != MaxLabelsPerItem {
		t.Errorf("被拒后数量不该变，实际 %d", len(todo.ItemLabels()))
	}

	// 但已存在的标签仍能取下。
	if on := todo.ToggleItemLabel("a"); on {
		t.Error("取下应返回 false（表示现在不在条目上）")
	}
	if HasLabel(todo.ItemLabels(), "a") {
		t.Error("取下后不该还在条目上")
	}
}

// TestPruneOrphansCleansLabels 验证读入老数据时标签会被自愈：
// 清控制字符、去重、截断，并如实报告「发生了改动」。
func TestPruneOrphansCleansLabels(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	data := NewDayData("2026-10-03", at)

	dirty := NewTodo("手改过的条目", KindFloating, "2026-10-03", at)
	dirty.Labels = []string{"星星\x00", "星星\x00", "", "  紧急  ", strings.Repeat("长", MaxLabelRunes+3)}
	data.Floating = append(data.Floating, dirty)

	if !data.PruneOrphans() {
		t.Fatal("脏标签应被报告为发生改动")
	}
	got := dirty.Labels
	if len(got) != 3 {
		t.Fatalf("自愈后应有 3 个标签（去重去空），实际 %v", got)
	}
	if got[0] != "星星" {
		t.Errorf("控制字符应被清掉，实际 %q", got[0])
	}
	if got[1] != "紧急" {
		t.Errorf("首尾空白应被清掉，实际 %q", got[1])
	}
	if n := len([]rune(got[2])); n != MaxLabelRunes {
		t.Errorf("超长标签应被截断到 %d，实际 %d", MaxLabelRunes, n)
	}

	// 干净的标签不该被报告为改动（避免每次读入都写盘）。
	clean := NewDayData("2026-10-03", at)
	ok := NewTodo("干净条目", KindFloating, "2026-10-03", at)
	ok.SetItemLabels([]string{"星星", "紧急"})
	clean.Floating = append(clean.Floating, ok)
	if clean.PruneOrphans() {
		t.Error("干净的标签不该被报告为改动")
	}
}
