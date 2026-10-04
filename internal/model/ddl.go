package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
)

// DDL（见需求 4）。
//
// 两类条目的粒度不同，这是需求明确要求的：
//   - TODO 的 DDL **只有时分**（`HH:MM`）：待办是每天重置的，所以它天然
//     指「今天 18:30」；过了这个点就表示今天已经超时。
//   - GOAL 的 DDL **只有年月日**（`YYYY-MM-DD`）：目标不随天重置，所以它
//     指「到这一天（逻辑日）结束为止」。
//
// 存字符串而不是时间戳：数据文件是给用户看和手改的（见 SKILL 的数据约定），
// `"due": "18:30"` 比一串纳秒时间戳可读得多，也不需要在里面塞用户不关心的日期。

// TimeLayout 是待办 DDL 的格式。
const TimeLayout = "15:04"

// DDLText 返回时间字符串的展示文本；空值返回空串。
func DDLText(due string) string { return strings.TrimSpace(due) }

// ParseDueTime 解析待办 DDL（“HH:MM”）。空串表示没有 DDL（返回空串、无错）。
//
// 返回的错误文案是给用户看的。
func ParseDueTime(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	// 手工按 “:” 切分，不用 time.Parse：
	// 本项目有过「用 fmt.Sscanf("%d:%d:%d") 解析 HH:MM 导致日界线永远失败」的事故，
	// 这里对分隔符与位数都做明确校验，错误信息也更好懂。
	parts, err := splitClock(s)
	if err != nil {
		return "", err
	}
	h, m := parts[0], parts[1]
	if h > 23 {
		return "", fmt.Errorf("小时需在 0-23 之间")
	}
	if m > 59 {
		return "", fmt.Errorf("分钟需在 0-59 之间")
	}
	return fmt.Sprintf("%02d:%02d", h, m), nil
}

// ParseDueDate 解析目标 DDL（“YYYY-MM-DD”）。空串表示没有 DDL。
func ParseDueDate(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	d, err := time.ParseInLocation(clock.DateFormat, s, time.Local)
	if err != nil {
		return "", fmt.Errorf("日期格式应为 YYYY-MM-DD")
	}
	return d.Format(clock.DateFormat), nil
}

// splitClock 把 “H:MM” / “HH:MM” 解析成小时与分钟。
func splitClock(s string) ([2]int, error) {
	var out [2]int
	bad := fmt.Errorf("时间格式应为 HH:MM")
	idx := strings.Index(s, ":")
	if idx <= 0 || idx == len(s)-1 {
		return out, bad
	}
	hs, ms := s[:idx], s[idx+1:]
	if len(hs) > 2 || len(ms) > 2 || len(ms) == 0 {
		return out, bad
	}
	h, err1 := atoiStrict(hs)
	m, err2 := atoiStrict(ms)
	if err1 != nil || err2 != nil {
		return out, bad
	}
	out[0], out[1] = h, m
	return out, nil
}

