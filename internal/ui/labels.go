package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/config"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// 标签（见需求 1）。
//
// 两个概念必须分清，名字很像但用途完全不同：
//   - 这里的「标签」是用户自己起的记号（星星 / 紧急 / 忽略），存在条目上。
//   - `Goal.Tag` 是标题的稳定指纹（`#a1b2c3`），程序算出来用于继承时避免
//     同名混淆，用户改不了。
//
// labelOption 是界面层使用的标签条目：名字 + 记号。
type labelOption struct {
	Name string
	// Glyph 是内置预设的记号字符；用户自定义的标签没有记号。
	Glyph string
}

// labelPresets 是内置标签预设。
//
// **已移到 model.LabelPresets**：它是一份数据而非排版逻辑，
// 而引擎版界面（internal/kxapp）也要用同一份。留在这里会导致两边各存一份，
// 迟早不一致。这里保留别名以避免改动旧界面里大量引用点。
var labelPresets = model.LabelPresets

// labelViewState 是标签编辑页的状态。
type labelViewState struct {
	active bool
	// target 是正在编辑的条目。注意它是指针，改动立刻反映到看板上。
	target model.Labeled
	// options 是本次会话可见的标签清单。打开时算一次，之后固定——
	// 每勾一个就重排会让光标在列表里乱跳。
	options []labelOption
	cursor  int
	scroll  int
	// customDirty 记录本次会话是否新增过自定义标签。
	//
	// 只有它为真时才在退出时写配置：saveConfig() 会重新载入数据（日界线可能
	// 随之改变），那会把 a.data 换成新对象，让这里的 target 指针指向旧数据。
	// 标签本身每次都随 saveDay 落盘，不需要靠退出时的这次配置写入。
	customDirty bool
}

// presetGlyph 返回预设记号；自定义标签返回空串。
func presetGlyph(name string) string {
	for _, p := range labelPresets {
		if p.Name == name {
			return p.Glyph
		}
	}
	return ""
}

// labelGlyph 返回标签在列表里展示时用的记号与显示文本。
//
// 没有记号的标签统一用 `#`，这样一行里「有标签」这件事本身一眼可见。
func labelGlyph(name string) string {
	if g := presetGlyph(name); g != "" {
		return g
	}
	return "#"
}

// renderLabelTag 渲染一个贴在看板行上的标签。
func renderLabelTag(name string) string {
	return labelGlyph(name) + name
}

// labelsInline 把标签拼成贴在条目行末尾的一段文本，例如“★星星 !紧急”。
//
// 有预算上限：标签在窄面板里很容易把标题挤掉，此时退化成只显示记号，
// 让用户至少知道「这条有标签」，而不是把标题整段顶出去。
func labelsInline(labels []string) string {
	return labelsInlineBudget(labels, 24)
}

// labelsInlineBudget 在给定显示宽度预算内渲染标签串。
func labelsInlineBudget(labels []string, budget int) string {
	if len(labels) == 0 {
		return ""
	}
	// 先试完整形式，超预算就退化成只显示记号。
	if s := joinLabelTags(labels, false); lipgloss.Width(s) <= budget {
		return s
	}
	return joinLabelTags(labels, true)
}

func joinLabelTags(labels []string, glyphOnly bool) string {
	parts := make([]string, 0, len(labels))
	for _, name := range labels {
		if glyphOnly {
			parts = append(parts, labelGlyph(name))
			continue
		}
		parts = append(parts, renderLabelTag(name))
	}
	return strings.Join(parts, " ")
}

// currentLabeled 返回当前焦点下可以打标签的条目。
//
// 菜单栏没有「条目」，所以焦点在中间栏时返回 nil，由调用方提示用户先选一个。
func (a *App) currentLabeled() model.Labeled {
	switch a.focus {
	case FocusFixed, FocusFloating:
		if t := a.currentTodo(); t != nil {
			return t
		}
	case FocusGoals:
		if g := a.selectedGoal(); g != nil {
			return g
		}
	}
	return nil
}

