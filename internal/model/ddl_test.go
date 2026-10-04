package model

import (
	"testing"
	"time"
)

// TestParseDueTime 验证待办 DDL（HH:MM）的解析与校验。
func TestParseDueTime(t *testing.T) {
	ok := map[string]string{
		"18:30": "18:30",
		"9:05":  "09:05",
		"00:00": "00:00",
		"23:59": "23:59",
		" 8:00": "08:00",
		"":      "",
		"   ":   "",
	}
	for in, want := range ok {
		got, err := ParseDueTime(in)
		if err != nil {
			t.Errorf("ParseDueTime(%q) 不应报错: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseDueTime(%q) = %q，期望 %q", in, got, want)
		}
	}

	bad := []string{"24:00", "18:60", "18", "18:", ":30", "abc", "18:3a", "1:2:3", "18:300", "-1:00", "18.30"}
	for _, in := range bad {
		if got, err := ParseDueTime(in); err == nil {
			t.Errorf("ParseDueTime(%q) 应报错，实际返回 %q", in, got)
		}
	}
}

// TestParseDueDate 验证目标 DDL（YYYY-MM-DD）的解析与校验。
func TestParseDueDate(t *testing.T) {
	if got, err := ParseDueDate("2026-10-31"); err != nil || got != "2026-10-31" {
		t.Errorf("合法日期应通过，实际 %q err=%v", got, err)
	}
	if got, err := ParseDueDate(""); err != nil || got != "" {
		t.Errorf("空值表示未设置，实际 %q err=%v", got, err)
	}
	bad := []string{"2026/10/31", "26-10-31", "2026-13-01", "2026-10-32", "20261031", "abc", "18:30"}
	for _, in := range bad {
		if got, err := ParseDueDate(in); err == nil {
			t.Errorf("ParseDueDate(%q) 应报错，实际返回 %q", in, got)
		}
	}
}

// TestDueAtForTodoUsesLogicalDay 验证待办的 DDL 落在**当前逻辑日**内。
//
// 这是日界线最容易出错的地方：日界线 04:00、凌晨 2 点时「今天」还是前一天，
// 那么 DDL 18:30 也必须落在那个逻辑日里，而不是自然日。
func TestDueAtForTodoUsesLogicalDay(t *testing.T) {
	loc := time.Local
	cut := 4 * time.Hour
	// 2026-10-03 凌晨 2 点，日界线 04:00 → 逻辑日仍是 2026-10-02。
	now := time.Date(2026, 10, 3, 2, 0, 0, 0, loc)

	at, ok := DueAt("18:30", true, now, cut, loc)
	if !ok {
		t.Fatal("应能算出到期时刻")
	}
	want := time.Date(2026, 10, 2, 18, 30, 0, 0, loc)
	if !at.Equal(want) {
		t.Errorf("到期时刻应为 %v（逻辑日内），实际 %v", want, at)
	}
	// 相对现在已超时。
	state, left := DueStatus("18:30", true, now, cut, loc)
	if state != DueOverdue {
		t.Errorf("18:30 相对凌晨 2 点应已超时，实际 state=%v left=%v", state, left)
	}

	// 日界线为 0 时就是自然日。
	at2, _ := DueAt("18:30", true, now, 0, loc)
	if want2 := time.Date(2026, 10, 3, 18, 30, 0, 0, loc); !at2.Equal(want2) {
		t.Errorf("日界线 0 时应为自然日 %v，实际 %v", want2, at2)
	}
}

// TestDueAtForGoalUsesDayEnd 验证目标的 DDL 是「到该日结束」。
func TestDueAtForGoalUsesDayEnd(t *testing.T) {
	loc := time.Local
	cut := 4 * time.Hour
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, loc)

	at, ok := DueAt("2026-10-31", false, now, cut, loc)
	if !ok {
		t.Fatal("应能算出到期时刻")
	}
	// 逻辑日 10-31 从 04:00 开始，到 11-01 04:00 结束。
	want := time.Date(2026, 11, 1, 4, 0, 0, 0, loc)
	if !at.Equal(want) {
		t.Errorf("目标 DDL 应落在逻辑日结束时刻 %v，实际 %v", want, at)
	}
}

