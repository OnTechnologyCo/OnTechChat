using Android.App;
using Android.Content;
using Android.Content.PM;
using Android.Graphics;
using Android.OS;
using Android.Text;
using Android.Views;
using Android.Webkit;
using Android.Widget;
using System.Net;
using TTask = System.Threading.Tasks.Task;

namespace OnTechChat;

[Activity(Name = "com.ontechdev.chat.MainActivity", Label = "@string/app_name", MainLauncher = true, Exported = true,
    LaunchMode = LaunchMode.SingleTop, ConfigurationChanges = ConfigChanges.Orientation | ConfigChanges.ScreenSize | ConfigChanges.KeyboardHidden)]
public sealed partial class MainActivity : Activity
{
    internal WebView? Web;
    internal Uri? ServerOrigin;
    internal readonly CancellationTokenSource Lifetime = new();
    private LinearLayout root = null!;
    private TextView status = null!, title = null!;
    private Handler ui = null!;
    private bool resumed, destroyed, checking, startupFailed;
    private string pendingRoom = "";
    private string lastDiagnostic = "";
    private DateTime pausedAt;
    private const int NotificationRequest = 31;
    private int Dp(int n) => (int)(n * Resources!.DisplayMetrics!.Density + 0.5f);
    protected override void OnCreate(Bundle? savedInstanceState)
    {
        base.OnCreate(savedInstanceState);
        ui = new Handler(Looper.MainLooper!);
        try
        {

            AcceptNotification(Intent);
            Shell();
            RegisterBackNavigation();
            var state = SecureState.Read();
            if (state.Session is { Active: true })
                Run(async () => { await ResumeSessionAsync(); });
            else ShowLogin(state.Session is not null ? "세션이 만료되었습니다. 다시 로그인하세요." : "");
            if (state.LocationSharingEnabled) LocationShareService.EnsureRunning(this);
            _ = LocationShareService.SyncPendingRevocationAsync();
            _ = PushCoordinator.RefreshAsync();
        }
        catch (Exception error)
        {
            startupFailed = true; StartupDiagnostics.Record(this, "MainActivity.OnCreate", error);
            ShowStartupFailure();
        }
    }
    private void ShowStartupFailure()
    {
        root = new LinearLayout(this) { Orientation = Orientation.Vertical };
        root.SetPadding(Dp(20), Dp(24), Dp(20), Dp(20));
        title = new TextView(this) { Text = "앱 시작 오류", TextSize = 22 }; root.AddView(title);
        status = new TextView(this) { Text = "로그인 정보는 유지했습니다. 아래 진단을 복사해 전달해 주세요.", TextSize = 15 }; root.AddView(status);
        var text = StartupDiagnostics.Read(this);
        var details = new TextView(this) { Text = text, TextSize = 12 };
        details.SetTextIsSelectable(true); root.AddView(details);
        var copy = new Button(this) { Text = "앱 실행 진단 복사" };
        copy.Click += (_, _) => {
            var clipboard = (Android.Content.ClipboardManager)GetSystemService(ClipboardService)!;
            clipboard.PrimaryClip = ClipData.NewPlainText("OnTech 앱 실행 진단", text);
        };
        root.AddView(copy);
        SetContentView(root);
    }
    private void ShowStartupDiagnostics()
    {
        var text = StartupDiagnostics.Read(this);
        new AlertDialog.Builder(this)!.SetTitle("앱 실행 진단")!.SetMessage(text)!
            .SetPositiveButton("닫기", (_, _) => { })!
            .SetNeutralButton("복사", (_, _) => {
                var clipboard = (Android.Content.ClipboardManager)GetSystemService(ClipboardService)!;
                clipboard.PrimaryClip = ClipData.NewPlainText("OnTech 앱 실행 진단", text);
            })!.Show();
    }
    private void Shell()
    {
        root = new LinearLayout(this) { Orientation = Orientation.Vertical };
        root.SetBackgroundColor(Color.Rgb(244, 246, 251));
        root.SetOnApplyWindowInsetsListener(new Insets());
        var bar = new LinearLayout(this) { Orientation = Orientation.Horizontal };
        bar.SetGravity(GravityFlags.CenterVertical);
        bar.SetPadding(Dp(16), Dp(4), Dp(8), Dp(4));
        title = new TextView(this) { Text = "ONTECH  /  CHAT", TextSize = 17, Typeface = Typeface.DefaultBold };
        title.SetTextColor(Color.Rgb(23, 52, 94));
        bar.AddView(title, new LinearLayout.LayoutParams(0, Dp(48), 1)); title.Gravity = GravityFlags.CenterVertical;
        var menu = new Button(this) { Text = "메뉴", TextSize = 13 };
        menu.Click += (_, _) => ShowMenu(); bar.AddView(menu);
        root.AddView(bar);
        status = new TextView(this) { TextSize = 12 };
        status.SetPadding(Dp(16), Dp(5), Dp(16), Dp(5)); status.SetTextColor(Color.Rgb(80, 96, 119));
        root.AddView(status);
        SetContentView(root); root.RequestApplyInsets();
    }
    private void ClearBody()
    {
        UpdatePageStarted();
        FinishPicker(null);
        if (Web is not null)
        {
            Web.StopLoading(); (Web.Parent as ViewGroup)?.RemoveView(Web);
            Web.SetWebViewClient(new WebViewClient()); Web.Destroy(); Web.Dispose(); Web = null;
        }
        while (root.ChildCount > 2) root.RemoveViewAt(2);
    }
    private void ShowLogin(string message = "")
    {
        if (destroyed) return;
        ClearBody(); ServerOrigin = null; title.Text = "ONTECH  /  CHAT";
        status.Text = message.Length == 0 ? "서버 주소와 채팅 계정으로 로그인하세요." : message;
        var scroll = new ScrollView(this) { FillViewport = true };
        var form = new LinearLayout(this) { Orientation = Orientation.Vertical };
        form.SetPadding(Dp(24), Dp(25), Dp(24), Dp(24)); scroll.AddView(form);
        form.AddView(new TextView(this) { Text = "어디서나 채팅과 인증함", TextSize = 24, Typeface = Typeface.DefaultBold });
        var hint = new TextView(this) { Text = "온테크 서버 주소가 기본으로 입력됩니다.\n주소 연결을 확인한 뒤 채팅 계정으로 로그인하세요.\n네이버 메일 비밀번호가 아닌, 관리자가 발급한 채팅 비밀번호를 입력합니다.", TextSize = 14 };
        hint.SetPadding(0, Dp(12), 0, Dp(25)); form.AddView(hint);
        var address = Input(form, "서버 HTTPS 주소", "https://chat.example.com", InputTypes.ClassText | InputTypes.TextVariationUri);
        var savedAddress = SecureState.Read().Origin.TrimEnd('/');
        address.Text = string.IsNullOrWhiteSpace(savedAddress) ? "https://175.119.187.91:49333" : savedAddress;
        var useDefault = new Button(this) { Text = "기본 서버 주소 사용" };
        useDefault.Click += (_, _) => { address.Text = "https://175.119.187.91:49333"; status.Text = "기본 주소를 입력했습니다. 주소 연결만 확인을 누르세요."; };
        form.AddView(useDefault);
        var username = Input(form, "채팅 아이디", "한글 아이디도 사용 가능", InputTypes.ClassText | InputTypes.TextVariationVisiblePassword);
        var password = Input(form, "채팅 비밀번호", "비밀번호", InputTypes.ClassText | InputTypes.TextVariationPassword);
        var diagnostic = new TextView(this) { TextSize = 13, Text = "연결 결과와 실패 원인이 여기에 표시됩니다." };
        diagnostic.SetTextIsSelectable(true); diagnostic.SetPadding(0,Dp(12),0,Dp(12));
        void DisplayReport(ConnectionReport report) { lastDiagnostic=report.Text; diagnostic.Text=lastDiagnostic; status.Text=report.Success?"주소 연결 확인 완료":"접속 실패 · 아래 진단을 확인하세요."; }
        var connect = new Button(this) { Text = "로그인하고 채팅 열기" };
        connect.Click += (_, _) => Run(async () =>
        {
            Uri? origin=null;connect.Enabled=false;status.Text="HTTPS 서버 확인 및 로그인 중…";
            try
            {
                origin=UrlPolicy.Origin(address.Text ?? "");address.Text=origin.GetLeftPart(UriPartial.Authority);
                if (string.IsNullOrWhiteSpace(username.Text) || string.IsNullOrEmpty(password.Text)) throw new ArgumentException("채팅 서버 아이디와 비밀번호를 입력하세요.");
                using var api = new ChatApi(origin);
                var session=await api.LoginAsync(username.Text!.Trim(),password.Text!,Lifetime.Token);
                password.Text="";
                SecureState.Update(s=>{s.Origin=origin.AbsoluteUri;s.Session=session;s.RegistrationKey="";});
                await OpenSessionAsync(session);AskNotificationPermission(false);_ = PushCoordinator.RefreshAsync(true);
            }
            catch(Exception e){if(!destroyed)DisplayReport(ConnectionDiagnostics.Failure(e,origin));}
            finally{if(!destroyed)connect.Enabled=true;}
        });
        form.AddView(connect);
        var check = new Button(this) { Text = "주소 연결만 확인" };
        check.Click += (_, _) => Run(async () =>
        {
            check.Enabled=false;connect.Enabled=false;status.Text="주소·HTTPS·채팅 서버 응답을 확인 중…";
            diagnostic.Text="비밀번호를 전송하지 않고 /healthz를 확인합니다.";
            try
            {
                var report=await ConnectionDiagnostics.CheckAsync(address.Text??"",Lifetime.Token);
                if(destroyed)return;
                if(report.Address.Length>0){address.Text=report.Address;if(report.Success)SecureState.Update(s=>s.Origin=report.Address+"/");}
                DisplayReport(report);
            }
            finally{if(!destroyed){check.Enabled=true;connect.Enabled=true;}}
        });
        form.AddView(check);form.AddView(diagnostic);
        var copy=new Button(this){Text="진단 결과 복사"};
        copy.Click+=(_,_)=>{if(lastDiagnostic.Length==0){Notice("주소 연결만 확인을 먼저 누르세요.");return;}var clipboard=(Android.Content.ClipboardManager)GetSystemService(ClipboardService)!;clipboard.PrimaryClip=ClipData.NewPlainText("OnTech 연결 진단",lastDiagnostic);Notice("주소와 진단코드만 복사했습니다. 비밀번호·토큰은 포함하지 않습니다.");};form.AddView(copy);
        var browser=new Button(this){Text="입력 주소를 브라우저로 열기"};
        browser.Click+=(_,_)=>Run(()=>{var origin=UrlPolicy.Origin(address.Text??"");StartActivity(new Intent(Intent.ActionView,Android.Net.Uri.Parse(origin.AbsoluteUri)));return TTask.CompletedTask;});form.AddView(browser);
        var foot = new TextView(this) { Text = "앱 1.8.7 · Firebase ontech-chat\nHTTP·인증서 오류 우회는 지원하지 않습니다.\n메일 비밀번호와 Firebase 서버 비공개 키는 앱에 저장하지 않습니다.", TextSize = 12 };
        foot.SetPadding(0, Dp(24), 0, 0); form.AddView(foot);
        root.AddView(scroll, new LinearLayout.LayoutParams(-1, 0, 1));
    }
    private EditText Input(LinearLayout parent, string label, string placeholder, InputTypes type)
    {
        parent.AddView(new TextView(this) { Text = label, TextSize = 13 });
        var input = new EditText(this) { Hint = placeholder, TextSize = 15, InputType = type };
        input.SetSingleLine(true); input.SetPadding(Dp(8), Dp(8), Dp(8), Dp(8));
        parent.AddView(input, new LinearLayout.LayoutParams(-1, Dp(56)) { BottomMargin = Dp(16) }); return input;
    }
    private bool restoringSession, connectionWaiting;
    internal bool ReconnectWebPending;
    private async TTask ResumeSessionAsync()
    {
        if (restoringSession || destroyed || startupFailed) return;
        var session = SecureState.Read().Session;
        if (session is not { Active: true }) { ShowLogin(); return; }
        restoringSession = true;
        connectionWaiting = true; status.Text = "저장된 로그인으로 연결 중…";
        try
        {
            using var api = new ChatApi(new Uri(session.Origin), session.Token);
            await api.JsonAsync("/api/me", HttpMethod.Get, ct: Lifetime.Token);
            if (SecureState.Read().Session?.Token != session.Token || destroyed) return;
            await OpenSessionAsync(session); _ = PushCoordinator.RefreshAsync();
        }
        catch (ApiException e) when (e.SessionInvalid)
        {
            if (SecureState.Read().Session?.Token == session.Token) {
                await LogoutAsync(false); status.Text = "로그인 권한이 해제되었습니다. 다시 로그인하세요.";
            }
        }
        catch (Exception ex)
        {
            if (!destroyed && SecureState.Read().Session?.Token == session.Token) ShowReconnect(session, ex);
        }
        finally { restoringSession = false; }
    }
    private void ShowReconnect(LoginSession session, Exception error)
    {
        ClearBody(); ServerOrigin = UrlPolicy.Origin(session.Origin);
        title.Text = "온테크 채팅 · " + session.Username;
        ShowConnectionFailure(error);
        lastDiagnostic = ConnectionDiagnostics.Failure(error, ServerOrigin).Text;
        var panel = new LinearLayout(this) { Orientation = Orientation.Vertical };
        panel.SetPadding(Dp(24), Dp(24), Dp(24), Dp(24));
        panel.AddView(new TextView(this) { Text = "서버가 다시 시작되면 자동으로 연결합니다.\n저장된 로그인은 유지됩니다.\n\n" + lastDiagnostic, TextSize = 15 });
        var retry = new Button(this) { Text = "지금 다시 연결" };
        retry.Click += (_, _) => Run(ResumeSessionAsync); panel.AddView(retry);
        root.AddView(panel, new LinearLayout.LayoutParams(-1, 0, 1));
    }
    private async TTask OpenSessionAsync(LoginSession session, bool showWeb = true)
    {
        ServerOrigin = UrlPolicy.Origin(session.Origin);
        var cookies = CookieManager.Instance!;
        cookies.SetAcceptCookie(true);
        string url = ServerOrigin.AbsoluteUri;
        if (!WebSessionCookie.Matches(cookies.GetCookie(url), session.Token))
        {
            var ready = new TaskCompletionSource<bool>(TaskCreationOptions.RunContinuationsAsynchronously);
            const long age = 400L * 24 * 60 * 60;
            string cookie = $"ontech_session={session.Token}; Path=/; Secure; HttpOnly; SameSite=Strict; Max-Age={age}";
            // Java 콜백의 managed 객체 타입 대신 실제 저장된 쿠키를 확인합니다.
            var callback = new Callback(_ => ready.TrySetResult(true));
            cookies.SetCookie(url, cookie, callback);
            try
            {
                await SessionRecovery.ConfirmCookieAsync(ready.Task, Lifetime.Token,
                    stored: () => WebSessionCookie.Matches(cookies.GetCookie(url), session.Token));
            }
            finally { GC.KeepAlive(callback); }
        }
        if (destroyed || startupFailed || SecureState.Read().Session?.Token != session.Token) return;
        cookies.Flush();
        if (showWeb) { ReconnectWebPending = false; ShowWeb(session); }
        connectionWaiting = false;
        status.Text = "서버 연결됨 · 로그인 유지";
    }

