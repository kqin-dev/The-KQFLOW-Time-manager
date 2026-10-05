package kxapp

import (
	"strings"

	"github.com/kqin-dev/kxflow/canvas"
	"github.com/kqin-dev/kxflow/plugin"
	"github.com/kqin-dev/kxflow/svc"
	"github.com/kqin-dev/kxflow/tile"
)

// noteOption 是"编辑随手记"看板选项。
//
// 它是**看板选项**而不是磁贴里的键：随手记是"对今天"的记录，
// 与选中哪一条无关；而且**空列表时也要能写**——那恰恰是最想写点什么的时候
// （与"添加目标"做成看板选项是同一个理由）。
type noteOption struct {
	src   Source
	state *HostState
}

func (o *noteOption) Label() string {
	if data := o.src.Day(); data != nil && strings.TrimSpace(data.Note) != "" {
		n := strings.Count(data.Note, "\n") + 1
		return "编辑随手记（" + itoa(n) + " 行）…"
	}
	return "写随手记…"
}

func (o *noteOption) Order() int { return 22 }

func (o *noteOption) Activate(svc.Services) (plugin.View, error) {
	return newNoteEditor(o.src).view(), nil
}

// noteEditor 是随手记的多行编辑器。
//
// 按键取舍照 2.1.0 的 note.go：
//
//	Enter    换行     ← 刻意避开"确认"，否则"写日记"与"确认"互相打架
//	ctrl+s   保存
//	esc      取消（有改动时先问一句"丢弃吗"）
//
// 编辑状态用**行切片 + 行内列**表示，而不是"整串里的一个偏移量"：
// 后者在换行/退格/跨行移动时全靠算下标（行首退格要并上一行、
// 上下移动要夹列……），极易出错；分行之后每个动作都是局部操作。
type noteEditor struct {
	src    Source
	lines  []string
	row    int
	col    int // 按 **rune** 计，不是字节
	dirty  bool
	asking bool   // 是否正在问"改动没保存，要丢弃吗"
	status string // 状态提示（保存失败等）
}

