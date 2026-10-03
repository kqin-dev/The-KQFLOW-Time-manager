// Package clock 负责“逻辑日”的计算。
//
// 需求 19：用户可以设定一天从几点开始。默认 00:00，对熬夜用户可以设为 04:00，
// 于是凌晨 2 点仍然算作前一天。所有“今天是第几日”的判断都经过这里。
package clock

import (
	"fmt"
	"time"
)

// DateFormat 是数据库中使用的日期格式。
const DateFormat = "2006-01-02"

// Clock 提供当前时间与逻辑日。
type Clock struct {
	// now 可被测试替换；生产代码中为 time.Now。
	now func() time.Time
	// loc 是与显示相关的本地时区。
	loc *time.Location
}

// New 创建一个使用系统时间的时钟。
func New() *Clock { return &Clock{now: time.Now, loc: time.Local} }

// NewWith 用给定的取时函数与地点创建时钟，便于测试。
func NewWith(now func() time.Time, loc *time.Location) *Clock {
	if loc == nil {
		loc = time.Local
	}
	return &Clock{now: now, loc: loc}
}

// Now 返回当前时间。
func (c *Clock) Now() time.Time { return c.now() }

// Location 返回该时钟使用的时区。
func (c *Clock) Location() *time.Location {
	if c.loc == nil {
		return time.Local
	}
	return c.loc
}

// LogicalDay 按日界线返回某个时刻所属的逻辑日。
//
// 日界线 cut 表示新的一天从当地时间几点开始；cut 为 0 表示午夜换日。
// 若时刻早于当日日界线，则它属于前一天。
func LogicalDay(t time.Time, cut time.Duration) string {
	if cut <= 0 {
		return t.Format(DateFormat)
	}
	shifted := t.Add(-cut)
	return shifted.Format(DateFormat)
}

// Today 返回当前的逻辑日。
func (c *Clock) Today(cut time.Duration) string { return LogicalDay(c.Now(), cut) }

// DayStart 返回某个逻辑日的开始时刻（当地时间）。
func DayStart(day string, cut time.Duration, loc *time.Location) (time.Time, error) {
	d, err := time.ParseInLocation(DateFormat, day, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("解析日期 %q 失败: %w", day, err)
	}
	return d.Add(cut), nil
}

// DayEnd 返回某个逻辑日的结束时刻，即下一日的开始。
func DayEnd(day string, cut time.Duration, loc *time.Location) (time.Time, error) {
	start, err := DayStart(day, cut, loc)
	if err != nil {
		return time.Time{}, err
	}
	return start.AddDate(0, 0, 1), nil
}

// PrevDay 返回给定逻辑日的前一天。
func PrevDay(day string, loc *time.Location) (string, error) {
	d, err := time.ParseInLocation(DateFormat, day, loc)
	if err != nil {
		return "", fmt.Errorf("解析日期 %q 失败: %w", day, err)
	}
	return d.AddDate(0, 0, -1).Format(DateFormat), nil
}

// Weekday 返回逻辑日对应的星期，用于看板标题。
func Weekday(day string, loc *time.Location) (time.Weekday, error) {
	d, err := time.ParseInLocation(DateFormat, day, loc)
	if err != nil {
		return time.Sunday, fmt.Errorf("解析日期 %q 失败: %w", day, err)
	}
	return d.Weekday(), nil
}

// Greeting 按当前时刻返回问候语（见需求 18）。
//
// 问候语使用“逻辑日 + 日界线”判断，凌晨 2 点且日界线为 04:00 时仍算夜晚，
// 与用户的作息保持一致。
func Greeting(t time.Time, cut time.Duration) string {
	// 用日界线把时刻平移到“自然作息”上：平移后的小时数更贴近用户的主观时段。
	local := t
	if cut > 0 {
		local = t.Add(-cut)
	}
	h := local.Hour()
	switch {
	case h < 5:
		return "Good night"
	case h < 11:
		return "Good morning"
	case h < 14:
		return "Good noon"
	case h < 18:
		return "Good afternoon"
	case h < 22:
		return "Good evening"
	default:
		return "Good night"
	}
}

// HumanDuration 把时长格式化成 “2h 05m” 或 “45m” 这样便于阅读的形式。
func HumanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %02dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

// WallClock 把日界线时长格式化成 “HH:MM”，用于看板展示。
func WallClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%02d:%02d", h, m)
}

// TimeOfDay 返回某个时刻在当天内的 “HH:MM”；一天结束的 24:00 会显示成 24:00，
// 而不是绕回 00:00，避免看板出现 “00:00 → 00:00” 这种看不出跨度的写法。
func TimeOfDay(t time.Time) string {
	return fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())
}

// DayEndLabel 返回逻辑日结束时刻的展示文本。
func DayEndLabel(t time.Time) string {
	if t.Hour() == 0 && t.Minute() == 0 {
		return "24:00"
	}
	return TimeOfDay(t)
}

// ClockString 把时长格式化成始终带秒的形式，用于计时器显示。
func ClockString(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
