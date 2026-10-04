package config

import (
	"strings"
	"testing"
)

// TestGenerateNtfyTopicIsHighEntropy 验证自动生成的频道名足够随机、互不重复。
//
// ntfy 频道默认全网公开：频道名就是访问凭据，必须不可预测。用户自己起的
// "myphone" 这类名字基本必然被猜到，所以这条测试盯的是「程序替用户把关」。
func TestGenerateNtfyTopicIsHighEntropy(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		topic, err := GenerateNtfyTopic()
		if err != nil {
			t.Fatalf("生成失败: %v", err)
		}
		if seen[topic] {
			t.Fatalf("出现重复频道名 %q（第 %d 次）", topic, i)
		}
		seen[topic] = true

		// 长度：32 字节 base32 无填充 = 52 个字符。
		if len(topic) < 40 {
			t.Errorf("频道名过短（%d 字符）：%q", len(topic), topic)
		}
		// URL 安全：不能含 / + = 这些会被路径转义搞乱的字符。
		for _, bad := range []string{"/", "+", "=", "?", "#", " "} {
			if strings.Contains(topic, bad) {
				t.Errorf("频道名不该含 %q：%q", bad, topic)
			}
		}
	}
	if len(seen) != 200 {
		t.Errorf("200 次生成应全部不同，实际 %d 个", len(seen))
	}
}

// TestEffectiveNtfyTopicPersists 验证频道名只生成一次并固化在配置里。
//
// 每次专注都换频道的话，用户手机上的订阅就永久失效了。
func TestEffectiveNtfyTopicPersists(t *testing.T) {
	cfg := Default()

	first, generated := cfg.EffectiveNtfyTopic()
	if !generated || first == "" {
		t.Fatal("首次应生成一个频道名")
	}
	if cfg.NtfyTopic != first {
		t.Errorf("生成的频道名应写回配置，实际 %q", cfg.NtfyTopic)
	}

	// 再取一次：必须还是同一个，且不再报告「新生成」。
	second, generatedAgain := cfg.EffectiveNtfyTopic()
	if generatedAgain {
		t.Error("第二次不该再生成新频道")
	}
	if second != first {
		t.Errorf("频道名应固化不变：%q → %q", first, second)
	}

	// 用户自己填过的话，用用户的。
	cfg.NtfyTopic = "my-own-topic"
	got, changed := cfg.EffectiveNtfyTopic()
	if got != "my-own-topic" || changed {
		t.Errorf("用户填过就该用用户的，实际 %q changed=%v", got, changed)
	}
}

// TestNtfyURL 验证订阅地址拼装。
func TestNtfyURL(t *testing.T) {
	cfg := Default()
	cfg.NtfyTopic = "abcdefghijklmnopqrstuvwxyz"
	if got, want := cfg.NtfyURL(), "https://ntfy.sh/abcdefghijklmnopqrstuvwxyz"; got != want {
		t.Errorf("默认服务地址应为 %q，实际 %q", want, got)
	}

	// 自建服务：末尾多余的 / 要去掉，不能拼出双斜杠。
	cfg.NtfyServer = "https://ntfy.example.com/"
	if got, want := cfg.NtfyURL(), "https://ntfy.example.com/abcdefghijklmnopqrstuvwxyz"; got != want {
		t.Errorf("自建服务应为 %q，实际 %q", want, got)
	}

	// 没有频道名时没有地址。
	cfg.NtfyTopic = "  "
	if got := cfg.NtfyURL(); got != "" {
		t.Errorf("没有频道名时不该有地址，实际 %q", got)
	}
}

// TestNtfyTopicIsWeak 验证弱频道名的识别。
//
// 判据刻意宽松（只拦明显危险的）：程序有义务把风险讲清楚，但不该拦住用户
// 自己做的选择。
func TestNtfyTopicIsWeak(t *testing.T) {
	weak := []string{
		"", "  ", "kqflow", "KQFLOW", "myphone", "phone", "test", "demo",
		"abc", "123", "topic", "my-kqflow-topic", "alert",
	}
	for _, w := range weak {
		if !NtfyTopicIsWeak(w) {
			t.Errorf("%q 应被判为弱频道名", w)
		}
	}

	strong := []string{
		"JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP", // 52 字符随机串
		"abcdefghijklmnopqrst",                             // 20 字符边界
	}
	for _, s := range strong {
		if NtfyTopicIsWeak(s) {
			t.Errorf("%q 不该被判为弱频道名", s)
		}
	}
}

// TestNtfyReady 验证推送就绪判定：开关与频道名必须同时满足。
//
// 需求明确要求「可以填了但是关掉」，所以两者是独立的。
func TestNtfyReady(t *testing.T) {
	cfg := Default()
	cfg.NtfyTopic = "abcdefghijklmnopqrstuvwxyz"

	if cfg.NtfyReady() {
		t.Error("默认应是关的")
	}
	cfg.NtfyEnabled = true
	if !cfg.NtfyReady() {
		t.Error("开关打开且填了频道就应就绪")
	}
	cfg.NtfyTopic = ""
	if cfg.NtfyReady() {
		t.Error("没有频道名不该就绪")
	}
}

// TestNotifyDisclaimerWording 验证免责与措辞合规（见 bug.md 注意 2）。
//
// 两点必须都在：说明 ntfy 是独立的第三方服务（不能被误解为附属软件）、
// 以及频道公开可读的风险与责任划分。
func TestNotifyDisclaimerWording(t *testing.T) {
	for _, want := range []string{
		"第三方",   // 不是我们的附属软件
		"没有隶属",  // 明确关系
		"不加密",   // 频道是公开的
		"告诉陌生人", // 不要泄露频道名
		"不要轻信",  // 奇怪消息
		"不承担",   // 责任划分
	} {
		if !strings.Contains(NotifyDisclaimer, want) {
			t.Errorf("免责说明应包含 %q，实际：%s", want, NotifyDisclaimer)
		}
	}
}

// TestEffectiveNotifyPresets 验证提醒预设的生效判定（空与 none 都表示关闭）。
func TestEffectiveNotifyPresets(t *testing.T) {
	cfg := Default()
	if cfg.EffectiveNotifyGlow() != "" || cfg.EffectiveNotifySound() != "" {
		t.Error("默认应该都是关闭的")
	}

	cfg.NotifyGlow = "none"
	if cfg.EffectiveNotifyGlow() != "" {
		t.Error("none 应等同于关闭")
	}
	cfg.NotifyGlow = "aurora"
	if cfg.EffectiveNotifyGlow() != "aurora" {
		t.Errorf("应返回 aurora，实际 %q", cfg.EffectiveNotifyGlow())
	}

	cfg.NotifySound = "none"
	if cfg.EffectiveNotifySound() != "" {
		t.Error("none 应等同于关闭")
	}
	cfg.NotifySound = "bowl"
	if cfg.EffectiveNotifySound() != "bowl" {
		t.Errorf("应返回 bowl，实际 %q", cfg.EffectiveNotifySound())
	}
}
