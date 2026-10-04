# 已踩过的坑

每条都对应一次真实的用户投诉或一次自伤事故。动手前读一遍。
这些坑的共同教训：**这一类要按「动作」收口，不要按「按键」打补丁。**

## 渲染与排版

| 现象 | 根因 | 正确做法 |
| --- | --- | --- |
| 弹窗把左右面板边框切出断口 | 弹窗拼接在整个看板上，必然横跨三栏 | 二级内容只占中间栏（见 conventions 2） |
| 「随手记没保存」的提问看不见 | `View()` 里 editor 优先级高于 pick | pick 必须最高（见 conventions 3） |
| 设置页选中行底色拖成一条长条 | 用 `pad(text, inner)` 铺满整行 | 用 `highlightWidth`（见 conventions 5） |
| 帮助页只剩按键名、说明全没了 | 整页宽度取了终端宽度而非实际面板宽度 | 宽度按面板实际内容区算 |
| 面板多出一行 / 少一行 | 相信了 Lipgloss 的 `Width`/`Height`（只是下限） | 渲染后实测自校正 + `clipBlock` |
| 小窗口（10×3）糊屏 | 提示文本固定输出 5 行 30 列 | 提示也必须裁剪到窗口内 |

## 光标与文本

| 现象 | 根因 | 正确做法 |
| --- | --- | --- |
| **中文一输入光标就跑偏** | `text[:cursor]` 是**字节**切片，`cursor` 是 rune 下标；纯英文时两者相等所以看不出来 | 按 rune 遍历算行号，行内按显示宽度定位 |
| 光标在低色彩终端里看不见 | 光标画成「反白的空格」，终端把反白降级掉了 | 用一个可见字符（项目用 `▏`） |
| 汉字被劈成两半 | 按显示列切分时落在宽字符中间 | 整个字符归左侧、光标跟在它后面（`splitAtColumn`） |
| 文本里混进 `\u0000` | 终端粘贴带进控制字符 | 入库前 `model.Sanitize` |

## 数据

| 现象 | 根因 | 正确做法 |
| --- | --- | --- |
| TODO 全删了，统计里还列着它们 | 删条目只从列表摘掉，计时引用与按名聚合的 activity 悬空 | `PruneOrphans()`，删除时与读入时都调 |
| 自由专注被误删 | 第一版按「名字不存在就删」 | 判据是「是否只出现在引用已删除条目的记录里」 |
| 未结束的计时被计入总计 | 统计时没过滤 `ended` 为空 | 只有 `ended != nil` 才算时长 |
| `activity` 变 nil 后 panic | 清理时把 map 置成 nil，别处直接往里写 | 保持非 nil 空 map（`omitempty` 同样不序列化） |
| 日界线永远解析失败 | 用 `fmt.Sscanf("%d:%d:%d")` 解析 `HH:MM` | 手工按 `:` 切分 |
| 月备份恢复找不到文件 | 备份前缀与恢复用的 glob 不是同一个 stem | 统一用月份 stem |
| 自定义专注「有记录但不计入当日专注」 | 归档只记「结束那一刻所在段」的类型；自定义方案是跨段的，结束在休息段时整段专注被算成休息 | 归档时**按段累计**专注时长（`Plan.FocusUpTo`）存进 `Session.Focus`；只有明确标成 `focus` 的段计入专注 |
| 正计时进度条显示「第 1 段 」且段名空白 | `Plan{Kind: TimerCountUp}` 一个段都没给 | 正计时也要有一段（`自由专注`）；只按 `Kind` 判断类型的代码遇到空方案会静默算错 |

### 统计口径会变时，必须给老数据留退路

`FocusTotal()` 早期只判断 `segment_kind == "break"`，其余都算专注。改成
「只有 `focus` 算专注」之后，历史数字会跟着变。做法是给新字段用**指针**
（`Focus *time.Duration`）：老数据是 `nil` → 退回旧口径；新数据一律写入，
于是「专注 0 秒」也被如实记成 0 而不是缺省。改任何统计口径前先问一句
「用户已有的历史数字会不会变」，变了就要留这条退路。

### 校验数据的代码，不能对「读不懂」睁一只眼闭一只眼

