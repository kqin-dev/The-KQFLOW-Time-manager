package svc

import "testing"

// TestCapabilityHas 覆盖能力标记查询（联动选项判断是否适用时依赖它）。
func TestCapabilityHas(t *testing.T) {
	c := Capability{"item.selection", "item.due"}
	if !c.Has("item.selection") {
		t.Error("应命中已有标记")
	}
	if c.Has("nope") {
		t.Error("未声明的标记不应命中")
	}
	if !c.HasAll([]string{"item.selection", "item.due"}) {
		t.Error("全部具备时应为真")
	}
	if c.HasAll([]string{"item.selection", "nope"}) {
		t.Error("有缺项时应为假")
	}
	if !c.HasAll(nil) {
		t.Error("空要求应视为满足")
	}
	var empty Capability
	if empty.Has("x") || !empty.HasAll(nil) {
		t.Error("空集合：单项查询应为假、空要求应为真")
	}
}

// TestNoopIsUsable 验证空实现可用且不 panic。
//
// 它的意义是"宿主少给一个能力时引擎不该崩"：任何能力都必须有可用的降级路径。
func TestNoopIsUsable(t *testing.T) {
	var s Services = Noop{}

	if s.Clock().IsZero() {
		t.Error("Noop 的时钟应返回真实时间，否则动画帧无法推进")
	}
	if err := s.Persist(PersistRequest{PluginID: "x", Kind: "day"}); err != nil {
		t.Errorf("Noop 的 Persist 不应报错，实际 %v", err)
	}
	s.Effect(Effect{Kind: EffectBell, Text: "响一下"}) // 不应 panic
	if caps := s.Capabilities(); caps != nil {
		t.Errorf("Noop 不应声称具备任何额外能力，实际 %v", caps)
	}
}

// TestEffectKindsDistinct 固化副作用类别互不相同。
//
// 类别被复用会让"响铃"和"流光"混成一个，那是很难查的观感问题。
func TestEffectKindsDistinct(t *testing.T) {
	kinds := []EffectKind{EffectBell, EffectFlash, EffectPush, EffectToast}
	seen := map[EffectKind]bool{}
	for _, k := range kinds {
		if seen[k] {
			t.Errorf("副作用类别 %d 重复", k)
		}
		seen[k] = true
	}
}
