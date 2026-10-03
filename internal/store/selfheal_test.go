package store_test

import (
	"testing"
	"time"

	"github.com/kqin-dev/The-Kairos-Time-manager/internal/model"
	"github.com/kqin-dev/The-Kairos-Time-manager/internal/store"
)

// TestSelfHealRoundTrip 验证“读入 → 自愈 → 写回 → 再读入”的数据是干净的。
//
// 复刻用户报的现象：TODO 全删了，但当日数据里仍留着指向它们的
// 计时记录引用和 activity 条目，看起来自相矛盾。
func TestSelfHealRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 3, 20, 0, 0, 0, time.Local)
	day := "2026-10-03"

	data := model.NewDayData(day, at)
	// 两条指向已删除条目的计时记录、一条自由专注。
	end := at
	data.Archive.Sessions = []model.Session{
		{ID: "s1", TodoRef: "todo_gone_a", TodoName: "删掉的任务A", Ended: &end, Elapsed: time.Minute, SegmentKind: "focus"},
		{ID: "s2", TodoRef: "todo_gone_b", TodoName: "删掉的任务B", Ended: &end, Elapsed: 2 * time.Minute, SegmentKind: "focus"},
		{ID: "s3", Ended: &end, Elapsed: 3 * time.Minute, SegmentKind: "focus"},
	}
	data.Activity = map[string]*model.Activity{
		"删掉的任务A": {Name: "删掉的任务A", Total: time.Minute, Sessions: 1},
		"删掉的任务B": {Name: "删掉的任务B", Total: 2 * time.Minute, Sessions: 1},
		"自由专注":   {Name: "自由专注", Total: 3 * time.Minute, Sessions: 1},
	}
	data.Note = "残留控制字符\x00"
	if err := st.SaveDay(data); err != nil {
		t.Fatal(err)
	}

	// 重新读入并自愈。
	got, err := st.Day(day)
	if err != nil {
		t.Fatal(err)
	}
	if !got.PruneOrphans() {
		t.Fatal("读入后应当检测到需要清理")
	}
	if err := st.SaveDay(got); err != nil {
		t.Fatal(err)
	}

	// 再读一次，确认落盘后的数据是干净的。
	final, err := st.Day(day)
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Activity) != 1 {
		t.Errorf("activity 应只剩“自由专注”，实际 %v", keysOf(final.Activity))
	}
	if _, ok := final.Activity["自由专注"]; !ok {
		t.Error("“自由专注”不该被清理")
	}
	if len(final.Archive.Sessions) != 3 {
		t.Errorf("计时记录不应被删除，实际 %d 条", len(final.Archive.Sessions))
	}
	for i, s := range final.Archive.Sessions {
		if s.TodoRef != "" {
			t.Errorf("第 %d 条记录的失效引用应被清空，实际 %q", i, s.TodoRef)
		}
	}
	if final.Note != "残留控制字符" {
		t.Errorf("随手记里的控制字符应被清掉，实际 %q", final.Note)
	}
	// 专注时长不受影响（历史不能被篡改）。
	focus, _ := final.FocusTotal()
	if focus != 6*time.Minute {
		t.Errorf("专注总时长应为 6m，实际 %s", focus)
	}
	if final.PruneOrphans() {
		t.Error("已经干净的数据不应再报告改动")
	}
}

func keysOf(m map[string]*model.Activity) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
