package config

import "testing"

// TestUseKXFLOW 固化迁移开关的判据。
//
// 关键约定：**只有明确写了 kxflow 才走新引擎**。
// 空串（老配置文件没有这个字段）与拼错的值都按 legacy 处理——
// 保守方向永远选"用户今天能干活"。
func TestUseKXFLOW(t *testing.T) {
	cases := []struct {
		engine string
		want   bool
	}{
		{"", false},        // 老配置：字段不存在
		{"legacy", false},  // 显式选老的
		{"kxflow", true},   // 显式选新的
		{"KXFLOW", true},   // 大小写不敏感（用户手写配置）
		{" kxflow ", true}, // 容忍空白
		{"kxflwo", false},  // 拼错 → 继续用能用的那个
		{"v3", false},      // 未知值 → legacy
	}
	for _, c := range cases {
		cfg := &Config{Engine: c.engine}
		if got := cfg.UseKXFLOW(); got != c.want {
			t.Errorf("Engine=%q：UseKXFLOW() = %v，期望 %v", c.engine, got, c.want)
		}
	}
	// nil 配置不该 panic（防御性：调用方可能传 nil）。
	var nilCfg *Config
	if nilCfg.UseKXFLOW() {
		t.Error("nil 配置应当按 legacy 处理")
	}
}
