# KXFLOW 渲染引擎 · v3.0.0 架构设计

**状态**：待评审（评审通过后再动代码）
**目标版本**：KQFLOW v3.0.0 / KXFLOW v0.1.0
**依据**：`req.md`（v3.0.0 开发需求）、`ROADMAP.md` 第三节、`KXflow.md` 构想、`SKILL/` 手册
**已确认的三项决策**（用户 2026-10-05 拍板）：
1. KXFLOW 作为**本仓库内的独立子包 + 单独 `go.mod`**（多模块同仓）——同仓是为了与 KQFLOW 联合开发、用它试出引擎的不足；独立模块是为了将来能拆出去。
2. 迁移采用**新旧并行 + 开关切换，按业务逐块搬**。
3. 先交**设计文档评审**，通过后再写代码。

---

## 0. 先说结论：这次要解决的是"结构性"，不是"这一处"

v2.1.0 的结论已经写在 `ROADMAP.md` 里，我复核后完全同意，并且能指出**具体是哪三个机制缺失**：

| 缺失的机制 | v2.1.0 的现状（实测） | 造成的历史症状 |
| --- | --- | --- |
| **唯一的尺寸权威** | 宽度有 `columnLayout()`（做对了），**高度没有权威**：`renderCenterBox()` 用 `columnLayout` 的 `bodyH`，`viewTooSmall()` 用 `a.width/a.height`，两套来源并存 | 「面板多一行/少一行」「栏位消失」 |
| **写入即受限** | 渲染靠"先渲染、再实测宽度自校正（`panel()` 里的循环）、最后 `clipBlock` 兜底" | 超宽靠兜底截断 = **丢字**，用户报了四次 |
| **渲染是纯的** | `pageContent()` 在渲染路径里写 `a.pageScroll`；响铃曾经挂在渲染路径上 | 「停在设置页提示音不响」 |

所以 v3.0.0 的验收标准不是"把某几个页面修好"，而是：**这三类问题在新架构里"想犯也犯不出来"**。

### 0.1 一条关键设计选择：为什么是"画布"而不是"更好的渲染函数"

把所有内容画进一块**固定尺寸的字符-样式网格（Canvas）**，然后一次性输出。

这样一来：

- **尺寸权威唯一**：画布就是权威，输出必然正好是 `W×H`。不需要 `clipBlock` 兜底，也不需要"渲染后自校正"。
- **越界是结构性不可能的**：每个格子写入前都要通过边界与裁剪区检查，写了就返回 `false`。超宽/超行不是"被截断"，而是**写不进去**，并且计数可观测（开发期直接报错）。
- **跨栏串字不可能**：每次绘制都在一个**裁剪区（Clip Region）**里进行。中栏的渲染器只拿到中栏的矩形，它**没有能力**画到左右栏去（v2.1.0 靠的是"记得别那么画"）。
- **带样式的宽度不再是难题**：每个格子自带样式下标，`Render()` 只对**连续的同样式**片段上色。因此不需要 `stripANSI()` 去反推宽度——v2.1.0 那套 `stripANSI`/`displayRange`/`replaceColumns` 按显示宽度切 ANSI 字符串的绕行代码可以整批删掉。
- **中文宽度问题一次性收口**：写入、折行、截断全部走同一套显示宽度判定（`lipgloss.Width` / `runewidth`，遵守 `SKILL` conventions 第 1 条）。

### 0.2 这条选择已经用原型验证过（不是纸上推演）

我在动手写文档前写了一个一次性原型（`sandbox/kvproto/`，**不属于交付物，评审后可删**）来验证画布是否真的成立。实测结果：

```
 60x16  渲染行数=16(期望16) 超宽行=0 被拒写入=0
 80x24  渲染行数=24(期望24) 超宽行=0 被拒写入=0
120x30  渲染行数=30(期望30) 超宽行=0 被拒写入=0
160x44  渲染行数=44(期望44) 超宽行=0 被拒写入=0
  1x1   渲染行数=1(期望1)   超宽行=0 被拒写入=0
  2x2   渲染行数=2(期望2)   超宽行=0 被拒写入=0
```

其中 `160x44` 正是用户报过四次问题的那种宽终端，`1x1` / `2x2` 是"小窗口糊屏"那一类。

原型还**当场暴露了两个真实的 API 陷阱**，直接改进了本文的设计：

1. **分割方向的歧义**：我先写的是 `footer, body := rest.SplitH(rest.H - 1)`，结果把底栏撑成了 22 行高、主体只剩 1 行。这就是"A 到底是上半还是下半"的经典事故。
   → 设计修正：**禁止 `SplitH/SplitV` 这类方向含糊的命名**，改为 `CutTop/CutBottom/CutLeft/CutRight` 四个显式方法（§3.1）。
2. **内容会画到边框上**：底栏高度只有 1 行、内区高度为 0 时，文本直接覆盖了边框线。
   → 设计修正：**裁剪区 + 覆盖检测（Collisions）**成为画布的强制部分（§3.2）。加上之后原型报告 `覆盖已有内容次数=0 越界写入次数=0`，三个栏位的边框完整。

---

## 1. 目标与非目标

### 1.1 目标（来自 `req.md`）

1. **统一的对象体系**：五层抽象（引擎 / 布局骨架 / 磁贴与槽位 / 主舞台与视图栈 / 状态与事件）。
2. **统一的渲染系统**：布局只有一处权威实现；渲染前就强制保证「不超宽、不超行、不越界」。
3. **解耦的功能接口**：内核插件 / 磁贴插件 / 选项插件三类接口 + 插件管理器 + 版本与冲突规范。
4. **View 概念**：磁贴位置可配置、可开关，槽位超限时有安置规则。
5. **产品即插件**：KQFLOW 自身降级为"KXFLOW 引擎 + KQFLOW 内核 + 若干业务插件"。

### 1.2 非目标（本版明确不做）

- **不改数据格式**：`schema_version` 不动，`kqflow-data/` 的结构不动，v2.1.0 的数据必须能被 v3.0.0 直接读（`SKILL/references/project-map.md` 的数据契约全部继续有效）。
- **不开放 CGO/动态库式插件**：v0.1.0 的插件是**编译期链接的 Go 包**。跨进程/脚本插件留到引擎能独立发布之后再谈。
- **不做鼠标交互**：保持"默认不接管鼠标"（pitfall：接管后终端无法选中文本，堵死复制粘贴中文）。
- **不重做业务逻辑**：计时、归档、继承、标签、DDL、统计的**行为与测试原样保留**，只换它们的渲染与事件外壳。

---

## 2. 目录与依赖方向

### 2.1 仓库布局（多模块同仓）

```text
The-KQFLOW-Time-manager/
├── go.work                     # 开发期把两个模块编到一起（提交进版本库）
├── go.work.sum
├── go.mod                      # 主模块：github.com/kqin-dev/The-KQFLOW-Time-manager
├── go.sum
├── cmd/kqf/main.go             # 入口（不变）
├── internal/                   # KQFLOW 私有实现（见 §2.3）
│   └── kxapp/                  # ★ 新增：KQFLOW→KXFLOW 的适配层（内核+插件的宿主）
├── kxflow/                     # ★ 独立子模块：github.com/kqin-dev/kxflow
│   ├── go.mod
│   ├── go.sum
│   ├── geometry/               # Rect、锚点、分割
│   ├── canvas/                 # Canvas、Cell、裁剪区、折行、渲染
│   ├── theme/                  # 调色板、样式表、能力降级
│   ├── layout/                 # KXShell：五层布局的唯一权威
│   ├── tile/                   # Tile 接口、TileSlot、TileState
│   ├── stage/                  # Stage、ViewStack、push/pop/borrow
│   ├── plugin/                 # 三类插件接口、Manifest、版本与冲突裁决
│   ├── chrome/                 # HeaderBar、FooterBar、ProgressBar、KeymapHints
│   ├── svc/                    # Services 接口（宿主提供的副作用边界）
│   ├── kxflow.go               # 引擎门面：App、Run、Update、View
│   └── internal/enginetest/    # 引擎自测工具（不对外）
├── docs/                       # ★ 新增：设计文档（含本文）
├── setup/                      # 安装包（不变）
└── SKILL/                      # 开发手册（本版需同步更新）
```

### 2.2 依赖方向（单向，必须可机器校验）

```text
cmd/kqf ──► internal/kxapp ──► kxflow/*        ① 宿主只依赖引擎
   │              │
   └──────────────┴──► internal/{model,store,config,clock,ui(legacy)}
                                               ② 引擎绝不依赖 KQFLOW
```

**硬规则**：`kxflow/` 下的任何包**不得** import `github.com/kqin-dev/The-KQFLOW-Time-manager/...`。
这条规则由 `kxflow/internal/enginetest/noimport_test.go` 用 `go list -deps` 自动校验（§7.1），
违反即测试失败。这是"将来能把引擎拆出去"的**唯一**技术保障，不能只写在文档里。

### 2.3 多模块的工程细节（这里有个真实的坑，必须写明）

