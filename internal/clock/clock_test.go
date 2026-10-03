package clock

import (
	"testing"
	"time"
)

// TestLogicalDayCutoff 验证日界线对“今天是第几日”的影响（见需求 19）。
func TestLogicalDayCutoff(t *testing.T) {
	loc := time.Local
	cases := []struct {
		name string
		at   string
		cut  time.Duration
		want string
	}{
		{"午夜日界线_白天属于当天", "2026-10-03 15:04:05", 0, "2026-10-03"},
		{"午夜日界线_凌晨属于当天", "2026-10-03 02:00:00", 0, "2026-10-03"},
		{"四点日界线_凌晨属于前一天", "2026-10-03 02:00:00", 4 * time.Hour, "2026-10-02"},
		{"四点日界线_三点五十九仍属前一天", "2026-10-03 03:59:59", 4 * time.Hour, "2026-10-02"},
		{"四点日界线_四点整属于新一天", "2026-10-03 04:00:00", 4 * time.Hour, "2026-10-03"},
		{"四点日界线_白天属于当天", "2026-10-03 12:00:00", 4 * time.Hour, "2026-10-03"},
		{"六点日界线_跨月属于上月最后一天", "2026-11-01 01:00:00", 6 * time.Hour, "2026-10-31"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at, err := time.ParseInLocation("2006-01-02 15:04:05", c.at, loc)
			if err != nil {
				t.Fatalf("解析时间失败: %v", err)
			}
			if got := LogicalDay(at, c.cut); got != c.want {
				t.Errorf("LogicalDay(%s, %v) = %s, 期望 %s", c.at, c.cut, got, c.want)
			}
		})
	}
}

// TestPrevDay 验证跨月、跨年的前一天计算。
func TestPrevDay(t *testing.T) {
	cases := map[string]string{
		"2026-10-03": "2026-10-02",
		"2026-11-01": "2026-10-31",
		"2026-01-01": "2025-12-31",
		"2024-03-01": "2024-02-29", // 闰年
	}
	for day, want := range cases {
		got, err := PrevDay(day, time.Local)
		if err != nil {
			t.Fatalf("PrevDay(%s) 出错: %v", day, err)
		}
		if got != want {
			t.Errorf("PrevDay(%s) = %s, 期望 %s", day, got, want)
		}
	}
}

// TestDayStartEnd 验证逻辑日的起止时刻。
func TestDayStartEnd(t *testing.T) {
	start, err := DayStart("2026-10-03", 4*time.Hour, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	if start.Hour() != 4 || start.Day() != 3 {
		t.Errorf("DayStart = %v, 期望 2026-10-03 04:00", start)
	}
	end, err := DayEnd("2026-10-03", 4*time.Hour, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	if end.Day() != 4 || end.Hour() != 4 {
		t.Errorf("DayEnd = %v, 期望 2026-10-04 04:00", end)
	}
}

// TestGreeting 验证问候语随时段变化。
func TestGreeting(t *testing.T) {
	at := func(h int) time.Time {
		return time.Date(2026, 10, 3, h, 0, 0, 0, time.Local)
	}
	cases := map[int]string{
		7:  "Good morning",
		12: "Good noon",
		15: "Good afternoon",
		20: "Good evening",
		23: "Good night",
		3:  "Good night",
	}
	for hour, want := range cases {
		if got := Greeting(at(hour), 0); got != want {
			t.Errorf("Greeting(%d 点) = %s, 期望 %s", hour, got, want)
		}
	}
}

// TestHumanDuration 验证时长格式化。
func TestHumanDuration(t *testing.T) {
	cases := map[time.Duration]string{
		45 * time.Minute:            "45m",
		time.Hour:                   "1h",
		2*time.Hour + 5*time.Minute: "2h 05m",
		90 * time.Second:            "2m",
		0:                           "0m",
		-5 * time.Minute:            "0m",
	}
	for d, want := range cases {
		if got := HumanDuration(d); got != want {
			t.Errorf("HumanDuration(%v) = %s, 期望 %s", d, got, want)
		}
	}
}

// TestClockString 验证计时显示格式。
func TestClockString(t *testing.T) {
	cases := map[time.Duration]string{
		25 * time.Minute:                          "25:00",
		5*time.Minute + 7*time.Second:             "05:07",
		time.Hour + 2*time.Minute + 3*time.Second: "1:02:03",
	}
	for d, want := range cases {
		if got := ClockString(d); got != want {
			t.Errorf("ClockString(%v) = %s, 期望 %s", d, got, want)
		}
	}
}