数据版本保护第一版把「文件解析失败」当成跳过，理由是「那不是版本问题」。
实机验证时被抓出来：Windows PowerShell 的 `Set-Content -Encoding utf8`
会写出**带 UTF-8 BOM** 的文件，BOM 让 `json.Unmarshal` 直接失败——一个
`schema_version=99` 的日数据文件就这样被放过去，程序照常启动并开始写数据，
守卫形同虚设。

教训：**校验器遇到读不懂的输入时，默认应该是「拦住」而不是「放行」**，
除非能证明它是无害的。这里只有两种情况可以放行：文件不存在（首次启动）、
空文件（没有结构可言）。其余一律阻断并说明原因。

顺带一条环境坑：用 PowerShell 造测试数据时，`Set-Content -Encoding utf8`
（Windows PowerShell 5.1）写出的不是纯 UTF-8，会带 BOM。要造 JSON 测试数据
就用编辑器工具写，或用 `[System.IO.File]::WriteAllText($p, $json, UTF8Encoding($false))`。

## 按键与交互

| 现象 | 根因 | 正确做法 |
| --- | --- | --- |
| 按 `esc` 把随手记丢了，`q` 却有保护 | 保护只写在一个按键分支里 | 按动作收口，列出全部入口逐个覆盖 |
| 退出菜单绕过保护直接退 | 该分支直接 `tea.Quit` | 一律走 `askQuit()` |
| 计时中退出，时长白记 | 退出只置 `quitting`，没归档 | 退出前 `stopTimer` + 写盘 |
| 三选一里按 `n` 把内容丢了 | 小选择框沿用了 `y`/`n` 快捷键 | 两个以上选项时禁用 `y`/`n`（`pickState.small`） |
| 单行输入框里打不出 `q` | 关闭保护误伤了单行输入 | 只对多行输入生效 |
| 鼠标一开就没法复制中文 | 接管鼠标后终端不能选中文本 | 默认不接管鼠标 |

### 同一个动作被写在两处，就会「按一下就触发」

`enter` 曾经同时挂在计时分支和看板 `switch` 的 `enter` 分支上：两边都调
`stopTimer`。于是用户在计时中按回车想进子任务，直接把整段专注清掉了。
**同一动作只允许有一个入口**；顺带也要检查「这个键在看板上本来是什么语义」——
`space`（勾选完成）和 `enter`（进入子任务）在计时中被借走，就等于计时期间
这两个功能不可用。计时这类**活跃状态**最终收成了单一入口：`p` 菜单。
（`esc` 与 `enter` 只在确认框里作为「确认 / 取消」使用，那是明确的状态，不是看板按键。）

### 关于输入法：不要写成「必须粘贴」

终端**能**正常使用输入法打字，上屏的文字会正常进入输入框。
唯一的差别是**组字窗口的位置**：终端不向程序报告光标坐标，输入法的候选窗因此不会
跟在光标旁边，而是贴在终端窗口的某个角落。

这**不是缺陷**，程序也无从改善（它只收到最终上屏的字符，收不到组字过程，
更没法告诉输入法光标在哪）。曾经在 README 里写成「直接敲拼音不会上屏、
必须粘贴」，被用户纠正过——这类说法会劝退中文用户，属于**把环境特性夸大成了
功能缺失**。写文档时按实际体验描述，别加戏。

粘贴依然是一条可用的替代路径（并且顺手支持了从别处整段搬文字的场景），
但它是**补充**，不是唯一方式。

## 环境与工具（本仓库的沙箱注意事项）

这些是**开发环境**的坑，不是产品问题，但会浪费大量时间：

- **Go 缓存**：沙箱下默认 `GOCACHE` 可能不可写。用工程内缓存：
  ```powershell
  $env:GOCACHE="<root>\.gocache"; $env:GOMODCACHE="<root>\.gomodcache"; $env:GOSUMDB="off"
  ```
