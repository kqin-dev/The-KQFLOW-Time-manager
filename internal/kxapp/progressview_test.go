package kxapp

import (
	"strings"
	"testing"
	"time"
)

// TestFooterProgressRendersInView 验证进度条**真的画在帧上**。
//
// 前面的用例只确认了"引擎把文字填进了 footer 字段"，
// 而进度条本身是 chrome 画的——这条把两者接起来：
// 只看字段会漏掉"字段对了但没画"这种情况。
func TestFooterProgressRendersInView(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)

	// 未计时：帧里不该有进度条的填充字符。
	plain := m.View()
	if strings.Contains(plain, "专注 10m") {
		t.Fatalf("未计时时不该有进度文字：\n%s", plain)
	}

	// 开始计时并走过 10 分钟，再渲染。
	if !l.State().Timer.Start(focusPlan("测试", 25), nil, testNow()) {
		t.Fatal("应当能开始计时")
	}
	setNow(src, testNow().Add(10*time.Minute))
	frame := m.View()

	if !strings.Contains(frame, "专注 10m") {
		t.Errorf("计时中下栏应显示「专注 10m」：\n%s", lastLines(frame, 3))
	}
	// 进度条用的填充字符（chrome.drawProgress 的 BarFilled）。
	if !strings.Contains(frame, "█") {
		t.Errorf("进度条应当画出填充块：\n%s", lastLines(frame, 3))
	}
	if !m.CanvasClean() {
		t.Errorf("画布诊断不干净：%s", m.Diagnostics())
	}
}

// TestFooterHeaderRendersInView 验证上栏内容真的画在帧上。
//
// 与进度条同理：只确认"内核提供了字段"不够，要看它有没有被画出来。
func TestFooterHeaderRendersInView(t *testing.T) {
	src := newMemSource(t, testNow())
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	frame := m.View()

	first := strings.Split(frame, "\n")[0]
	if !strings.Contains(first, "2026-10-03") {
		t.Errorf("上栏应含逻辑日，实际首行 %q", first)
	}
	if !strings.Contains(first, "日界线") {
		t.Errorf("上栏应含日界线（用户要一眼看到自己设的换日点），实际首行 %q", first)
	}
	if !strings.Contains(first, "今日专注") {
		t.Errorf("上栏右侧应含今日专注，实际首行 %q", first)
	}

	// 昵称设了就要出现在问候语里。
	src.Config().Nickname = "张三"
	m2, _, _ := NewLoader(src, src.Config()).Build()
	m2.Resize(120, 40)
	if got := strings.Split(m2.View(), "\n")[0]; !strings.Contains(got, "张三") {
		t.Errorf("设了昵称后上栏应出现它，实际首行 %q", got)
	}
}

// TestFooterHeaderHidesEmptyNickname 验证没设昵称时不留"你好，"这种半句。
func TestFooterHeaderHidesEmptyNickname(t *testing.T) {
	src := newMemSource(t, testNow())
	src.Config().Nickname = ""
	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	first := strings.Split(m.View(), "\n")[0]
	if strings.Contains(first, "，！") || strings.Contains(first, "！！") {
		t.Errorf("没设昵称时不该出现空的称呼位，实际首行 %q", first)
	}
}

// lastLines 取末尾 n 行，便于失败信息聚焦在下栏。
func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
