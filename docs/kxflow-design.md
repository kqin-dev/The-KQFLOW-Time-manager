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
	shell   layout.Shell
	stage   *stage.Stage
	tiles   *tile.Registry
	plugins *plugin.Manager
	theme   *theme.Theme
	svc     svc.Services
	anim    AnimState
}

func New(cfg Config) *App
func (a *App) Init() tea.Cmd
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (a *App) View() string   // 内部：一帧 = 一次 Layout + 一次 Canvas 绘制
func (a *App) Resize(w, h int)
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

### 4.1 三类插件接口

```go
type Kind uint8
const (
	KindKernel Kind = iota // 内核插件：把引擎"武装"成某个 CLI 工具
	KindTile               // 磁贴插件：占用槽位的独立业务组件
	KindOption             // 选项插件：中栏选项列表里的一个条目
)

// 所有插件的共同元数据（版本与冲突裁决的唯一依据）。
type Manifest struct {
	ID          string          // 全局唯一，建议 "kqflow.focus"
	Name        string          // 显示名（中文）
	Kind        Kind
	Version     semver.Version  // 插件自身版本
	EngineAPI   semver.Range    // 需要的 KXFLOW API 版本范围，如 ">=0.1 <0.2"
	DataSchema  int             // 它读写的数据结构版本（对应 KQFLOW 的 schema_version）
	Slots       SlotPreference  // 磁贴才有：期望位置与优先级
	Conflicts   []string        // 显式声明与哪些插件 ID 冲突
	Provides    []string        // 提供的能力标记，用于"谁满足谁"的裁决
	Requires    []string        // 需要的能力标记
}

// 内核插件：中栏渲染本内核 LOGO 与 "Power by KXFLOW"，并提供基本选项（设置等）。
type KernelPlugin interface {
	Manifest() Manifest
	Logo(width int, pal *theme.Palette) []canvas.Line
	Options() []Option          // 主看板上的基本选项
	Dashboard() stage.View      // Stage 的默认底层视图
	DecorateHeader(h *chrome.HeaderBar)
	DecorateFooter(f *chrome.FooterBar)
}

// 磁贴插件
type TilePlugin interface {
	Manifest() Manifest
	New(svc svc.Services) tile.Tile   // 工厂：同一插件可实例化多份（未来配置多实例）
}

// 选项插件
type OptionPlugin interface {
	Manifest() Manifest
	Label() string
	// Activate 返回要在舞台上展示的视图（这就是"向中栏请求界面"）。
	Activate(svc svc.Services) (stage.View, error)
}
```

### 4.2 插件管理器与冲突裁决（`req.md` 的四类冲突）

`req.md` 明确要求考虑四类冲突，对应的裁决规则**必须可预测、可解释**（不允许"看注册顺序"）：

| 冲突类型 | 裁决依据 | 冲突时的行为 |
| --- | --- | --- |
| 插件 ↔ 引擎 | `Manifest.EngineAPI` 语义化范围 | 不匹配 → **拒绝加载该插件**，其余照常启动，并在启动报告里列出原因 |
| 内核插件 ↔ 内核插件 | 同 `Kind` 只能有一个激活 | 第二个 → 拒绝加载（内核是单例）；多个候选时由宿主配置指定 |
| 磁贴/选项插件 ↔ 自身 | `Manifest.Conflicts` + 同 `ID` 重复 | 显式冲突 → 后加载者被拒；同 ID 重复 → 后者被拒（先到先得，且记录） |
| 插件 ↔ 数据库 | `Manifest.DataSchema` vs 数据文件实际版本 | 插件要求的 schema **低于**数据 → 拒绝（宁可不用，也不写坏数据，沿用 v2.1.0 的保守策略） |

```go
type Manager struct {
	km     KernelPlugin
	tiles  []RegisteredTile
	opts   []RegisteredOption
	report LoadReport
}

// Load 按上表逐条裁决，全部失败都记录而不 panic：
// CLI 工具"少一个磁贴"必须仍然可用，"起不来"才是事故。
func (m *Manager) Load(engineAPI semver.Version, dataSchema int, ps ...Plugin) LoadReport

type LoadReport struct {
	Loaded  []Manifest
	Rejected []Rejection // {Plugin, Reason, Kind}
}
```

**裁决必须可解释**：启动报告（以及设置页里的"插件"一栏）要能回答
"为什么我的磁贴没出现"——直接对应 `req.md` 的"bug 定位困难"。

### 4.3 数据版本策略（与 v2.1.0 一致，不动）

- `schema_version` 是**数据**版本，程序版本号是另一套编号，二者独立（`SKILL/references/release.md` §0）。
- 开发期**一律不动** `schema_version`（用户明确要求过）。
- 引擎的 `EngineAPI` 版本从 `0.1.0` 起，并遵守：**引擎的破坏性改动必须抬 minor**（0.x 阶段
  minor 即破坏性变更位），这是插件 `EngineAPI` 范围能起作用的前提。