Go 的 `go.work` 只对**工作目录之下的构建**生效；而**仓库根目录的模块不包含嵌套子模块的包**。
也就是说：如果主模块需要 `import "github.com/kqin-dev/kxflow/..."`，就必须让 Go 能解析它。
三种让法各有代价：

| 方案 | 开发体验 | 全新克隆能否直接构建 | 将来拆分 |
| --- | --- | --- | --- |
| A. 只靠 `go.work` | 好 | **不能**（无 go.work 时找不到该模块，且私仓拉不到） | 最好 |
| B. `go.mod` 里加 `replace github.com/kqin-dev/kxflow => ./kxflow` | 好 | **能**（自包含，无需网络） | 拆的时候删掉 `replace` + 发布 `kxflow` 打 tag，改动就两行 |
| C. 先发布 kxflow 再用 require 拉版本 | 差（每次改引擎都要发版） | 能 | 一般 |

**本设计选 B**：提交 `go.work`（开发便利）+ 主模块 `go.mod` 里 `replace`（自包含、可复现）。
理由：这个仓库的分发物是**单个 `kqf.exe` + Inno Setup 安装包**，`setup/build-installer.ps1` 会执行
`go build ./cmd/kqf`；方案 A 会让"刚克隆的人"和"没有网络的人"构建失败，代价大于收益。
`replace` 的存在不阻碍拆分——拆分时删掉它并给 `kxflow` 打 tag 即可，且那时才会真正需要 `require`。

> **评审点 1**：是否接受 B 方案（`replace` + `go.work` 双提交）？若你希望严格保持"零 `replace`"，那就得接受 A 方案带来的"先 `go work sync` 才能构建"。

---

## 3. 五层抽象与核心 API

命名遵循 `req.md` 第 47 行以后与 `KXflow.md` 的词汇表（`KXShell`/`HeaderBar`/`SideColumn`/`TileSlot`/`Stage`/`CenterDock`/`push_view`/`pop_view`）。
下面给出**接口签名**，评审重点在这里。

### 3.1 第一层：几何与画布（`geometry` / `canvas`）

```go
// geometry
type Rect struct{ X, Y, W, H int }

// 分割：方法名自带方向，杜绝"A 到底是上半还是下半"。
// （原型里真的踩过一次，见 §0.2）
func (r Rect) CutTop(h int) (top, rest Rect)
func (r Rect) CutBottom(h int) (rest, bottom Rect)
func (r Rect) CutLeft(w int) (left, rest Rect)
func (r Rect) CutRight(w int) (rest, right Rect)
func (r Rect) Inset(dx, dy int) Rect      // 去边框/内边距后的内容区
func (r Rect) Intersect(o Rect) Rect
func (r Rect) Contains(x, y int) bool
func (r Rect) Empty() bool

type Anchor uint8
const (
	AnchorLeftTop Anchor = iota
	AnchorLeftBottom
	AnchorRightTop
	AnchorRightBottom
	AnchorCenterBottomLeft
	AnchorCenterBottomRight
)
```

```go
// canvas —— 全项目唯一的绘制入口
type Cell struct {
	R       rune
	Cont    bool   // 宽字符右半格
	StyleID uint16 // 样式表下标
}

type Canvas struct {
	W, H int
	// Diag 是可选诊断通道；开发模式下由引擎接上，超界/覆盖直接变成错误。
	Diag *Diagnostics
}

func NewCanvas(w, h int) *Canvas

// 裁剪区：所有写入都被它限制。中栏渲染器拿不到左右栏的矩形，就画不过去。
func (c *Canvas) Clip(r Rect) (restore func())

// 写入：越界或超出裁剪区一律返回 false 并计入 Diag。
func (c *Canvas) Set(x, y int, r rune, st StyleID) bool
func (c *Canvas) Text(x, y int, s string, st StyleID) (cols int, clipped bool)
func (c *Canvas) Fill(r Rect, rn rune, st StyleID)
func (c *Canvas) Box(r Rect, frame FrameStyle, st StyleID)

// 折行：全项目唯一的折行实现。
func Wrap(s string, width int) []string

// 输出：必然正好 W 行、每行不超过 W 列。
func (c *Canvas) Render(pal *theme.Palette) string

// 诊断：开发期必须全为 0，测试直接断言。
type Diagnostics struct {
	Overflow   int   // 越界/越裁剪区的写入次数
	Collisions int   // 覆盖了非空格子的次数（跨栏串字、边框被切断）
	Events     []DiagEvent
}
```

**为什么 `Wrap` 在 `canvas` 而不是工具包**：v2.1.0 有 `wrap` / `wrapBalanced` / `truncate` /
`truncateCells` / `truncateCellsFromEnd` / `splitAtColumn` / `displayRange` / `replaceColumns` /
`fitColumns` / `padOrClipLines` / `clipLines` / `clipBlock` / `fit` / `fitText` **十四个**宽度工具散在
`dashboard.go`/`views.go`/`app.go` 三个文件里。新架构里宽度相关的公共工具**只允许**存在于
`canvas` 包，且以"画布宽度"而非"传入的魔数宽度"为依据。

### 3.2 第二层：布局骨架（`layout`）

```go
// KXShell 是布局的唯一权威：所有栏宽、槽位、行数在这里算一次。
type Shell struct {
	Header Rect
	Footer Rect
	Left   Column
	Center Lane
	Right  Column
}

type Column struct {
	Rect  Rect
	Slots [2]Rect // Slot[0] 上、Slot[1] 下；只有一个磁贴时按长条给整个 Rect
}

type Lane struct {
	Rect      Rect
	Stage     Rect // 主控区
	Dock      Rect // 中栏下方停靠区（CenterDock），0~2 个磁贴
	DockSlots [2]Rect
}

// Layout 一次算完。纯函数：给定终端尺寸与视图配置，结果完全确定。
func Layout(size geometry.Size, cfg LayoutConfig) Shell

type LayoutConfig struct {
	HeaderRows int          // 默认 1（有 toast 时 2）
	FooterRows int          // 默认 1
	SideWidths SideWidths   // 宽度断点表（见下）
	DockRows   int          // 0 = 隐藏停靠区
	MaxBodyW   int          // 正文最大列数（对应 v2.1.0 的 maxContentWidth=92）
}
```

**规则（把 v2.1.0 隐式做对的东西写成显式约束）**：

- `Shell.Header` 在最上、`Shell.Footer` 在最下，`Left/Center/Right` 在中间**等高**。
- 三栏宽度：默认 `34/剩余/30`，断点 `<110` → `28/24`，`<90` → `24/22`（沿用 v2.1.0 实测值，
  已在 60~160 列被测试覆盖，不擅自改）。
- **中栏最小宽度 48**，不足时从左右栏按比例借，但左栏不得低于 18、右栏不得低于 16（沿用 v2.1.0 的兜底逻辑）。
- 任何 `Rect.W` 或 `Rect.H` 不得为负：`Layout` 必须在极小尺寸（1×1）下也返回合法矩形。
- **高度权威在此**：`bodyH = H - HeaderRows - FooterRows`，`viewTooSmall` 那条路径也要用它，
  不允许再出现第二个高度来源。

### 3.3 第三层：磁贴与槽位（`tile`）

```go
// Tile 是挂载到插槽中的独立业务组件，也是第三方接入点。
type Tile interface {
	// Meta 声明身份、版本与容量需求（供插件管理器裁决冲突）。
	Meta() plugin.Manifest

	// Title 是磁贴标题栏文本（不含快捷键记号）。
	Title() string

	// Render 把内容画进自己的矩形。ctx.Rect 已经是"保证合法的"内容区。
	// 实现者不允许、也没有能力画到 Rect 之外（画布裁剪区）。
	Render(ctx RenderCtx)

	// Update 处理落在本磁贴上的按键/消息。
	// 返回的 Action 由宿主（内核）解释——引擎不认识业务语义。
	Update(ctx EventCtx, ev Event) Action
}

type TileState uint8
const (
	TileIdle TileState = iota
	TileFocused
	TileCollapsed
	TileOccupied // 被 Stage 借调占用（焦点在舞台上，磁贴只渲染不接收按键）
)

type RenderCtx struct {
	Canvas *canvas.Canvas
	Rect   geometry.Rect // 内容区（已去掉边框与内边距）
	Frame  geometry.Rect // 含边框的外框
	State  TileState
	Theme  *theme.Theme
	Svc    svc.Services // 只读能力：时钟、翻译、只读数据
	// Focus 只有 State==TileFocused 时为真。
	Focus bool
}
```

**槽位容量与安置规则**（`req.md`："单个磁贴时渲染为长条，两个磁贴时各占一半"）：

- `Column.Slots[0]` 与 `Slots[1]` 在装载 1 个磁贴时合并为整条（长条）；装载 2 个时各占一半。
- 槽位上限 2。**超限不报错、不覆盖，而是进入"未安置磁贴"列表**，由用户在视图配置里决定谁上谁下（§6）。
- 中栏 `Dock` 同理，且 `DockRows = 0` 时整块隐藏（`req.md`："可选 0~2 个磁贴"）。