- **不要把 PowerShell 的 `Set-Content` / `-replace` 用在 Go 源码上**：
  会破坏 UTF-8（曾经把两个文件弄坏、只能整份重写）。用编辑器工具写文件。
  **这条被同一个坑连续咬过两次**：第一次是把中文注释写坏；第二次是
  `(Get-Content $f -Raw) -replace 'a','b' | Set-Content $f`——读取按控制台
  代码页解码、写入再编码一次，中文注释全变成 `鐩存帴`，而且注释尾部的换行被
  吃掉、把下一行 `func` 吞进注释里，报出 `expected declaration, found t` 这种
  指向别处的语法错误。**要改源码就用编辑器工具，别绕 PowerShell。**
- **PowerShell 脚本要带 UTF-8 BOM**：Windows PowerShell 会把无 BOM 的 UTF-8 当 ANSI 读，
  中文注释直接变成语法错误。
- **Inno Setup 的 `.iss` 也要带 BOM**，否则中文字符串会让 Pascal 编译器报出
  莫名其妙的语法错误（报错行号还在别处）。
- **`.iss` 里不能在注释中写花括号常量**（例如 `{usertemp}`）：预处理器照样会去解析它，
  报 "Unknown constant"。而且 Inno Setup **没有 `{usertemp}` 这个常量**。
- **`ExpandConstant` 的常量名拼错是「运行期」错误，编译器不报**：
  写 `{userprofile}`（正确是 `{userpf}`）时编译通过，只有执行到那一行才抛
  "Unknown constant"，静默安装直接以退出码 1 失败，日志里只有一句内部错误。
  改任何 `ExpandConstant` 之后，**必须实际安装到会走到那行代码的路径**去验证。
- **验证安装包不能只挑 `%TEMP%` 下的路径**：安装到 `%TEMP%` 时，
  「位置是否合理」的判断会在更靠前的 `{%TEMP}` 分支提前返回，
  根本走不到后面出错的常量——真装到 `C:\Program Files\...` 才会崩。
  至少覆盖一个「默认风格」路径。
- **Inno Setup 里 `Exit(value)` 不可用**：用 `Result := value; Exit;`。
- **`[Tasks]` 里的一条任务本身不做任何事，它只是个勾选框。**
  必须另外把它绑到实际动作：`[Icons]` / `[Run]` 等段用 `Tasks: <name>` 参数，
  或在 `[Code]` 里用 `WizardIsTaskSelected('<name>')` 判断。
  **曾经出过的事故**：`addtopath` 的任务、文案、提示、`ChangesEnvironment=yes`
  全都写了，唯独漏了写入动作——用户勾了「加入 PATH」却什么都没发生，
  而且是发布后才被用户发现。
  改安装脚本后请逐个核对：**每一条任务都要能指出它在哪一行被执行**。
- **`ChangesEnvironment=yes` 不等于「会写环境变量」**：它只负责安装后广播
  「环境变量已变更」，让新开的进程读到新值；写值要自己动手
  （本项目在 `[Code]` 里读写 `HKCU\Environment` 的 `Path`）。
- **`[Registry]` 段不支持 `Tasks` 参数**（只有 `[Icons]` / `[Run]` 等支持）。
  要按任务条件写注册表，得在 `[Code]` 里判断，或用 `Check` 函数。
- **`[Code]` 的 Pascal 注释里有两类字符会破坏解析**：
  单独的右花括号会提前结束注释；以方括号开头的行（例如写段名开头）会被
  预处理器当成段标记，报 "Invalid section tag" 且行号指在别处。
  花括号常量（例如 olddata 那种占位写法）写在注释里同样会被解析。
- **Pascal Script 没有 `WizardSelectTask`**：只能读（`WizardSelectedTasks`），不能程序化勾选任务。
- **函数必须先声明后使用**：辅助函数放到调用者前面。
- **`git clone` 在本机沙箱下会取不到 TLS 凭据**：报
  `schannel: AcquireCredentialsHandle failed: SEC_E_NO_CREDENTIALS (0x8009030E)`。
  这不是网络问题（`Test-NetConnection github.com 443` 是通的，走不走代理都一样报），
  而是 git 默认的 `http.sslBackend=schannel` 在本机拿不到凭据。换用 openssl 即可：
  ```powershell
  git -c http.sslBackend=openssl clone <url> <dir>
  git -C <dir> config http.sslBackend openssl   # 只写进本仓库，别动全局配置
  ```
  写进**仓库本地**配置就够了，后续 fetch/push 都正常，也不会影响用户其它仓库。
