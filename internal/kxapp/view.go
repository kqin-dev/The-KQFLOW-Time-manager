package kxapp

import (
	"github.com/kqin-dev/kxflow"
	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/geometry"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/svc"
	"github.com/kqin-dev/kxflow/tile"
)

// ViewOptionID 是"界面布局"看板选项的 ID。
const ViewOptionID = "kqflow.settings.view"

// viewOption 是"界面布局"看板选项（仍属设置包）。
//
// 用户点名的第三条："还没有 view 设置这种功能"。
// req.md 把"视图"列为独立能力：**包开关**决定"功能在不在"，
// **视图配置**决定"磁贴摆哪儿、显不显示"——两者粒度不同，都要有。
type viewOption struct {
	src   Source
	state *HostState
	// engine 由引擎在装配后注入：视图设置要读"有哪些磁贴""现在怎么摆的"。
	//
	// 用回调而不是直接持有 *kxflow.Model：kxapp 是宿主适配层，
	// 让它反向依赖引擎的门面类型会把两者绑死（而引擎是要能单独拆出去的）。
	engineView func() viewEngine
	apply      func(plugin.ViewConfig)
}

// viewEngine 是视图设置需要的引擎能力（一个窄接口，便于测试替身）。
type viewEngine struct {
	Tiles   []plugin.TileRef
	Current plugin.ViewConfig
	// Placed 是**实际落位**（含自动安置的），不只是用户指派的那些。
	//
	// 这两个必须分开：Current.Slots 只记"用户明确指派的"，
	// 而用户打开布局页想看到的是"**现在**每个槽位放着谁"（含自动安置）。
	// 拿 Current.Slots 当答案会显示一片"（空）"——而磁贴其实好好地摆着。
	// 我第一版就是这么错的，症状是"进布局页看到所有槽位都空着"。
	Placed []plugin.Placement
}

// anchorOfPlaced 返回某磁贴当前实际所在的槽位。
func (e viewEngine) anchorOfPlaced(pid string) (geometry.Anchor, bool) {
	for _, p := range e.Placed {
		if p.PluginID == pid {
			return p.Anchor, true
		}
	}
	return geometry.AnchorUnset, false
}

// pidAt 返回某槽位上实际放着的磁贴。
func (e viewEngine) pidAt(a geometry.Anchor) string {
	for _, p := range e.Placed {
		if p.Anchor == a {
			return p.PluginID
		}
	}
	return ""
}

func (o *viewOption) Label() string { return "界面布局 / Layout" }
func (o *viewOption) Order() int    { return 56 }

func (o *viewOption) Activate(svc.Services) (plugin.View, error) {
	return newViewSettingsView(o.src, o.engineView, o.apply), nil
}

