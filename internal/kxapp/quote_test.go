package kxapp

import (
	"strings"
	"testing"
	"time"

	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/svc"
)

// TestDashboardShowsQuote 验证看板正中显示字条。
//
// 2.1.0 的看板正中一直有句话（Quotes + 每 12 秒轮换），引擎版一开始漏了，
// 看板因此显得空。它不是功能，但是**产品气质**的一部分。
func TestDashboardShowsQuote(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	out := m.View()
	found := false
	for _, q := range Quotes {
		if strings.Contains(out, q) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("看板应当显示一条字条：\n%s", out)
	}
}

// TestQuotesRotateOnTick 验证字条按时间轮换（不是永远那一句）。
func TestQuotesRotateOnTick(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	// 第一帧确立起点。
	m.Tick(testNow())
	first := firstQuoteLine(m.View())
	if first == "" {
		t.Fatal("看板应当有字条")
	}

	// 不到一个间隔：不该换。
	m.Tick(testNow().Add(QuoteEvery - time.Second))
	if got := firstQuoteLine(m.View()); got != first {
		t.Errorf("不到 %v 不该换字条：%q → %q", QuoteEvery, first, got)
	}

	// 过了间隔：应当换。
	m.Tick(testNow().Add(QuoteEvery + time.Second))
	if got := firstQuoteLine(m.View()); got == first {
		t.Errorf("过了 %v 应当换字条，但仍然显示 %q", QuoteEvery, got)
	}
}

// TestCustomQuotesWin 验证配置里设了自定义字条时用用户那份。
//
// 与 2.1.0 的 customQuotes 同口径：自定义**完全替代**内置，
// 而不是混在一起（混起来会让用户不知道哪句是自己加的）。
func TestCustomQuotesWin(t *testing.T) {
	src := newMemSource(t, testNow())
	src.Config().Quotes = []string{"我自己的第一句", "我自己的第二句"}

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	out := m.View()

	if !strings.Contains(out, "我自己的") {
		t.Errorf("应当显示自定义字条：\n%s", out)
	}
	for _, q := range Quotes {
		if strings.Contains(out, q) {
			t.Errorf("设了自定义字条后不该再显示内置的 %q", q)
		}
	}
}

// TestQuoteIgnoresInvisibleChars 验证自定义字条里的控制字符不会进界面。
func TestQuoteIgnoresInvisibleChars(t *testing.T) {
	src := newMemSource(t, testNow())
	src.Config().Quotes = []string{"带\x00NUL\x00的字条"}
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	out := m.View()
	if strings.ContainsRune(out, 0) {
		t.Errorf("字条里的 NUL 不该进界面：%q", out)
	}
	if !strings.Contains(out, "带NUL的字条") {
		t.Errorf("清掉控制字符后剩下的文字应当还在：\n%s", out)
	}
}

// TestQuoteRotationIsDeterministic 验证轮换是顺序的、可预期的。
//
// 2.1.0 用随机并刻意避开重复；但在这个频率下顺序更可预期——
// 随机会让用户觉得"有时重复了"。
func TestQuoteRotationIsDeterministic(t *testing.T) {
	k := &kernel{src: newMemSource(t, testNow())}
	n := len(Quotes)
	if got := k.CurrentQuote(); got != Quotes[0] {
		t.Errorf("起始应当是第一条，实际 %q", got)
	}
	for i := 1; i <= n; i++ {
		k.NextQuote()
		want := Quotes[i%n]
		if got := k.CurrentQuote(); got != want {
			t.Fatalf("第 %d 次轮换应到 %q，实际 %q", i, want, got)
		}
	}
	// 只有一条时不轮换（否则每次都"换"到同一条，白费力气）。
	k2 := &kernel{src: newMemSource(t, testNow())}
	k2.src.Config().Quotes = []string{"唯一一句"}
	k2.NextQuote()
	if got := k2.CurrentQuote(); got != "唯一一句" {
		t.Errorf("只有一条时应当保持它，实际 %q", got)
	}
	_ = svc.Noop{}
	_ = plugin.None()
}

// firstQuoteLine 从渲染结果里取出字条那一行（返回空表示没找到）。
func firstQuoteLine(view string) string {
	for _, line := range strings.Split(view, "\n") {
		for _, q := range Quotes {
			if strings.Contains(line, q) {
				return strings.TrimSpace(line)
			}
		}
		if strings.Contains(line, "我自己的") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