// availableLabels 汇总本次可见的标签：内置预设 + 配置里的自定义标签 +
// 所有条目上已经用过的标签，按名字去重。
//
// 顺序固定为「预设 → 已用过的自定义 → 配置里的自定义」，用户找东西的位置稳定。
func (a *App) availableLabels() []labelOption {
	out := make([]labelOption, 0, len(labelPresets)+8)
	seen := map[string]bool{}
	add := func(name string) {
		name = model.LabelName(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, labelOption{Name: name, Glyph: presetGlyph(name)})
	}

	for _, p := range labelPresets {
		add(p.Name)
	}
	for _, t := range a.data.All() {
		for _, l := range t.Labels {
			add(l)
		}
	}
	for _, g := range a.goalList() {
		for _, l := range g.Labels {
			add(l)
		}
	}
	for _, l := range a.cfg.CustomLabelList() {
		add(l)
	}
	return out
}

// openLabels 打开标签编辑页。
func (a *App) openLabels() {
	target := a.currentLabeled()
	if target == nil {
		a.setToast("先在左栏或右栏选中一个条目，再按 l 打标签", toastWarn)
		return
	}
	a.labelView = labelViewState{
		active:  true,
		target:  target,
		options: a.availableLabels(),
	}
	a.view = ViewLabels
	a.pageScroll = 0
	// 打开时把光标落在该条目已有的第一个标签上，方便直接改。
	if labels := target.ItemLabels(); len(labels) > 0 {
		for i, opt := range a.labelView.options {
			if opt.Name == labels[0] {
				a.labelView.cursor = i
				break
			}
		}
	}
}

// closeLabels 关闭标签编辑页。
//
// 返回需要执行的保存命令：标签本身在每次开关时已经随 saveDay 落盘，这里只在
// 本次会话新增过自定义标签时补一次配置写入。
func (a *App) closeLabels() tea.Cmd {
	dirty := a.labelView.customDirty
	a.labelView = labelViewState{}
	a.view = ViewDashboard
	if !dirty {
		return nil
	}
	// 先记住要落盘的内容，再让命令去写——saveConfig 会重建 a.data，
	// 不能等到那时才读状态。
	paths := a.pathsForSave()
	cfg := a.cfg
	return func() tea.Msg {
		if err := config.Save(paths, cfg); err != nil {
			return savedMsg{err: err}
		}
		return savedMsg{}
	}
}

// handleLabelKey 处理标签编辑页的按键。
func (a *App) handleLabelKey(key string) (tea.Model, tea.Cmd) {
	lv := &a.labelView
	switch key {
	case "j", "down":
		if n := len(lv.options) + 1; n > 0 {
			lv.cursor = (lv.cursor + 1) % n
		}
	case "k", "up":
		if n := len(lv.options) + 1; n > 0 {
			lv.cursor = (lv.cursor - 1 + n) % n
		}
	case "g", "home":
		lv.cursor = 0
	case "G", "end":
		lv.cursor = len(lv.options)
	case "enter", " ":
		return a.activateLabelOption()
	case "a", "n":
		// 新增自定义标签：输入框里用空格或逗号分隔可以一次加多个。
		a.editor.set("新增标签（空格或逗号分隔，可一次多个）", "")
		a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
			return a.commitNewLabels(value)
		}
	case "d":
		// 从当前条目上摘掉选中的标签；标签本身仍留在清单里（可能别的条目在用）。
		if lv.cursor < len(lv.options) {
			name := lv.options[lv.cursor].Name
			if model.HasLabel(lv.target.ItemLabels(), name) {
				lv.target.ToggleItemLabel(name)
				a.setToast(fmt.Sprintf("已从「%s」取下 %s", lv.target.ItemTitle(), renderLabelTag(name)), toastInfo)
				return a, saveCmd(a.saveDay)
			}
			a.setToast("这一项还没打在当前条目上", toastWarn)
		}
	case "esc", "q":
		return a, a.closeLabels()
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	}
	return a, nil
}

// activateLabelOption 执行当前选中项：列表项是开关标签，最后一项是新增。
func (a *App) activateLabelOption() (tea.Model, tea.Cmd) {
	lv := &a.labelView
	if lv.cursor == len(lv.options) {
		a.editor.set("新增标签（空格或逗号分隔，可一次多个）", "")
		a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
			return a.commitNewLabels(value)
		}
		return a, nil
	}
	if lv.cursor > len(lv.options) {
		return a, nil
	}

	name := lv.options[lv.cursor].Name
	// 已经到达单个条目的标签数上限时，明确告诉用户，别让他以为按键失灵。
	if !model.HasLabel(lv.target.ItemLabels(), name) && len(lv.target.ItemLabels()) >= model.MaxLabelsPerItem {
		a.setToast(fmt.Sprintf("一个条目最多 %d 个标签", model.MaxLabelsPerItem), toastWarn)
		return a, nil
	}
	on := lv.target.ToggleItemLabel(name)
	verb := "已加上"
	if !on {
		verb = "已取下"
	}
	a.setToast(fmt.Sprintf("%s %s", verb, renderLabelTag(name)), toastInfo)
	return a, saveCmd(a.saveDay)
}

