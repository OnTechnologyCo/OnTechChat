using Android.App;
using Android.Content;
using Android.Content.PM;
using Android.Gms.Tasks;
using Android.OS;
using Firebase.Messaging;
using System.Security.Cryptography;
using System.Text;
using TTask = System.Threading.Tasks.Task;

namespace OnTechChat;

public static class PushCoordinator
{
    private static readonly SemaphoreSlim Gate = new(1, 1);
    public static string Status { get; private set; } = "FCM 연결 준비 중";
    public static bool LoggingOut { get; private set; }
    public static event Action? Changed;
    public static void SetStatus(string text) { Status = text; Changed?.Invoke(); }
    public static async TTask RefreshAsync(bool force = false)
    {
        if (!await Gate.WaitAsync(0)) return;
        try
        {
            if (LoggingOut) return;
            var token = await FirebaseTokenAsync();
            var state = SecureState.Update(s => s.FcmToken = token);
            if (state.Session is not { Active: true } session) { SetStatus("FCM 토큰 준비됨 · 서버 로그인 필요"); return; }
            string key = Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(session.Origin + session.UserId + token)));
            if (!force && state.RegistrationKey == key && state.DeviceRegisteredAt>0 && DateTimeOffset.UtcNow.ToUnixTimeSeconds()-state.DeviceRegisteredAt is >=0 and <3600) { SetStatus("기기 등록 완료 · 알림 수신은 별도 확인"); return; }
            using var api = new ChatApi(new Uri(session.Origin), session.Token);
            using var timeout = new System.Threading.CancellationTokenSource(TimeSpan.FromSeconds(10));
            bool enabled = await api.RegisterDeviceAsync(state.DeviceId, (Build.Manufacturer + " " + Build.Model)[..Math.Min((Build.Manufacturer + " " + Build.Model).Length, 70)], token, timeout.Token);
            SecureState.Update(s => { if (s.Session?.Token == session.Token){s.RegistrationKey = key;s.DeviceRegisteredAt=DateTimeOffset.UtcNow.ToUnixTimeSeconds();} });
            try{await NotificationCenter.LoadPreferencesAsync(session,state.DeviceId);}catch{/* 미리보기는 확인되기 전 숨김 */}
            SetStatus(enabled ? "기기 등록 완료 · 서버 FCM 켜짐" : "기기 등록 완료 · 서버 FCM 꺼짐");
        }
        catch (ApiException ex) { SetStatus(ex.Status == System.Net.HttpStatusCode.Unauthorized ? "알림 기기 등록 인증 확인 대기 · 채팅 로그인은 별도 확인" : "알림 기기 등록 실패 · 서버 설정/네트워크 확인"); }
        catch { SetStatus("FCM 연결 대기 · Google Play 서비스와 인터넷을 확인하세요"); }
        finally { Gate.Release(); }
    }
    public static async System.Threading.Tasks.Task<string> FirebaseTokenAsync()
    {
        var completion = new TaskCompletionSource<string>(TaskCreationOptions.RunContinuationsAsynchronously);
        var listener = new TokenListener(completion);
        FirebaseMessaging.Instance.GetToken().AddOnCompleteListener(listener);
        return await completion.Task.WaitAsync(TimeSpan.FromSeconds(15));
    }
    private sealed class TokenListener(TaskCompletionSource<string> completion) : Java.Lang.Object, IOnCompleteListener
    {
        public void OnComplete(Android.Gms.Tasks.Task task)
        {
            string token = task.IsSuccessful ? task.Result?.ToString() ?? "" : "";
            if (token.Length >= 20) completion.TrySetResult(token);
            else completion.TrySetException(new IOException("FCM 토큰 발급 대기"));
        }
    }
    private sealed class DeleteListener(TaskCompletionSource<bool> done):Java.Lang.Object,IOnCompleteListener{public void OnComplete(Android.Gms.Tasks.Task task)=>done.TrySetResult(task.IsSuccessful);}
    public static async System.Threading.Tasks.Task<bool> LogoutAsync()
    {
        LoggingOut = true;
        LocationHistoryService.Forget(Android.App.Application.Context);
        LocationShareService.DisableLocally(Android.App.Application.Context);
        await Gate.WaitAsync();
        bool revoked = true;
        try
        {
            var state = SecureState.Read();
            if (state.Session is { } session)
            {
                try
                {
                    using var api = new ChatApi(new Uri(session.Origin), session.Token);
                    await api.LogoutAsync(state.DeviceId);
                }
                catch { revoked = false; }
            }
            SecureState.Update(s => { s.Session = null; s.RegistrationKey = ""; s.FcmToken = "";s.NotificationPreview=false;s.NotificationPreferenceAccount="";s.DeviceRegisteredAt=0;s.LocationSharingEnabled=false;s.LocationShareAllFriends=false;s.LocationAllowedUsers=[];s.LocationGrantRevision="";s.LocationSharingAccount="";s.LocationSharingRevocationPending=false; });
            try{var done=new TaskCompletionSource<bool>(TaskCreationOptions.RunContinuationsAsynchronously);FirebaseMessaging.Instance.DeleteToken().AddOnCompleteListener(new DeleteListener(done));if(!await done.Task.WaitAsync(TimeSpan.FromSeconds(12)))revoked=false;}catch{revoked=false;}
            (Android.App.Application.Context.GetSystemService(Context.NotificationService) as NotificationManager)?.CancelAll();
            SetStatus("로그아웃됨");
        }
        finally { Gate.Release(); LoggingOut = false; }
        return revoked;
    }
}

