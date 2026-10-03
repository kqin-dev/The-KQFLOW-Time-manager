# 项目地图

## 这是什么

Kairos（可执行文件 `kair.exe`）是一个用 Go 写的命令行时间管理器。
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
cmd/kair/main.go        入口：命令行参数、配置装配、启动 Bubble Tea
internal/version/       Version 常量 —— 全项目版本号唯一权威来源
internal/clock/         逻辑日、日界线、问候语、时长与时钟格式化
internal/config/        配置结构、读写、数据目录定位（KAIROS_HOME > DataDir > exe 同级）
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

数据目录默认是**可执行文件同级的 `kairos-data/`**，可用 `KAIROS_HOME`
环境变量或配置里的 `data_dir` 覆盖。

```
kairos-data/
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
- **`activity` 按条目名聚合**，不是按 ID。所以重命名条目会产生新的 key；
  删条目后由 `DayData.PruneOrphans()` 在读入时清理悬空项。
- **写入一律原子**（临时文件 + rename），写前备份。恢复逻辑从 `backup/` 找同月文件。
- 数据文件是给用户看和手改的，**保持可读、稳定**；改字段名必须能读老文件。

## 数据结构要点

- `Todo`：`ID` / `Title` / `Kind`(fixed|floating) / `Status` / `Tasks` / `Day` / 时间戳。
  状态有 todo / doing / done 三态，`SyncFromTasks` 由子任务反推父条目状态。
- `Task`：子任务，有 `Status` 与 `DoneAt`。
- `Goal`：长期目标；`ArchivedDay` 非空表示它归档在某一天（存在日数据里），
  为空表示它活跃在 `goals.json` 里。
- `Session`：一次计时。`TodoRef` 是条目 ID，`TodoName` 是当时的名字（冗余保存，
  这样条目被删后历史仍可读）。`SegmentKind` 区分 focus / break。
- `Activity`：按名字聚合的累计投入，用于「今日最投入的条目」。
- `DayData.PruneOrphans()`：数据自愈入口，读入时调用。它会把指向已删除条目的
  `todo_ref` 清空、把确实由这些孤儿记录产生的 `activity` 项删掉
  （**但保留「自由专注」这类不来自任何条目的记录**），并清掉文本里的控制字符。
  返回是否发生改动，调用方据此决定要不要写盘。
- `model.Sanitize(s, keepNewline)`：清控制字符 + 折叠首尾空白。所有用户文本入库前都走它。

## 常用改动入口

| 想改什么 | 从哪里入手 |
| --- | --- |
| 看板三栏宽度 / 高度分配 | `columnLayout()`（唯一来源） |
| 按键 | `handleKey` 的路由 + `handleEditorKey` / `handlePickKey` / `handleSettingsKey` |
| 菜单项 | `menuItems` |
| 设置项 | `settingItems` |
| 二级页内容 | `helpLines` / `settingsLines` / `historyLines` / `carryContent` |
| 退出确认 | `askQuit()`；关闭编辑器确认是 `askCloseEditor()` |
| 统计与柱状图 | `stats.go` |
| 版本号 | `internal/version/version.go` |
| 安装包 | `setup/kairos.iss` + `setup/build-installer.ps1` |
