; Kairos 安装脚本（Inno Setup 6 / 7）
;
; 四条设计目标，对应脚本里的四处关键设置：
;   1. 允许自选安装路径，但对不合理的位置给出提醒  -> [Code] ShouldInstallDirWarn / NextButtonClick
;   2. 默认把安装目录加入 PATH，并提醒用户保持勾选  -> [Tasks] addtopath + ChangesEnvironment=yes
;   3. 旧版本可直接覆盖升级，且绝不碰用户数据      -> AppId 固定 + 数据目录不在 [Files] 里
;   4. 卸载时询问是否删除数据，默认保留            -> [Code] CurUninstallStepChanged
;
; 版本号与输出文件名由 build-installer.ps1 通过 /D 传入，不要在这里硬编码：
; 版本号只有一个权威来源 —— internal/version/version.go。

#ifndef AppVersion
  #define AppVersion "0.0.0-dev"
#endif
#ifndef RepoRoot
  #define RepoRoot ".."
#endif

#define AppName "Kairos"
#define AppPublisher "kqin-dev"
#define AppURL "https://github.com/kqin-dev/The-Kairos-Time-manager"
#define AppExeName "kair.exe"
; 数据目录名必须与 internal/config 里的默认值一致：
; 程序默认把数据放在“可执行文件同级的 kairos-data”。
#define DataDirName "kairos-data"

; 升级时若用户换了安装目录，注册表里记着的老目录能帮我们找到旧数据。
; 键名由上面的 AppId 推导而来（Inno 的规则：去掉最外层大括号再加 _is1）。
; 这里写字面量是因为 #define 里嵌 SetupSetting() 会让预处理器解析失败；
; 如果哪天改了 AppId，这一行必须跟着改。
#define UninstallKey "Software\Microsoft\Windows\CurrentVersion\Uninstall\{8F1C2A64-3B7D-4E52-9A10-6C5E7D2F41B8}_is1"