### 3.4 第四层：主舞台与视图栈（`stage`）——本设计的核心亮点

```go
// Stage 是中栏主控区，它是一个视图栈。
// 默认底层是 Dashboard（Base View）；任何磁贴可以 push 一个视图来"借调"主控区。
type Stage struct {
	base  View   // Dashboard
	stack []View // 借调视图栈，栈空时显示 base
}

// 借调（req.md 的 Borrow / Requisition）
func (s *Stage) Push(v View)              // stage.push_view(submenu_view)
func (s *Stage) Pop() (View, bool)        // 完成后交还控制权
func (s *Stage) PopToBottom()             // 一路退回 Dashboard
func (s *Stage) Replace(v View)           // 同级替换（向导的下一步）
func (s *Stage) Top() View                // 当前接受按键与渲染的视图
func (s *Stage) Depth() int
func (s *Stage) SetBase(v View)

// View 是所有可在 Stage 中展示的视图基类。
type View interface {
	Name() string
	Render(ctx RenderCtx)
	// Update 返回 Action；RequestClose 为真时引擎自动 Pop 并把焦点交还原磁贴。
	Update(ctx EventCtx, ev Event) (act Action, requestClose bool)
	// OnEnter / OnExit 用于把焦点还给原磁贴（req.md 的 Handover / Yield）。
	OnEnter(origin Origin)
	OnExit()
}

// Origin 记录"谁借调了舞台"，用于 Pop 之后把焦点交还。
type Origin struct {
	TileID string
	Anchor geometry.Anchor
}
```

**借调时序**（`req.md` 第 101~116 行伪代码的 Go 版）：

```text
用户在「专注计时」磁贴上按 enter
  → tile.Update 返回 stage.Borrow(NewTimerWizard(...))
  → Stage.Push(wizard)，记录 Origin{TileID:"focus", Anchor:AnchorLeftTop}
  → 原磁贴进入 TileOccupied（只渲染、不收按键）
  → wizard 内部再 push 一级（多级菜单 = 栈深 > 1），esc 逐级 Pop
  → 业务完成：requestClose=true → 引擎 Pop，焦点按 Origin 交还原磁贴
  → 栈空 → 主控区回到 Dashboard
```

**与 v2.1.0 的对应关系**：v2.1.0 的"二级内容一律复用中间栏"（conventions 第 2 条）
本来就是这套机制的手工版——`renderView()` 里那条 11 分支的优先级 if 链
（`stopAsk → ntfyHelp → pick → editor → custom → view 枚举`）正是**没有视图栈时的替代品**。
新架构把它换成**显式的栈**，并且栈里的每一项都自带渲染与按键处理，不再集中在一个巨型 `switch` 里。

### 3.5 第五层：状态与事件（`kxflow.go` + `svc`）

```go
// 引擎门面：事件循环与终端驱动。保持 Bubble Tea 的 Elm 结构，宿主无需改习惯。
type App struct {
	shell    layout.Shell
	stage    *stage.Stage
	manager  *plugin.Manager // 装载好的整合包（见 §4）
	theme    *theme.Theme
	svc      svc.Services
	anim     AnimState
	// selection 是全局"当前选中上下文"，由引擎维护、供联动选项消费（见 §4.2）。
	// 它必须由引擎持有而不是某个磁贴私有：否则"设 DDL"这类选项
	// 就得认识具体是哪个磁贴在持有选中项。
	selection Selection
	focus     Focus // 当前焦点落在哪块区域（侧栏槽位 / 中栏停靠区 / 舞台）
}

func New(cfg Config) *App
func (a *App) Init() tea.Cmd
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (a *App) View() string // 内部：一帧 = 一次 Layout + 一次 Canvas 绘制
func (a *App) Resize(w, h int)

// Selection 由磁贴通过 ActionSelect 上报，引擎负责归一化与广播。
func (a *App) SetSelection(s Selection)
func (a *App) Selection() Selection
// ContextOptions 返回当前选中上下文下"适用"的联动选项（见 §4.2）。
func (a *App) ContextOptions() []plugin.ContextOption
```

```go
// svc：宿主提供的副作用边界。引擎保持纯粹，副作用（写盘/响铃/推送）全部经由它。
type Services interface {
	Clock() time.Time
	// Persist 是唯一的写盘通道；引擎不关心落盘格式，也不直接碰文件。
	Persist(ctx context.Context, req PersistRequest) error
	// Effect 播一次副作用（响铃、流光触发、系统通知）。引擎不实现平台细节。
	Effect(e Effect)
	// Capabilities 报告当前终端能力，供样式降级使用。
	Capabilities() Capabilities
}

type Effect struct {
	Kind EffectKind // EffectBell | EffectFlash | EffectPush
	Text string
	Data map[string]any
}
```

**这一层直接修掉一个历史 bug**：`SKILL/references/pitfalls.md` 记录"响铃曾经挂在看板的渲染路径上，
用户停在设置页就不响"。新架构里 `Effect` 只能从 `Update` 里发出，渲染函数是纯的
（`Render(ctx)` 不返回值、不写状态），这类错误**没有地方可以犯**。

> 同理，v2.1.0 里 `pageContent()` 在渲染路径里写 `a.pageScroll` 的做法在新架构被禁止：
> 滚动偏移属于 `Stage`/视图的状态，只能在 `Update` 里改。

---

## 4. 插件体系（`plugin`）

> **本节在评审后按用户意见重写**（2026-10-05）。用户指出两件事，其中第二件补掉了
> 原方案的一个结构性缺口，因此改为**两层模型**：
>
> 1. "选项插件"有歧义——**联动选项**（如给选中条目设 DDL）与**看板选项**
>    （如设置/帮助）不是同类东西，必须在种类上分开。
> 2. 一个功能天然横跨多种插件类型时（DDL = 磁贴 + 联动选项；计时 = 磁贴 + 视图 + 联动选项），
>    原方案只能让内核硬编码它们的内部连线——**那等于把耦合藏进内核**，
>    正是我们要消灭的东西。用户提出**整合包（Pack）**：一整套有机结合、
>    一般不能独立开关、彼此有调用关系的插件。
>
> 这正是"引擎只认插件、宿主只认包"的分工：**包内有机耦合，包间声明式解耦**。

### 4.1 两层模型：包（装载单位）与插件（渲染单位）

```text
整合包 Pack          ← 装载 / 版本 / 冲突 / 用户开关 的单位
  └── 插件 Plugin     ← 引擎认识的最小渲染与事件单位
        ├── Tile           磁贴：占槽位
        ├── BoardOption    看板选项：常驻中栏选项列表
        └── ContextOption  联动选项：只在特定"选中上下文"成立时才有意义
```

| | Pack（整合包） | Plugin（插件） |
| --- | --- | --- |
| 是谁的单位 | 装载、版本、冲突裁决、**用户开关** | 渲染与事件 |
| 包内关系 | **允许**任意互相调用（一起写、一起发版） | 不关心同伴 |
| 包间关系 | 只允许声明式 `Requires` / `Conflicts` | **不得**跨包调用 |
| 用户能否单独开关 | **能**（这就是开关粒度） | 不能 |
| 与数据的关系 | 声明 `DataSchema` | 只读写包内约定的数据 |

**为什么开关粒度是包而不是插件**：`DDL` 的"磁贴"与"联动选项"拆开都不成立——
只留磁贴就没法设时间，只留选项就看不见已有截止时间。用户要的是"我要不要 DDL 这个功能"，
不是"我要 DDL 的左半边"。**而"磁贴放哪、显示不显示"仍然自由**（§6 的 `ViewConfig`），
两者是不同层的问题，不要混为一谈。

### 4.2 五种插件类型

```go
type Kind uint8
const (
	KindKernel        Kind = iota // 内核：把引擎"武装"成某个 CLI 工具（单例）
	KindTile                      // 磁贴：占用槽位
	KindBoardOption               // 看板选项：常驻中栏选项列表（永远在）
	KindContextOption             // 联动选项：依赖"当前选中"上下文（条件出现）
	KindService                   // 服务：不渲染，只提供能力（提醒、持久化钩子等）
)
```

**联动选项 vs 看板选项的区别就在"存在条件"**：

| | 看板选项 `BoardOption` | 联动选项 `ContextOption` |
| --- | --- | --- |
| 何时出现 | 永远 | 仅当 `Selection` 满足 `AppliesTo` |
| 例子 | 设置 / 帮助 / 历史 / 退出 / 随手记 | 给选中条目打标签、设 DDL |
| 需要什么 | 无 | 引擎提供的 `Selection`（选中上下文） |
| 触发的界面 | 通常是常驻视图 | 通常借调 `Stage` 显示二级页 |