[Service(Exported = false, Name = "com.ontechdev.chat.ChatMessagingService")]
[IntentFilter(["com.google.firebase.MESSAGING_EVENT"])]
public sealed class PushService : FirebaseMessagingService
{
    public const string Channel = "ontech_messages";
    public override void OnNewToken(string token)
    {
        try { SecureState.Update(s => { s.FcmToken = token; s.RegistrationKey = ""; }); }
        catch (StateStorageException error) { StartupDiagnostics.Record(this, "FCM.TokenSave", error); PushCoordinator.SetStatus("알림 토큰 저장 대기 · 로그인 정보 유지"); }
        // 다음 포그라운드 복귀에서도 재등록하므로 프로세스 종료/네트워크 단절에 대비합니다.
        _ = PushCoordinator.RefreshAsync(true);
    }
    public static void CreateChannel(Context context)
    {
        if (context.GetSystemService(Context.NotificationService) is not NotificationManager manager) return;
        manager.CreateNotificationChannel(new NotificationChannel(Channel, "채팅 · 인증번호 알림", NotificationImportance.Default)
        { Description = "새 메시지 도착 알림. 실제 인증번호는 로그인한 앱 안에서 확인합니다.", LockscreenVisibility = NotificationVisibility.Private });
    }
    public override void OnMessageReceived(RemoteMessage message)
    {
        if (SecureState.Read().Session is not { Active: true } session) return;
        var data=message.Data;
        string recipient=data is not null&&data.TryGetValue("recipient_id",out var r)?r:"";
        bool appDelivery=data is not null&&data.TryGetValue("delivery",out var delivery)&&delivery=="app";
        if((appDelivery&&recipient!=session.UserId)||(recipient.Length>0&&recipient!=session.UserId))return;
        if(appDelivery&&data is not null&&data.TryGetValue("location_auto",out var automatic)&&automatic=="1")
        {
            var state=SecureState.Read();
            string requestID=data.TryGetValue("location_request_id",out var rq)?rq:"";
            string revision=data.TryGetValue("location_grant_revision",out var gr)?gr:"";
            if(state.LocationSharingEnabled&&state.LocationGrantRevision==revision&&
               state.LocationSharingAccount==NotificationPolicy.AccountKey(session))
                LocationShareService.Enqueue(this,requestID,revision);
            _=NotificationCenter.ReconcileAsync(this);
            return;
        }
        var room = message.Data is { } rd && rd.TryGetValue("room_id", out var rv) ? rv : "";
        var id = message.Data is { } md && md.TryGetValue("message_id", out var mv) ? mv : "";
        var kind = message.Data is { } kd && kd.TryGetValue("kind", out var kv) ? kv : "";
        var n=message.GetNotification();
        string? previewTitle=appDelivery&&data!.TryGetValue("title",out var pt)?pt:n?.Title;
        string? previewBody=appDelivery&&data!.TryGetValue("body",out var pb)?pb:n?.Body;
        string popupRequestID=data is not null&&data.TryGetValue("location_request_id",out var lookup)?lookup:"";
        Show(this, room, id, kind, false,previewTitle,previewBody,popupRequestID); _ = NotificationCenter.ReconcileAsync(this);
    }
    public static void Show(Context context, string room, string id, string kind, bool localTest,string? previewTitle=null,string? previewBody=null,string requestID="")
    {
        CreateChannel(context);
        if (OperatingSystem.IsAndroidVersionAtLeast(33) && context.CheckSelfPermission(Android.Manifest.Permission.PostNotifications) != Permission.Granted) return;
        if (context.GetSystemService(Context.NotificationService) is not NotificationManager manager || !manager.AreNotificationsEnabled()) return;
        using var open = new Intent(context, typeof(MainActivity));
        open.AddFlags(ActivityFlags.ClearTop | ActivityFlags.SingleTop);
        if (UrlPolicy.ValidID(room)) open.PutExtra("room_id", room);
        string tag = UrlPolicy.ValidID(requestID) ? "location-request-"+requestID : localTest ? "local-test" : "message-" + (long.TryParse(id, out _) ? id : Guid.NewGuid().ToString("N"));
        open.SetData(Android.Net.Uri.Parse("ontechchat://notification/" + Uri.EscapeDataString(tag)));
        var pending = PendingIntent.GetActivity(context, 0, open, PendingIntentFlags.UpdateCurrent | PendingIntentFlags.Immutable);
        using var hidden = new Notification.Builder(context, Channel).SetSmallIcon(Resource.Drawable.ic_notification)
            .SetContentTitle("온테크 채팅")!.SetContentText("새 알림이 있습니다.")!.Build();
        var state=SecureState.Read();bool preview=state.Session is{Active:true} session&&state.NotificationPreferenceAccount==NotificationPolicy.AccountKey(session)&&state.NotificationPreview;
        var text=NotificationPolicy.Display(kind,preview,previewTitle,previewBody,localTest);
        using var notification = new Notification.Builder(context, Channel).SetSmallIcon(Resource.Drawable.ic_notification)
            .SetContentTitle(text.Title)!
            .SetContentText(text.Body)!.SetStyle(new Notification.BigTextStyle().BigText(text.Body))!
            .SetContentIntent(pending)!.SetAutoCancel(true)!.SetOnlyAlertOnce(true)!
            .SetVisibility(NotificationVisibility.Private).SetPublicVersion(hidden)!.Build();
        manager.Notify(tag, 0, notification);
    }
}
