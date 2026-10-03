package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("打开数据层失败: %v", err)
	}
	return s
}

// TestDayRoundTrip 验证按日分库的读写。
func TestDayRoundTrip(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, 10, 3, 10, 0, 0, 0, time.Local)

	data, err := s.EnsureDay("2026-10-03", now)
	if err != nil {
		t.Fatal(err)
	}
	data.Fixed = append(data.Fixed, model.NewTodo("晨跑", model.KindFixed, "2026-10-03", now))
	data.Floating = append(data.Floating, model.NewTodo("写周报", model.KindFloating, "2026-10-03", now))
	if err := s.SaveDay(data); err != nil {
		t.Fatal(err)
	}

	got, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("读取到的日数据为空")
	}
	if len(got.Fixed) != 1 || got.Fixed[0].Title != "晨跑" {
		t.Errorf("固定 TODO 读取不一致: %+v", got.Fixed)
	}
	if len(got.Floating) != 1 || got.Floating[0].Title != "写周报" {
		t.Errorf("临时 TODO 读取不一致: %+v", got.Floating)
	}
	if got.Fixed[0].Kind != model.KindFixed {
		t.Errorf("固定 TODO 的 Kind 应为 fixed，实际 %q", got.Fixed[0].Kind)
	}
}

// TestDayIsolation 验证不同日期的数据互不影响（见需求 7）。
func TestDayIsolation(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	d1, err := s.EnsureDay("2026-10-03", now)
	if err != nil {
		t.Fatal(err)
	}
	d1.Floating = append(d1.Floating, model.NewTodo("今天的事", model.KindFloating, "2026-10-03", now))
	if err := s.SaveDay(d1); err != nil {
		t.Fatal(err)
	}
	// 跨月，确保落到不同文件。
	d2, err := s.EnsureDay("2026-11-04", now)
	if err != nil {
		t.Fatal(err)
	}
	d2.Floating = append(d2.Floating, model.NewTodo("下月的事", model.KindFloating, "2026-11-04", now))
	if err := s.SaveDay(d2); err != nil {
		t.Fatal(err)
	}

	back1, _ := s.Day("2026-10-03")
	back2, _ := s.Day("2026-11-04")
	if len(back1.Floating) != 1 || back1.Floating[0].Title != "今天的事" {
		t.Errorf("10-03 数据被污染: %+v", back1.Floating)
	}
	if len(back2.Floating) != 1 || back2.Floating[0].Title != "下月的事" {
		t.Errorf("11-04 数据被污染: %+v", back2.Floating)
	}

	days, err := s.Days()
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 {
		t.Errorf("应有 2 天数据，实际 %d: %v", len(days), days)
	}
}

// TestCarryFixed 验证固定 TODO 的继承（见需求 14）。
func TestCarryFixed(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	prev, _ := s.EnsureDay("2026-10-03", now)
	fixed := model.NewTodo("每日阅读", model.KindFixed, "2026-10-03", now)
	fixed.Tasks = append(fixed.Tasks, model.NewTask("读 20 页"))
	// 即使前一天已经完成，固定事项第二天也要继续出现。
	fixed.Done = true
	prev.Fixed = append(prev.Fixed, fixed)
	if err := s.SaveDay(prev); err != nil {
		t.Fatal(err)
	}

	added, err := s.CarryFixed("2026-10-03", "2026-10-04", now)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Fatalf("应继承 1 项，实际 %d", added)
	}

	today, _ := s.Day("2026-10-04")
	if len(today.Fixed) != 1 {
		t.Fatalf("今日固定 TODO 应为 1 项，实际 %d", len(today.Fixed))
	}
	got := today.Fixed[0]
	if got.Done {
		t.Error("继承过来的固定 TODO 不应是已完成状态")
	}
	if got.CarriedFrom != "2026-10-03" {
		t.Errorf("CarriedFrom 应为 2026-10-03，实际 %q", got.CarriedFrom)
	}
	if len(got.Tasks) != 1 || got.Tasks[0].Done() {
		t.Error("继承的子任务应被重置为未完成")
	}
	if got.ID == fixed.ID {
		t.Error("继承应生成新的 ID，避免与昨日数据串联")
	}

	// 重复继承不应产生重复项。
	again, err := s.CarryFixed("2026-10-03", "2026-10-04", now)
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Errorf("已继承过则不应重复添加，实际添加 %d", again)
	}
}