```go
// Selection 是引擎暴露给联动选项的"当前选中上下文"。
//
// 关键点：它由**引擎**统一维护，而不是由某个磁贴私有。这样"给选中条目设 DDL"
// 这类选项既能被侧栏磁贴里的选中项触发，也能被中栏停靠区的选中项触发——
// 而它自己不需要认识任何一个磁贴。
type Selection struct {
	// Kind 是选中对象的类别，如 "todo" / "goal" / "session" / "none"。
	Kind string
	// ID 是选中对象的稳定标识（空表示没有选中）。
	ID string
	// Title 供选项显示（如"给「写架构文档」设截止时间"）。
	Title string
	// Can 描述该选中对象支持哪些能力（capability 标记），
	// 联动选项据此判断自己是否适用，而不必认识具体类型。
	Can Capability
}

// ContextOption 只在 Selection 满足条件时才有意义。
type ContextOption interface {
	Plugin
	// AppliesTo 声明它适用于哪类选中对象（对应 Selection.Kind）。
	AppliesTo() []string
	// Requires 声明它需要的能力标记（对应 Selection.Can）。
	Requires() []string
	// Label 用 Selection 生成显示文本（可以带上选中项的名字）。
	Label(s Selection) string
	// Activate 返回要借调舞台的视图。
	Activate(s Selection, svc svc.Services) (stage.View, error)
}
```

### 4.3 插件与包的元数据

```go
// Plugin 是所有插件的共同接口。身份、版本与冲突声明都在这里。
type Plugin interface {
	// Manifest 声明身份与版本需求。引擎与包管理器只信它。
	Manifest() Manifest
	// New 由包在装配时调用，注入宿主能力并返回可用的组件实例。
	New(svc svc.Services) (Component, error)
}

// Component 是"已经装配好、可以渲染与收事件"的组件。
type Component interface {
	Title() string
	Render(ctx RenderCtx)
	Update(ctx EventCtx, ev Event) Action
}

// Manifest 描述一个插件。版本与冲突裁决只依据这里，不看注册顺序。
type Manifest struct {
	ID         string         // 全局唯一，建议 "kqflow.todo.fixed"
	Name       string         // 显示名（中文）
	Kind       Kind
	Version    semver.Version // 插件自身版本
	EngineAPI  semver.Range   // 需要的 KXFLOW API 版本范围，如 ">=0.1 <0.2"
	DataSchema int            // 它读写的数据结构版本（对应 KQFLOW 的 schema_version）
	Slots      SlotPreference // 磁贴才有：期望位置与优先级
}

// Pack 是装载与开关的单位：一整套有机结合、彼此有调用关系的插件。
//
// 包**内部**的调用关系由包自己装配（New 的时候把彼此接上），引擎不管；
// 包**之间**只允许下面这两个声明，不允许直接调用。
type Pack interface {
	ID() string
	Name() string
	Version() semver.Version
	// EngineAPI 是整个包共同要求的引擎版本（包内取交集，最严的那个生效）。
	EngineAPI() semver.Range
	// Members 返回包内全部插件。它们不单独开关，只随包一起装载或卸载。
	Members() []Plugin
	// Requires 声明依赖哪些**其他包**的能力标记；缺失则整包拒绝加载。
	Requires() []string
	// Provides 声明本包提供哪些能力标记。
	Provides() []string
	// Conflicts 声明与哪些包 ID 互斥。
	Conflicts() []string
	// Assemble 由宿主在装载时调用：把包内成员的互相引用接好（注入 Services、
	// 解析 Selection 依赖、共享包内状态）。引擎不解释它的内部结构。
	Assemble(svc svc.Services) (Assembled, error)
}

// Assembled 是装载完成的包：引擎从这里取到已经装好的组件。
type Assembled interface {
	Pack() Pack
	// Tiles 返回本包提供的磁贴（按 Manifest.Slots 参与槽位安置）。
	Tiles() []Component
	// BoardOptions / ContextOptions 返回本包提供的两类选项。
	BoardOptions() []BoardOption
	ContextOptions() []ContextOption
	// Services 返回本包提供的服务（不渲染，只提供能力）。
	Services() []Service
	// Dispose 释放包内资源（定时器、网络连接等）。
	Dispose()
}
```

**内核插件的特殊性**：内核是单例，且它**本身也是一个包**（`Pack` 里只有它一个成员）。
这样装载路径只有一条，不必为内核开特例。内核额外提供 LOGO 与默认看板视图：

```go
type Kernel interface {
	Pack
	Logo(width int, pal canvas.Palette) []canvas.Line
	Dashboard() stage.View // Stage 的默认底层视图
	DecorateHeader(h *chrome.HeaderBar)
	DecorateFooter(f *chrome.FooterBar)
}
```

### 4.4 插件管理器与冲突裁决（`req.md` 的四类冲突）

`req.md` 要求考虑四类冲突。加了包这一层之后，**每类冲突的裁决单位都是包**，
规则必须可预测、可解释（不允许"看注册顺序"）：

| 冲突类型 | 裁决单位 | 裁决依据 | 冲突时的行为 |
| --- | --- | --- | --- |
| 包 ↔ 引擎 | 包 | `Pack.EngineAPI()` 语义化范围 | 不匹配 → **整包拒绝**（错误），其余包照常启动，报告里列出原因 |
| 内核 ↔ 内核 | 包 | 内核是单例 | 第二个内核包 → 拒绝（错误）；多个候选时由宿主配置指定 |
| 包 ↔ 包 | 包 | `Conflicts` + 重复 `ID` | 显式冲突/重复 ID → 后者被拒（错误） |
| **包内**成员冲突 | 插件 | 同 `ID` / 同 `Kind` 的两个内核级成员 | **视为包自身缺陷，装载期直接失败**（开发者错误，不该静默降级） |
| 包 ↔ 数据库 | 包 | 包内成员 `DataSchema` 的最大值 vs 数据实际版本 | 要求低于数据的包 → 拒绝（错误，沿用 v2.1.0 的保守策略） |
| **包 ↔ 接纳它的组件** | 包 | `Requires` 是否被满足 | **不是冲突，是警告**（见下 §4.5） |

```go
type Manager struct {
	kernel Kernel
	packs  []Assembled
	report LoadReport
}

// Load 按上表逐条裁决。除"包内冲突"外，失败一律记录而不 panic：
// CLI 工具"少一个包"必须仍然可用，"起不来"才是事故。
func (m *Manager) Load(engineAPI semver.Version, dataSchema int, packs ...Pack) LoadReport

// LoadReport 有三类互不相同的成员，**不要混用**：
type LoadReport struct {
	Loaded   []string    // 装载成功且已装配（顺序即依赖序）
	Inactive []string    // 声明正常、依赖也满足，但当前没有组件接纳它（警告）
	Rejected []Rejection // 包**有问题**，被拒（错误）
	Warnings []Warning   // 全部警告（含 Disabled 与 Inactive 的原因）
	Disabled []string    // 用户主动关闭
	KernelID string
	Capabilities []string
}

// OK 只看"有没有内核"与"有没有错误"。
//
// **不看警告**：把警告算进来会让"少一个可选功能"变成"启动失败"，方向就错了。
func (r LoadReport) OK() bool
```

**裁决必须可解释**：启动报告（以及设置页的"插件"一栏）要能回答
"为什么我的 DDL 没出现"——直接对应 `req.md` 的"bug 定位困难"。

### 4.5 警告 vs 错误：「没有水瓶给水」（评审后新增）

这一节来自用户的一条修正，它指出了原方案里一个**真实的语义错误**：

> "如果某个插件如 labels 包，在由于缺乏必要包时会失效，如没有 todo 包也没有 goal 包，
> 这种情况属于安装了一个正常的包，但是没有任何包可以接纳它。这种和整合包不同的是，
> 缺乏其一只是功能缺失，而不是全部都不能用，因此只需要在包管理器里给个警告（而不是错误）
> 提示此包是正常的，但是虽然启用却没有任何作用（缺少接纳它的组件），因此静默失效了，
> 因为它不是有问题，而是『没有水瓶给水』这种警告类型的问题。"

原方案把"依赖没满足"一律记成 `RejectMissingCapability`（**错误**），
于是用户看到的是"我的标签包坏了"——而它根本没坏。

**现在的规则**：

| 情形 | 归类 | 报告位置 | 说明措辞 |
| --- | --- | --- | --- |
| 声明缺陷 / 版本不匹配 / 冲突 / 数据版本 | **错误** | `Rejected` | 指出哪一条不满足 |
| 用户关闭 | 警告 | `Warnings` + `Disabled` | "它本身没有问题" |
| 缺的能力**只有被关闭的包**提供 | 警告 | `Warnings` + `Inactive` | **可操作指引**："开启 X 包即可让本包生效" |
| 完全没有提供者 | 警告 | `Warnings` + `Inactive` | "没有任何组件会接纳它（包本身没有问题）" |
| 依赖成环而互相等待 | 警告 | `Warnings` + `Inactive` | 点出是环，不说它有毛病 |
| 磁贴没有位置可放 | 警告 | `Warnings` | 由视图配置决定，不是包的错 |

```go
// WarnReason 是**警告**的原因：包是正常的，只是当前没起作用。
const (
	WarnDisabled         // 用户关掉了它
	WarnNoHost           // 没有任何组件愿意接纳它 ← "没有水瓶给水"
	WarnProviderDisabled // 它需要的组件存在，但被关了（给可操作指引）
	WarnTileUnplaced     // 磁贴没有位置可放
)
```

