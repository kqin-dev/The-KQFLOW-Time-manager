# 发布清单

发一个版本要动的东西不多，但顺序不能乱。**版本号只有一个权威来源。**

## 0. 两套「版本号」不是一回事（先分清再动手）

仓库里有两个都叫「版本」的东西，它们的命名与用途**完全独立**，不要混用：

| | 启动器版本（App Version） | 数据架构版本（schema_version） |
| --- | --- | --- |
| 权威来源 | `internal/version/version.go` 的 `Version` | `model.SchemaVersion`（日数据 / GOAL）；`config.ConfigSchemaVersion`（配置） |
| 取值风格 | 语义化版本 `2.0.1`，可带 `-dev.N` 之类的预发布后缀 | **单调递增的小整数**：1、2、3…… |
| 含义 | 「这个程序是第几个发布版本」 | 「这份 JSON 的数据结构长什么样」 |
| 何时改 | **每次发布都改**（见下一节） | **只有数据结构发生不兼容变化时才 +1**；纯加字段且老程序仍能读、能自愈补默认值时不必改 |
| 谁在用它 | 看板底栏、`kqf -version`、`kqf -h`、安装包文件名、Release tag | 启动时的数据版本保护（`store.CheckDataVersion`、`config.Load`） |

**为什么必须分清**：两者没有换算关系。程序可以连着发 2.0.2、2.0.3 而
`schema_version` 一直是 1（只改了界面），也可以在一次发布里把
`schema_version` 从 1 提到 2（数据结构变了）而程序版本只从 2.0.1 到 2.1.0。
`schema_version` 是**判断兼容性的证据**：启动时读到比本程序支持的更大，
就说明这份数据来自更新的程序，旧程序不能安全处理，于是拒绝启动
（见 [project-map.md](project-map.md) 的数据版本保护一节）。

推论，写代码时要守住：

- **不要**把 `schema_version` 当成程序版本写进去，也不要拿它做展示。
- **不要**因为「发了个新版本」就顺手把 `schema_version` 加一——那会让所有
  用户的旧数据在自己机器上被判成「来自未来」，直接起不来。
- 反过来，**真的改了数据结构却忘了 +1**，就等于放弃了保护。

判断加不加一的标准是**旧程序读到新数据会不会出错或丢东西**，而不是
「我改了多少行」。

**当前采取的口径**：只把「老程序会读错、会算出错误结果」的变化算作需要升版
（改字段语义、改类型、改嵌套结构）。**纯加可选字段不升版**——老程序读得到、
只是不认识，能照常工作。

### 开发期一律不升版；只有对外发布稳定版时才考虑

用户明确要求：**开发 / 开发者版本一律不动 `schema_version`**。

理由：开发版没有真实用户，数据都是自己造的、随时可弃；每加一个字段就升版只会
让「旧开发版打不开新数据」这种无意义的阻塞反复出现，白白增加调试成本。
`schema_version` 的存在意义是保护**真实用户**的数据，而不是给开发期添麻烦。

所以规则是：

- 开发阶段（`2.x.y-dev.N` 这类构建、以及任何还没对外发布的改动）：**不动它**。
  加字段、改字段、甚至临时改结构都直接改，靠自愈与容错兜住自己的旧数据。
- **只有在切一个对外发布的稳定版**、且这个版本的数据结构变化会让更早的
  **已发布**版本读错或算出错误结果时，才在那一次发布里把它 +1。
- 判断依据始终是「已发布的老程序读到新数据会不会出错」，不是「我改了多少行」，
  也不是「这个字段重不重要」。

**已知且刻意接受的代价**：写盘是整份 JSON 回写，所以「新版本写了新字段 →
用户回去开旧版本 → 旧版本写回」会静默丢掉新字段。只有在发布稳定版时才可能
需要为此升版；开发期不管。

## 1. 定版本号

只改 `internal/version/version.go` 一处：

```go
var Version = "1.0.0"
```

看板底栏、帮助页、`kqf -version`、`kqf -h` 都会跟着变。
不要在别处硬编码版本字符串。

## 2. 全量验证

```powershell
$root = '<仓库根>'
$env:GOCACHE="$root\.gocache"; $env:GOMODCACHE="$root\.gomodcache"; $env:GOSUMDB="off"

gofmt -l cmd internal        # 必须无输出
go vet ./...
go test ./... -count=1
```

## 3. 通读 README

README 是用户在 Hub 上看到的第一份东西，发布前必须过一遍：

- 安装方式与实际产物一致（安装包文件名、绿色版说明、源码构建命令）。
- 快捷键表与实际实现一致（改过按键就要更新）。
- 顶部示例看板与实际渲染一致（改过排版/logo 就要更新）。
- 「已知限制」诚实说明，但**不要把环境特性夸大成功能缺失**。
  例：输入法在终端里能正常打字，只是候选窗位置不在光标旁边——
  写成「必须粘贴才能输入中文」会劝退中文用户（曾因此被用户纠正，
  详见 [pitfalls.md](pitfalls.md)）。

## 4. 打安装包

需要 **Inno Setup 6 或 7**（脚本兼容两者）。构建脚本会自动查找 `ISCC.exe`
（PATH、`Program Files\Inno Setup 7|6`、`D:\Inno_Setup_7|6`、盘根浅层搜索），
也可以用 `-Iscc` 指定。

