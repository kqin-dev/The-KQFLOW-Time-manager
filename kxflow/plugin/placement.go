package plugin

import (
	"sort"

	"github.com/kqin-dev/kxflow/geometry"
)

// ViewConfig 是用户对磁贴位置的配置（"视图"概念，req.md 第 41 行）。
//
// 它属于**偏好**，与日界线无关，因此存在宿主配置里而不进日数据
// （与 v2.1.0 的"收藏的自定义专注方案"同一类判断）。
//
// 关键区分（design §4.1）：**开关粒度是包，摆放粒度是磁贴**。
// 用户决定"要不要 DDL 这个功能"（包开关），
// 以及在功能可用时"把 DDL 面板放在哪、显示不显示"（这里）。
type ViewConfig struct {
	// Slots 是锚点 → 插件 ID 的映射；空值表示该槽位空着。
	Slots map[geometry.Anchor]string
	// Hidden 是用户显式隐藏的插件 ID（功能还在，只是不占槽位）。
	Hidden []string
	// DockVisible 为假时中栏停靠区整块隐藏（req.md："可选 0~2 个磁贴"）。
	DockVisible bool
}

// NewViewConfig 返回一个空配置（停靠区可见、所有槽位空着）。
func NewViewConfig() ViewConfig {
	return ViewConfig{Slots: map[geometry.Anchor]string{}, DockVisible: true}
}

// SlotOf 返回某锚点上配置的插件 ID（空表示没配）。
func (v ViewConfig) SlotOf(a geometry.Anchor) string { return v.Slots[a] }

// WithSlot 返回一个把 pid 放到锚点 a 的新配置（便于测试与链式构造）。
func (v ViewConfig) WithSlot(a geometry.Anchor, pid string) ViewConfig {
	out := v.clone()
	if pid == "" {
		delete(out.Slots, a)
	} else {
		out.Slots[a] = pid
	}
	return out
}

// WithHidden 返回一个追加隐藏项的新配置。
func (v ViewConfig) WithHidden(pid string) ViewConfig {
	out := v.clone()
	out.Hidden = append(out.Hidden, pid)
	return out
}

func (v ViewConfig) clone() ViewConfig {
	out := ViewConfig{DockVisible: v.DockVisible, Slots: map[geometry.Anchor]string{}}
	for k, val := range v.Slots {
		out.Slots[k] = val
	}
	out.Hidden = append([]string(nil), v.Hidden...)
	return out
}

// IsHidden 报告插件是否被用户隐藏。
func (v ViewConfig) IsHidden(pid string) bool {
	for _, h := range v.Hidden {
		if h == pid {
			return true
		}
	}
	return false
}

// Placement 是一个磁贴的最终落位。
type Placement struct {
	PluginID string
	PackID   string
	Anchor   geometry.Anchor
	// ByUser 为真表示这个位置来自用户配置；为假表示按优先级自动安置。
	ByUser bool
}

// Placed 是安置结果：锚点 → 该锚点上的插件 ID（空串表示槽位空着）。
type Placed struct {
	Slots map[geometry.Anchor]string
}

// IDAt 返回锚点上的插件 ID。
func (p Placed) IDAt(a geometry.Anchor) string { return p.Slots[a] }

// TileRef 是一个已装载的磁贴引用。
type TileRef struct {
	PackID   string
	Manifest Manifest
	Tile     Component
}

// Tiles 返回全部已装载的磁贴（按包装载顺序）。
//
// **顺序契约**：`Assembled.Tiles()` 必须按 `Pack.Members()` 里磁贴成员
// 的声明顺序返回组件。这条约定由假包的测试守住，理由见 tileRefs。
func (m *Manager) Tiles() []TileRef {
	var out []TileRef
	for _, a := range m.packs {
		out = append(out, tileRefs(a)...)
	}
	return out
}

// tileRefs 把包的成员声明与它返回的组件**按顺序对齐**，得到带身份的磁贴引用。
//
// 为什么需要对齐：组件接口上只有 Title，没有身份（构造时不给它看 Manifest，
// 免得组件自己能改身份声明）。因此身份只能由"成员声明顺序 = 组件返回顺序"
// 这条契约推出。数量对不上时**不猜**：多出来的组件记为无名磁贴并保留，
// 少掉的成员不产生引用——宁可报告，也不把身份张冠李戴。
func tileRefs(a Assembled) []TileRef {
	if a == nil {
		return nil
	}
	pid := a.Pack().ID()
	comps := a.Tiles()
	var decls []Manifest
	for _, pl := range a.Pack().Members() {
		if pl == nil {
			continue
		}
		if mf := pl.Manifest(); mf.Kind == KindTile {
			decls = append(decls, mf)
		}
	}
	out := make([]TileRef, 0, len(comps))
	for i, c := range comps {
		if c == nil {
			continue
		}
		ref := TileRef{PackID: pid, Tile: c}
		if i < len(decls) {
			ref.Manifest = decls[i]
		} else {
			// 组件多于声明：身份未知，用 Title 兜底但 ID 留空，
			// 这样它不会被误当成某个声明过的插件去参与用户配置。
			ref.Manifest = Manifest{Name: c.Title(), Kind: KindTile}
		}
		out = append(out, ref)
	}
	return out
}

// TileByID 按插件 ID 找已装载的磁贴。
func (m *Manager) TileByID(id string) (TileRef, bool) {
	for _, t := range m.Tiles() {
		if t.Manifest.ID == id {
			return t, true
		}
	}
	return TileRef{}, false
}