// atoiStrict 解析纯数字（不接受空串、符号、前导空白）。
func atoiStrict(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a digit")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

// DueAt 返回 DDL 对应的到期时刻；没有 DDL 或格式非法时返回 ok=false。
//
//   - 待办：把 “HH:MM” 落到**当前逻辑日**内。日界线为 04:00 时，凌晨 2 点
//     仍算前一天，于是 DDL 也落在那个逻辑日里——与用户对「今天」的感知一致。
//   - 目标：落到该日期逻辑日的**结束**时刻，也就是「到这天结束为止」。
//
// now / cut / loc 由调用方给出，model 层不依赖全局时间，便于测试。
func DueAt(due string, isTodo bool, now time.Time, cut time.Duration, loc *time.Location) (time.Time, bool) {
	due = strings.TrimSpace(due)
	if due == "" {
		return time.Time{}, false
	}
	if loc == nil {
		loc = time.Local
	}
	if isTodo {
		parsed, err := ParseDueTime(due)
		if err != nil {
			return time.Time{}, false
		}
		parts, _ := splitClock(parsed)
		// 日界线只决定「算哪个逻辑日」，**不能**把日界线加到钟点上：
		// 日界线为 04:00 时 DayStart 是当天 04:00，若在它上面再加 18:30
		// 就变成 22:30 了。这个 bug 是被测试抓出来的。
		day := clock.LogicalDay(now, cut)
		base, err := time.ParseInLocation(clock.DateFormat, day, loc)
		if err != nil {
			return time.Time{}, false
		}
		return base.Add(time.Duration(parts[0])*time.Hour + time.Duration(parts[1])*time.Minute), true
	}

	parsed, err := ParseDueDate(due)
	if err != nil {
		return time.Time{}, false
	}
	end, err := clock.DayEnd(parsed, cut, loc)
	if err != nil {
		return time.Time{}, false
	}
	return end, true
}

// DueState 描述 DDL 相对当前时间的状态。
type DueState int

const (
	// DueNone 表示没有设置 DDL。
	DueNone DueState = iota
	// DueOverdue 表示已经超时。
	DueOverdue
	// DueSoon 表示就快到了（阈值内）。
	DueSoon
	// DueLater 表示还早。
	DueLater
)

// DueSoonWindow 是待办「快到了」的判定窗口。
//
// 待办只到分、粒度细，2 小时是合理的提醒范围。目标只到天，用同一个窗口没有
// 意义（「今天到期」的目标可能还剩 20 小时），所以目标用 DueSoonDays 判定。
const DueSoonWindow = 2 * time.Hour

// DueSoonDays 是目标「快到了」的判定天数：今天之内到期就算快到。
const DueSoonDays = 1

// DueStatus 返回 DDL 状态与剩余时长。
//
// 剩余为负表示已经超时。没有 DDL 或格式非法时返回 DueNone，由界面按「没设 DDL」
// 处理——格式非法不该让界面崩，也不该假装有一个 DDL。
func DueStatus(due string, isTodo bool, now time.Time, cut time.Duration, loc *time.Location) (DueState, time.Duration) {
	at, ok := DueAt(due, isTodo, now, cut, loc)
	if !ok {
		return DueNone, 0
	}
	left := at.Sub(now)
	window := DueSoonWindow
	if !isTodo {
		window = DueSoonDays * 24 * time.Hour
	}
	switch {
	case left < 0:
		return DueOverdue, left
	case left <= window:
		return DueSoon, left
	default:
		return DueLater, left
	}
}

// DueCountdown 把剩余时长写成紧凑文本，例如 “2h10m”“45m”“已过 12m”。
//
// 面板很窄，这里不用 clock.HumanDuration 的 “2h 10m”（带空格更占列）。
func DueCountdown(left time.Duration) string {
	overdue := left < 0
	if overdue {
		left = -left
	}
	left = left.Round(time.Minute)
	h := int(left.Hours())
	m := int(left.Minutes()) % 60
	var body string
	switch {
	case h > 0 && m > 0:
		body = fmt.Sprintf("%dh%02dm", h, m)
	case h > 0:
		body = fmt.Sprintf("%dh", h)
	default:
		body = fmt.Sprintf("%dm", m)
	}
	if overdue {
		return "已过 " + body
	}
	return body
}

// Ddl 是「可以设 DDL 的条目」的公共接口，与 Labeled 同样是为了让界面层
// 用一套代码处理 TODO 与 GOAL。
type Ddl interface {
	// ItemTitle 返回用于展示的标题。
	ItemTitle() string
	// DueText 返回 DDL 原文（“HH:MM” 或 “YYYY-MM-DD”，空表示未设置）。
	DueText() string
	// DueIsTodo 报告这是待办（只有时分）还是目标（只有年月日）。
	DueIsTodo() bool
	// SetDue 写入 DDL 原文；空串表示清除。
	SetDue(string)
}

func (t *Todo) DueText() string { return t.Due }
func (t *Todo) DueIsTodo() bool { return true }
func (t *Todo) SetDue(s string) { t.Due = strings.TrimSpace(s) }

func (g *Goal) DueText() string { return g.Due }
func (g *Goal) DueIsTodo() bool { return false }
func (g *Goal) SetDue(s string) { g.Due = strings.TrimSpace(s) }