**为什么"提供者被关闭"要单独一类**：用户需要知道的是**去开哪个包**，
而不是只被告知"缺少能力标记 `item.selection`"。

**`Inactive` 的处置**：这类包**不装配**（它没有宿主，装上也永远不会出现），
但绝不阻断引擎启动。它出现在设置页的"插件"一栏里，带一句"已启用但当前无作用"。

### 4.6 数据版本策略（与 v2.1.0 一致，不动）


- `schema_version` 是**数据**版本，程序版本号是另一套编号，二者独立（`SKILL/references/release.md` §0）。
- 开发期**一律不动** `schema_version`（用户明确要求过）。
- 引擎的 `EngineAPI` 版本从 `0.1.0` 起，并遵守：**引擎的破坏性改动必须抬 minor**（0.x 阶段
  minor 即破坏性变更位），这是插件 `EngineAPI` 范围能起作用的前提。
- 包的版本与成员的版本是两套：包版本是"功能整体"的版本（用户看这个），
  成员版本是"接口"的版本（引擎看这个）。

### 4.7 磁贴安置（视图配置的执行者）

```go
// Placements 把装载成功的磁贴按 ViewConfig 安置到槽位。
//
// 返回三样东西，**都要用上**：
//   placed   锚点 → 插件 ID
//   unplaced 没位置可放的磁贴（不是错误，由用户改视图配置解决）
//   issues   配置与现状对不上的地方（例如指向了一个没装载的包）
func (m *Manager) Placements(vc ViewConfig) (placed Placed, unplaced []Manifest, issues []PlacementIssue)
```

安置规则（可预测、可复现，由测试穷举）：

1. **用户配置优先**：视图配置里显式放置的磁贴先占位；
2. 配置里指定了、但那个包没装载 → 记一条 `PlacementIssue`。
   绝不静默忽略：否则用户看到的是"我明明设了，它却没了"；
3. **磁贴明确声明了槽位就只放那儿**：那儿被占了、或那是被关闭的停靠区 →
   进 `unplaced`，**不另找地方**。理由：一个专门声明"我要在停靠区"的磁贴
   被塞进侧栏，它的排版假设全是错的；
4. 只声明了优先级（`AnchorUnset`）的磁贴，按优先级降序、同优先级按 ID 字典序，
   在**可见**的槽位里找空位（停靠区关掉时跳过停靠槽位）；
5. 放不下的进 `unplaced`——**不报错、不覆盖**。

> 第 3、4 条都在实现时出过错（分别是"关掉停靠区东西还在"与"配置指向停靠区
> 却被塞进侧栏"），现在各有测试守着。

**停靠区的形态**（按用户 2026-10-05 实机反馈确定，见 §10.5）：

```text
0 个磁贴  → 不开停靠区，整个中栏都是看板的
1 个磁贴  → 占下半中栏的整条（长条，适合需要大空间的磁贴）
2 个磁贴  → 下左 / 下右 各半（**不是上下叠着放**）
```

"槽位装不下就判空、绝不让两个槽位重叠"是硬规则：早先版本在矩形太矮时让
两个槽位都指向整块，注释里还写着"不会出现重叠"——**实际就是重叠**，
实测 40×10 下画布报出 62 次"覆盖已有内容"。

---

## 5. KQFLOW 如何降级为"内核 + 整合包"

`internal/kxapp/` 是宿主适配层：把现有 `model`/`store`/`config`/`clock` 接到引擎的
`svc.Services` 与各整合包上。**业务逻辑一行不改**，只换外壳。

### 5.1 KQFLOW 的整合包划分

**划分原则**：一个包 = 一个"用户能理解并单独开关的功能整体"。
判断标准是问一句"**把它拆开一半，另一半还说得通吗**"——
`DDL` 拆开就说不通（只有面板没法设时间、只有设置项看不到结果），所以它必须是一个包；
`随手记` 的磁贴与选项拆开各自都说得通，所以它们的内部关系可以留在包内自由处理。

| 包 ID | 成员 | 成员类型 | 承载的现有功能 | 现有代码来源 |
| --- | --- | --- | --- | --- |
| `kqflow.core`（内核） | `kqflow.kernel` | 内核 | LOGO、"Power by KXFLOW"、**看板选项**（设置/帮助/历史/退出）、日界线、默认看板 | `ui/logo.go`、`menuItems`、`helpLines`、`settingsLines`、`historyLines` |
| `kqflow.todo` | `kqflow.todo.fixed`、`kqflow.todo.floating`、`kqflow.todo.task`、`kqflow.todo.ctx` | 磁贴 ×3 + **联动选项** | 固定/临时 TODO、子任务、勾选与增删改入口 | `renderTodoPanel`、`app.go` 的 todo 动作 |
| `kqflow.goal` | `kqflow.goal.list`、`kqflow.goal.ctx` | 磁贴 + **联动选项** | GOAL 列表（含归档）与目标操作 | `renderGoalPanel`、`handleGoalAction` |
| `kqflow.ddl` | `kqflow.ddl.panel`、`kqflow.ddl.ctx` | 磁贴 + **联动选项** | 截止时间面板（看板）+ 给选中条目设时间（选项） | `renderDdlPanel`、`ui/ddl.go` |
| `kqflow.labels` | `kqflow.labels.ctx` | **联动选项** | 给选中条目打标签 | `ui/labels.go` |
| `kqflow.timer` | `kqflow.timer.tile`、`kqflow.timer.board`、`kqflow.timer.ctx` | 磁贴 + 看板选项 + 联动选项 | 四种计时的入口、计时中的 `p` 菜单、计时摘要、归档 | `ui/state.go` 的 timer、`runAction`、`renderTimerBar` |
| `kqflow.note` | `kqflow.note.tile`、`kqflow.note.board` | 磁贴 + 看板选项 | 随手记（看板预览 + 编辑入口） | `ui/note.go` |
| `kqflow.stats` | `kqflow.stats.tile` | 磁贴 | 连续 7 天柱状图 | `ui/stats.go` |
| `kqflow.notify` | `kqflow.notify.svc`、`kqflow.notify.board` | 服务 + 看板选项 | 时段切换提醒（流光/响铃/ntfy 推送）与其测试入口 | `ui/notify.go`、`ui/notify_ntfy.go` |

**这张表纠正了评审前的一个错划分**：原先建议"固定 TODO / 临时 TODO 拆成两个**包**"。
按包的定义看它们不成立——它们是**同一个包 `kqflow.todo` 的两个磁贴**（同一功能的两半），
拆成两个包会让用户能关掉一半、留下语义残缺的功能。
**但它们在 `ViewConfig` 里各自可以显示/隐藏、换槽位**，灵活性没有损失（§6）。

**`kqflow.ddl` 是"包"这个概念最好的例子**：它同时提供
① 看板上的 DDL 面板（磁贴）、② 给当前选中条目设截止时间（联动选项）。
两者共享同一份排序与到期计算逻辑，**必须一起装载**；而它们与
`kqflow.todo` / `kqflow.goal` 之间**没有**直接调用关系——
它们只通过引擎的 `Selection` 与 `Action` 交互。这就是"包内有机耦合、
包间声明式解耦"的落点。

**包间依赖示例**：

```text
kqflow.todo   Provides: ["item.selection"]        （提供"条目选中"上下文）
kqflow.goal   Provides: ["item.selection"]
kqflow.ddl    Requires: ["item.selection"]        （没有可选中条目就没有意义）
kqflow.labels Requires: ["item.selection"]
kqflow.timer  Provides: ["focus.session"]         （提供计时会话记录）
kqflow.stats  Requires: ["focus.session"]         （柱状图要读专注时长）
kqflow.notify Provides: ["notify.channel"]
```

好处是**可解释**：如果用户关掉了 `kqflow.todo` 与 `kqflow.goal`，
报告会明确写出"`kqflow.ddl` 已启用但当前无作用：没有任何已启用的包提供
`item.selection`"——而且措辞是**警告**不是错误（§4.5）。
它没有坏，只是没有接纳它的组件。

#### 核心功能推荐包

有一类包**不是内核自带，但属于推荐安装的核心功能**：它们要么提供关键上下文
（`kqflow.todo`、`kqflow.goal` 提供"条目选中"），要么是被依赖的基础
（`kqflow.timer` 提供计时会话记录）。README 里应当把这一点讲清楚——
它们可以关，但关掉之后依赖它们的包会静默失效（引擎会给警告说明原因）。

| 分类 | 包 | 说明 |
| --- | --- | --- |
| 内核自带 | `kqflow.core` | 不可卸载：LOGO、全局选项、默认看板 |
| **推荐核心** | `kqflow.todo`、`kqflow.goal`、`kqflow.timer` | 关掉会导致别的包失去接纳者 |
| 普通功能 | `kqflow.ddl`、`kqflow.labels`、`kqflow.note`、`kqflow.stats`、`kqflow.notify` | 独立完整，关了只是少一个功能 |

