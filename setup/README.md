# 打包与安装

本目录放 Windows 安装包的构建脚本。产物是单文件安装程序
`dist\KQFLOW-<版本>-setup.exe`。

## 依赖

[Inno Setup](https://jrsoftware.org/isdl.php) **6 或 7**（脚本兼容两者）。
只用它的命令行编译器 `ISCC.exe`，构建脚本会自动查找：

1. `PATH` 里的 `iscc.exe`
2. `%ProgramFiles%\Inno Setup 7|6\ISCC.exe` 与 x86 对应路径
3. `D:\Inno_Setup_7|6`、`C:\Inno_Setup_7`
4. 常见盘根下浅层搜索（深度 3）

找不到时用 `-Iscc` 显式指定。

## 用法

```powershell
# 正常构建：读版本号 → 编译 kqf.exe → 出安装包
pwsh -File setup\build-installer.ps1

# 复用已有的 kqf.exe（不再编译）
pwsh -File setup\build-installer.ps1 -SkipGoBuild

# 指定 ISCC 与输出目录
pwsh -File setup\build-installer.ps1 -Iscc 'D:\Inno_Setup_7\ISCC.exe' -OutDir .\dist
```

版本号从 `internal/version/version.go` 读，并注入二进制，
所以 `kqf -version` 与安装包文件名一定一致。脚本最后会自己核对一次。

> 如果 PowerShell 报「未对文件进行数字签名」，用进程级策略绕过即可：
> `Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force`。
> 本目录的 `.ps1` 带 UTF-8 BOM —— Windows PowerShell 会把无 BOM 的 UTF-8 当 ANSI 读，
> 中文注释会直接变成语法错误。改脚本时请保留 BOM。

## 文件

| 文件 | 作用 |
| --- | --- |
| `kqflow.iss` | Inno Setup 安装脚本 |
| `build-installer.ps1` | 构建脚本（读版本、编译、调 ISCC） |

## 安装程序的四项行为约定

改脚本时**不要破坏**这几条，它们都对应明确的用户需求：

1. **可自选安装路径 + 位置提醒**
   默认 `{autopf}\KQFLOW`。选了桌面 / 下载 / 文档 / 临时目录 / 盘符根 /
   系统关键目录会弹提醒；用户选「否」就留在目录页重选。

2. **默认勾选加入 PATH + 提醒**
   `addtopath` 任务 + `[Setup] ChangesEnvironment=yes`（Inno 负责写环境变量并广播变更）。
   取消勾选时会提醒一次。
   注意：Pascal Script **没有**程序化勾选任务的 API（`WizardSelectedTasks` 只读），
   所以只能提醒，不能替用户改回来。

3. **覆盖升级绝不碰用户数据**
   数据目录 `kqflow-data` **不在 `[Files]` 里**，所以 Inno 永远不会覆盖或删除它。
   卸载的 `[UninstallDelete]` 只列了程序自己放进去的文件；
   **不要**加 `filesandordirs` 去删整个 `{app}`，那会把用户数据一起带走。
   如果用户换了安装目录，`PrepareToInstall` 会从注册表的 `InstallLocation`
   找到旧数据并询问是否复制过去（旧目录始终保留）。

4. **卸载默认保留数据**
   删除必须点两次明确的按钮。不要写成「MsgBox 结果不是 Yes 就删」——
   用户顺手关掉对话框就会误删记录。

## 两个不能动的标识

- **`AppId`**：一经发布就不能改。Inno 靠它识别「这是同一个程序的升级」；
  改了会被当成两个不同的程序，控制面板里出现两条卸载项。
- **`#define UninstallKey`**：`AppId` 的字面量副本（Inno 的规则是去掉最外层
  大括号再加 `_is1`）。改 `AppId` 时这里必须同步改。
  之所以写死字面量，是因为在 `#define` 里嵌 `{#SetupSetting("AppId")}` 会让
  预处理器解析失败。

## 手动验证安装包

```powershell
$installer = '.\dist\KQFLOW-1.0.0-setup.exe'
$dir = "$env:TEMP\kqf-check"

# 装
& $installer /VERYSILENT /SUPPRESSMSGBOXES /NORESTART "/DIR=$dir" "/LOG=$dir.log"
& "$dir\kqf.exe" -version

# 升级（数据应原封不动）
& $installer /VERYSILENT /SUPPRESSMSGBOXES /NORESTART "/DIR=$dir"

# 卸载（默认保留数据）
& "$dir\unins000.exe" /VERYSILENT /SUPPRESSMSGBOXES /NORESTART
```

验证完记得卸载测试安装，并确认注册表里没有残留的 KQFLOW 卸载项。
完整发布清单见 [../SKILL/references/release.md](../SKILL/references/release.md)。
