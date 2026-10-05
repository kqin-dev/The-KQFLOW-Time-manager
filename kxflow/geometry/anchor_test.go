package geometry

import "testing"

// TestAllAnchorsOrderIsContract 固化锚点顺序。
//
// AllAnchors 的顺序是**契约**：装载报告、视图配置遍历、按优先级安置
// 都依赖它可复现。改了它等于改了安置规则（同一个布局会摆出不同结果），
// 因此这里逐个断言顺序，而不是只断言长度。
func TestAllAnchorsOrderIsContract(t *testing.T) {
	want := []Anchor{
		AnchorLeftTop,
		AnchorLeftBottom,
		AnchorRightTop,
		AnchorRightBottom,
		AnchorCenterDockLeft,
		AnchorCenterDockRight,
	}
	if len(AllAnchors) != len(want) {
		t.Fatalf("槽位数量应为 %d，实际 %d", len(want), len(AllAnchors))
	}
	for i := range want {
		if AllAnchors[i] != want[i] {
			t.Errorf("第 %d 个槽位应为 %v，实际 %v", i, want[i], AllAnchors[i])
		}
	}
}

// TestAnchorProperties 覆盖锚点的归属与槽位下标。
func TestAnchorProperties(t *testing.T) {
	cases := []struct {
		a      Anchor
		valid  bool
		isSlot bool
		column string
		index  int
	}{
		{AnchorUnset, true, false, "", -1},
		{AnchorLeftTop, true, true, "left", 0},
		{AnchorLeftBottom, true, true, "left", 1},
		{AnchorRightTop, true, true, "right", 0},
		{AnchorRightBottom, true, true, "right", 1},
		{AnchorCenterDockLeft, true, true, "dock", 0},
		{AnchorCenterDockRight, true, true, "dock", 1},
		// 越界取值（例如反序列化出来的野值）必须是"不合法"，让校验拦住它。
		{Anchor(200), false, false, "", -1},
	}
	for _, c := range cases {
		if got := c.a.Valid(); got != c.valid {
			t.Errorf("%v.Valid() = %v，期望 %v", c.a, got, c.valid)
		}
		if got := c.a.IsSlot(); got != c.isSlot {
			t.Errorf("%v.IsSlot() = %v，期望 %v", c.a, got, c.isSlot)
		}
		if got := c.a.Column(); got != c.column {
			t.Errorf("%v.Column() = %q，期望 %q", c.a, got, c.column)
		}
		if got := c.a.SlotIndex(); got != c.index {
			t.Errorf("%v.SlotIndex() = %d，期望 %d", c.a, got, c.index)
		}
	}
}

// TestRealAnchorsCoverEveryColumnPair 验证"每栏恰好两个真实槽位"。
//
// 这是 req.md 的硬约束（"每栏最多放两个磁贴"）：如果某个栏少一个槽位，
// 用户就会遇到"配置里说能放两个，实际只能放一个"。
func TestRealAnchorsCoverEveryColumnPair(t *testing.T) {
	counts := map[string]int{}
	for _, a := range AllAnchors {
		if !a.IsSlot() {
			t.Errorf("AllAnchors 里不应出现 %v（它不是真实槽位）", a)
			continue
		}
		counts[a.Column()]++
	}
	for _, col := range []string{"left", "right", "dock"} {
		if counts[col] != 2 {
			t.Errorf("栏位 %q 应有 2 个槽位，实际 %d", col, counts[col])
		}
	}
	if len(counts) != 3 {
		t.Errorf("应恰好有 3 个栏位，实际 %d：%v", len(counts), counts)
	}
}

// TestAnchorStringIsReadable 验证锚点有中文说明（装载报告要用）。
func TestAnchorStringIsReadable(t *testing.T) {
	for _, a := range append([]Anchor{AnchorUnset}, AllAnchors...) {
		s := a.String()
		if s == "" || s == "未知锚点" {
			t.Errorf("%v 应有人读的说明，实际 %q", a, s)
		}
	}
	if got := Anchor(200).String(); got != "未知锚点" {
		t.Errorf("越界取值应显示为未知锚点，实际 %q", got)
	}
}
