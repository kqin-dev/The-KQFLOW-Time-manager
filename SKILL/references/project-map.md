# 项目地图

## 这是什么

KQFLOW（可执行文件 `kqf.exe`）是一个用 Go 写的命令行时间管理器。
单个静态可执行文件，无运行环境依赖，数据以 JSON 存在本地。
目标平台是 Windows（安装包为 Inno Setup），但代码本身是跨平台的。

用户可见的功能：

| 功能 | 说明 |
| --- | --- |
| 每日 TODO | 分「固定」（每天自动出现）与「临时」两类，各有独立面板 |
| 子任务 | 每个 TODO / GOAL 可以有子任务，完成情况回写父条目 |
| GOAL | 与日程无关的长期目标；完成或取消时在长期库与当天归档之间双向移动 |
| 番茄钟 / 倒计时 / 正计时 / 自定义时段 | 四种计时；时长与段数全部可在设置里改 |
| 计时归档 | 无论正常结束还是中途打断，实际时长都写入当天记录并累计到条目 |
| 随手记 / 日记 | 中间栏里的多行编辑器，按日保存、过日重置，可选显示在看板 |
| 连续 7 天统计 | 滚动 7 天窗口，竖向柱状图（归一化相对高度） |
| 历史记录 | 最近若干天的完成度、专注时长、GOAL 数、最投入条目 |
| 设置 | 日界线、各类时长、昵称、自定义字条、数据目录、时区、是否展示随手记 |
| 逻辑日 / 日界线 | 「今天」从几点算起，默认 00:00，熬夜用户可设 04:00 |
| 自定义随机字条 | 看板中间栏滚动的便签，用户可自定义多条 |

## 代码结构

```
cmd/kqf/main.go        入口：命令行参数、配置装配、启动 Bubble Tea
internal/version/       Version 常量 —— 全项目版本号唯一权威来源
internal/clock/         逻辑日、日界线、问候语、时长与时钟格式化
internal/config/        配置结构、读写、数据目录定位（KQFLOW_HOME > DataDir > exe 同级）
internal/model/         TODO / GOAL / TASK / Session / Activity / DayData 等数据结构
internal/store/         按日分库的持久化、原子写、备份与恢复
internal/ui/            Bubble Tea 界面（最大的一块，见下）
setup/                  Inno Setup 脚本 + build-installer.ps1
SKILL/                  本手册
```

### internal/ui 细分

| 文件 | 职责 |
| --- | --- |
| `app.go` | `App` 结构、`Update`、按键路由、模态（pick）处理、菜单、退出确认 |
| `dashboard.go` | `View` 入口、三栏布局 `columnLayout`、看板与面板渲染、底栏 |
| `views.go` | 二级页内容（帮助/设置/历史/继承/选择框/输入框）、中间栏内容渲染 |
| `actions.go` | 增删改、设置项、输入框按键、计时动作 |
| `custom.go` | 自定义时段编辑器 |
| `note.go` | 随手记 |
| `stats.go` | 连续 7 天统计与柱状图 |
| `state.go` | 编辑器状态、选择框状态、计时器状态、庆祝动画状态 |
| `styles.go` | 主题与 Lipgloss 样式 |
| `logo.go` | ASCII 艺术字（三种字号）与渐变着色 |

## 数据存储

数据目录默认是**可执行文件同级的 `kqflow-data/`**，可用 `KQFLOW_HOME`
环境变量或配置里的 `data_dir` 覆盖。

```
kqflow-data/
├── config.json          配置
├── goals.json           长期 GOAL
├── days/
│   └── YYYY-MM.json     按月分片，内含该月每一天的完整数据
└── backup/              每次写入前的备份，按数量滚动清理
```

`days/YYYY-MM.json` 的结构：

```jsonc
{
  "schema_version": 1,
  "month": "2026-10",
  "days": {
    "2026-10-03": {
      "schema_version": 1,
      "day": "2026-10-03",
      "fixed":    [ /* Todo */ ],
      "floating": [ /* Todo */ ],
      "archive": {
        "day": "2026-10-03",
        "goals": [ /* Goal：当天完成的目标归档 */ ],
        "sessions": [ /* Session：计时记录，专注时长的唯一来源 */ ]
      },
      "activity": { "<条目名>": { /* Activity：累计投入 */ } },
      "note": "当天的随手记",
      "carry_asked": false,
      "created_at": "…",
      "updated_at": "…"
    }
  }
}
```

关键约定：

- **`archive.sessions` 是专注时长的唯一来源**，不要在别处再存一份。
  只有 `ended` 不为空的记录才计入总计（未结束的计时不算）。
  专注时长读 `Session.Focus`（`*time.Duration`，按段累计）；老数据为 `nil`
  时退回旧口径（休息不算、其余都算），见 `Session.FocusDur` / `HasBreakdown`。
  `FocusRecord` 是早期遗留类型，统计口径已不再依赖它。
