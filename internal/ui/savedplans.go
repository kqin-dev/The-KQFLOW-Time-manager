package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/config"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// 收藏的自定义专注方案（见需求 2）。
//
// 两条用法，都是需求里点名的：
//   - 「直接调用」：从计时菜单选一套收藏，直接开始计时。
//   - 「调出来作为模板改」：选「载入为模板」，把方案装进自定义时段编辑器，
//     改完再开始；也可以顺手用 s 存成新的收藏。
//
// 动作串用**下标**而不是方案名：方案名是用户随便起的，可能带冒号等字符，
// 拼进 action 串里再切分迟早出错（本项目在日界线解析上踩过同类坑）。

// savedPlans 返回当前收藏的方案（已校验、去重）。
func (a *App) savedPlans() []model.Plan {
	if a.cfg == nil {
		return nil
	}
	return a.cfg.SavedPlanList()
}

// savedPlanLabel 返回方案在菜单里的展示名。
func savedPlanLabel(p model.Plan, index int) string {
	label := p.Label
	if strings.TrimSpace(label) == "" {
		label = model.AutoPlanLabel(p)
	}
	return fmt.Sprintf("%s（%d 段 · %s）", label, len(p.Segments), totalDurText(p))
}

// totalDurText 把方案总时长写成便于扫读的文本。
func totalDurText(p model.Plan) string {
	total := p.Total()
	if total <= 0 {
		return "无终点"
	}
	h := int(total.Hours())
	m := int(total.Minutes()) % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%02dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

// openSavedPlans 打开「收藏的方案」菜单。
func (a *App) openSavedPlans() {
	plans := a.savedPlans()
	if len(plans) == 0 {
		a.setToast("还没有收藏的方案：自定义时段里按 s 收藏", toastWarn)
		return
	}
	items := make([]pickItem, 0, len(plans)*2+1)
	for i, p := range plans {
		index := strconv.Itoa(i)
		items = append(items,
			pickItem{Label: "▶ 开始 " + savedPlanLabel(p, i), Action: "saved_start:" + index},
			pickItem{Label: "✎ 载入为模板 " + savedPlanLabel(p, i), Action: "saved_edit:" + index},
		)
	}
	items = append(items, pickItem{Label: "管理收藏（删除）", Action: "saved_manage"})
	items = append(items, pickItem{Label: "取消", Action: "cancel"})
	a.pick = &pickState{
		title: "收藏的方案",
		items: items,
		// 两个以上的选项，禁用 y/n 快捷键，避免误触（见 pitfalls）。
		small: true,
	}
}

// openSavedPlanManager 打开收藏管理页（目前只支持删除）。
func (a *App) openSavedPlanManager() {
	plans := a.savedPlans()
	if len(plans) == 0 {
		a.setToast("没有可管理的收藏", toastWarn)
		return
	}
	items := make([]pickItem, 0, len(plans)+1)
	for i, p := range plans {
		items = append(items, pickItem{
			Label:  "删除 " + savedPlanLabel(p, i),
			Action: "saved_del:" + strconv.Itoa(i),
		})
	}
	items = append(items, pickItem{Label: "返回", Action: "cancel"})
	a.pick = &pickState{title: "管理收藏的方案", items: items, small: true}
}

// startSavedPlan 直接开始一套收藏方案。
func (a *App) startSavedPlan(index int) (tea.Model, tea.Cmd) {
	plans := a.savedPlans()
	if index < 0 || index >= len(plans) {
		a.setToast("这套收藏已经不存在了", toastWarn)
		return a, nil
	}
	// 深拷贝：直接把收藏里的方案交给计时器，等于把原件交出去了。
	plan := model.ClonePlan(plans[index])
	plan.Kind = model.TimerCustom
	plan.Label = ""
	a.chooseTimerTodo(plan)
	return a, nil
}

// loadSavedPlanIntoTemplate 把收藏方案载入自定义时段编辑器当模板改。
func (a *App) loadSavedPlanIntoTemplate(index int) (tea.Model, tea.Cmd) {
	plans := a.savedPlans()
	if index < 0 || index >= len(plans) {
		a.setToast("这套收藏已经不存在了", toastWarn)
		return a, nil
	}
	if a.refuseSecondTimer() {
		return a, nil
	}
	// 模板必须是副本：否则用户在这里改一段时长，收藏里的原件也跟着变了。
	plan := model.ClonePlan(plans[index])
	plan.Kind = model.TimerCustom
	plan.Label = ""
	a.custom = &customState{plan: plan}
	label := plans[index].Label
	if label == "" {
		label = model.AutoPlanLabel(plans[index])
	}
	a.setToast(fmt.Sprintf("已载入「%s」作为模板，改完按 enter 开始", label), toastInfo)
	return a, nil
}

// deleteSavedPlan 删除一套收藏。
func (a *App) deleteSavedPlan(index int) (tea.Model, tea.Cmd) {
	plans := a.savedPlans()
	if index < 0 || index >= len(plans) {
		a.setToast("这套收藏已经不存在了", toastWarn)
		return a, nil
	}
	label := plans[index].Label
	if !a.cfg.RemoveSavedPlan(label) {
		a.setToast("删除失败：找不到这套收藏", toastErr)
		return a, nil
	}
	a.setToast(fmt.Sprintf("已删除收藏「%s」", label), toastInfo)
	paths := a.pathsForSave()
	cfg := a.cfg
	return a, func() tea.Msg {
		if err := config.Save(paths, cfg); err != nil {
			return savedMsg{err: err}
		}
		return savedMsg{}
	}
}

// savedPlanActions 处理「收藏的方案」相关的动作串。
//
// 单独抽出来是为了让 runAction 的分支保持简短，也便于单测直接覆盖。
func (a *App) savedPlanActions(action string) (tea.Model, tea.Cmd, bool) {
	switch {
	case action == "timer_saved":
		a.openSavedPlans()
		return a, nil, true
	case action == "saved_manage":
		a.openSavedPlanManager()
		return a, nil, true
	case strings.HasPrefix(action, "saved_start:"):
		if idx, ok := savedPlanIndex(action, "saved_start:"); ok {
			m, cmd := a.startSavedPlan(idx)
			return m, cmd, true
		}
		a.setToast("这套收藏已经不存在了", toastWarn)
		return a, nil, true
	case strings.HasPrefix(action, "saved_edit:"):
		if idx, ok := savedPlanIndex(action, "saved_edit:"); ok {
			m, cmd := a.loadSavedPlanIntoTemplate(idx)
			return m, cmd, true
		}
		a.setToast("这套收藏已经不存在了", toastWarn)
		return a, nil, true
	case strings.HasPrefix(action, "saved_del:"):
		if idx, ok := savedPlanIndex(action, "saved_del:"); ok {
			m, cmd := a.deleteSavedPlan(idx)
			return m, cmd, true
		}
		a.setToast("这套收藏已经不存在了", toastWarn)
		return a, nil, true
	}
	return a, nil, false
}

// savedPlanIndex 从动作串里取出方案下标。
func savedPlanIndex(action, prefix string) (int, bool) {
	idx, err := strconv.Atoi(strings.TrimPrefix(action, prefix))
	if err != nil || idx < 0 {
		return 0, false
	}
	return idx, true
}