// TestCarryFloating 验证只继承昨日未完成的临时 TODO（见需求 14）。
func TestCarryFloating(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	prev, _ := s.EnsureDay("2026-10-03", now)
	doneItem := model.NewTodo("已完成的事", model.KindFloating, "2026-10-03", now)
	doneItem.Toggle(now)
	openItem := model.NewTodo("未完成的事", model.KindFloating, "2026-10-03", now)
	openItem.Tasks = append(openItem.Tasks, model.NewTask("子步骤"))
	prev.Floating = append(prev.Floating, doneItem, openItem)
	if err := s.SaveDay(prev); err != nil {
		t.Fatal(err)
	}

	added, err := s.CarryFloating("2026-10-03", "2026-10-04", now)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Fatalf("应继承 1 项未完成事项，实际 %d", added)
	}

	today, _ := s.Day("2026-10-04")
	if len(today.Floating) != 1 {
		t.Fatalf("今日临时 TODO 应为 1 项，实际 %d", len(today.Floating))
	}
	if today.Floating[0].Title != "未完成的事" {
		t.Errorf("继承的应是未完成事项，实际 %q", today.Floating[0].Title)
	}
}

// TestGoalsPersist 验证与日期无关的 GOAL 存储。
func TestGoalsPersist(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	g := model.NewGoal("跑完半程马拉松", now)
	g.Tasks = append(g.Tasks, model.NewTask("每周跑 3 次"))
	g.Done = true
	g.ArchivedDay = "2026-10-03"

	if err := s.SaveGoals([]model.Goal{*g}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Goals()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("应有 1 个 GOAL，实际 %d", len(got))
	}
	if got[0].Title != "跑完半程马拉松" || got[0].ArchivedDay != "2026-10-03" {
		t.Errorf("GOAL 读取不一致: %+v", got[0])
	}
	if got[0].Tag == "" {
		t.Error("GOAL 应带有稳定标签")
	}
}

// TestCorruptDataRecoversFromBackup 验证数据损坏时回退到备份（见需求 21）。
func TestCorruptDataRecoversFromBackup(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	data, _ := s.EnsureDay("2026-10-03", now)
	data.Floating = append(data.Floating, model.NewTodo("重要事项", model.KindFloating, "2026-10-03", now))
	if err := s.SaveDay(data); err != nil {
		t.Fatal(err)
	}
	// 再写一次，产生一份包含该事项的备份。
	if err := s.SaveDay(data); err != nil {
		t.Fatal(err)
	}

	// 破坏主文件。
	path := s.monthFile("2026-10-03")
	if err := os.WriteFile(path, []byte("{ 这不是合法 JSON"), 0o644); err != nil {
		t.Fatal(err)
	}

	recovered, err := s.Day("2026-10-03")
	if err != nil {
		t.Fatalf("应从备份恢复而不是报错: %v", err)
	}
	if recovered == nil {
		t.Fatal("恢复的数据为空")
	}
	if len(recovered.Floating) != 1 || recovered.Floating[0].Title != "重要事项" {
		t.Errorf("从备份恢复的内容不正确: %+v", recovered.Floating)
	}
}

// TestAtomicWriteNoLeftovers 验证写入不会留下临时文件。
func TestAtomicWriteNoLeftovers(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	data, _ := s.EnsureDay("2026-10-03", now)
	if err := s.SaveDay(data); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(s.daysDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if len(name) > 0 && name[0] == '.' {
			t.Errorf("残留了临时文件: %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(s.daysDir(), "2026-10.json")); err != nil {
		t.Errorf("期望生成 2026-10.json: %v", err)
	}
}

// TestFocusTotal 验证专注时长统计（见需求 18）。
func TestFocusTotal(t *testing.T) {
	now := time.Now()
	d := model.NewDayData("2026-10-03", now)
	end := now.Add(25 * time.Minute)
	d.Archive.Sessions = []model.Session{
		{Elapsed: 25 * time.Minute, SegmentKind: "focus", Ended: &end},
		{Elapsed: 5 * time.Minute, SegmentKind: "break", Ended: &end},
		{Elapsed: 10 * time.Minute, SegmentKind: "focus"}, // 未结束，不计入
	}
	focus, rest := d.FocusTotal()
	if focus != 25*time.Minute {
		t.Errorf("专注时长应为 25m，实际 %v", focus)
	}
	if rest != 5*time.Minute {
		t.Errorf("休息时长应为 5m，实际 %v", rest)
	}
}