// newNoteEditor 构造编辑器，初值是当天的随手记。
//
// 它自己从 src 读初值（而不是由调用方传入）：这样"打开时读到什么"
// 与"保存时写回哪里"用的是同一个来源，不会出现两者指不同数据的情况。
func newNoteEditor(src Source) *noteEditor {
	e := &noteEditor{src: src}
	text := ""
	if data := src.Day(); data != nil {
		// 统一换行符：Windows 上粘贴进来的可能是 \r\n。
		text = strings.ReplaceAll(data.Note, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
	}
	if text == "" {
		e.lines = []string{""}
	} else {
		e.lines = strings.Split(text, "\n")
	}
	// 打开时把光标放到**末尾**：写随手记通常是接着上次继续写。
	e.row = len(e.lines) - 1
	e.col = len([]rune(e.lines[e.row]))
	return e
}

// view 把编辑器包成插件视图。
//
// 拆成独立方法而不是在构造函数里返回：编辑器需要**跨帧保持状态**
// （光标、脏标记、待确认），因此它是一个对象而不是一次性的闭包。
func (e *noteEditor) view() plugin.View {
	return &plugin.ViewFunc{
		ViewName: "随手记",
		// 有未保存改动（或正在问）时，esc 必须由本编辑器处理：
		// 它要先把"丢弃吗"问出来，而不是被引擎直接关掉。
		OwnsEscFn: func() bool { return e.asking || e.dirty },
		HintFn: func(plugin.RenderCtx) []plugin.KeyHint {
			if e.asking {
				return []plugin.KeyHint{
					{Key: "y", Desc: "丢弃并退出"},
					{Key: "n/esc", Desc: "继续写"},
				}
			}
			return []plugin.KeyHint{
				{Key: "enter", Desc: "换行"},
				{Key: "ctrl+s", Desc: "保存"},
				{Key: "esc", Desc: "取消"},
			}
		},
		RenderFn: func(ctx plugin.RenderCtx) {
			y := ctx.Rect.Y
			put := func(s string, style canvas.StyleID) {
				y = drawWrapped(ctx, y, ctx.Rect, s, style)
			}
			title := "随手记"
			if data := e.src.Day(); data != nil {
				title = "随手记 · " + data.Day
			}
			if e.dirty {
				title += "（未保存）"
			}
			put(title, tile.StyleTitle)
			put("", tile.StyleMuted)

			if e.asking {
				put("改动还没保存，要丢弃吗？", tile.StyleWarn)
				put("", tile.StyleMuted)
				put("y 丢弃并退出 · n 继续写", tile.StyleHint)
				return
			}

			// 正文：光标行必须可见（否则用户不知道自己在编辑哪一行）。
			avail := ctx.Rect.Y1() - y
			if avail < 1 {
				return
			}
			start := 0
			if e.row >= avail {
				start = e.row - avail + 1
			}
			for i := start; i < len(e.lines); i++ {
				if y >= ctx.Rect.Y1() {
					break
				}
				line := DisplayTitle(e.lines[i])
				if i == e.row {
					// 光标行：标出记号，并把光标位置画成一条竖线。
					line = insertCaret(line, e.col)
					y = drawWrappedInset(ctx, y, ctx.Rect, "▸ ", "  ", line, tile.StyleTitleFocused)
					continue
				}
				// 空行也要占一行，否则用户看不到自己留的空行。
				if line == "" {
					line = " "
				}
				y = drawWrappedInset(ctx, y, ctx.Rect, "  ", "  ", line, tile.StyleMuted)
			}
			if y < ctx.Rect.Y1() {
				put("", tile.StyleMuted)
				put("enter 换行 · ctrl+s 保存 · esc 取消", tile.StyleHint)
			}
			if e.status != "" && y < ctx.Rect.Y1() {
				put(e.status, tile.StyleStatus)
			}
		},
		UpdateFn: func(ec plugin.EventCtx, ev plugin.Event) (plugin.Action, bool) {
			// 正在问"丢弃吗"时只认 y / n（别的键一律忽略，
			// 免得用户以为自己在继续写、实际在回答一个没看见的问题）。
			if e.asking {
				switch ev.Key {
				case "y", "Y":
					return plugin.None(), true // 丢弃：直接关，不写回
				case "n", "N", "esc":
					e.asking = false
					return plugin.None(), false
				}
				return plugin.None(), false
			}
			switch ev.Key {
			case "ctrl+s":
				return e.save()
			case "esc":
				if e.dirty {
					e.asking = true
					return plugin.None(), false
				}
				return plugin.None(), true
			case "enter":
				e.splitLine()
				return plugin.None(), false
			case "backspace":
				e.backspace()
				return plugin.None(), false
			case "up":
				e.moveRow(-1)
				return plugin.None(), false
			case "down":
				e.moveRow(1)
				return plugin.None(), false
			case "left":
				e.moveCol(-1)
				return plugin.None(), false
			case "right":
				e.moveCol(1)
				return plugin.None(), false
			}
			// 插入文本。控制字符不进（它们会以 \u0000 的形式写进 JSON）。
			inserted := false
			for _, r := range ev.Runes {
				if isSettingRune(r) {
					e.insert(r)
					inserted = true
				}
			}
			if inserted {
				e.status = ""
			}
			return plugin.None(), false
		},
	}
}

// insert 在光标处插入一个字符。
func (e *noteEditor) insert(r rune) {
	line := []rune(e.lines[e.row])
	if e.col > len(line) {
		e.col = len(line)
	}
	line = append(line[:e.col], append([]rune{r}, line[e.col:]...)...)
	e.lines[e.row] = string(line)
	e.col++
	e.dirty = true
}

// splitLine 在光标处断行（Enter）。
func (e *noteEditor) splitLine() {
	line := []rune(e.lines[e.row])
	if e.col > len(line) {
		e.col = len(line)
	}
	head, tail := string(line[:e.col]), string(line[e.col:])
	e.lines[e.row] = head
	e.lines = append(e.lines[:e.row+1],
		append([]string{tail}, e.lines[e.row+1:]...)...)
	e.row++
	e.col = 0
	e.dirty = true
}

// backspace 删除光标前一个字符；在行首时把本行并到上一行。
func (e *noteEditor) backspace() {
	if e.col == 0 {
		if e.row == 0 {
			return // 已经在最开头，没什么可删
		}
		prev := []rune(e.lines[e.row-1])
		e.lines[e.row-1] = string(prev) + e.lines[e.row]
		e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
		e.row--
		e.col = len(prev)
		e.dirty = true
		return
	}
	line := []rune(e.lines[e.row])
	e.lines[e.row] = string(append(line[:e.col-1], line[e.col:]...))
	e.col--
	e.dirty = true
}

// moveRow 上下移动光标行。
//
// 列超出目标行长度时**夹到行尾**（而不是拒绝移动）：
// 用户从长行往短行按时，直觉是"光标移到那一行末尾"。
func (e *noteEditor) moveRow(d int) {
	next := e.row + d
	if next < 0 || next >= len(e.lines) {
		return
	}
	e.row = next
	if n := len([]rune(e.lines[e.row])); e.col > n {
		e.col = n
	}
}

// moveCol 左右移动光标列，跨行时走到相邻行首/行尾。
func (e *noteEditor) moveCol(d int) {
	n := len([]rune(e.lines[e.row]))
	switch {
	case d < 0 && e.col > 0:
		e.col--
	case d < 0 && e.row > 0:
		e.row--
		e.col = len([]rune(e.lines[e.row]))
	case d > 0 && e.col < n:
		e.col++
	case d > 0 && e.row < len(e.lines)-1:
		e.row++
		e.col = 0
	}
}

// contents 返回当前文本（清理行尾空白，保留行首缩进）。
//
// 行首缩进刻意保留：用户可能是在排版（列表、缩进引用）。
// 只清右侧空白与末尾多余空行——那是"打多了"的痕迹。
func (e *noteEditor) contents() string {
	lines := make([]string, len(e.lines))
	for i, l := range e.lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// save 保存到当天数据并落盘。
func (e *noteEditor) save() (plugin.Action, bool) {
	data := e.src.Day()
	if data == nil {
		e.status = "没有当天数据，无法保存"
		return plugin.None(), false
	}
	data.Note = e.contents()
	e.dirty = false
	// 走 ActionPersist 而不是直接调 src.Save：这是"插件只说意图"的落点，
	// 引擎据此统一处理保存失败的提示（见 kxflow.runAction）。
	return plugin.Persist(NotePackID, "day", data), true
}

// insertCaret 在显示文本的第 col 个**字符**前插入一个光标记号。
//
// 按 rune 数定位（不是字节、也不是显示宽度）：随手记常中英混排，
// 按字节会插到半个汉字里面去。
func insertCaret(s string, col int) string {
	runes := []rune(s)
	if col < 0 {
		col = 0
	}
	if col > len(runes) {
		col = len(runes)
	}
	return string(runes[:col]) + "▏" + string(runes[col:])
}