// newViewSettingsView 构造界面布局设置页。
//
// 形态沿用设置页：一列条目 + j/k 移动 + enter 动作。
// 条目分三段：
//
//	① 停靠区开关（整块区域的显隐）
//	② 每个槽位一行：现在放着谁（enter 进入"选磁贴放这儿 / 清空"）
//	③ 每个已装载磁贴一行：显示 / 隐藏
//
// 为什么槽位与磁贴分开列：它们是两种不同的动作——
// "这个位置放什么"是**位置**视角，"这个磁贴显不显示"是**磁贴**视角。
// 混在一起用户会分不清"我把它关了"和"我把它换走了"。
func newViewSettingsView(src Source, engine func() viewEngine, apply func(plugin.ViewConfig)) plugin.View {
	cursor := 0
	var status string

	type row struct {
		kind   string // "dock" / "slot" / "tile"
		anchor geometry.Anchor
		tile   plugin.TileRef
		label  string
		value  string
	}

	// rows 每帧重建：视图配置随时可能被改动，缓存会显示过期内容。
	rows := func() []row {
		var out []row
		var ev viewEngine
		if engine != nil {
			ev = engine()
		}
		out = append(out, row{
			kind:  "dock",
			label: "中栏停靠区",
			value: onOff(ev.Current.DockVisible),
		})
		for _, a := range geometry.AllAnchors {
			pid := ev.pidAt(a)
			name := "（空）"
			if pid != "" {
				name = tileName(ev.Tiles, pid)
			}
			out = append(out, row{
				kind: "slot", anchor: a,
				label: "槽位 " + a.String(),
				value: name,
			})
		}
		for _, t := range ev.Tiles {
			var state string
			switch {
			case ev.Current.IsHidden(t.Manifest.ID):
				state = "已隐藏"
			default:
				if anchor, ok := ev.anchorOfPlaced(t.Manifest.ID); ok {
					state = "显示于 " + anchor.String()
				} else {
					state = "未安置（没有空槽位）"
				}
			}
			out = append(out, row{
				kind: "tile", tile: t,
				label: "磁贴 " + DisplayTitle(t.Manifest.Name),
				value: state,
			})
		}
		return out
	}

	return &plugin.ViewFunc{
		ViewName: "界面布局",
		HintFn: func(plugin.RenderCtx) []plugin.KeyHint {
			return []plugin.KeyHint{
				{Key: "j/k", Desc: "移动"},
				{Key: "enter", Desc: "修改"},
				{Key: "esc", Desc: "返回"},
			}
		},
		RenderFn: func(ctx plugin.RenderCtx) {
			list := rows()
			cur := ClampCursor(cursor, len(list))
			y := ctx.Rect.Y
			put := func(s string, style canvas.StyleID) {
				y = drawWrapped(ctx, y, ctx.Rect, s, style)
			}
			put("界面布局", tile.StyleTitle)
			put("", tile.StyleMuted)
			for i, r := range list {
				if y >= ctx.Rect.Y1() {
					break
				}
				mark, style := "  ", tile.StyleMuted
				if i == cur {
					mark, style = "▸ ", tile.StyleTitleFocused
				}
				put(mark+r.label+"  →  "+r.value, style)
			}
			if y < ctx.Rect.Y1() {
				put("", tile.StyleMuted)
				put("j/k 移动 · enter 修改 · esc 返回", tile.StyleHint)
			}
			if status != "" && y < ctx.Rect.Y1() {
				put(status, tile.StyleStatus)
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			list := rows()
			cur := ClampCursor(cursor, len(list))
			switch ev.Key {
			case "esc", "q":
				return plugin.None(), true
			case "j", "down":
				cursor = MoveCursor(cur, 1, len(list))
				return plugin.None(), false
			case "k", "up":
				cursor = MoveCursor(cur, -1, len(list))
				return plugin.None(), false
			case "enter", " ":
				r := list[cur]
				var ev2 viewEngine
				if engine != nil {
					ev2 = engine()
				}
				switch r.kind {
				case "dock":
					vc := ev2.Current
					vc.DockVisible = !vc.DockVisible
					if apply != nil {
						apply(vc)
					}
					saveView(src, vc)
					status = "停靠区已" + onOff(vc.DockVisible)
					return plugin.Persist(SettingPackID, "config", nil), false
				case "slot":
					if apply == nil {
						return plugin.None(), false
					}
					return plugin.Borrow(newSlotPicker(src, r.anchor, ev2, apply)), false
				case "tile":
					if apply == nil {
						return plugin.None(), false
					}
					vc := ev2.Current
					// 切换显隐：已隐藏的恢复显示（腾出它声明的槽位让它自己回去），
					// 显示中的则隐藏并清掉它占的槽位。
					if vc.IsHidden(r.tile.Manifest.ID) {
						vc = unhide(vc, r.tile.Manifest.ID)
						status = "已显示：" + DisplayTitle(r.tile.Manifest.Name)
					} else {
						vc = vc.WithHidden(r.tile.Manifest.ID)
						vc = clearSlotOf(vc, r.tile.Manifest.ID)
						status = "已隐藏：" + DisplayTitle(r.tile.Manifest.Name)
					}
					apply(vc)
					saveView(src, vc)
					return plugin.Persist(SettingPackID, "config", nil), false
				}
			}
			return plugin.None(), false
		},
	}
}

// newSlotPicker 是"这个槽位放哪个磁贴"的选择界面。
//
// 单独一层而不是就在设置页里循环切换：槽位有好几个、磁贴有十来个，
// 循环切换会让人数不清"按几下才轮到我要的那个"。
func newSlotPicker(src Source, anchor geometry.Anchor, ev viewEngine, apply func(plugin.ViewConfig)) plugin.View {
	cursor := 0
	// 候选 = 全部磁贴 + "清空"。
	type option struct {
		pid   string
		label string
	}
	options := func() []option {
		out := []option{{pid: "", label: "（清空这个槽位）"}}
		for _, t := range ev.Tiles {
			// 已经被摆在**别的**槽位上的磁贴也列出来：选中它等于"搬过来"
			//（这是用户最自然的操作，不该逼他先去别处腾位置）。
			label := DisplayTitle(t.Manifest.Name)
			if cur, ok := ev.anchorOfPlaced(t.Manifest.ID); ok && cur != anchor {
				label += "（从 " + cur.String() + " 搬来）"
			}
			out = append(out, option{pid: t.Manifest.ID, label: label})
		}
		return out
	}

	return &plugin.ViewFunc{
		ViewName: "选择磁贴",
		HintFn: func(plugin.RenderCtx) []plugin.KeyHint {
			return []plugin.KeyHint{
				{Key: "j/k", Desc: "选择"},
				{Key: "enter", Desc: "放这里"},
				{Key: "esc", Desc: "取消"},
			}
		},
		RenderFn: func(ctx plugin.RenderCtx) {
			list := options()
			cur := ClampCursor(cursor, len(list))
			y := ctx.Rect.Y
			put := func(s string, style canvas.StyleID) {
				y = drawWrapped(ctx, y, ctx.Rect, s, style)
			}
			put("槽位 "+anchor.String()+" 放哪个磁贴", tile.StyleTitle)
			put("", tile.StyleMuted)
			for i, o := range list {
				if y >= ctx.Rect.Y1() {
					break
				}
				mark, style := "  ", tile.StyleMuted
				if i == cur {
					mark, style = "▸ ", tile.StyleTitleFocused
				}
				put(mark+o.label, style)
			}
			if y < ctx.Rect.Y1() {
				put("", tile.StyleMuted)
				put("enter 确认 · esc 取消", tile.StyleHint)
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev2 plugin.Event) (plugin.Action, bool) {
			list := options()
			cur := ClampCursor(cursor, len(list))
			switch ev2.Key {
			case "esc", "q":
				return plugin.None(), true
			case "j", "down":
				cursor = MoveCursor(cur, 1, len(list))
				return plugin.None(), false
			case "k", "up":
				cursor = MoveCursor(cur, -1, len(list))
				return plugin.None(), false
			case "enter", " ":
				vc := ev.Current
				pid := list[cur].pid
				if pid == "" {
					// 清空：只去掉这个槽位的指派。
					vc = vc.WithSlot(anchor, "")
				} else {
					// 一个磁贴只能在一个槽位：先把它从原槽位摘掉。
					vc = clearSlotOf(vc, pid)
					// 目标槽位若已有别人，把它挤成未安置（**不**自动另找位置：
					// 自动搬家会让用户看不懂"我的东西怎么自己动了"）。
					vc = vc.WithSlot(anchor, pid)
				}
				if apply != nil {
					apply(vc)
				}
				saveView(src, vc)
				return plugin.Persist(SettingPackID, "config", nil), true
			}
			return plugin.None(), false
		},
	}
}

// clearSlotOf 把某个插件从它占的槽位上摘下来（不改隐藏状态）。
func clearSlotOf(vc plugin.ViewConfig, pid string) plugin.ViewConfig {
	for a, cur := range vc.Slots {
		if cur == pid {
			vc = vc.WithSlot(a, "")
		}
	}
	return vc
}

// unhide 取消隐藏。
func unhide(vc plugin.ViewConfig, pid string) plugin.ViewConfig {
	kept := make([]string, 0, len(vc.Hidden))
	for _, h := range vc.Hidden {
		if h != pid {
			kept = append(kept, h)
		}
	}
	vc.Hidden = kept
	return vc
}

// tileName 按插件 ID 找名字（找不到就退回 ID，便于排查）。
func tileName(tiles []plugin.TileRef, pid string) string {
	for _, t := range tiles {
		if t.Manifest.ID == pid {
			return DisplayTitle(t.Manifest.Name)
		}
	}
	return pid
}

// saveView 把视图配置写进 cfg（落盘由调用方发起的 Persist 负责）。
func saveView(src Source, vc plugin.ViewConfig) {
	cfg := src.Config()
	if cfg == nil {
		return
	}
	SaveViewConfig(cfg, vc)
}

// viewEngineFrom 把引擎门面收窄成视图设置需要的那点信息。
//
// 这一步是必要的隔离：设置页只需要"有哪些磁贴、现在怎么摆的"，
// 不该拿到整个引擎（那样它就能顺手干别的，边界会慢慢烂掉）。
func viewEngineFrom(m *kxflow.Model) viewEngine {
	if m == nil {
		return viewEngine{Current: plugin.NewViewConfig()}
	}
	return viewEngine{
		Tiles:   m.AllTiles(),
		Current: m.ViewConfig(),
		Placed:  m.Placements(),
	}
}
