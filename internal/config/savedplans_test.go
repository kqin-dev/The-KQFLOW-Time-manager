package config

import (
	"testing"
	"time"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// plan 造一套两段的方案。
func plan(name string, first, second time.Duration) model.Plan {
	return model.Plan{
		Kind:  model.TimerCustom,
		Label: name,
		Segments: []model.Segment{
			{Name: "深度工作", Kind: model.SegmentKindFocus, Dur: first},
			{Name: "休息", Kind: model.SegmentKindBreak, Dur: second},
		},
	}
}

// TestAddSavedPlan 验证收藏：空名自动生成名字，同名覆盖。
func TestAddSavedPlan(t *testing.T) {
	cfg := Default()

	// 没给名字时用内容生成一个。
	label := cfg.AddSavedPlan(plan("", 25*time.Minute, 5*time.Minute))
	if label == "" {
		t.Fatal("应生成默认名字，实际为空")
	}
	list := cfg.SavedPlanList()
	if len(list) != 1 || list[0].Label != label {
		t.Fatalf("应有 1 套收藏且名字为 %q，实际 %+v", label, list)
	}
	if list[0].Segments[0].Dur != 25*time.Minute {
		t.Errorf("段时长应保留，实际 %v", list[0].Segments[0].Dur)
	}

	// 同名覆盖，不该变成两套。
	cfg.AddSavedPlan(plan(label, 50*time.Minute, 10*time.Minute))
	list = cfg.SavedPlanList()
	if len(list) != 1 {
		t.Fatalf("同名应覆盖，实际 %d 套", len(list))
	}
	if list[0].Segments[0].Dur != 50*time.Minute {
		t.Errorf("覆盖后应取新时长，实际 %v", list[0].Segments[0].Dur)
	}

	// 换个名字应新增，且顺序按保存先后。
	cfg.AddSavedPlan(plan("深度两段", 90*time.Minute, 15*time.Minute))
	list = cfg.SavedPlanList()
	if len(list) != 2 {
		t.Fatalf("应有 2 套收藏，实际 %d", len(list))
	}
	if list[0].Label != label || list[1].Label != "深度两段" {
		t.Errorf("顺序应与保存顺序一致，实际 %q / %q", list[0].Label, list[1].Label)
	}
}

// TestAddSavedPlanRejectsInvalid 验证非法方案被拒（不写进收藏）。
func TestAddSavedPlanRejectsInvalid(t *testing.T) {
	cfg := Default()

	cases := map[string]model.Plan{
		"没有段":    {Kind: model.TimerCustom},
		"时长为 0":  {Kind: model.TimerCustom, Segments: []model.Segment{{Name: "专注"}}},
		"缺少名字":   {Kind: model.TimerCustom, Segments: []model.Segment{{Dur: time.Minute}}},
		"时长是负数":  {Kind: model.TimerCustom, Segments: []model.Segment{{Name: "专注", Dur: -time.Minute}}},
		"只有空白名字": {Kind: model.TimerCustom, Segments: []model.Segment{{Name: "   ", Dur: time.Minute}}},
	}
	for name, p := range cases {
		if label := cfg.AddSavedPlan(p); label != "" {
			t.Errorf("%s：应被拒绝，实际返回 %q", name, label)
		}
	}
	if len(cfg.SavedPlanList()) != 0 {
		t.Errorf("非法方案不该写进收藏，实际 %+v", cfg.SavedPlanList())
	}
}

// TestSavedPlanListCleansDirtyData 验证手改过的配置里的脏数据不会进到计时逻辑。
func TestSavedPlanListCleansDirtyData(t *testing.T) {
	cfg := Default()
	cfg.SavedPlans = []model.Plan{
		plan("好的方案", 25*time.Minute, 5*time.Minute),
		{Kind: model.TimerCustom},                                          // 没有段
		{Kind: model.TimerCustom, Segments: []model.Segment{{Name: "专注"}}}, // 时长为 0
		plan("", 10*time.Minute, 2*time.Minute),                            // 没名字 → 自动生成
	}
	list := cfg.SavedPlanList()
	if len(list) != 2 {
		t.Fatalf("脏数据应被过滤掉，只剩 2 套，实际 %d: %+v", len(list), list)
	}
	if list[0].Label != "好的方案" {
		t.Errorf("第一套应保留原名，实际 %q", list[0].Label)
	}
	if list[1].Label == "" {
		t.Error("没名字的方案应自动补一个名字")
	}
}

// TestClonePlanIsIndependent 验证「当模板改」不会改到收藏里的原件。
//
// 这是本需求最容易踩的坑：Segments 是切片，浅拷贝会让两边共享底层数组。
func TestClonePlanIsIndependent(t *testing.T) {
	cfg := Default()
	cfg.AddSavedPlan(plan("模板", 25*time.Minute, 5*time.Minute))

	template := model.ClonePlan(cfg.SavedPlanList()[0])
	template.Segments[0].Dur = 99 * time.Minute
	template.Segments[0].Name = "改过的名字"

	list := cfg.SavedPlanList()
	if list[0].Segments[0].Dur != 25*time.Minute {
		t.Errorf("改模板不该影响收藏，实际 %v", list[0].Segments[0].Dur)
	}
	if list[0].Segments[0].Name != "深度工作" {
		t.Errorf("改模板不该影响收藏里的名字，实际 %q", list[0].Segments[0].Name)
	}
}

// TestRemoveSavedPlan 验证删除。
func TestRemoveSavedPlan(t *testing.T) {
	cfg := Default()
	cfg.AddSavedPlan(plan("甲", 25*time.Minute, 5*time.Minute))
	cfg.AddSavedPlan(plan("乙", 50*time.Minute, 10*time.Minute))

	if !cfg.RemoveSavedPlan("甲") {
		t.Fatal("删除应成功")
	}
	list := cfg.SavedPlanList()
	if len(list) != 1 || list[0].Label != "乙" {
		t.Fatalf("应只剩乙，实际 %+v", list)
	}
	if cfg.RemoveSavedPlan("不存在") {
		t.Error("删除不存在的方案应返回 false")
	}
}

// TestAutoPlanLabel 验证默认名字可读且不超长。
func TestAutoPlanLabel(t *testing.T) {
	two := plan("", 25*time.Minute, 5*time.Minute)
	if got := model.AutoPlanLabel(two); got != "深度工作 + 1 段" {
		t.Errorf("两段方案的名字应为「深度工作 + 1 段」，实际 %q", got)
	}

	one := model.Plan{Segments: []model.Segment{{Name: "长专注", Dur: time.Minute}}}
	if got := model.AutoPlanLabel(one); got != "长专注" {
		t.Errorf("单段方案直接用段名，实际 %q", got)
	}

	// 超长段名要被截断，不能让「+ N 段」把它顶爆。
	long := model.Plan{Segments: []model.Segment{
		{Name: "一个非常非常非常非常非常长的时段名字", Dur: time.Minute},
		{Name: "休息", Dur: time.Minute},
	}}
	if n := len([]rune(model.AutoPlanLabel(long))); n > model.MaxPlanLabelRunes {
		t.Errorf("默认名字不应超过 %d 字符，实际 %d：%q",
			model.MaxPlanLabelRunes, n, model.AutoPlanLabel(long))
	}
	if got := model.AutoPlanLabel(model.Plan{}); got != "空方案" {
		t.Errorf("空方案应给出可读名字，实际 %q", got)
	}
}