- **`activity` 按条目名聚合**，不是按 ID。所以重命名条目会产生新的 key；
  删条目后由 `DayData.PruneOrphans()` 在读入时清理悬空项。
- **写入一律原子**（临时文件 + rename），写前备份。恢复逻辑从 `backup/` 找同月文件。
- 数据文件是给用户看和手改的，**保持可读、稳定**；改字段名必须能读老文件。
- **数据版本保护已经实现**（`internal/store/dataversion.go` 的 `CheckDataVersion`，
  由 `cmd/kqf` 在 `store.Open` **之前**调用）。保守策略：只要发现数据来自更新的
  版本，就拒绝启动并列出文件名与版本号，请用户升级程序或把文件移走。
  **`schema_version` 与程序版本号是两套独立的编号，命名规则见
  [release.md](release.md) 第 0 节**——那是判断兼容性的证据，不是程序版本。
  两个要点：
  - **存在却读不出内容的文件同样拒绝启动**。认不出内容就无法保证它不是新版本
    的数据，而本项目是整份 JSON 读进来再整份写回，写回就等于抹掉不认识的字段。
  - 检查过程**只读、绝不写入**，也不建任何目录——否则用户想回退就没有干净数据。
  - `config.json` 由 `config` 包自己把关（`config.Load` 返回
    `IncompatibleConfigError`），并有独立的 `config.ConfigSchemaVersion` 常量。
    两个包各留一个版本常量是因为 `model` 不依赖任何内部包、`config` 引用它会绕成环。
    注意 `config.Load` 对「损坏」仍然是退回默认值不阻断启动，只对「来自更新版本」
    拒绝——这两种情况必须区别对待。
  - **开发期一律不动 `schema_version`**（用户明确要求）：开发版没有真实用户、
    数据随时可弃，每加字段就升版只会让「旧开发版打不开新数据」反复阻塞调试。
    只有**对外发布稳定版**、且变化会让更早的**已发布**版本读错时才升。
    详见 [release.md](release.md) 第 0 节。

## 数据结构要点

- `Todo`：`ID` / `Title` / `Kind`(fixed|floating) / `Status` / `Tasks` / `Day` / 时间戳。
  状态有 todo / doing / done 三态，`SyncFromTasks` 由子任务反推父条目状态。
- **标签（Labels）有两个同名的东西，别混**（见 `internal/model/label.go`）：
  - `Todo.Labels` / `Goal.Labels`：**用户**起的记号（星星 / 紧急 / 自定义），
    `[]string`，可以增删。这是需求 1 的「标签」。
  - `Goal.Tag`：**程序**算出的标题指纹（`#a1b2c3`），用于继承时避免同名混淆，
    用户改不了。
  标签没有全局标签库：可用标签 = 内置预设（`labelPresets`）+ 配置里的
  `custom_labels`（只存用户新造的）+ 所有条目上已用过的，去重后得到。
  这样标签库不会随使用膨胀，也不会出现「库里有用不上的悬空项」。
  单个条目上限 `MaxLabelsPerItem`，单个标签长度上限 `MaxLabelRunes`，
  入库前一律走 `LabelName` 清洗（读入时由 `PruneOrphans` 自愈）。
- `Task`：子任务，有 `Status` 与 `DoneAt`。
- `Goal`：长期目标；`ArchivedDay` 非空表示它归档在某一天（存在日数据里），
  为空表示它活跃在 `goals.json` 里。
- `Session`：一次计时。`TodoRef` 是条目 ID，`TodoName` 是当时的名字（冗余保存，
  这样条目被删后历史仍可读）。`SegmentKind` 区分 focus / break / other，
  记录的是**结束时**所在的那一段；`Focus` 才是这次计时真正的专注时长
  （按段累计，跨段方案也正确）。
- **DDL（截止时间）的粒度按条目类型分开**（见 `internal/model/ddl.go`）：
  `Todo.Due` 是 `"HH:MM"`（待办每天重置，18:30 天然指「今天 18:30」）；
  `Goal.Due` 是 `"YYYY-MM-DD"`（目标不随天重置，指「到该逻辑日结束为止」）。
  存字符串而不是时间戳，是为了数据文件可读、可手改。
  算到期时刻时会经过日界线：**日界线只决定「算哪个逻辑日」，不能加到钟点上**
  （这个 bug 被测试抓到过——日界线 04:00 时 18:30 曾被算成 22:30）。
- **收藏的自定义专注方案**（见需求 2）存在**配置**里（`config.saved_plans`），
  不是日数据：它是「偏好」，与日界线无关，也不该随某天数据被清理。
  实现上「一套收藏」就是一个带了 `Plan.Label` 的 `Plan`，没有另造类型；
  相关工具在 `internal/model/plan.go`（校验、自动命名、`ClonePlan`）。
  **`ClonePlan` 必须用**：`Segments` 是切片，直接赋值会让「当模板改」改到收藏
  原件（这一点有专门的测试）。动作串用**下标**（`saved_start:0`）而不是方案名，
  因为方案名是用户随便起的、可能含冒号等字符。
