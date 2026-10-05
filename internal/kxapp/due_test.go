package kxapp

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestHumanDue 固化"距截止还有多久"的显示口径。
//
// 这条测试来自一个真实观感问题：把 31 天后的目标显示成「751h 50m」。
// 数字没错，但用户在界面上看到"751 小时"必须自己做除法才知道是"一个月后"——
// 那等于没显示。因此这里把分级规则逐个钉住。
func TestHumanDue(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "即将到期"},
		{30 * time.Second, "即将到期"},
		{59 * time.Second, "即将到期"},
		{time.Minute, "1m"},
		{23 * time.Minute, "23m"},
		{59 * time.Minute, "59m"},
		{time.Hour, "1h"},
		{2*time.Hour + 30*time.Minute, "2h30m"},
		{23*time.Hour + 59*time.Minute, "23h59m"},
		{24 * time.Hour, "1天"},
		{25 * time.Hour, "1天1h"},
		{48 * time.Hour, "2天"},
		{31*24*time.Hour + 7*time.Hour, "31天7h"},
	}
	for _, c := range cases {
		if got := humanDue(c.d); got != c.want {
			t.Errorf("humanDue(%v) = %q，期望 %q", c.d, got, c.want)
		}
	}
	// 超时（负时长）显示"超了多久"，与正数同口径——"超了 2 小时"比 "-2h" 好懂。
	if got := humanDue(-2 * time.Hour); got != "2h" {
		t.Errorf("超时应取绝对值显示，实际 %q", got)
	}
	if got := humanDue(-31 * 24 * time.Hour); got != "31天" {
		t.Errorf("超时很久时应显示天数，实际 %q", got)
	}
}

// TestHumanDueUsesDaysBeyondOneDay 是那个观感问题的专门墓碑。
//
// 断言"满一天之后就不再以小时为主单位"：这是"能不能一眼读懂"的界线。
// 曾经 31 天被显示成 751h，测试要挡住它再回来。
func TestHumanDueUsesDaysBeyondOneDay(t *testing.T) {
	for days := 1; days <= 60; days++ {
		d := time.Duration(days) * 24 * time.Hour
		got := humanDue(d)
		if !strings.HasPrefix(got, strconv.Itoa(days)+"天") {
			t.Errorf("%v（%d 天）应以 %q 开头，实际 %q", d, days, strconv.Itoa(days)+"天", got)
		}
		// 天之上的单位不应再出现"百小时"这种量级。
		if strings.Contains(got, "h") {
			// 带小时是允许的（例如 1天3h），但小时数必须在 0~23。
			idx := strings.Index(got, "天")
			hourPart := strings.TrimSuffix(got[idx+len("天"):], "h")
			if hourPart == "" {
				t.Errorf("%d 天：出现 h 但小时部分为空：%q", days, got)
				continue
			}
			h, err := strconv.Atoi(hourPart)
			if err != nil || h < 0 || h > 23 {
				t.Errorf("%d 天：小时部分应在 0~23，实际 %q", days, got)
			}
		}
	}
}
