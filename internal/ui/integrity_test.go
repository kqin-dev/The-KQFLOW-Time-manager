package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/kqin-dev/The-Kairos-Time-manager/internal/clock"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/model"
)

// TestDayBarLabelDistinguishesFromWorkProgress 验证底部那条是“今日已过”而不是“今日进度”。
//
// 用户看到“今日进度 68%”而自己把所有 TODO 都删了，以为是数据坏了；
// 其实那是**当天时间流逝的比例**，与待办完成度无关。标签必须说清楚，
// 而且起止时刻要能看出跨度（04:00 → 次日 04:00，而不是 04:00 → 04:00）。
func TestDayBarLabelDistinguishesFromWorkProgress(t *testing.T) {
	// 日界线 04:00，当前 20:31 → 已过 (20:31-04:00)/24h ≈ 68%
	at := time.Date(2026, 10, 3, 20, 31, 0, 0, time.Local)
	app, _, cfg := newTestApp(t, at)
	cfg.DayCutoff = "04:00"
	app.width = 120

	out := app.renderDayBar(app.width)
	flat := strings.Join(strings.Fields(stripStyles(out)), "")

	if !strings.Contains(flat, "今日已过68%") {
		t.Errorf("应显示“今日已过 68%%”，实际: %q", flat)
	}
	if strings.Contains(flat, "今日进度") {
		t.Error("不应再出现“今日进度”这种会与待办完成度混淆的说法")
	}
	if !strings.Contains(flat, "04:00→次日04:00") {
		t.Errorf("满一天的逻辑日应标出“次日”，实际: %q", flat)
	}
	// 与待办数量无关：没有 TODO 时也照样显示当天已过的比例。
	if len(app.data.Fixed)+len(app.data.Floating) != 0 {
		t.Fatal("这个用例假定没有 TODO")
	}
}

// TestDayRangeLabel 验证跨天与不跨天的逻辑日起止写法。
func TestDayRangeLabel(t *testing.T) {
	loc := time.Local
	cases := []struct {
		day  string
		cut  time.Duration
		want string
	}{
		{"2026-10-03", 4 * time.Hour, "04:00 → 次日 04:00"},
		{"2026-10-03", 0, "00:00 → 24:00"},
	}
	for _, c := range cases {
		start, err := clock.DayStart(c.day, c.cut, loc)
		if err != nil {
			t.Fatal(err)
		}
		end, err := clock.DayEnd(c.day, c.cut, loc)
		if err != nil {
			t.Fatal(err)
		}
		got := dayRangeLabel(start, end, c.cut)
		if got != c.want {
			t.Errorf("day=%s cutoff=%v：起止应显示 %q，实际 %q", c.day, c.cut, c.want, got)
		}
	}
}

// TestPruneOrphansRemovesStaleReferences 验证删除 TODO 后不再留下悬空引用。
//
// 用户报的“数据结构缺陷”：TODO 全删了，当日数据里的 activity
// 还列着三个已经不存在的条目，计时记录也还指着已删除的 ID。
func TestPruneOrphansRemovesStaleReferences(t *testing.T) {
	at := time.Date(2026, 10, 3, 20, 0, 0, 0, time.Local)
	data := model.NewDayData("2026-10-03", at)

	// 造两条计时记录：一条指向存在的 TODO，一条指向已删除的 TODO；
	// 再加一条没有引用（自由专注）的。
	kept := model.NewTodo("还在的任务", model.KindFixed, "2026-10-03", at)
	data.Fixed = []*model.Todo{kept}

	end := at
	data.Archive.Sessions = []model.Session{
		{ID: "s1", TodoRef: kept.ID, TodoName: kept.Title, Ended: &end, Elapsed: time.Minute},
		{ID: "s2", TodoRef: "todo_已删除", TodoName: "删掉的任务", Ended: &end, Elapsed: time.Minute},
		{ID: "s3", Ended: &end, Elapsed: time.Minute},
	}
	data.Activity = map[string]*model.Activity{
		"还在的任务": {Name: "还在的任务", Total: time.Minute, Sessions: 1},
		"删掉的任务": {Name: "删掉的任务", Total: time.Minute, Sessions: 1},
		"自由专注":  {Name: "自由专注", Total: time.Minute, Sessions: 1},
	}

	changed := data.PruneOrphans()
	if !changed {
		t.Fatal("应当报告发生了清理")
	}

	// activity 里指向已删除条目的项应被清掉；自由专注保留。
	if _, ok := data.Activity["删掉的任务"]; ok {
		t.Error("已删除条目的 activity 应被清理")
	}
	if _, ok := data.Activity["还在的任务"]; !ok {
		t.Error("仍存在的条目不应被清理")
	}
	if _, ok := data.Activity["自由专注"]; !ok {
		t.Error("自由专注（无 TODO 引用）不应被清理")
	}

	// session 全部保留（时长是真实发生的），只清掉失效引用。
	if len(data.Archive.Sessions) != 3 {
		t.Fatalf("计时记录不应被删除，实际剩 %d 条", len(data.Archive.Sessions))
	}
	if data.Archive.Sessions[0].TodoRef != kept.ID {
		t.Error("有效引用不应被清空")
	}
	if data.Archive.Sessions[1].TodoRef != "" {
		t.Error("失效引用应被清空")
	}
	if data.Archive.Sessions[1].TodoName != "删掉的任务" {
		t.Error("清理引用时不应丢掉任务名（历史仍要可读）")
	}

	// 再跑一次应当没有改动（幂等）。
	if data.PruneOrphans() {
		t.Error("重复清理不应再报告改动")
	}
	// activity 必须是可写的 map，别处会直接往里写。
	if data.Activity == nil {
		t.Fatal("activity 不应被置为 nil")
	}
}

