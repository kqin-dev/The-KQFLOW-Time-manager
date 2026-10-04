package model

import "strings"

// 标签（Labels）是用户给 TODO / GOAL 打的记号，例如「星星」「紧急」「忽略」。
//
// 与 Goal.Tag 区分清楚：那个 Tag 是**标题的稳定指纹**（`#a1b2c3`），由程序算出来、
// 用于继承 GOAL 时避免同名混淆，用户不能改；这里的 Labels 完全是**用户自己起的名**，
// 可以任意增删。两者名字像、用途完全不同，改代码时不要混。
//
// 数据结构约定：
//   - 存在条目上（Todo.Labels / Goal.Labels），**没有全局标签库**。可用标签 =
//     内置预设 + 配置里的自定义标签 + 所有条目上已用过的标签，三者去重。
//     这样用户不会遇到「标签库里有、但哪个条目都没用」的悬空项。
//   - 加字段用 omitempty，老数据读入时为 nil，不需要迁移。
//   - 入库前一律走 LabelName 清洗，避免控制字符与超长文本。

// MaxLabelRunes 是单个标签的最大长度（rune 数）。
//
// 标签是贴在列表行上的短记号，过长会把标题挤掉；超长直接截断而不是报错，
// 用户不至于因为多打了两个字就被拦住。
const MaxLabelRunes = 16

// MaxLabelsPerItem 是单个条目最多能带的标签数。
const MaxLabelsPerItem = 8

// LabelName 清洗用户输入的标签名：去掉控制字符、折叠空白、截断到上限。
//
// 返回空串表示这个标签无效，调用方应忽略它。
func LabelName(s string) string {
	cleaned := Sanitize(s, false)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return ""
	}
	r := []rune(cleaned)
	if len(r) > MaxLabelRunes {
		r = r[:MaxLabelRunes]
	}
	return string(r)
}

// NormalizeLabels 清洗一串标签：逐个规范化、去掉空项与重复（保留首次出现的顺序）。
func NormalizeLabels(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		name := LabelName(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
		if len(out) >= MaxLabelsPerItem {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// HasLabel 报告条目是否带有某个标签（按清洗后的名字比较）。
func HasLabel(labels []string, name string) bool {
	name = LabelName(name)
	if name == "" {
		return false
	}
	for _, l := range labels {
		if l == name {
			return true
		}
	}
	return false
}

// LabelsText 把标签拼成便于编辑的一行文本，例如“星星 紧急”。
func LabelsText(labels []string) string { return strings.Join(labels, " ") }

// sameLabels 报告两份标签列表是否等价（都为空也算等价）。
func sameLabels(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ParseLabelsText 把“星星 紧急”这类输入解析成标签列表。
//
// 空格与中英文逗号都当分隔符，用户按哪种习惯打都能用。
func ParseLabelsText(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', ',', '，', '、', ';', '；':
			return true
		}
		return false
	})
	return NormalizeLabels(fields)
}

// Labeled 是「可以打标签的条目」的公共接口，让界面层用一套代码处理 TODO 与 GOAL。
type Labeled interface {
	// ItemTitle 返回用于展示的标题。
	ItemTitle() string
	// ItemLabels 返回该条目当前的标签。
	ItemLabels() []string
	// SetItemLabels 整份替换标签，用于编辑器提交。
	SetItemLabels([]string)
	// ToggleItemLabel 打开/关闭单个标签，返回切换后的状态。
	ToggleItemLabel(name string) bool
}

// ---------- Todo 的标签实现 ----------

func (t *Todo) ItemTitle() string    { return t.Title }
func (t *Todo) ItemLabels() []string { return t.Labels }

func (t *Todo) SetItemLabels(labels []string) { t.Labels = NormalizeLabels(labels) }

func (t *Todo) ToggleItemLabel(name string) bool {
	t.Labels, _ = toggleLabel(t.Labels, name)
	return HasLabel(t.Labels, name)
}

// ---------- Goal 的标签实现 ----------

func (g *Goal) ItemTitle() string    { return g.Title }
func (g *Goal) ItemLabels() []string { return g.Labels }

func (g *Goal) SetItemLabels(labels []string) { g.Labels = NormalizeLabels(labels) }

func (g *Goal) ToggleItemLabel(name string) bool {
	g.Labels, _ = toggleLabel(g.Labels, name)
	return HasLabel(g.Labels, name)
}

// toggleLabel 在标签列表上打开/关闭一项，返回新列表与切换后的状态。
//
// 达到上限时不再新增（返回原列表、状态为 false），由调用方提示用户。
func toggleLabel(labels []string, name string) ([]string, bool) {
	name = LabelName(name)
	if name == "" {
		return NormalizeLabels(labels), false
	}
	for i, l := range labels {
		if l == name {
			out := make([]string, 0, len(labels)-1)
			out = append(out, labels[:i]...)
			out = append(out, labels[i+1:]...)
			return NormalizeLabels(out), false
		}
	}
	if len(NormalizeLabels(labels)) >= MaxLabelsPerItem {
		return NormalizeLabels(labels), false
	}
	out := append(append([]string{}, labels...), name)
	return NormalizeLabels(out), true
}
