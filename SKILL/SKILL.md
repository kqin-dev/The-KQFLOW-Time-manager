---
name: kqflow-project-dev
description: Develop, fix, test, and release the KQFLOW Go terminal time-manager (The-KQFLOW-Time-manager). Use when the task mentions KQFLOW or kqf, its Bubble Tea dashboard/TODO/GOAL/pomodoro UI, its day-boundary or day-sharded JSON storage, its Inno Setup installer, its version number, or when continuing development of this repository in any form.
---

# KQFLOW 项目开发手册

本 Skill 让 Agent 快速成为**熟悉本项目细节、能继续管理和开发它**的开发者。
它只写模型猜不到的东西：本仓库特有的构建方式、渲染约定、已经踩过的坑和验证方式。

## When to use this skill

**适用**：任何针对本仓库（KQFLOW / `kqf`）的改动——修 bug、加功能、改排版、
调数据格式、改安装包、发版本、审查代码。

**不适用**：与 KQFLOW 无关的通用 Go 或 Bubble Tea 问题；与其它项目的时间管理工具。
如果只是问「怎么用 kqf」而不是「怎么改 kqf」，读 [references/project-map.md](references/project-map.md)
的用户功能部分即可，不要按开发流程走。

## 0. 先做这三件事

1. **确认仓库根目录**。所有命令都以它为基准；本文档用 `<root>` 表示。
2. **读 [references/conventions.md](references/conventions.md)**。渲染与数据约定必须遵守，
   违反它就会重现历史 bug。改动前必读。
3. **读 [references/pitfalls.md](references/pitfalls.md)**。这里是本项目已经踩过的坑，
   每条都对应一次真实的用户投诉。

## 0.5 先读这一条：当前处于「原型冻结」状态

**v2.1.0 是原型，已正式发布并冻结**（`main` 分支就是它）。业务逻辑可用且有测试覆盖，
但**渲染层是已知有缺陷的**——这不是没修完，是刻意的决定：

- 界面缺少统一的布局/渲染层，每个页面各自算宽度、各自折行，导致同一类排版问题
  （长中文换行诡异、看起来像被截断、栏位消失）反复出现且难以根治；
- 这些问题**只在特定终端宽度下出现**，开发环境（离屏渲染 + 合成尺寸）**无法复现**；
- 因此团队决定**停止打补丁**，把渲染层重写列为 3.0 目标（代号 **KXFLOW**，
  分层方案见仓库根的 [KXFLOW 设计构想](../../KXflow.md) 与 [ROADMAP.md](../../ROADMAP.md)）。

**这条对你怎么做事有直接影响**：

1. **不要再单独修"某个页面文字显示不全"这类排版问题**（除非用户明确要求）。
   已经试过四轮，每次都是治了一个宽度、坏在另一个宽度。**先问用户是否属于 3.0 的范围。**
2. 用户报告排版问题时，**先索要截图 + 终端宽高**。没有宽高基本无法定位——
   本项目就是吃了这个亏。
3. 业务逻辑的 bug、数据安全问题、按键行为问题**照常修**（这些没有结构性问题）。
4. **改数据格式时格外保守**：3.0 会复用现在的数据模型，别为了让当前原型好过而
   把格式改得难以迁移。开发期的纯增字段**不要**提升 `schema_version`
   （见 [references/release.md](references/release.md) §0）。

## Output contract

交付任何改动时，必须满足：

- `gofmt -l cmd internal` **无输出**（有输出即为不合格）。
- `go vet ./...` 通过。
- `go test ./... -count=1` 全绿；新增行为必须带测试，不要只靠手工验证。
- 提交信息用中文、说明**根因**而不只是现象；一次提交聚焦一件事。
- 交付说明里注明：改了哪些文件、为什么这么改、验证方式、以及**未验证的部分**
  （见 Edge cases：真实终端观感在本环境无法验证，必须如实说明）。
- 不要为了让测试通过而改测试断言。断言写错时先确认**实现**是否真的有 bug
  （本项目已经发生过多次「测试写错反而掩盖了真 bug」，见 pitfalls）。

## Workflow

1. **定位**：先读相关源文件，不要凭函数名猜行为。UI 改动的入口几乎都在
   `internal/ui/`，数据改动在 `internal/model/` 与 `internal/store/`。
2. **判断改动属于哪一类**，并按对应约定做：
   - 排版 / 面板 / 弹窗 → 走「一律复用中间栏」的既有通道（见 conventions）。
   - 涉及中文或任何非 ASCII 文本 → 列位置一律按**显示宽度**算（见 conventions）。
   - 数据字段 → 必须考虑**老数据兼容**与「读入时自愈」，并同步更新
     [references/project-map.md](references/project-map.md) 里的数据结构说明。
   - 版本号 → 只改 `internal/version/version.go` 一处。
3. **写测试**。优先写成「逐个入口走查」的形式（例如一批按键的清单），
   而不是只测用户报的那一个入口——这是本项目最重要的一条经验。
4. **本地全量验证**：`gofmt` → `go vet` → `go test ./... -count=1`。
5. **渲染类改动**：用离屏渲染并**逐行检查显示宽度**，不要靠想象。
6. **提交**（如需推送，见 [references/pitfalls.md](references/pitfalls.md) 的沙箱注意事项）。
7. **发版本或打安装包**时按 [references/release.md](references/release.md) 走完整清单。

## Edge cases

- **无法验证真实终端观感**：本环境没有交互式终端。TUI 的最终观感只能由用户在
  真实终端确认。交付时必须明确说明「已用离屏渲染验证到什么程度」，不要声称已确认观感。
- **不要用 `Start-Process` 直接跑 `kqf.exe`**：TUI 在没有真控制台时会挂住，
  留下僵死进程。验证数据逻辑请调 Go 接口（`App` / `store`）而不是跑二进制。
  验证安装包用 `/VERYSILENT` 静默参数。
- **修改编辑器 / 输入相关的按键时**：一定要想「这个键本来是文本吗」。
  单行输入框里的 `q` 是要输入的字符；随手记（多行）里的 `esc` / `q` 才是关闭意图。
- **删除任何东西之前**：先确认它是「程序产生的」还是「用户的」。
  用户数据（`kqflow-data/`）永远不主动删除，只在用户明确确认后删。
- **数据字段改名或改语义**：必须能在读入老文件时补默认值，不能让程序崩或丢记录。
- **不确定用户意图时**：先问，不要顺手扩大改动范围。用户明确说「先解决已知问题」时，
  只修那个问题，不要夹带其它改动。

## Resources

按需读取，不要一次全读：

- [references/project-map.md](references/project-map.md)
  功能清单、代码结构与数据存储结构。**改数据或加功能前读**。
- [references/conventions.md](references/conventions.md)
  渲染、宽度、光标、布局、测试的硬性约定。**改 UI 前必读**。
- [references/pitfalls.md](references/pitfalls.md)
  已踩过的坑与沙箱环境注意事项。**动手前必读**。
- [references/release.md](references/release.md)
  发版本、打安装包、发布 Release 的完整清单。**发版前读**。

## Scripts

本 Skill 不带脚本。项目自身的构建脚本是 `setup/build-installer.ps1`
（打 Windows 安装包），说明见 [references/release.md](references/release.md)。
它的副作用：会重新编译 `<root>/kqf.exe` 并写出 `<root>/dist/` 下的安装包。