- **时段切换提醒**（见需求 3）的调度中心是 `internal/ui/notify.go`：
  跨过时段边界（`timerState.lastSeg` 变化）或**计时自然结束**（`timerDoneMsg`）时
  触发提醒。三档各自可关（配置里都是布尔开关），默认全关：
  - 流光 `notifyLogo` → **只做在 LOGO 那几行**。实机反馈走过两轮弯路：一开始
    只替换一行（用户说"太微弱"），改成铺满整个中间栏（用户说"确实很丑"），
    最终定成"只在 LOGO 上"——Logo 本来就有流动渐变，提醒期间换更亮的配色、
    加快流动并加 `◈` 记号即可，不动中间栏其它内容、不动左右面板。
  - 提示音 `notifySoundCmd` → **只有终端响铃**（`\a`）。曾经用代码合成过颂钵/
    风铃/白噪音三种音频并通过 PowerShell SoundPlayer 播放（见 git 历史），
    但用户实测在本机都放不出声，而系统响铃好听且可用，所以整条合成音频与
    平台播放的代码都被删掉了，配置里它是个开/关。
  - 手机推送 `notify_ntfy.go` → POST 到 `<server>/<topic>`，标题放 `X-Title`。
    **纯单向、失败就算**（不重试不排队）。用户实测能正常收到短信。
- **ntfy 频道名由程序生成**（`config.GenerateNtfyTopic`，crypto/rand + base32，
  32 字节熵）：ntfy 频道默认**全网公开**，谁猜到名字都能收、也能发，所以
  不把安全防线寄托在用户的安全意识上。生成一次就固化在配置里（每次换频道会
  让手机订阅失效），用户可在设置里主动「重新生成手机频道」。
  `config.NotifyDisclaimer` 是必须原样展示的风险说明（第三方关系、不加密、
  不要泄露、不承担责任），措辞合规见 bug.md 注意 2 / 用户要求。
- **二维码已经被移除，不要再加回来**。曾经自研过一个精简 QR 编码器
  （字节模式 + L 级 + 纠错 + 掩码）并配了测试用解码器，数学闭环、格式信息与
  标准逐位一致、几何断言也全过，但**真机始终扫不出来**（"连识别都识别不出
  是二维码"，改过模块比例与静默区后仍然不行）。用户决定弃用该功能，改为直接
  显示订阅地址让用户复制。**教训**：测试能证明的只有"矩阵在数学上合法"，
  证明不了"手机能读"；这类与人/硬件交互的功能，自己造轮子的验证成本极高，
  要么用成熟库，要么干脆不做。
- `Activity`：按名字聚合的累计投入，用于「今日最投入的条目」。
- `DayData.PruneOrphans()`：数据自愈入口，读入时调用。它会把指向已删除条目的
  `todo_ref` 清空、把确实由这些孤儿记录产生的 `activity` 项删掉
  （**但保留「自由专注」这类不来自任何条目的记录**），并清掉文本里的控制字符。
  返回是否发生改动，调用方据此决定要不要写盘。
- `model.Sanitize(s, keepNewline)`：清控制字符 + 折叠首尾空白。所有用户文本入库前都走它。

## 常用改动入口

| 想改什么 | 从哪里入手 |
| --- | --- |
| 看板三栏宽度 / 高度分配 | `columnLayout()`（唯一来源）；左栏是「固定 / 临时 / 汇总」，右栏是「GOAL / DDL / 汇总」，两边高度分配对称 |
| 按键 | `handleKey` 的路由 + `handleEditorKey` / `handlePickKey` / `handleSettingsKey` |
| 标签 | `labels.go`（页面 + 渲染）、`model/label.go`（数据与清洗） |
| DDL / 截止时间 | `ddl.go`（页面 + 排序）、`model/ddl.go`（粒度与到期计算） |
| 收藏的专注方案 | `savedplans.go`（菜单 + 三种动作）、`model/plan.go`（校验与克隆） |
| 时段切换提醒 | `notify.go`（调度 + LOGO 流光）、`notify_ntfy.go`（推送） |
| 菜单项 | `menuItems` |
| 设置项 | `settingItems` |
| 二级页内容 | `helpLines` / `settingsLines` / `historyLines` / `carryContent` |
| 退出确认 | `askQuit()`；关闭编辑器确认是 `askCloseEditor()` |
| 统计与柱状图 | `stats.go` |
| 版本号 | `internal/version/version.go` |
| 安装包 | `setup/kqflow.iss` + `setup/build-installer.ps1` |