> 这条区分是用户在评审里提出的："这种类型的包属于核心功能推荐包，
> 后续可以在 readme 中提出：虽然其不是内核自带的，但是其是推荐安装的核心功能包。"

### 5.2 跨包协调：一切经由 `Selection` 与 `Action`

包与包之间不允许互相调用（否则又耦合了）。跨包的动作由内核统一转译：

```go
// 插件返回的 Action 是"意图"，不是"操作"。
type Action struct {
	Kind    ActionKind // ActionPersist | ActionEffect | ActionBorrow | ActionSelect | ActionToast
	Payload any
}
```

两个方向各有一个统一通道：

- **数据方向 `Selection`**：谁被选中由引擎维护。`kqflow.todo` 里的磁贴
  把选中项报给引擎（`ActionSelect`），引擎更新 `Selection`；
  `kqflow.ddl.ctx` / `kqflow.labels.ctx` 据此决定自己是否出现。
  **联动选项因此不需要认识任何磁贴**——连"选中项来自侧栏还是中栏停靠区"都不必知道。
- **动作方向 `Action`**：`kqflow.todo.floating` 勾选完成
  → `Action{ActionPersist, ToggleTodo{...}}` → 内核执行 `store.SaveDay`，
  再检查"今日全部完成" → 若完成则发 `Action{ActionEffect, Celebrate}`。
  **磁贴不知道存储、不知道庆祝动画存在**。

### 5.3 内核的职责边界

内核包（`kqflow.core`）只做四件事，**不做业务**：

1. 渲染 LOGO 与"Power by KXFLOW"，提供默认看板视图；
2. 提供全局看板选项（设置/帮助/历史/退出）——它们是**内核自带**的选项，
   不是"选项插件"，因为任何基于 KXFLOW 的产品都需要它们；
3. 解释 `Action`（落盘、播副作用、借调舞台、更新 `Selection`）；
4. 维护 `Selection` 与焦点，供联动选项使用。

> **评审点 2（已按用户意见修订）**：这张表就是最终的功能归属。
> 请确认两处：
> ① `kqflow.ddl` / `kqflow.labels` 作为**只含联动选项的独立包**是否合理
> （它们依赖 `item.selection`，所以关掉 TODO 与 GOAL 时会连带不装载）；
> ② `kqflow.timer` 同时含磁贴 / 看板选项 / 联动选项三种成员，是否同意它们属于一个包。

---

## 6. 视图配置（View，`req.md` 第 41 行）

```go
// ViewConfig 存在配置里（与日界线无关，属于"偏好"），不进日数据。
type ViewConfig struct {
	// Slot 到插件 ID 的映射；"" 表示空槽。
	Left   [2]string `json:"left"`
	Right  [2]string `json:"right"`
	Dock   [2]string `json:"dock"`
	DockVisible bool `json:"dock_visible"`
}

func (v ViewConfig) Validate(avail []plugin.Manifest) []PlacementIssue
```

槽位超限时的安置规则（明确、可预测）：

1. 已在 `ViewConfig` 里显式安置的磁贴优先占位；
2. 其余磁贴按 `Manifest.Slots.Priority` 降序、同优先级按 ID 字典序填充空槽；
3. 仍然放不下的进入"未安置"列表，在设置页可见，用户可显式替换某个槽位。

配置兼容：`view_config` 是**新增字段**，老配置读入时为 nil → 用内核给的默认布局；
按 conventions 第 8 条走 `omitempty`，**不抬 `schema_version`**。

---

## 7. 验证策略（这一节决定方案能不能算"完成"）

### 7.1 引擎自身：结构性保证必须被测试证明

| 测试 | 断言 |
| --- | --- |
| `TestCanvasNeverOverflows` | 在 1×1 到 200×60 的尺寸网格上，随机内容 + 随机矩形；每帧 `Render()` 的行数恰好 = H、每行宽度 ≤ W |
| `TestCanvasDiagnosticsClean` | 正常渲染路径下 `Diag.Overflow == 0 && Diag.Collisions == 0`（**开发期必须为 0**，不是"尽量小"） |
| `TestClipSurvivesMaliciousTile` | 故意写一个"想画到全屏"的恶意磁贴，结果只能落在自己矩形内，且 `Collisions > 0` 被诊断抓到 |
| `TestLayoutIsPureAndTotal` | 同一 `(size, cfg)` 调用 `Layout` 两次结果相同；尺寸从 0×0 到 400×200 全部返回非负矩形 |
| `TestWrapNeverLosesText` | 折行后把各行拼接（去空白）**逐字包含**原文——直接搬 `TestDisclaimerCompleteInBothViews` 的思路（pitfall 明确要求） |
| `TestStageStackOrder` | push/pop 顺序、`PopToBottom`、`Origin` 交还焦点、栈深上限 |
| `TestNoEngineImportsApp` | `go list -deps ./kxflow/...` 里不得出现 `The-KQFLOW-Time-manager`（§2.2 的机器校验） |

### 7.2 迁移期：既有测试是护栏，不许为了让新代码过而改断言

`internal/ui` 现有测试（`overflow_test.go`、`cursor_test.go`、`timer_stop_test.go`、
`editor_close_test.go`、`version_quit_test.go`、`app_test.go` 等）在 v3.0.0 里分两类处理：

- **业务类**（计时归档、退出保护、数据自愈、版本守卫）→ **原样保留**，新路径必须让它们继续绿。
- **排版类**（`assertNoOverflow`、`cellAt` 列位置）→ 迁移到**引擎侧的等价断言**，并且标准提高：
  从"不在真实终端里超宽"提高到"画布诊断恒为 0"。旧断言不删除，改为同时跑，直到该页面完全迁移。

新增"入口清单"测试（conventions 第 9 条）：新引擎下的**每个视图 × 每个按键**都走一遍，
而不是只测用户报的那一条。

### 7.3 真机验证：必须交 exe

本环境跑不了交互式 TUI（`SKILL` Edge cases 明确：不要用 `Start-Process` 跑 `kqf.exe`，
会留僵死进程）。因此：

- 每个可交付里程碑都编译一个 **`-dev.N` 后缀的 `kqf.exe`** 给你做实机对比
  （正式版本号只发版时改）；
- 交付说明里如实写明"用离屏渲染验证到什么程度"和"哪些观感未经真机确认"。

---

## 8. 分阶段路线（并行 + 开关切换）

开关：配置里新增 `engine: "legacy" | "kxflow"`，**默认 `legacy`**，
`kxflow` 只对开发者/尝鲜者开放，直到 §8 的 M5 完成。

| 里程碑 | 内容 | 交付与验收 |
| --- | --- | --- |
| **M0** | 设计文档评审（本文） | 你确认 §2.3 / §4.1 / §5.1 三个评审点 |
| **M1** | `kxflow` 模块落地：`geometry` + `canvas` + `theme` + `Wrap`；引擎自测全套（§7.1） | 引擎测试全绿；`go test ./kxflow/...`；**不动** KQFLOW 任何现有代码 |
| **M2** | `layout`（KXShell）+ `tile` + `stage` + `chrome`；`kxflow demo` 子命令（一个假内核 + 两个假磁贴的演示界面） | 可运行的独立演示 exe，你能在真机上看到"上栏/下栏/左右槽位/舞台借调"；验证 §3.4 的借调手感 |
| **M3** | `plugin`（Manifest、Manager、四类冲突裁决）+ `svc`；引擎 API 冻结到 `v0.1.0` | 冲突裁决的单元测试（每类冲突至少一正一反） |
| **M4** | `internal/kxapp`：KQFLOW 内核 + 只读磁贴（固定/临时 TODO、GOAL、DDL、统计）。看板在 `engine: kxflow` 下可用 | 交 `-dev.1` exe；与 v2.1.0 看板并排截图对比；旧路径与旧测试保持全绿 |
| **M5** | 交互类插件（计时、随手记、标签、设置、历史、帮助、提醒）+ ViewConfig 视图配置 | 交 `-dev.2` exe；**默认引擎切到 `kxflow`**；`legacy` 保留一个版本作为退路 |
| **M6** | 清账：删除 `internal/ui` 中被取代的渲染函数与十四个宽度工具；更新 `SKILL/`、`README.md`、`ROADMAP.md`；引擎独立发布的准备（去掉 `replace` 的说明） | 全量测试绿 + `gofmt`/`vet` 干净；SKILL 增写"如何写一个 KXFLOW 插件" |

**每个里程碑一个提交（或一组聚焦提交）**，中文提交信息写**根因**不写现象（沿用 conventions 第 10 条）。

---