- **`git push` 在受限沙箱下会失败**（凭据助手需要创建进程管道）。需要推送时
  用一次性放宽权限执行。用户已授权推送本仓库。
- **读 Go 源码不要用 PowerShell 的 `Get-Content`**：它按控制台代码页解码，
  会把 UTF-8 中文显示成「鍖?version 淇濆瓨…」这种乱码。**文件本身是好的**，
  别据此判断编码坏了、更别去「修」它。用能按 UTF-8 读的工具（编辑器工具/`read`）。
  写入侧的同类坑见上面 `Set-Content` 那条。
- **不要用 `Start-Process` 跑 `kqf.exe` 做验证**：TUI 没有真控制台会挂住并留下僵死进程。
  验证安装包用 `/VERYSILENT /SUPPRESSMSGBOXES /NORESTART`。
- **只做离屏验证、却不给可执行文件，用户没法做实机测试**：本环境跑不了 TUI
  （见上一条），所以涉及交互的改动必须**编译一个 exe 交给用户**，否则「已完成」
  只是纸面上的。编译方式与打包一致（注意注入版本号，别让测试构建冒充正式版本）：
  ```powershell
  go build -trimpath -ldflags "-s -w -X github.com/kqin-dev/The-KQFLOW-Time-manager/internal/version.Version=2.0.1-dev.1" -o kqf.exe ./cmd/kqf
  ```
  `version.go` 里的正式版本号**不要动**——版本号只发版时改；`-dev.N` 后缀让用户
  一眼能确认跑的是哪次改动，也不会和已发布版本混淆。产物名保持 `kqf.exe`
  （`data_dir` 默认按可执行文件同级解析，改名会让预览图与说明书对不上）。
- **沙箱会拦住安装器的注册表/进程操作，导致 PATH 类改动无法在本机验证**：
  `HKCU\Environment` 在受限模式下不可写，安装器会以退出码 4 失败（连日志都不产生）；
  放宽权限后能写注册表，但安装器子进程又会遇到工作区不可写的问题。
  **这类改动必须交给用户在真实环境验证**，并在交付说明里如实讲清楚。
- **写完 commit message 用 `[System.IO.File]::WriteAllText` + UTF8Encoding($false)**，
  别用 `Set-Content -Encoding utf8`（会加 BOM）。

## 数据写入：Windows 上的偶发失败

| 现象 | 根因 | 正确做法 |
| --- | --- | --- |
| 界面偶尔弹「保存失败：… rename …tmpXXXX…」 | 原子写入最后一步 `os.Rename` 在 Windows 走 `MoveFileEx(MOVEFILE_REPLACE_EXISTING)`，目标文件正被别的进程打开时返回 `Access is denied` | **退避重试**，别一次失败就报错 |

`Access is denied` 的常见来源：杀毒/Defender 实时防护刚扫完一个**刚写完的** json、
Windows 索引服务、资源管理器预览、同步盘（OneDrive 等）读取。这些锁几乎总是瞬时的。

要点：

- 重试是**安全**的：rename 失败不会损坏原文件（目标内容保持原样）。
- 但重试预算要短（项目里约 0.55 秒），否则界面会卡住。
- 错误识别别只匹配英文文案，Windows 会本地化（中文是「拒绝访问」）。
- 临时文件要 `defer os.Remove` 兜底，失败时不留碎屑。

**测试这类逻辑的坑**：用独占锁复现时，别用 `os.ReadFile` 去验证“原文件没被改坏”——
你自己也持有锁，读同样被拒绝，取到的是空内容，会误判成文件被清空。
用 `os.Stat` 看大小。另外用 `GENERIC_WRITE` 加锁会把文件截断成 0 字节，
制造锁时只用 `GENERIC_READ`。

## 上一次测试写错反而掩盖真 bug

发生过不止一次：测试里的断言本身用错了坐标系（按 rune 下标比对显示列），
于是实现真的有 bug、测试却是绿的。

**结论**：测试失败时，先确认**断言**是否符合约定（conventions 第 1 条），
再去改实现。断言用的辅助函数也要按显示宽度写。
