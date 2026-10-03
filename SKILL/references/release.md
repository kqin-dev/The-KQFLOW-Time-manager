# 发布清单

发一个版本要动的东西不多，但顺序不能乱。**版本号只有一个权威来源。**

## 1. 定版本号

只改 `internal/version/version.go` 一处：

```go
var Version = "1.0.0"
```

看板底栏、帮助页、`kair -version`、`kair -h` 都会跟着变。
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

脚本做三件事：读版本号 → 编译 `kair.exe`（注入版本）→ 调 ISCC 出安装包。
产物：`dist\Kairos-<版本>-setup.exe`。

有用参数：

| 参数 | 作用 |
| --- | --- |
| `-SkipGoBuild` | 复用已有的 `kair.exe`（不再编译） |
| `-Iscc <路径>` | 指定 `ISCC.exe` |
| `-OutDir <目录>` | 指定输出目录，默认 `<root>\dist` |

## 5. 验证安装包（不要跳过）

至少验这五件事，全部可以用静默参数完成：

```powershell
$installer = '<root>\dist\Kairos-<版本>-setup.exe'
$dir = "$env:TEMP\kair-check"

# a) 装得上
& $installer /VERYSILENT /SUPPRESSMSGBOXES /NORESTART "/DIR=$dir" "/LOG=$dir.log"
& "$dir\kair.exe" -version          # 应与版本号一致

# b) 升级不丢数据
#    在 $dir\kairos-data 里放一份数据，再装一次，确认文件哈希不变
# c) 换目录升级会把旧数据带过去（静默下默认“是”）
# d) 卸载默认保留数据
& "$dir\unins000.exe" /VERYSILENT /SUPPRESSMSGBOXES /NORESTART

# e) 换一个「默认风格」的安装路径再装一次（最关键、最容易漏）
& $installer /VERYSILENT /SUPPRESSMSGBOXES /NORESTART "/DIR=C:\Program Files\Kairos"
```

**第 e 件为什么必须做**：曾出过这样的事故——脚本里
`ExpandConstant('{userprofile}')` 常量名写错（正确是 `{userpf}`）。
这是**运行期**错误，编译器不报；而「位置是否合理」的判断在 `%TEMP%` 路径下
会提前返回，所以只测 `%TEMP%` 永远发现不了，用户装到
`C:\Program Files\Kairos` 才会以退出码 1 失败。**只测一种路径不足以验证安装包。**

改过 `.iss` 之后要重走这个清单，并且**重新上传 Release 附件**——
用户下载的是附件，不是你本地那份。

**验证完记得卸载测试安装**，否则会在注册表里留下卸载项。
检查残留：

```powershell
Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*',
                 'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*' -ErrorAction SilentlyContinue |
  Where-Object { $_.DisplayName -like '*Kairos*' }
```

## 6. 安装包的行为约定（改脚本时不要破坏）

- **允许自选安装路径**，默认 `{autopf}\Kairos`；选了桌面 / 下载 / 文档 / 临时 /
  盘符根 / 系统关键目录会提醒，用户反悔可以留在目录页重选。
- **默认勾选加入 PATH**（`addtopath`，`ChangesEnvironment=yes` 负责广播变更）；
  取消勾选会提醒一次。注意 Pascal Script **没有**程序化勾选任务的 API。
- **升级不碰数据**：`kairos-data` 不在 `[Files]` 里，所以 Inno 永远不会覆盖或删除它。
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

1. 打 tag：`git tag -a v1.0.0 -m "Kairos v1.0.0"`，推送 tag。
2. 新建 Release，选该 tag，标题写 `Kairos v1.0.0`。
3. **上传 `dist\Kairos-<版本>-setup.exe`** 作为附件。
4. Release 说明里写：新增 / 修复 / 破坏性变更（如有）、以及升级方式
   （直接覆盖安装，数据不受影响）。
5. 确认 `LICENSE`（MIT）与仓库地址无误。

## 9. 发布后自检

- Release 页面的安装包能下载，文件名与版本号对得上。
- 全新机器（或干净目录）装一次能跑起来。
- README 里的下载链接指向 Releases 页而不是写死的文件名。