## 9. 已知风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 画布的性能 | `160×60` = 9600 格，动画 8fps 全量重绘 | 画布复用 + 脏矩形；M1 里直接写 benchmark 断言单帧预算（如 < 2ms） |
| 迁移期间两套渲染并存 | 同一个 bug 要修两遍 | 开关默认 `legacy`，**新功能只在新引擎里做**；老路径只做数据安全级修复（SKILL §0.5 的边界） |
| `internal/ui` 里业务逻辑与渲染纠缠很深（如 `renderCenterPanel` 直读 `a.data`/`a.cfg`/`a.timer`） | M4/M5 可能变成"重写"而不是"搬家" | 先做 M4 的只读磁贴（不碰 `Update`），验证适配层够不够用，再动交互类 |
| 引擎被 KQFLOW 的实际需求倒逼出破坏性改动 | 插件 API 反复返工 | 0.x 阶段明确"minor 即破坏性"；M4 结束前不对外宣称稳定 |
| 真机观感无法在本环境验证 | 交付质量靠你反馈 | 每个里程碑交 `-dev.N` exe（§7.3） |

---

## 10. 评审记录与遗留问题

### 10.1 评审结论（2026-10-05）

**设计已批准**（`docs/kxflow-design.md`，用户 2026-10-05 明确批准）。批准时用户提出
一条重要修正，已并入 §4 与 §5：

> "选项插件"可能有歧义……DDL 同时又具有独立的磁贴，这可能是需要特殊考虑的事情，
> 还有一种选项是中栏的看板选项，这可能需要在插件种类中区分。并且这个问题揭露出：
> 如果有的功能同时需要磁贴、选项、和其他磁贴联动，这怎么办？也许我们可以提出
> **整合包**的概念，其包含了一整套有机结合的插件，他们一般不能独立开关，相互有调用关系。

**采纳结果**：

1. 插件种类由 3 种扩为 5 种，其中选项拆成 **`BoardOption`（看板选项，永远在）**
   与 **`ContextOption`（联动选项，依赖 `Selection` 才出现）**——§4.2。
2. 新增 **整合包 `Pack`** 作为**装载 / 版本 / 冲突 / 用户开关的单位**，
   插件降为**渲染与事件的单位**；**包内有机耦合，包间声明式解耦**——§4.1、§4.4。
3. 修正了一处错划分：固定/临时 TODO 不是两个包，而是同一个包 `kqflow.todo`
   的两个磁贴（`ViewConfig` 仍可各自显示/隐藏与换槽位）——§5.1。
4. `Selection`（全局选中上下文）由**引擎**持有，联动选项据此自动出现/消失，
   因此**不需要认识任何磁贴**——§3.5、§5.2。

### 10.2 三项原评审点的确认状态

| # | 问题 | 状态 |
| --- | --- | --- |
| 1 | 多模块构建：`go.work` + 根 `go.mod` 的 `replace` | 已随设计整体批准；M1 已按此落地并实测"无 `go.work` 也能构建" |
| 2 | 功能归属（原"固定/临时拆包"、"标签/DDL 是不是选项"） | **已按用户意见修订**为 §5.1 的整合包表；其中两处细节待确认（见下） |
| 3 | 插件 API 冻结时机：M3 冻结 `v0.1.0`，M4 前不宣称稳定 | 已批准 |

### 10.3 评审结论（第二次，2026-10-05）

用户对 §10.2 三项的回复：

> **1. 关于"缺少接纳它的组件"**：如果某个插件如 labels 包，在由于缺乏必要包时
> 会失效（如没有 todo 包也没有 goal 包），这种情况属于安装了一个正常的包，
> 但是没有任何包可以接纳它。这种和整合包不同的是，缺乏其一只是功能缺失，
> 而不是全部都不能用，因此只需要在包管理器里给个**警告（而不是错误）**提示
> 此包是正常的，但是虽然启用却没有任何作用（缺少接纳它的组件），因此静默失效了，
> 因为它不是有问题，而是「**没有水瓶给水**」这种警告类型的问题。
>
> **2. 关于 `kqflow.timer` 同含三类成员**：**同意**。且这种类型的包属于
> **核心功能推荐包**，后续可以在 readme 中提出：虽然其不是内核自带的，
> 但是其是推荐安装的核心功能包。

**采纳结果**：

1. 装载报告拆成 **错误 / 警告 / Inactive** 三类（§4.5）。原先把"没有接纳者"
   记成错误，会让用户以为包坏了——这是原方案里一处真实的语义错误，已修正。
2. 新增 `WarnReason` 四种：`WarnDisabled`、`WarnNoHost`（"没有水瓶给水"）、
   `WarnProviderDisabled`（给"去开哪个包"的可操作指引）、`WarnTileUnplaced`。
3. `LoadReport.OK()` **只看错误**，不看警告：否则"少一个可选功能"会变成"启动失败"。
4. §5.1 增加"核心功能推荐包"一节，并标注 README 需要说明这一点。

顺手修掉的两个真问题（都是这次改动暴露出来的）：
- `admit` 忘了跳过"已定性（用户关闭）"的候选，于是**没有声明依赖的被关闭包
  照样被装载**；
- 判定"提供者被关闭"时用了"在表里找不到就返回"，导致表里根本没有的能力标记
  返回空字符串（`""`），把"根本没人提供"误判成"提供者被关闭"。

### 10.4 已废弃的旧提问（保留以便追溯）

| # | 原问题 | 处理 |
| --- | --- | --- |
| 4 | `SKILL/` 手册是否新增"如何写 KXFLOW 插件"一章 | 已同意；M6 执行。M1 已先把 `kxflow/` 与两条硬规则写进 `project-map.md` |
| 5 | 设计文档放 `docs/` 还是提升为仓库根 `KXFLOW.md` | 放 `docs/`（现状），`ROADMAP.md` 的死链已改指它 |

### 10.5 第三次评审：真机反馈（2026-10-05）

用户在真实终端 + 真实数据（`D:\KQFLOW\kqflow-data`）上试用后给出四条反馈。
**借调手感被认可**（"手感很好"），其余三条是问题：

| # | 用户反馈 | 结论与处置 |
| --- | --- | --- |
| 1 | 借调 → 二级 → 连按 esc，焦点如预期回到原磁贴 | ✅ 机制正确，无需改动 |
| 2 | 选中待办后中栏**没有多出**"打标签 / 设截止时间" | ⚠️ **真实缺陷**：选项机制是对的，但**界面上没有任何地方显示它们**。内核看板现在会列出当前可用选项与键位（`Kernel.SetOptionSource`） |
| 3 | 停靠区磁贴"很小"、"叠着放" | ⚠️ **设计要改**：见 §4.7 的新规则（下半中栏、1 个占整条、2 个左右各半） |
| 4 | 「关于」里文字只渲染出半句话 | ⚠️ **通用教训**：小磁贴里长文本必须**折行**，不能截断 |

**第 4 条的结论值得单独记下来**，因为它是"磁贴这种小对象"的通用约束：

- 引擎已提供 `RenderCtx.Wrap / WrapLines / WrapInset`，磁贴应当用它们；
- `canvas.LogoLines(width)` 让 LOGO 按宽度**降档**（宽框 → 单行 → 纯文字），
  而不是被截成 "K Q F L"；
- 页脚提示的**按键名永远完整显示**，只允许说明文字带省略号——
  按键是用户唯一能"照着按"的信息，截成 "ta" 毫无意义；
- **长内容应当借调舞台显示**：磁贴只放一行摘要，"关于"这类段落文字
  按 enter 借调中栏。这是"小磁贴 + 大文本"的正确解法，
  而不是把段落硬塞进 24 列。

顺带被这次反馈照出的两个真 bug（都已修 + 加测试）：

1. **选项重复**：`Manager.BoardOptions` 既从 `m.kernel` 收内核选项，
   又遍历所有包再收一遍（内核本身也是包）。看板上因此出现
   "1 帮助 / 2 帮助 / 3 关于 / 4 关于"，按 1 与按 2 效果相同。
2. **滚动与绘制对"占几行"判定不一致**：滚动按一个宽度算、绘制按另一个
   （漏算续行缩进）算，长条目会算少一行，光标跑到可视区外。
   现在两边共用 `itemRowWidth` / `textAreaWidth`，并有测试守住。

### 10.7 第四次评审：交互模型（2026-10-05）

用户第二次真机试用，指出三处，其中第 2、3 条**改动了交互模型本身**：

| # | 用户反馈 | 结论与处置 |
| --- | --- | --- |
| 1 | 保存失败：`Access is denied`（沙盒里的正常现象） | 信息本身是对的。补一条测试**锁住**"落盘失败必须显示给用户"——用户改了东西却不知道没存上，是数据工具最不能接受的静默失败 |
| 2 | "我 Tab 离开，选项还在舞台上——也就是选项现在是随着光标触发改变的" | ⚠️ **模型错了**：我把选项做成了"光标的副产品" |
| 3 | "舞台本身不是一种磁贴……理论上应该可以通过 TAB 回到舞台按方向键"；"这种事务的触发就不应该是光标，而是某个按键" | ⚠️ **模型要改**：需要"未决事务"这个概念 |

**改后的模型**（这三条是硬约定，改动前先读这里）：