```powershell
pwsh -File setup\build-installer.ps1
```

脚本做三件事：读版本号 → 编译 `kqf.exe`（注入版本）→ 调 ISCC 出安装包。
产物：`dist\KQFLOW-<版本>-setup.exe`。

有用参数：

| 参数 | 作用 |
| --- | --- |
| `-SkipGoBuild` | 复用已有的 `kqf.exe`（不再编译） |
| `-Iscc <路径>` | 指定 `ISCC.exe` |
| `-OutDir <目录>` | 指定输出目录，默认 `<root>\dist` |

## 5. 验证安装包（不要跳过）

至少验这五件事，全部可以用静默参数完成：

```powershell
$installer = '<root>\dist\KQFLOW-<版本>-setup.exe'
$dir = "$env:TEMP\kqf-check"

# a) 装得上
& $installer /VERYSILENT /SUPPRESSMSGBOXES /NORESTART "/DIR=$dir" "/LOG=$dir.log"
& "$dir\kqf.exe" -version          # 应与版本号一致

# b) 升级不丢数据
#    在 $dir\kqflow-data 里放一份数据，再装一次，确认文件哈希不变
# c) 换目录升级会把旧数据带过去（静默下默认“是”）
# d) 卸载默认保留数据
& "$dir\unins000.exe" /VERYSILENT /SUPPRESSMSGBOXES /NORESTART

# e) 换一个「默认风格」的安装路径再装一次（最关键、最容易漏）
& $installer /VERYSILENT /SUPPRESSMSGBOXES /NORESTART "/DIR=C:\Program Files\KQFLOW"
```

**第 e 件为什么必须做**：曾出过这样的事故——脚本里
`ExpandConstant('{userprofile}')` 常量名写错（正确是 `{userpf}`）。
这是**运行期**错误，编译器不报；而「位置是否合理」的判断在 `%TEMP%` 路径下
会提前返回，所以只测 `%TEMP%` 永远发现不了，用户装到
`C:\Program Files\KQFLOW` 才会以退出码 1 失败。**只测一种路径不足以验证安装包。**

改过 `.iss` 之后要重走这个清单，并且**重新上传 Release 附件**——
用户下载的是附件，不是你本地那份。

**验证完记得卸载测试安装**，否则会在注册表里留下卸载项。
检查残留：

```powershell
Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*',
                 'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*' -ErrorAction SilentlyContinue |
  Where-Object { $_.DisplayName -like '*KQFLOW*' }
```

## 6. 安装包的行为约定（改脚本时不要破坏）

- **允许自选安装路径**，默认 `{autopf}\KQFLOW`；选了桌面 / 下载 / 文档 / 临时 /
  盘符根 / 系统关键目录会提醒，用户反悔可以留在目录页重选。
- **默认勾选加入 PATH**（`addtopath`）。真正写入的是 `[Code]` 里的
  `AddDirToUserPath`（由 `CurStepChanged` 在 `ssPostInstall` 调用），
  卸载时 `RemoveDirFromUserPath`（`usUninstall`）负责摘掉；
  `ChangesEnvironment=yes` 只负责安装后广播变更。
  **改任务名或删任务时，务必确认这两处代码仍引用 `addtopath`**——
  任务本身只是个勾选框，不绑动作就等于没实现（这个坑真实发生过，
  而且是发布后被用户发现的）。
- **升级不碰数据**：`kqflow-data` 不在 `[Files]` 里，所以 Inno 永远不会覆盖或删除它。
  卸载的 `[UninstallDelete]` 只列了程序自己放进去的文件，**不要**加
  `filesandordirs` 删整个 `{app}`。
- **换目录升级会问是否复制旧数据**（`PrepareToInstall` + 注册表里的 `InstallLocation`）。
- **卸载默认保留数据**：删除必须点两次明确的按钮。不要用
  「MsgBox 结果不是 Yes 就删」的写法——用户关掉窗口就会误删。
- `AppId` **一经发布不能改**：改了会被当成两个不同的程序，出现两条卸载项。
  脚本里 `#define UninstallKey` 是它的字面量副本，改 `AppId` 时两处都要改。

## 7. 提交与推送

提交信息写清**改了什么、为什么**。推送前确认工作区干净：

```powershell
git status --short
git add -A
git commit -F <消息文件>       # 消息文件用 UTF8Encoding($false) 写，别带 BOM
git push origin main
```

沙箱下 `git push` 可能需要一次性放宽权限（凭据助手要创建进程管道）。

## 8. 在 GitHub 建 Release

1. 打 tag：`git tag -a v1.0.0 -m "KQFLOW v1.0.0"`，推送 tag。
2. 新建 Release，选该 tag，标题写 `KQFLOW v1.0.0`。
3. **上传 `dist\KQFLOW-<版本>-setup.exe`** 作为附件。
4. Release 说明里写：新增 / 修复 / 破坏性变更（如有）、以及升级方式
   （直接覆盖安装，数据不受影响）。
5. 确认 `LICENSE`（MIT）与仓库地址无误。

## 9. 发布后自检

- Release 页面的安装包能下载，文件名与版本号对得上。
- 全新机器（或干净目录）装一次能跑起来。
- README 里的下载链接指向 Releases 页而不是写死的文件名。
