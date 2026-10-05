package kxapp

import (
	"strings"
	"testing"
	"time"

	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/svc"
)

// TestTimerReportsFooterProgress 验证专注进度会出现在**下栏进度条**上。
//
// 用户反馈："专注还没有进度条。"下栏本来就有进度条能力
// （chrome.FooterBar.HasProgress），只是没人往上填。这件事只有正在跑的
// 计时磁贴知道，因此由它通过 plugin.FooterStatus 申报。
func TestTimerReportsFooterProgress(t *testing.T) {
	src := newMemSource(t, testNow())
	st := NewHostState()
	tile := &timerTile{src: src, state: st, svc: svc.Noop{}}

	// 未计时：不申报（下栏保持默认状态文本）。
	ctx := plugin.RenderCtx{Svc: clockOnly{testNow()}}
	if _, _, ok := tile.FooterProgress(ctx); ok {
		t.Fatal("未计时时不该申报进度")
	}

	// 开始一次 25 分钟专注。
	plan := focusPlan("测试", 25)
	if !st.Timer.Start(plan, nil, testNow()) {
		t.Fatal("应当能开始计时")
	}

	// 走了 10 分钟 → 进度 0.4。
	//
	// FooterProgress 走 ctx.Svc.Clock()，因此这里直接把时间喂给它，
	// 保证"组件算的时间"与"测试以为的时间"是同一个（不然就是在测幻觉）。
	setNow(src, testNow().Add(10*time.Minute))
	ctx = plugin.RenderCtx{Svc: clockOnly{src.Now()}}

	progress, text, ok := tile.FooterProgress(ctx)
	if !ok {
		t.Fatal("计时中应当申报进度")
	}
	if progress < 0.39 || progress > 0.41 {
		t.Errorf("10/25 的进度应约为 0.4，实际 %.3f", progress)
	}
	if !strings.Contains(text, "专注") {
		t.Errorf("进度文字里应说明在做什么，实际 %q", text)
	}
	// 文字要带上"已过 / 总共"——只有比例说不清"还剩多久"。
	if !strings.Contains(text, "/") {
		t.Errorf("进度文字应含「已过 / 总共」，实际 %q", text)
	}

	// 暂停时文字要标出来（进度条本身看不出来）。
	st.Timer.Pause(src.Now())
	if _, text, _ := tile.FooterProgress(ctx); !strings.Contains(text, "暂停") {
		t.Errorf("暂停时进度文字应标明，实际 %q", text)
	}

	// 继续，然后走到方案结束。
	//
	// 必须**先恢复再拨表**：暂停期间时间不走（这是计时的核心口径），
	// 拨了也不会完成。我第一版忘了恢复，测试于是"红得莫名其妙"。
	st.Timer.Resume(src.Now())
	setNow(src, testNow().Add(26*time.Minute))
	ctx = plugin.RenderCtx{Svc: clockOnly{src.Now()}}
	if _, text, _ := tile.FooterProgress(ctx); !strings.Contains(text, "已完成") {
		t.Errorf("完成后进度文字应标明，实际 %q", text)
	}
}

// clockOnly 是只暴露时钟的只读服务（供进度测试精确控制时间）。
type clockOnly struct{ t time.Time }

func (c clockOnly) Clock() time.Time             { return c.t }
func (c clockOnly) Capabilities() svc.Capability { return nil }

// TestFooterProgressEndToEnd 验证进度真的画到了界面上。
//
// 只测组件申报还不够——引擎那条"问所有磁贴、取第一个申报的"的链路
// 也要走通，否则组件报了也没人画。
func TestFooterProgressEndToEnd(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	// 未计时：下栏是默认状态文本。
	before := m.FooterText()
	if !strings.Contains(before, "Power by") {
		t.Errorf("未计时时下栏应显示默认状态文本，实际 %q", before)
	}

	// 直接开始计时（避免依赖界面文案）。
	if !l.State().Timer.Start(focusPlan("测试", 25), nil, testNow()) {
		t.Fatal("应当能开始计时")
	}
	setNow(src, testNow().Add(10*time.Minute))
	// 渲染一帧：refreshChrome 在这一步填进度条。
	_ = m.View()

	after := m.FooterText()
	if !strings.Contains(after, "专注") {
		t.Errorf("计时中下栏应显示专注进度，实际 %q", after)
	}
	if !strings.Contains(after, "/") {
		t.Errorf("下栏进度文字应含「已过 / 总共」，实际 %q", after)
	}
}