// TestPruneOrphansKeepsSubtaskActivity 验证子任务的投入统计不会被误删。
func TestPruneOrphansKeepsSubtaskActivity(t *testing.T) {
	at := time.Date(2026, 10, 3, 20, 0, 0, 0, time.Local)
	data := model.NewDayData("2026-10-03", at)
	parent := model.NewTodo("父任务", model.KindFixed, "2026-10-03", at)
	parent.Tasks = []model.Task{{ID: "task_1", Title: "子任务", Status: model.StatusTodo}}
	data.Fixed = []*model.Todo{parent}
	data.Activity = map[string]*model.Activity{
		"子任务": {Name: "子任务", Total: time.Minute, Sessions: 1},
	}
	if data.PruneOrphans() {
		t.Error("子任务仍存在时不应触发清理")
	}
	if _, ok := data.Activity["子任务"]; !ok {
		t.Error("子任务的投入统计不应被清理")
	}
}

// TestPruneOrphansCleansControlChars 验证老数据里的控制字符也会被清掉。
//
// 用户数据里出现过 note 末尾带 NUL（\u0000）的情况，读进来顺手修好。
func TestPruneOrphansCleansControlChars(t *testing.T) {
	at := time.Date(2026, 10, 3, 20, 0, 0, 0, time.Local)
	data := model.NewDayData("2026-10-03", at)
	data.Note = "你好，Kairos\n\x00"
	item := model.NewTodo("带\x00控制字符", model.KindFixed, "2026-10-03", at)
	item.Tasks = []model.Task{{ID: "task_1", Title: "子\x07任务", Status: model.StatusTodo}}
	data.Fixed = []*model.Todo{item}

	if !data.PruneOrphans() {
		t.Fatal("应当报告发生了清理")
	}
	if data.Note != "你好，Kairos" {
		t.Errorf("随手记里的 NUL 应被清掉，实际 %q", data.Note)
	}
	if data.Fixed[0].Title != "带控制字符" {
		t.Errorf("条目标题里的控制字符应被清掉，实际 %q", data.Fixed[0].Title)
	}
	if data.Fixed[0].Tasks[0].Title != "子任务" {
		t.Errorf("子任务标题里的控制字符应被清掉，实际 %q", data.Fixed[0].Tasks[0].Title)
	}
	if data.PruneOrphans() {
		t.Error("重复清理不应再报告改动")
	}
}

// TestStripControlChars 验证粘贴进来的控制字符会被丢掉。
//
// 用户数据里出现了 note 末尾带 NUL（\u0000）的情况：终端粘贴偶尔会带上它，
// 落到 JSON 里既看不见又让文件变脆。
func TestStripControlChars(t *testing.T) {
	cases := []struct {
		in          string
		keepNewline bool
		want        string
	}{
		// 多行模式去掉控制字符外，还会去掉末尾的空行/空白（见 model.Sanitize）。
		{"你好，Kairos\n\x00", true, "你好，Kairos"},
		{"你好，Kairos\n\x00", false, "你好，Kairos"},
		{"a\x00b\x07c", false, "abc"},
		{"第一行\n第二行", true, "第一行\n第二行"},
		{"第一行\n第二行\n\n", true, "第一行\n第二行"},
		{"第一行\n第二行", false, "第一行第二行"},
		{"带\r回车", false, "带回车"},
		{"正常文本 with spaces", false, "正常文本 with spaces"},
	}
	for _, c := range cases {
		got := stripControlChars(c.in, c.keepNewline)
		if got != c.want {
			t.Errorf("stripControlChars(%q, %v) = %q，期望 %q", c.in, c.keepNewline, got, c.want)
		}
	}
}

// TestPasteDropsControlChars 验证粘贴路径确实会过滤掉 NUL。
func TestPasteDropsControlChars(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.Local)
	app, _, _ := newTestApp(t, at)
	app.openNote()
	app.insertEditorText("你好\x00\n世界\x07")
	if got := string(app.editor.value); strings.ContainsRune(got, 0) || strings.ContainsRune(got, 7) {
		t.Errorf("粘贴后不应残留控制字符，实际 %q", got)
	}
	if got := string(app.editor.value); got != "你好\n世界" {
		t.Errorf("粘贴结果应为 %q，实际 %q", "你好\n世界", got)
	}
}