// Placements 按用户配置与磁贴自身偏好，把已装载的磁贴安置到槽位。
//
// 规则（design §6，必须可预测、可复现）：
//
//  1. 用户配置里显式放置的磁贴**优先**占位；
//  2. 用户配置指向"已装载的包"里不存在的插件 → 记为 PlacementIssue，
//     而不是静默忽略。否则用户看到的是"我明明设了，它却没了"这种无法解释的状态；
//  3. 其余磁贴按 `Slots.Priority` 降序、同优先级按插件 ID 字典序，
//     优先填自己声明的 Anchor；锚点被占或未指定时按 AllAnchors 固定顺序找空槽；
//  4. 放不下的进入 unplaced 列表（**不报错、不覆盖**），由用户在视图配置里调整。
func (m *Manager) Placements(vc ViewConfig) (Placed, []Manifest, []PlacementIssue) {
	out := Placed{Slots: map[geometry.Anchor]string{}}
	var issues []PlacementIssue

	all := m.Tiles()
	byID := map[string]TileRef{}
	for _, t := range all {
		if t.Manifest.ID != "" {
			byID[t.Manifest.ID] = t
		}
	}
	hidden := map[string]bool{}
	used := map[string]bool{}

	// 1) 用户显式配置优先。AllAnchors 已经是"从左上到右下"的稳定顺序，
	//    直接用它遍历即可复现（不需要再排一次序）。
	anchors := geometry.AllAnchors
	for _, a := range anchors {
		pid := vc.SlotOf(a)
		if pid == "" {
			continue
		}
		if vc.IsHidden(pid) {
			hidden[pid] = true
			continue
		}
		if _, ok := byID[pid]; !ok {
			issues = append(issues, PlacementIssue{Anchor: a, Slot: pid,
				Detail: "配置里指定了这个磁贴，但它所属的包没有装载；请检查包是否被关闭或缺少依赖"})
			continue
		}
		if isDock(a) && !vc.DockVisible {
			// 配置指向停靠区，但停靠区被关掉了。这是**正常的组合**，不是矛盾：
			// 用户关掉停靠区就是想让它别占地方，"视图配置里仍留着那个位置"
			// 是常见状态（关掉再打开时还能回到原处）。因此这里静默不安置，
			// 但**不把它当成错误**报出来干扰用户。
			//
			// 关键是它到此为止：绝不能接着走下面的自动安置把它塞进侧栏——
			// 一个为停靠区设计的磁贴被塞进侧栏，它的排版假设全是错的。
			used[pid] = true
			continue
		}
		if used[pid] {
			issues = append(issues, PlacementIssue{Anchor: a, Slot: pid,
				Detail: "这个磁贴已被放到别的槽位，同一个磁贴只能占一个槽位"})
			continue
		}
		out.Slots[a] = pid
		used[pid] = true
	}

	// 2) 其余按优先级自动安置。
	rest := make([]TileRef, 0, len(all))
	for _, t := range all {
		if t.Manifest.ID == "" || used[t.Manifest.ID] || vc.IsHidden(t.Manifest.ID) {
			continue
		}
		rest = append(rest, t)
	}
	sort.SliceStable(rest, func(i, j int) bool {
		pi, pj := rest[i].Manifest.Slots.Priority, rest[j].Manifest.Slots.Priority
		if pi != pj {
			return pi > pj // 优先级大的先占
		}
		return rest[i].Manifest.ID < rest[j].Manifest.ID
	})

	var unplaced []Manifest
	for _, t := range rest {
		if a, ok := m.findAnchor(t, out, vc.DockVisible); ok {
			out.Slots[a] = t.Manifest.ID
			used[t.Manifest.ID] = true
			continue
		}
		unplaced = append(unplaced, t.Manifest)
	}
	return out, unplaced, issues
}

// findAnchor 为磁贴找一个空槽。
//
// 三条规则，顺序不能变：
//
//  1. 磁贴**明确声明了**要放哪（Anchor 是真实槽位）→ 就放那儿；那儿被占了、
//     或那是被关掉的停靠区 → 进未安置列表，**不另找地方**。
//     理由：一个专门声明"我要在停靠区"的磁贴，被塞到侧栏是错误的行为
//     （它会以为自己在中栏，排版假设全不对）。用户的出路是打开停靠区
//     或改视图配置。
//  2. 磁贴只声明了优先级（AnchorUnset）→ 按固定顺序找第一个空槽。
//  3. 停靠区被关掉时，找空槽要**跳过**停靠槽位——否则会出现
//     "我把停靠区关了，东西却还在那儿"。
func (m *Manager) findAnchor(t TileRef, placed Placed, dockVisible bool) (geometry.Anchor, bool) {
	if want := t.Manifest.Slots.Anchor; want.IsSlot() {
		if isDock(want) && !dockVisible {
			return geometry.AnchorUnset, false
		}
		if placed.Slots[want] == "" {
			return want, true
		}
		return geometry.AnchorUnset, false
	}
	for _, a := range geometry.AllAnchors {
		if isDock(a) && !dockVisible {
			continue
		}
		if placed.Slots[a] == "" {
			return a, true
		}
	}
	return geometry.AnchorUnset, false
}

func isDock(a geometry.Anchor) bool {
	return a == geometry.AnchorCenterDockLeft || a == geometry.AnchorCenterDockRight
}