// TestDueStatusBuckets 验证超时 / 快到 / 还早三档的判定。
func TestDueStatusBuckets(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, loc)

	cases := []struct {
		due   string
		want  DueState
		isTod bool
	}{
		{"11:00", DueOverdue, true},       // 已过
		{"13:00", DueSoon, true},          // 1 小时后，在 2 小时窗口内
		{"15:00", DueLater, true},         // 3 小时后
		{"2026-10-02", DueOverdue, false}, // 昨天结束
		{"2026-10-03", DueSoon, false},    // 今天结束：目标按「当天内」算快到
		{"2026-10-04", DueLater, false},   // 明天结束（> 24 小时）
		{"2026-11-30", DueLater, false},
	}
	for _, c := range cases {
		got, _ := DueStatus(c.due, c.isTod, now, 0, loc)
		if got != c.want {
			t.Errorf("DueStatus(%q, isTodo=%v) = %v，期望 %v", c.due, c.isTod, got, c.want)
		}
	}

	// 没有 DDL 与格式非法都算 DueNone，界面按「未设置」处理。
	for _, due := range []string{"", "  ", "25:00", "abc", "2026-13-01"} {
		if got, _ := DueStatus(due, true, now, 0, loc); got != DueNone {
			t.Errorf("DueStatus(%q) 应为 DueNone，实际 %v", due, got)
		}
	}
}

// TestDueCountdown 验证倒计时文本，含超时。
func TestDueCountdown(t *testing.T) {
	cases := []struct {
		left time.Duration
		want string
	}{
		{2*time.Hour + 10*time.Minute, "2h10m"},
		{2 * time.Hour, "2h"},
		{45 * time.Minute, "45m"},
		{0, "0m"},
		{-12 * time.Minute, "已过 12m"},
		{-(time.Hour + 5*time.Minute), "已过 1h05m"},
	}
	for _, c := range cases {
		if got := DueCountdown(c.left); got != c.want {
			t.Errorf("DueCountdown(%v) = %q，期望 %q", c.left, got, c.want)
		}
	}
}

// TestTodoAndGoalImplementDdl 验证两个条目类型都实现了 Ddl 接口且粒度正确。
func TestTodoAndGoalImplementDdl(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)

	items := map[string]Ddl{
		"todo": NewTodo("写论文", KindFloating, "2026-10-03", at),
		"goal": NewGoal("跑完半程马拉松", at),
	}
	for name, item := range items {
		t.Run(name, func(t *testing.T) {
			if item.DueText() != "" {
				t.Errorf("新建条目不该带 DDL，实际 %q", item.DueText())
			}
			item.SetDue("  2026-10-31  ")
			if got := item.DueText(); got != "2026-10-31" {
				t.Errorf("SetDue 应去掉首尾空白，实际 %q", got)
			}
			item.SetDue("")
			if item.DueText() != "" {
				t.Errorf("清除后应为空，实际 %q", item.DueText())
			}
		})
	}

	if !items["todo"].DueIsTodo() {
		t.Error("Todo 应为待办粒度（只到时分）")
	}
	if items["goal"].DueIsTodo() {
		t.Error("Goal 应为目标粒度（只到年月日）")
	}
}

// TestPruneOrphansCleansDue 验证读入老数据时 DDL 的控制字符被清掉，
// 但格式非法的值刻意保留原样（不静默改写用户手打的内容）。
func TestPruneOrphansCleansDue(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	data := NewDayData("2026-10-03", at)

	dirty := NewTodo("带控制字符的 DDL", KindFloating, "2026-10-03", at)
	dirty.Due = "18:30\x00"
	odd := NewTodo("手打成非法值的 DDL", KindFloating, "2026-10-03", at)
	odd.Due = "25:00"
	data.Floating = append(data.Floating, dirty, odd)

	if !data.PruneOrphans() {
		t.Fatal("脏 DDL 应被报告为发生改动")
	}
	if dirty.Due != "18:30" {
		t.Errorf("控制字符应被清掉，实际 %q", dirty.Due)
	}
	if odd.Due != "25:00" {
		t.Errorf("格式非法的 DDL 应保留原样供用户发现，实际 %q", odd.Due)
	}
	if state, _ := DueStatus(odd.Due, true, at, 0, time.Local); state != DueNone {
		t.Error("非法 DDL 应被当作「未设置」而不是算出一个到期时间")
	}
}