[Setup]
; AppId 一经发布就不能改：Inno Setup 靠它识别“这是同一个程序的升级”。
; 改了会导致新旧版本并存、控制面板里出现两条卸载项。
AppId={{8F1C2A64-3B7D-4E52-9A10-6C5E7D2F41B8}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}/issues
AppUpdatesURL={#AppURL}/releases
VersionInfoVersion={#AppVersion}
VersionInfoCompany={#AppPublisher}
VersionInfoProductName={#AppName}
VersionInfoProductVersion={#AppVersion}
VersionInfoDescription={#AppName} Setup

; 默认装到标准程序目录；用户可以在目录页改成别处（不合理的位置会提醒）。
DefaultDirName={autopf}\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
; 升级时沿用上次选的安装目录，避免用户换了盘符后新旧两份并存。
UsePreviousAppDir=yes
AllowNoIcons=yes
; 允许普通用户装到自己的目录（安装向导里可以切换）。
PrivilegesRequiredOverridesAllowed=dialog
; addtopath 任务靠这个生效：Inno 会按需写环境变量并在安装后广播变更，
; 新开的终端立刻能找到 kair。
ChangesEnvironment=yes

ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
SolidCompression=yes
Compression=lzma2/max
WizardStyle=modern
MinVersion=10.0

OutputDir={#RepoRoot}\dist
OutputBaseFilename={#AppName}-{#AppVersion}-setup
UninstallDisplayName={#AppName} {#AppVersion}
UninstallDisplayIcon={app}\{#AppExeName}

[Languages]
Name: "chinese"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[CustomMessages]
chinese.CreateDesktopIcon=创建桌面快捷方式
chinese.LaunchApp=立即运行 {#AppName}
chinese.AddToPath=把安装目录加入 PATH（推荐：之后可在任意终端直接输入 kair）
chinese.DirWarnTitle=安装位置提醒
chinese.DirWarnBody=你选择的目录可能不太合适：%n%n%1%n%n建议装在系统程序目录（如 C:\Program Files\Kairos），或你自己的用户目录下。请不要放在桌面、下载、临时目录，也不要塞进别的程序目录里。%n%n仍要使用这个目录吗？
chinese.PathWarnTitle=建议加入 PATH
chinese.PathWarnBody=你取消了“加入 PATH”。这样安装后需要输入完整路径才能启动（例如 "C:\Program Files\Kairos\kair.exe"）。%n%n确定要取消吗？
chinese.UninstallKeepDataTitle=是否保留数据
chinese.UninstallKeepDataBody=检测到你的数据目录：%n%n%1%n%n是否保留它？%n%n选“是”会保留全部记录（推荐，方便以后重装或升级）。选“否”后会再确认一次。
chinese.UninstallDeleteConfirmBody=即将永久删除以下数据，且无法恢复：%n%n%1%n%n确定要删除吗？如果只是想卸载程序，请选“否”。
chinese.UninstallDataKept=数据已保留：%1
chinese.UpgradeMoveDataBody=检测到旧版本的数据：%n%n%1%n%n你这次安装到了新目录：%n%2%n%n要把旧数据复制过来吗？（旧目录里的数据不会被删除）%n%n选“否”则新目录从空白开始，旧数据仍留在原处。
chinese.UpgradeMoveDataFailed=复制旧数据失败（旧目录：%1）。程序已装好，但新目录里没有旧数据；你可以手动复制该目录。
chinese.RunningPrompt={#AppName} 正在运行，请先关闭它再继续。

english.CreateDesktopIcon=Create a desktop shortcut
english.LaunchApp=Launch {#AppName}
english.AddToPath=Add the install folder to PATH (recommended: lets you type kair in any terminal)
english.DirWarnTitle=Install location
english.DirWarnBody=The folder you chose may not be suitable:%n%n%1%n%nPrefer a program folder such as C:\Program Files\Kairos, or a folder under your own user profile. Avoid the desktop, Downloads, TEMP, or nesting it inside another program.%n%nUse this folder anyway?
english.PathWarnTitle=PATH recommended
english.PathWarnBody=You turned off "Add to PATH". You will then need the full path to launch Kairos (for example "C:\Program Files\Kairos\kair.exe").%n%nTurn it off anyway?
english.UninstallKeepDataTitle=Keep your data?
english.UninstallKeepDataBody=Your data folder was found:%n%n%1%n%nKeep it?%n%nYes keeps all records (recommended, in case you reinstall or upgrade). No asks once more before deleting.
english.UninstallDeleteConfirmBody=This will permanently delete the following data, and it cannot be undone:%n%n%1%n%nDelete it? If you only meant to uninstall the app, choose No.
english.UninstallDataKept=Your data was kept at: %1
english.UpgradeMoveDataBody=Existing data from a previous install was found:%n%n%1%n%nYou are installing into a new folder this time:%n%2%n%nCopy the old data over? (The old folder is never deleted.)%n%nChoose No to start fresh in the new folder; your old data stays where it is.
english.UpgradeMoveDataFailed=Could not copy the old data (source: %1). The app is installed, but the new folder has no old data. You can copy that folder manually.
english.RunningPrompt={#AppName} is running. Please close it before continuing.

[Tasks]
; addtopath 依赖 [Setup] 的 ChangesEnvironment=yes：
; Inno 会在安装/卸载后广播环境变量变更，新开的终端立刻能用 kair。
Name: "addtopath"; Description: "{cm:AddToPath}"; GroupDescription: "{cm:AddToPath}"; Flags: checkedonce
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:CreateDesktopIcon}"; Flags: unchecked

[Files]
; 只分发程序与说明文件。用户数据（{#DataDirName}）永远不在这个列表里，
; 所以升级只会覆盖程序文件，数据原封不动。
Source: "{#RepoRoot}\kair.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#RepoRoot}\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#RepoRoot}\LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#AppName}"; Filename: "{app}\{#AppExeName}"
Name: "{group}\卸载 {#AppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#AppExeName}"; Description: "{cm:LaunchApp}"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
; 只删自己放进去的程序文件。卸载时若用 filesandordirs 删整个 {app}，
; 会把用户的 {#DataDirName} 一并带走。
Type: files; Name: "{app}\{#AppExeName}"
Type: files; Name: "{app}\README.md"
Type: files; Name: "{app}\LICENSE"

[Code]
{ 常量与工具函数。
  PATH 的增删本身交给 Inno Setup 的 ChangesEnvironment + addtopath 任务，
  这里只负责“提醒”和“卸载时问数据怎么处理”两件事。 }

{ ---------- 小工具 ---------- }

function TrimTrailingSlash(const S: string): string;
begin
  Result := S;
  while (Length(Result) > 3) and (Result[Length(Result)] = '\') do
    Delete(Result, Length(Result), 1);
end;

function LowerPath(const S: string): string;
begin
  Result := LowerCase(TrimTrailingSlash(S));
end;

function AppDirPath(): string;
begin
  Result := TrimTrailingSlash(ExpandConstant('{app}'));
end;

{ 数据目录：与 internal/config 的默认规则一致（可执行文件同级的 kairos-data）。 }
function DataDirFor(const Dir: string): string;
begin
  Result := TrimTrailingSlash(Dir) + '\{#DataDirName}';
end;

{ ---------- 1. 安装位置提醒 ---------- }

{ 判断目录是否属于“不太合适”的位置。 }
function DirLooksUnwise(const Dir: string): Boolean;
var
  D: string;
begin
  D := LowerPath(Dir);
  if D = '' then begin Result := False; Exit; end;

  { 桌面 / 下载 / 文档 / 临时等个人目录。
    注意：Inno Setup 没有 usertemp 这个常量，临时目录要用 %TEMP% 或 tmp。 }
  if Pos(LowerPath(ExpandConstant('{userdesktop}')), D) = 1 then begin Result := True; Exit; end;
  if Pos(LowerPath(ExpandConstant('{userdocs}')), D) = 1 then begin Result := True; Exit; end;
  if Pos(LowerPath(ExpandConstant('{%TEMP}')), D) = 1 then begin Result := True; Exit; end;
  if Pos(LowerPath(ExpandConstant('{%TMP}')), D) = 1 then begin Result := True; Exit; end;
  { 盘符根目录，或系统关键目录本身。 }
  if (Length(D) = 2) and (D[2] = ':') then begin Result := True; Exit; end;
  if D = LowerPath(ExpandConstant('{userprofile}')) then begin Result := True; Exit; end;
  if D = LowerPath(ExpandConstant('{win}')) then begin Result := True; Exit; end;
  if D = LowerPath(ExpandConstant('{sys}')) then begin Result := True; Exit; end;

  Result := False;
end;

{ 1) 目录页：位置不合理就提醒；用户反悔则留在本页重选。
  2) 任务页：取消“加入 PATH”就提醒一次。
     （只用 MsgBox，不改勾选状态 —— Pascal Script 没有程序化勾选任务的 API，
     且该项默认已勾上，提醒到位即可。） }
function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;

  if CurPageID = wpSelectTasks then
  begin
    if not WizardIsTaskSelected('addtopath') then
      MsgBox(CustomMessage('PathWarnBody'), mbInformation, MB_OK);
    Exit;
  end;

  if CurPageID <> wpSelectDir then
    Exit;

  if not DirLooksUnwise(WizardDirValue()) then
    Exit;

  if MsgBox(FmtMessage(CustomMessage('DirWarnBody'), [WizardDirValue()]),
       mbConfirmation, MB_YESNO) = IDNO then
  begin
    { 返回 False 会留在目录页，让用户重新选。 }
    Result := False;
  end;
end;

{ ---------- 3. 升级：换了安装目录就把旧数据带过来 ---------- }

{ 递归复制整个数据目录。 }
function CopyDataDir(const Src, Dst: string): Boolean;
var
  FindRec: TFindRec;
  Sub: string;
begin
  Result := True;
  if not DirExists(Dst) then
    Result := ForceDirectories(Dst);

  if not FindFirst(Src + '\*', FindRec) then
    Exit;

  try
    repeat
      if (FindRec.Name = '.') or (FindRec.Name = '..') then
        Continue;
      Sub := Src + '\' + FindRec.Name;
      if FindRec.Attributes and FILE_ATTRIBUTE_DIRECTORY <> 0 then
      begin
        if not CopyDataDir(Sub, Dst + '\' + FindRec.Name) then
          Result := False;
      end
      else
      begin
        if not CopyFile(Sub, Dst + '\' + FindRec.Name, False) then
          Result := False;
      end;
    until not FindNext(FindRec);
  finally
    FindClose(FindRec);
  end;
end;

{ 安装开始前检查：如果这是升级、而且数据不在新目录里，
  就询问是否把旧数据复制过来。这样“换目录升级”不会让用户以为数据丢了。

  注意：数据不放在 [Files] 里，所以 Inno 永远不会覆盖或删除它；
  这里只是“复制一份”，失败也不影响安装（顶多是新目录里没有数据）。 }
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  OldDir, OldData, NewData: string;
begin
  Result := '';
  NewData := DataDirFor(AppDirPath());

  { 新目录已经有数据了，什么都不用做。 }
  if DirExists(NewData) then
    Exit;

  { 找旧安装目录：从卸载注册表里读上一次装到哪儿。 }
  OldDir := '';
  if not (RegQueryStringValue(HKLM, '{#UninstallKey}', 'InstallLocation', OldDir) or
          RegQueryStringValue(HKCU, '{#UninstallKey}', 'InstallLocation', OldDir)) then
    Exit;

  OldDir := TrimTrailingSlash(OldDir);
  if OldDir = '' then
    Exit;
  if LowerPath(OldDir) = LowerPath(AppDirPath()) then
    Exit;

  OldData := DataDirFor(OldDir);
  if not DirExists(OldData) then
    Exit;

  if MsgBox(FmtMessage(CustomMessage('UpgradeMoveDataBody'), [OldData, NewData]),
       mbConfirmation, MB_YESNO) = IDNO then
    Exit;

  if not CopyDataDir(OldData, NewData) then
    MsgBox(FmtMessage(CustomMessage('UpgradeMoveDataFailed'), [OldData]), mbError, MB_OK);
end;

{ ---------- 4. 卸载：询问是否保留数据 ---------- }

{ 数据是用户最珍贵的东西，所以默认按“保留”处理。

  这里刻意**不用** Yes/No 直接决定删不删：用户顺手关掉对话框、
  按 ESC 或用右键关闭时，返回值不是 IDYES，如果写成
  “不是 Yes 就删”，一次误操作就会把记录清空。

  改成两段式：先问“要不要保留”（默认保留），
  只有在用户明确选择“保留”之后，才再问一次“真的保留吗”，
  仍然回答“是”才继续保留；否则什么都不做。
  真正删除必须点两个明确的按钮，代价高但安全。 }
function AskKeepData(const DataDir: string): Boolean;
begin
  { 第一问：是否保留（默认为“是”）。 }
  if MsgBox(FmtMessage(CustomMessage('UninstallKeepDataBody'), [DataDir]),
       mbConfirmation, MB_YESNO) = IDYES then
  begin
    Result := True;
    Exit;
  end;

  { 第二问：确认删除。只有明确再点一次“是”才删。 }
  Result := MsgBox(FmtMessage(CustomMessage('UninstallDeleteConfirmBody'), [DataDir]),
    mbConfirmation, MB_YESNO) = IDNO;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  DataDir: string;
begin
  if CurUninstallStep <> usPostUninstall then
    Exit;

  DataDir := DataDirFor(AppDirPath());
  if not DirExists(DataDir) then
  begin
    { 没有数据目录时也要收掉可能残留的空目录。 }
    RemoveDir(AppDirPath());
    Exit;
  end;

  if AskKeepData(DataDir) then
    MsgBox(FmtMessage(CustomMessage('UninstallDataKept'), [DataDir]), mbInformation, MB_OK)
  else
  begin
    DelTree(DataDir, True, True, True);
    RemoveDir(AppDirPath());
  end;
end;
