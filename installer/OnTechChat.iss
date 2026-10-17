; The only release Setup definition. Invoked only by root build.ps1.
; The embedded runtime owns service/data migration, rollback and the existing
; uninstall registration. Do not add a second uninstaller or delete ProgramData.
#ifndef AppVersion
  #error AppVersion must be supplied by build.ps1
#endif
#ifndef RuntimeSource
  #error RuntimeSource must be supplied by build.ps1
#endif
#ifndef ReleaseDir
  #error ReleaseDir must be supplied by build.ps1
#endif

[Setup]
AppId=OnTechChatServer
AppName=OnTech Chat Server
AppVersion={#AppVersion}
AppPublisher=OnTech
VersionInfoVersion={#AppVersion}
VersionInfoDescription=OnTech Chat Server Inno Setup
DefaultDirName={autopf}\OnTechChatServer
CreateAppDir=no
Uninstallable=no
CreateUninstallRegKey=no
DisableDirPage=yes
DisableProgramGroupPage=yes
DisableWelcomePage=no
DisableReadyPage=no
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
WizardStyle=modern
SetupIconFile=..\tray\icons\running.ico
OutputDir={#ReleaseDir}
OutputBaseFilename=OnTechChat-Server-Setup-v{#AppVersion}-x64
Compression=lzma2/normal
SolidCompression=yes
CloseApplications=no
RestartApplications=no
SetupLogging=yes

[Languages]
Name: "korean"; MessagesFile: "compiler:Languages\Korean.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[CustomMessages]
korean.InstallSummary=서버와 트레이를 설치하고 Windows 서비스를 시작합니다.%n%n기존 설정·계정·대화·첨부파일은 보존합니다.%n업데이트 중에는 실행 중인 서버와 트레이가 잠시 재시작됩니다.%n%n프로그램: %1%n데이터: %2%n%n도메인 HTTPS는 설치 후 서버 설정 → 외부 접속에서 설정하세요.
english.InstallSummary=Installs the server and tray and starts the Windows service.%n%nExisting settings, accounts, chats and attachments are preserved.%nThe server and tray restart briefly during an update.%n%nProgram: %1%nData: %2%n%nConfigure domain HTTPS in Server Settings after installation.
korean.InstallingServer=서버를 설치하고 서비스를 시작하고 있습니다...
english.InstallingServer=Installing the server and starting the service...
korean.InstallFailed=서버 설치가 완료되지 않았습니다. 오류를 확인한 뒤 다시 시도하세요.
english.InstallFailed=Server installation did not complete. Review the error and retry.
korean.OpenSettings=서버 설정 화면 열기
english.OpenSettings=Open server settings
korean.FinishedNote=서버 설치와 서비스 시작을 완료했습니다.%n%n초기 관리자 정보: 시작 메뉴 → OnTech Chat Server → 초기 관리자 정보%n도메인 HTTPS: 서버 설정 → 외부 접속%nAI 로그인: 트레이 메뉴에서 진행%n%n서버 PC가 켜져 있어야 외부에서 접속할 수 있습니다.
english.FinishedNote=Server installation and service start completed.%n%nInitial account: Start menu > OnTech Chat Server > Initial administrator information%nDomain HTTPS: Server Settings > External Access%nAI sign-in: tray menu%n%nThe server PC must remain running for remote access.

[Files]
Source: "{#RuntimeSource}"; DestName: "OnTechChat.Runtime.exe"; Flags: dontcopy

[Run]
Filename: "{commonprograms}\OnTech Chat Server\설정 화면.url"; Description: "{cm:OpenSettings}"; Flags: postinstall shellexec skipifsilent

[Code]
var
  ServerInstalled: Boolean;
  InstallWarnings: String;

function UpdateReadyMemo(Space, NewLine, MemoUserInfoInfo, MemoDirInfo,
  MemoTypeInfo, MemoComponentsInfo, MemoGroupInfo, MemoTasksInfo: String): String;
begin
  Result := FmtMessage(CustomMessage('InstallSummary'), [ExpandConstant('{autopf}\OnTechChatServer'),
     ExpandConstant('{commonappdata}\OnTechChatServer')]);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode, I: Integer;
  Captured: TExecOutput;
  ErrorDetails: String;
begin
  Result := '';
  if ServerInstalled then Exit;
  WizardForm.PreparingLabel.Caption := CustomMessage('InstallingServer');
  ExtractTemporaryFile('OnTechChat.Runtime.exe');
  if not ExecAndCaptureOutput(ExpandConstant('{tmp}\OnTechChat.Runtime.exe'),
    '--install-worker', ExpandConstant('{tmp}'), SW_HIDE,
    ewWaitUntilTerminated, ResultCode, Captured) then
  begin
    Result := CustomMessage('InstallFailed') + #13#10 + SysErrorMessage(ResultCode);
    Exit;
  end;
  ErrorDetails := '';
  for I := 0 to GetArrayLength(Captured.StdErr) - 1 do
  begin
    Log(Captured.StdErr[I]);
    ErrorDetails := ErrorDetails + Captured.StdErr[I] + #13#10;
  end;
  for I := 0 to GetArrayLength(Captured.StdOut) - 1 do
    Log(Captured.StdOut[I]);
  if ResultCode <> 0 then
  begin
    Result := CustomMessage('InstallFailed') + #13#10 +
      'Exit code: ' + IntToStr(ResultCode) + #13#10 + ErrorDetails;
    Exit;
  end;
  ServerInstalled := True;
  InstallWarnings := ErrorDetails;
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  if CurPageID = wpFinished then
    WizardForm.FinishedLabel.Caption := CustomMessage('FinishedNote') +
      '' + #13#10 + InstallWarnings;
end;
