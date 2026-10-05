package kxapp

import (
	"strings"
	"testing"
	"time"

	"github.com/kqin-dev/kxflow/plugin"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// TestHistoryAggregatesDays 验证历史页的多天聚合口径。
//
// 口径必须与 2.1.0 一致（Counts / FocusTotal / Archive / Activity
// 都直接用 model 上的方法），否则两个界面的"历史"会对不上——
// 而用户在新旧界面之间来回看时，数字不一致比数字难看严重得多。
func TestHistoryAggregatesDays(t *testing.T) {
	src := newMemSource(t, testNow())

	// 造三天：昨天、前天、大前天（RecentDays 约定"最近的在后"）。
	mk := func(day string, doneTitles []string, focus time.Duration, goals int, activity map[string]time.Duration) *model.DayData {
		d := model.NewDayData(day, testNow())
		for _, title := range doneTitles {
			todo := model.NewTodo(title, model.KindFixed, day, testNow())
			todo.Done = true
			todo.Status = model.StatusDone
			d.Fixed = append(d.Fixed, todo)
		}
		d.Archive.Goals = make([]model.Goal, goals)
		// 用一条已结束的会话表达专注时长（FocusTotal 只统计已结束的会话）。
		if focus > 0 {
			end := testNow()
			f := focus
			d.Archive.Sessions = append(d.Archive.Sessions, model.Session{
				ID: "s_" + day, Started: testNow().Add(-focus), Ended: &end,
				Elapsed: focus, Focus: &f,
			})
		}
		if len(activity) > 0 {
			d.Activity = map[string]*model.Activity{}
			for name, dur := range activity {
				d.Activity[name] = &model.Activity{Name: name, Total: dur, Sessions: 1}
			}
		}
		return d
	}

	src.extraDays = []*model.DayData{
		mk("2026-10-01", []string{"a"}, 30*time.Minute, 1, map[string]time.Duration{"写文档": 30 * time.Minute}),
		mk("2026-10-02", []string{"b", "c"}, 60*time.Minute, 0, map[string]time.Duration{"读论文": 40 * time.Minute, "写文档": 20 * time.Minute}),
		mk("2026-10-03", nil, 0, 2, nil),
	}

	stats, err := collectHistory(src, HistoryDays)
	if err != nil {
		t.Fatalf("不该报错：%v", err)
	}
	if len(stats) != 3 {
		t.Fatalf("应有 3 天，实际 %d", len(stats))
	}

	// 第一天：1 项完成、专注 30 分、1 个归档目标、最投入"写文档"。
	if stats[0].Day != "2026-10-01" || stats[0].Done != 1 || stats[0].Total != 1 {
		t.Errorf("第一天统计不对：%+v", stats[0])
	}
	if stats[0].Focus != 30*time.Minute {
		t.Errorf("第一天专注应为 30m，实际 %v", stats[0].Focus)
	}
	if stats[0].Goals != 1 {
		t.Errorf("第一天归档目标应为 1，实际 %d", stats[0].Goals)
	}
	if stats[0].TopItem != "写文档" {
		t.Errorf("第一天最投入应为「写文档」，实际 %q", stats[0].TopItem)
	}

	// 第二天：最投入应当是"读论文"（40 分 > 写文档 20 分）。
	if stats[1].TopItem != "读论文" || stats[1].TopAmount != 40*time.Minute {
		t.Errorf("第二天最投入应为「读论文」40m，实际 %q %v", stats[1].TopItem, stats[1].TopAmount)
	}

	// 第三天没有内容：不该有最投入，但也要出现在列表里（"那天没干活"
	// 本身就是信息，跳过它会让历史看起来少一天）。
	if stats[2].Day != "2026-10-03" {
		t.Errorf("第三天应当出现，实际 %+v", stats[2])
	}
	if stats[2].TopItem != "" {
		t.Errorf("没有活动时不该有最投入，实际 %q", stats[2].TopItem)
	}
}

// TestHistoryTieBreaksByName 验证"最投入"并列时结果稳定。
//
// 并列时若顺序随机，同一个界面每次打开都可能不一样，
// 用户会以为数据变了。
func TestHistoryTieBreaksByName(t *testing.T) {
	src := newMemSource(t, testNow())
	d := model.NewDayData("2026-10-03", testNow())
	d.Activity = map[string]*model.Activity{
		"乙": {Name: "乙", Total: 10 * time.Minute},
		"甲": {Name: "甲", Total: 10 * time.Minute},
	}
	src.extraDays = []*model.DayData{d}

	// ⚠️ 期望值是「乙」而不是「甲」——因为按码位 **乙(U+4E59) < 甲(U+7532)**。
	// 我第一版把期望写成了「甲」，测试因此"红得莫名其妙"；
	// 这条注释留着，免得下次又按字形顺序猜。
	const wantTop = "乙"

	// 多跑几次：结果必须每次都一样。
	for i := 0; i < 5; i++ {
		stats, err := collectHistory(src, HistoryDays)
		if err != nil {
			t.Fatalf("不该报错：%v", err)
		}
		if len(stats) != 1 {
			t.Fatalf("应有 1 天，实际 %d", len(stats))
		}
		if stats[0].TopItem != wantTop {
			t.Fatalf("并列时应取码位最小的那个（%q），第 %d 次得到 %q",
				wantTop, i, stats[0].TopItem)
		}
	}
}

// TestHistoryViewRenders 验证历史页能渲染出表格与合计。
func TestHistoryViewRenders(t *testing.T) {
	src := newMemSource(t, testNow())
	src.addTodo("今天的待办", model.KindFixed)

	l := NewLoader(src, src.Config())
	m, _, rep := l.Build()
	if !containsStr(rep.Loaded, HistoryPackID) {
		t.Fatalf("历史包应被装载，实际 %v", rep.Loaded)
	}
	m.Resize(120, 40)

	if !openMenuOptionOK(t, m, "历史") {
		t.Fatalf("菜单里应有历史，实际 %v", m.Options())
	}
	out := m.View()
	for _, want := range []string{"历史", "日期", "TODO", "专注", "GOAL", "合计"} {
		if !strings.Contains(out, want) {
			t.Errorf("历史页应有 %q：\n%s", want, out)
		}
	}
	if !m.CanvasClean() {
		t.Errorf("画布诊断不干净：%s", m.Diagnostics())
	}
	// esc 返回。
	m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
}

// TestHistoryEmptyExplains 验证没有历史数据时**说明原因**而不是空白。
func TestHistoryEmptyExplains(t *testing.T) {
	src := newMemSource(t, testNow())
	src.extraDays = []*model.DayData{} // 明确"一天都没有"

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	m.Resize(120, 40)
	if !openMenuOptionOK(t, m, "历史") {
		t.Fatal("菜单里应有历史")
	}
	out := m.View()
	if !strings.Contains(out, "还没有历史数据") {
		t.Errorf("空历史应说明原因：\n%s", out)
	}
}

// TestHistoryNeverOverflows 在尺寸网格上断言历史页不溢出。
//
// 表格是最容易撑破边框的东西（列宽是拼出来的），因此单独扫一遍。
func TestHistoryNeverOverflows(t *testing.T) {
	src := newMemSource(t, testNow())
	// 造一天数据，且"最投入"的名字很长（最容易撑破）。
	d := model.NewDayData("2026-10-03", testNow())
	d.Activity = map[string]*model.Activity{
		"一个非常非常长的条目名字用来试探表格会不会撑破边框": {Name: "一个非常非常长的条目名字用来试探表格会不会撑破边框", Total: 90 * time.Minute},
	}
	end := testNow()
	f := 90 * time.Minute
	d.Archive.Sessions = append(d.Archive.Sessions, model.Session{
		ID: "s1", Started: testNow().Add(-f), Ended: &end, Elapsed: f, Focus: &f,
	})
	src.extraDays = []*model.DayData{d}

	l := NewLoader(src, src.Config())
	m, _, _ := l.Build()
	for _, size := range []struct{ w, h int }{{120, 40}, {100, 30}, {80, 24}, {60, 16}, {40, 12}} {
		m.Resize(size.w, size.h)
		if !openMenuOptionOK(t, m, "历史") {
			t.Fatalf("%dx%d：菜单里应有历史", size.w, size.h)
		}
		// 找到历史那一层（菜单之上）。
		if !m.CanvasClean() {
			t.Errorf("%dx%d：画布诊断不干净：%s", size.w, size.h, m.Diagnostics())
		}
		// 退回去准备下一轮。
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
		m.Dispatch(plugin.Event{Kind: plugin.EventKey, Key: "esc"})
	}
}