// commitNewLabels 处理「新增标签」输入框的提交。
//
// 新标签会加进当前条目，同时记进配置的自定义标签库，下次不用重新输入。
func (a *App) commitNewLabels(value string) (tea.Model, tea.Cmd) {
	lv := &a.labelView
	if lv.target == nil {
		return a, nil
	}
	names := model.ParseLabelsText(value)
	if len(names) == 0 {
		a.setToast("标签不能为空", toastWarn)
		return a, nil
	}

	added, skipped, full := 0, 0, false
	for _, name := range names {
		if model.HasLabel(lv.target.ItemLabels(), name) {
			skipped++
			continue
		}
		if len(lv.target.ItemLabels()) >= model.MaxLabelsPerItem {
			full = true
			break
		}
		lv.target.ToggleItemLabel(name)
		lv.options = append(lv.options, labelOption{Name: name, Glyph: presetGlyph(name)})
		added++
	}
	// 记进自定义标签库：内置预设不算「自定义」。
	for _, name := range names {
		if presetGlyph(name) == "" && a.cfg.AddCustomLabel(name) {
			lv.customDirty = true
		}
	}

	switch {
	case full:
		a.setToast(fmt.Sprintf("一个条目最多 %d 个标签，其余未加入", model.MaxLabelsPerItem), toastWarn)
	case added == 0:
		a.setToast("这些标签已经在条目上了", toastWarn)
	default:
		msg := fmt.Sprintf("已加上 %d 个标签", added)
		if skipped > 0 {
			msg += fmt.Sprintf("（%d 个已存在）", skipped)
		}
		a.setToast(msg, toastInfo)
	}
	return a, saveCmd(a.saveDay)
}

// labelsContent 渲染标签编辑页的内容，只占中间栏。
func (a *App) labelsContent() string {
	lv := &a.labelView
	inner := a.contentWidth()

	var lines []string
	lines = append(lines, a.modalLine(a.st.Title, "标签 / Labels", inner))
	lines = append(lines, "")
	lines = append(lines, a.modalLine(a.st.Text, truncate("条目："+lv.target.ItemTitle(), inner), inner))

	current := lv.target.ItemLabels()
	has := "（暂无）"
	if len(current) > 0 {
		tags := make([]string, 0, len(current))
		for _, name := range current {
			tags = append(tags, renderLabelTag(name))
		}
		has = strings.Join(tags, " ")
	}
	lines = append(lines, a.modalLine(a.st.Accent, "已有："+has, inner))
	lines = append(lines, "")

	for i, opt := range lv.options {
		mark := "  "
		if model.HasLabel(current, opt.Name) {
			mark = "✔ "
		}
		text := fmt.Sprintf("%s%s", labelGlyph(opt.Name), opt.Name)
		if i == lv.cursor {
			lines = append(lines, a.st.ModalCursor.Render(padTo("▸ "+mark+text, highlightWidth("▸ "+mark+text, inner))))
			continue
		}
		lines = append(lines, a.st.Text.Render(truncate("  "+mark+text, inner)))
	}

	addRow := "＋ 新增标签…"
	if lv.cursor == len(lv.options) {
		lines = append(lines, a.st.ModalCursor.Render(padTo("▸ "+addRow, highlightWidth("▸ "+addRow, inner))))
	} else {
		lines = append(lines, a.st.Muted.Render(truncate("  "+addRow, inner)))
	}

	lines = append(lines, "")
	lines = append(lines, a.modalLine(a.st.Muted, "enter 打开/关闭 · a 新增自定义", inner))
	lines = append(lines, a.modalLine(a.st.Muted, "d 取下 · j/k 移动 · esc 完成", inner))
	return strings.Join(lines, "\n")
}