```text
选中        = 光标所在的那一条（引擎持有，磁贴上报，只有一处权威）
移动光标    → 只改选中，**不打开任何界面**
按 l        → 打开"未决事务"：当前选中项的操作菜单，占满舞台
未决事务    → 独占焦点：tab/shift+tab 失效（提示"请先处理当前操作"）
方向键      → 在菜单里选择；回车进入具体的编辑界面（再叠一层）
esc         → 先退回编辑界面 → 再 esc 关闭事务，焦点交还给原磁贴
数字键      → 直接触发选项的快捷方式（与 l 打开菜单等价）
```

为什么"独占焦点"是必须的（而不是可选的体贴）：如果 tab 能随时走开，
事务就退化成"一个恰好画在中栏的东西"，用户一走神就再也回不来。
req.md 把这类界面叫**未决事务**，这个词是对的。

**舞台因此成了焦点环里的一站**：新增伪锚点 `geometry.AnchorStage`。
它不是槽位（不在 `AllAnchors` 里，`IsSlot()` 为假），代表"中栏那块被借调
出去的地方"。没有它，"中栏有东西"与"键往哪走"就是两件互不相干的事，
用户只能靠记住数字键操作——那正是反馈里说的"不合理"。

**一个必须留的开口**：提示条要就地盖掉上栏的内容，而"覆盖已有内容"
是全项目最重要的诊断不变量。为此新增 `Canvas.BeginOverlay()` ——
**显式声明**这一段是有意覆盖，只有声明过的写入才放行；
恢复之后覆盖照旧报冲突（有测试守着）。

### 10.8 第五次评审：交互模型定稿（2026-10-05）

用户看完 §10.7 的实现后，把模型一次讲清了。这三条现在是**定稿**：

> **1. 不应该保留数字作为快捷键**：如果有 10 个选项怎么办呢？
>
> **2. 光标一般不会触发什么东西**。如果触发的东西需要一个菜单，
> 中栏看板才会临时变成这个菜单。
>
> **3. 如果光标在无动作时不会触发什么东西，那么下栏的操作提示就需要
> 跟着光标的操作提示改变，显然这种提示需要插件包提供**。这样一来
> 光标只有悬停的作用，实际产生交互的其实是按键动作，
> 且中栏是否变成菜单取决于你的按键动作是否需求一个菜单栈。

**定稿的职责划分**：

| 角色 | 职责 | **不**负责 |
| --- | --- | --- |
| 光标 | 悬停：决定"作用于谁"，并让下栏提示跟着变 | 打开任何界面 |
| 按键 | 产生交互：`l` 打开菜单、`space` 勾选、`j/k` 移动 | 决定看板内容 |
| 中栏看板 | 常驻底色：LOGO、概况、**当前可用操作清单** | 因光标移动而变化 |
| 中栏菜单 | 未决事务：按 `l` 才出现，独占焦点 | 常驻 |

对应实现上的四个决定：

1. **删掉数字快捷键**（`OptionKeys`/`digitIndex`/`activateOption` 全部移除）。
   选项数量与可用按键数量不再耦合——菜单里用方向键选，多少项都行。
2. **`l` 是唯一入口**，菜单里分两段：`当前条目`（联动，作用在光标所指项上）
   与`通用`（看板：开始专注/帮助/关于）。去掉数字键后，通用选项也得有入口，
   与其再发明一套按键，不如让 `l` 一并承担。
3. **下栏提示由插件包提供**：新增可选接口 `plugin.KeyHinter`
   （`KeyHints(RenderCtx) []KeyHint`），引擎每帧向"当前焦点所有者"
   （借调视图优先，否则是焦点磁贴）索要提示，再附上通用的 `tab`/`q`。
   实测效果（真实数据）：
   `待办列表 → j/k 移动 · space 勾选 · l 操作`；
   `计时（未开始）→ l 开始专注`；`计时中 → space 暂停 · l 计时操作`；
   `随手记（只读）→ 只剩 tab/q`；`目标列表 → space 完成`。
   空列表返回 nil —— 那时确实没什么可按的，如实显示比列一堆没用的更好。
4. **看板不因光标移动而变**：它只随"选中/选项"变化；光标移动的唯一
   副作用是下栏提示刷新（`refreshHints` 每帧重算）。

用户第 2 条里"中栏看板才会临时变成这个菜单"与第 3 条的"中栏是否变成菜单
取决于按键动作"是同一件事：**看板是底色，菜单是事务，后者只能由按键产生**。



为了让看板列出选项（§10.5 第 2 条），我把它的可用宽度放开，
结果连着挖出两个更早就存在的错误——它们一直藏着，只因为看板一直很窄：

**① 舞台宽度被砍成一半**（`stageRect`）

原来写的是 `wide.Intersect(vertical)`。`Intersect` 取
`min(r.X1(), o.X1())` 作右边界，而 `wide.X` 是**整行**起点（0）、
`vertical.X` 是**中栏**起点（34）。于是 `wide.X1()`（=56）成了右边界，
56 列的中栏被算成 22 列。

教训不是"Intersect 写错了"——它按定义是对的——而是
**"组合两个矩形的不同维度"这件事不能用交集表达**：
要的是"整行的横向跨度 + 主控区的纵向跨度"，
交集只会给出两者的共同子集。

**② 底色画在磁贴之后**（`View` 的绘制次序）

舞台栈空时画的是**看板底色**，它是背景；磁贴是前景。
原先"先磁贴后舞台"的顺序在看板很窄时看不出问题，
一旦看板拿到完整宽度，它就变成一块大底板，把先画的磁贴整片盖掉——
画面上是"侧栏的框被擦掉了、只剩几根线"。

现在的次序是固定的四步，且与 z 序一致：

```text
1) 上下栏     2) 舞台（底色）     3) 磁贴（前景）     4) 提示浮层（最上层）
```

配套的新不变量（都有测试）：

- 有停靠区时，舞台就是 `Center.Stage`：**横向纵向都不外借**；
- 舞台不得与停靠区或左右栏相交（`TestDashboardDoesNotShrinkOrCrossColumns`）；
- 磁贴标题在任何尺寸下都必须可见（`TestStageDrawsBeforeTiles`）。





---

## 附录 A：与 v2.1.0 的代码对应关系（迁移地图）

| v2.1.0 的位置 | v3.0.0 的归属 | 处置 |
| --- | --- | --- |
| `dashboard.go: columnLayout()` | `kxflow/layout.Layout` | **语义保留**，改为纯函数 + 显式 `LayoutConfig` |
| `dashboard.go: panel()`（渲染后自校正循环） | 删除 | 画布结构上不需要校正 |
| `dashboard.go: clipBlock/padOrClipLines/clipLines` | 删除 | 画布即裁剪 |
| `dashboard.go: replaceColumns/takeColumns/dropColumns/displayRange/fitColumns` | 删除 | 有了逐格样式，不再需要按显示宽度切 ANSI 字符串 |
| `dashboard.go: overlayBox/overlayCenter` | `canvas` 的合成操作 | 保留能力，纳入画布（同样受裁剪区约束） |
| `dashboard.go: renderHeader/renderFooter/renderHints` | `kxflow/chrome` | 搬家 + 用 `Text`/`Wrap` 重写 |
| `dashboard.go: renderView()` 的 11 分支 if 链 | `stage.Stage` 视图栈 | **替换**（这是最大的一处结构改动） |
| `dashboard.go: centerContent` 的高度预算算法 | `layout.Layout` + 视图自己的 `Render` | 高度不再由内容"争抢"，由布局给定 |
| `views.go: contentWidth()/pageContent()/truncateCells*` | `canvas` + `layout` | 搬家；`pageContent` 的滚动改到 `Update` 里 |
| `app.go: handleKey` 的按键路由 | 引擎分发 + 各插件自己的 `Update` | **替换**为"焦点 → 接收者"分发 |
| `app.go: runAction` 的巨型 switch | 内核的 Action 转译 | 拆分到内核 + 各插件 |
| `app.go: pickState` / `editorState` / `customState` | `stage.View` 的实现 | 三态收敛成"栈里的视图"，模态优先级变成栈顺序 |
| `state.go: timerState` | `kqflow.timer` 插件内部状态 | 业务逻辑不动，换外壳 |
| `state.go: celebrateFrame` | 内核的一个 `Effect` | 动画仍由引擎的 `anim` 帧驱动 |
| `notify.go: consumeBell` 的渲染路径输出 | `svc.Effect(EffectBell)` | **修掉历史 bug**（只能在 `Update` 里发） |
| `logo.go: GradientLogo` | `kqflow.kernel` 的 `Logo()` | 搬家，返回 `[]canvas.Line`（画布行，而非带 ANSI 的字符串） |
| `styles.go: Styles` | `kxflow/theme` | 拆为"主题（配色）+ 样式表（下标 → lipgloss.Style）"，配合画布的 `StyleID` |

## 附录 B：原型证据

`sandbox/kvproto/` 是评审用的**一次性原型**（`go run .` 可复现 §0.2 的数字），
用来证明"画布 + 裁剪区 + 诊断"三件套真的能让超宽/串栏结构性不可能发生。
它**不进入仓库**，M1 会把它的结论以正式实现 + 单元测试的形式固化。