---

## 5. KQFLOW 如何降级为"内核 + 插件"

`internal/kxapp/` 是宿主适配层：把现有 `model`/`store`/`config`/`clock` 接到引擎的
`Services` 与三类插件上。**业务逻辑一行不改**，只换外壳。

### 5.1 这一版的插件划分（按业务独立性切）

| 插件 | 类型 | 承载的现有功能 | 现有代码来源 |
| --- | --- | --- | --- |
| `kqflow.kernel` | 内核 | LOGO、"Power by KXFLOW"、基本选项（设置/帮助/历史/退出）、日界线 | `ui/logo.go`、`ui/app.go` 的 `menuItems` |
| `kqflow.todo.fixed` | 磁贴 | TODAY 固定 TODO | `renderTodoPanel(Fixed)` |
| `kqflow.todo.floating` | 磁贴 | TODAY 临时 TODO | `renderTodoPanel(Floating)` |
| `kqflow.goal` | 磁贴 | GOAL 列表（含归档） | `renderGoalPanel` |
| `kqflow.ddl` | 磁贴 | 截止时间面板 | `ui/ddl.go` + `renderDdlPanel` |
| `kqflow.stats` | 磁贴 | 连续 7 天柱状图 | `ui/stats.go` |
| `kqflow.timer` | 磁贴 + 选项 | 四种计时、计时菜单、归档 | `ui/state.go` 的 timer、`app.go` 的 `runAction` |
| `kqflow.note` | 磁贴 + 选项 | 随手记 | `ui/note.go` |
| `kqflow.labels` | 选项 | 标签编辑页 | `ui/labels.go` |
| `kqflow.history` | 选项 | 历史记录页 | `views.go` 的 `historyLines` |
| `kqflow.settings` | 选项 | 设置页 | `views.go` 的 `settingsLines` + `actions.go` |
| `kqflow.help` | 选项 | 帮助页 | `views.go` 的 `helpLines` |
| `kqflow.notify` | 选项/服务 | 时段切换提醒（流光/响铃/ntfy 推送） | `ui/notify.go`、`ui/notify_ntfy.go` |

**划分原则**：一个插件 = 一个"能独立开关而不影响别的"功能单元。
因此"固定 TODO"与"临时 TODO"拆成两个磁贴（它们各自可关），而"标签/DDL"是**选项插件**
（它们作用于"当前选中条目"，由内核通过 `Focus` 上下文提供，不适合做成磁贴）。

> **评审点 2**：这张表就是最终的功能归属。请确认"固定/临时 TODO 拆成两个磁贴"以及
> "标签与 DDL 是选项插件而不是磁贴"这两条是否符合你的直觉。

### 5.2 内核负责的跨插件协调

磁贴彼此不能直接调用（否则又耦合了）。跨插件的动作由**内核**统一转译：

```go
// 插件返回的 Action 是"意图"，不是"操作"。
type Action struct {
	Kind    ActionKind // ActionPersist | ActionEffect | ActionBorrow | ActionFocus | ActionToast
	Payload any
}
```

例：`kqflow.todo.floating` 勾选完成 → 返回 `Action{Kind: ActionPersist, Payload: ToggleTodo{...}}`
→ 内核解释为 `store.SaveDay` + 检查"今日全部完成" → 若完成则 `Action{Kind: ActionEffect, Effect: Celebrate}`。
**磁贴不知道存储、不知道庆祝动画存在**——这就是"解耦"的实际含义。

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

## 10. 需要评审确认的问题

| # | 问题 | 我的建议 |
| --- | --- | --- |
| 1 | 多模块的构建方式：`replace` + `go.work` 双提交（§2.3 B 方案） | 建议接受，理由：仓库分发物是单 exe + 安装包，必须保证"全新克隆即可构建" |
| 2 | 功能归属：固定/临时 TODO 拆为两个磁贴；标签与 DDL 做成选项插件而非磁贴（§5.1） | 建议按此，因为它们各自可独立开关 |
| 3 | 插件 API 冻结时机：M3 冻结 `v0.1.0`，M4 结束前不宣称稳定 | 建议接受，避免过早承诺 |
| 4 | `SKILL/` 手册的更新：v3.0.0 完成后需要新增"如何写 KXFLOW 插件"一章，并把"渲染层缺陷"一节改为历史记录 | 建议接受 |
| 5 | 是否现在就把本文从 `docs/kxflow-design.md` 提升为仓库根的 `KXFLOW.md`（替换 `ROADMAP.md` 引用的、当前缺失的 `../KXflow.md`） | 建议放 `docs/`，并在 `ROADMAP.md` 里把失效链接指向它 |

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