    private void ShowWeb(LoginSession session)
    {
        if (destroyed) return;
        ClearBody(); ServerOrigin = UrlPolicy.Origin(session.Origin); title.Text = "온테크 채팅 · " + session.Username;
        status.Text = "서버 연결됨 · 로그인 유지";
        Web = new WebView(this);
        var appVersion = PackageManager!.GetPackageInfo(PackageName!, (PackageInfoFlags)0)!.VersionName ?? "0.0.0";
        Web.Settings.UserAgentString += " OnTechChatAndroid/" + appVersion;
        Web.Settings.JavaScriptEnabled = true; Web.Settings.DomStorageEnabled = true;
        Web.Settings.AllowFileAccess = false; Web.Settings.AllowContentAccess = true;
        Web.Settings.MixedContentMode = MixedContentHandling.NeverAllow;
        Web.Settings.CacheMode = CacheModes.NoCache; Web.Settings.SetGeolocationEnabled(false);
        Web.Settings.MediaPlaybackRequiresUserGesture = true; Web.Settings.JavaScriptCanOpenWindowsAutomatically = false;
        Web.Settings.SetSupportMultipleWindows(false);
        CookieManager.Instance!.SetAcceptThirdPartyCookies(Web, false);
        Web.SetWebViewClient(new ChatWebClient(this)); Web.SetWebChromeClient(new ChatChrome(this));
        Web.SetDownloadListener(new ChatDownload(this));
        if (resumed) { Web.OnResume();Web.ResumeTimers(); }
        root.AddView(Web, new LinearLayout.LayoutParams(-1, 0, 1));
        string suffix = UrlPolicy.ValidID(pendingRoom) ? "?room=" + Uri.EscapeDataString(pendingRoom) : "";
        pendingRoom = ""; Web.LoadUrl(new Uri(ServerOrigin, suffix.Length == 0 ? "/" : "/" + suffix).AbsoluteUri);
    }
    internal async TTask LogoutAsync(bool reportFailure)
    {
        measuringLocation?.Cancel(); pendingInstall=null; waitingInstallPermission=false; bool revoked = await PushCoordinator.LogoutAsync();
        CookieManager.Instance!.RemoveAllCookies(null); CookieManager.Instance.Flush(); WebStorage.Instance!.DeleteAllData();
        ShowLogin(reportFailure && !revoked ? "앱에서 로그아웃했습니다. 서버 연결 실패로 기기 해제는 확인하지 못했습니다. 서버 기기 관리에서도 확인하세요." : "로그아웃했습니다.");
    }
    private void ShowMenu()
    {
        bool loggedIn = SecureState.Read().Session is { Active: true };
        var items = loggedIn ? new[] { "새로고침", "서버 설정 화면", "알림 상태·권한", "알림 미리보기·소리", "앱 업데이트", "FCM 기기 등록 다시 시도", "연결 진단", "앱 실행 진단", "로그아웃·서버 변경", "GPS 이동 기록", "GPS 알림 숨기기" }
                             : new[] { "저장된 로그인으로 다시 연결", "알림 상태·권한", "연결 진단", "앱 실행 진단", "로그아웃·서버 변경" };
        new AlertDialog.Builder(this)!.SetTitle("온테크 채팅")!.SetItems(items, (_, e) =>
        {
            switch (items[e.Which])
            {
                case "새로고침": if (Web is null) Run(ResumeSessionAsync); else Web.Reload(); break;
                case "내 위치 공유": ShowLocationHistorySettings(); break;
                case "GPS 이동 기록": ShowLocationHistorySettings(); break;
                case "GPS 알림 숨기기": OpenHistoryNotificationSettings(); break;
                case "서버 설정 화면": if (ServerOrigin is not null) Web?.LoadUrl(new Uri(ServerOrigin, "/?settings=1").AbsoluteUri); break;
                case "연결 진단": new AlertDialog.Builder(this)!.SetTitle("서버 연결 진단")!.SetMessage(lastDiagnostic.Length == 0 ? "저장된 연결 오류가 없습니다." : lastDiagnostic)!.SetPositiveButton("닫기", (_, _) => { })!.Show(); break;
                case "앱 실행 진단": ShowStartupDiagnostics(); break;
                case "알림 상태·권한": ShowPushInfo(); break;
                case "알림 미리보기·소리": ShowNotificationOptions(); break;
                case "앱 업데이트": BeginUpdateCheck(); break;
                case "FCM 기기 등록 다시 시도": Run(async () => { await PushCoordinator.RefreshAsync(true); ShowPushInfo(); }); break;
                case "저장된 로그인으로 다시 연결": Run(ResumeSessionAsync); break;
                default: new AlertDialog.Builder(this)!.SetMessage("이 휴대전화의 서버 로그인과 푸시 기기 등록을 해제할까요?")!.SetNegativeButton("취소", (_, _) => { })!.SetPositiveButton("로그아웃", (_, _) => Run(() => LogoutAsync(true)))!.Show(); break;
            }
        })!.Show();
    }
    internal void RequestSessionRecovery(WebView? source)
    {
        RunOnUiThread(() =>
        {
            if (destroyed || source != Web || SecureState.Read().Session is not { Active: true }) return;
            ReconnectWebPending = true; connectionWaiting = true;
            status.Text = "저장된 로그인으로 웹 화면을 복구하고 있습니다…";
            if (!resumed || checking || restoringSession) return;
            ui.RemoveCallbacks(CheckSession); ui.Post(CheckSession);
        });
    }
    private void ShowConnectionFailure(Exception error)
    {
        if (destroyed) return;
        connectionWaiting = true;
        var report = ConnectionDiagnostics.Failure(error, ServerOrigin);
        lastDiagnostic = report.Text;
        Notice("로그인 유지 중 · " + report.Code + " · 자동 재연결 대기 (메뉴 → 연결 진단)");
    }
    private void ShowPushInfo()
    {
        bool allowed = GetSystemService(NotificationService) is NotificationManager n && n.AreNotificationsEnabled();
        var s = SecureState.Read();
        new AlertDialog.Builder(this)!.SetTitle("알림 상태")!.SetMessage($"Firebase: ontech-chat\n알림 권한: {(allowed ? "허용" : "꺼짐")}\nFCM 토큰: {(s.FcmToken.Length > 0 ? "발급됨" : "아직 없음")}\n{PushCoordinator.Status}\n\n기기 등록 성공과 실제 푸시 수신 성공은 다릅니다.")!
            .SetPositiveButton("알림 권한 설정", (_, _) => AskNotificationPermission(true))!
            .SetNeutralButton("로컬 알림 테스트", (_, _) => { if (!allowed) AskNotificationPermission(true); else PushService.Show(this, "", "", "", true); })!
            .SetNegativeButton("닫기", (_, _) => { })!.Show();
    }
    private void AskNotificationPermission(bool explicitRequest)
    {
        if (OperatingSystem.IsAndroidVersionAtLeast(33) && CheckSelfPermission(Android.Manifest.Permission.PostNotifications) != Permission.Granted)
        {
            if (!SecureState.Read().NotificationPrompted || ShouldShowRequestPermissionRationale(Android.Manifest.Permission.PostNotifications))
            { SecureState.Update(s => s.NotificationPrompted = true); RequestPermissions([Android.Manifest.Permission.PostNotifications], NotificationRequest); return; }
            if (!explicitRequest) return;
        }
        else if (!explicitRequest) return;
        using var intent = new Intent(Android.Provider.Settings.ActionAppNotificationSettings);
        intent.PutExtra(Android.Provider.Settings.ExtraAppPackage, PackageName); StartActivity(intent);
    }
    protected override void OnNewIntent(Intent? intent) { base.OnNewIntent(intent); Intent = intent; AcceptNotification(intent); if (Web is not null && ServerOrigin is not null && UrlPolicy.ValidID(pendingRoom)) { Web.LoadUrl(new Uri(ServerOrigin, "/?room=" + Uri.EscapeDataString(pendingRoom)).AbsoluteUri); pendingRoom = ""; } }
    private void AcceptNotification(Intent? intent) { string? room = intent?.GetStringExtra("room_id"); if (UrlPolicy.ValidID(room)) pendingRoom = room!; intent?.RemoveExtra("room_id"); }
    protected override void OnResume()
    {
        base.OnResume(); resumed = true; if (startupFailed) return; _ = ResumeHistoryAsync(); Web?.OnResume(); Web?.ResumeTimers(); ResumeUpdateInstall(); QueueNotificationCheck(); QueueUpdateCheck();
        if (SecureState.Read().LocationSharingEnabled) LocationShareService.EnsureRunning(this);
        _ = LocationShareService.SyncPendingRevocationAsync();
        if (Web is not null && DateTime.UtcNow - pausedAt > TimeSpan.FromSeconds(60)) Web.Reload();
        ui?.RemoveCallbacks(CheckSession);ui?.PostDelayed(CheckSession, 1000); _ = PushCoordinator.RefreshAsync();
    }
    protected override void OnPause() { measuringLocation?.Cancel(); ui?.RemoveCallbacks(NotificationTick); ui?.RemoveCallbacks(QueueAutomaticUpdateCheck); ui?.RemoveCallbacks(CheckSession); resumed = false; pausedAt = DateTime.UtcNow; Web?.OnPause(); Web?.PauseTimers(); base.OnPause(); }
    private async void CheckSession()
    {
        if (destroyed || startupFailed || !resumed || checking) return;
        checking = true;
        LoginSession? session = null;
        try
        {
            session = SecureState.Read().Session;
            if (!restoringSession && session is { Active: true }) {
                if (Web is null) await ResumeSessionAsync();
                else {
                    using var api = new ChatApi(new Uri(session.Origin), session.Token);
                    await api.JsonAsync("/api/me", HttpMethod.Get, ct: Lifetime.Token);
                    if (!destroyed && SecureState.Read().Session?.Token == session.Token)
                        await OpenSessionAsync(session, ReconnectWebPending);
                }
            }
        }
        catch (ApiException ex) when (ex.SessionInvalid) {
            if (session is not null && SecureState.Read().Session?.Token == session.Token) await LogoutAsync(false);
        }
        catch (Exception ex) { ShowConnectionFailure(ex); }
        finally { checking = false; if (!destroyed && resumed) ui.PostDelayed(CheckSession, Web is null || ReconnectWebPending || connectionWaiting ? 5000 : 30000); }
    }
    internal void Notice(string text) { if (!destroyed) RunOnUiThread(() => status.Text = text); }
    internal async void Run(Func<TTask> operation)
    {
        try { await operation(); }
        catch (System.OperationCanceledException) { Notice("연결 시간이 초과되었거나 작업이 취소되었습니다."); }
        catch (HttpRequestException ex) { lastDiagnostic=ConnectionDiagnostics.Failure(ex,ServerOrigin).Text;Notice(lastDiagnostic); }
        catch (Exception ex) { Notice(ex is ArgumentException or ApiException or IOException ? ex.Message : "작업을 완료하지 못했습니다. 다시 시도하세요."); }
    }
#pragma warning disable CS0672, CS0618
    public override void OnBackPressed() => NavigateBack();
#pragma warning restore CS0672, CS0618
    protected override void OnDestroy()
    {
        UnregisterBackNavigation();
        measuringLocation?.Cancel(); pendingInstall=null; destroyed = true; resumed = false; Lifetime.Cancel(); FinishPicker(null);
        ui.RemoveCallbacksAndMessages(null); ClearBody(); Lifetime.Dispose(); base.OnDestroy();
    }
    private sealed class Insets : Java.Lang.Object, View.IOnApplyWindowInsetsListener
    {
        public WindowInsets OnApplyWindowInsets(View? view, WindowInsets? inset)
        {
            if (inset is null) return null!;
            if (OperatingSystem.IsAndroidVersionAtLeast(30))
            { var b = inset.GetInsets(WindowInsets.Type.SystemBars() | WindowInsets.Type.DisplayCutout() | WindowInsets.Type.Ime()); view?.SetPadding(b.Left, b.Top, b.Right, b.Bottom); }
            return inset;
        }
    }
    internal sealed class Callback(Action<Java.Lang.Object?> done) : Java.Lang.Object, IValueCallback
    { public void OnReceiveValue(Java.Lang.Object? value) => done(value); }
}

