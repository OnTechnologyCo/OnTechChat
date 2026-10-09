using Android.App;
using Android.Content;
using Android.Content.PM;
using Android.Locations;
using Android.OS;
using TTask = System.Threading.Tasks.Task;

namespace OnTechChat;

[Service(Name = "com.ontechdev.chat.LocationShareService", Exported = false,
    ForegroundServiceType = ForegroundService.TypeLocation)]
public sealed partial class LocationShareService : Service
{
    public const string Channel = "ontech_location_share";
    private const int NotificationId = 4301;
    private const string ActionStart = "com.ontechdev.chat.LOCATION_SHARE_START";
    private const string ActionStop = "com.ontechdev.chat.LOCATION_SHARE_STOP";
    private const string ActionRequest = "com.ontechdev.chat.LOCATION_SHARE_REQUEST";
    private static readonly SemaphoreSlim Work = new(1, 1);
    private static LocationShareService? instance;
    private CancellationTokenSource? measuring;
    private volatile bool stopping;
    private readonly System.Collections.Concurrent.ConcurrentDictionary<string, byte> pending = new();

    public override IBinder? OnBind(Intent? intent) => null;

    public override void OnCreate()
    {
        base.OnCreate();
        instance = this;
        CreateChannel(this);
    }

    public override StartCommandResult OnStartCommand(Intent? intent, StartCommandFlags flags, int startId)
    {
        string action = intent?.Action ?? ActionStart;
        if (action == ActionStop)
        {
            DisableLocally(this);
            _ = RevokeServerGrantAsync();
            StopNow();
            return StartCommandResult.NotSticky;
        }
        if (action != ActionRequest) { StopNow(); return StartCommandResult.NotSticky; }
        stopping = false;
        string requestID = intent?.GetStringExtra("request_id") ?? "";
        string requestRevision = intent?.GetStringExtra("revision") ?? "";
        var state = SecureState.Read();
        if (!ValidLocalGrant(state) || !HasRequiredLocationPermission(this, true))
        {
            _ = ReportFailureAsync(requestID, requestRevision, "permission");
            StopNow();
            return StartCommandResult.NotSticky;
        }
        try
        {
            StartForeground(NotificationId, BuildNotification(state, "현재 위치 측정 중"));
        }
        catch
        {
            _ = ReportFailureAsync(requestID, requestRevision, "background_restricted");
            StopNow();
            return StartCommandResult.NotSticky;
        }
        if (action == ActionRequest)
        {
            string id = intent?.GetStringExtra("request_id") ?? "";
            string revision = intent?.GetStringExtra("revision") ?? "";
            QueueRequest(id, revision);
        }
        return StartCommandResult.NotSticky;
    }

    public override void OnDestroy()
    {
        measuring?.Cancel();
        measuring?.Dispose();
        measuring = null;
        if (ReferenceEquals(instance, this)) instance = null;
        base.OnDestroy();
    }

    private static bool IsRevision(string value)
    {
        if (value.Length != 32) return false;
        foreach (char c in value)
            if (!(char.IsDigit(c) || c is >= 'a' and <= 'f')) return false;
        return true;
    }

    private void QueueRequest(string requestID, string revision)
    {
        if (!UrlPolicy.ValidID(requestID) || !IsRevision(revision)) { if(pending.IsEmpty) StopNow(); return; }
        if(pending.TryAdd(requestID,0)) _ = ProcessRequestAsync(requestID, revision);
    }

    // 공유 허용은 설정만 저장합니다. 조회 요청이 있을 때만 짧게 서비스를 실행합니다.
    public static void EnsureRunning(Context context)
    {
        if(instance is { } current) { if(current.pending.IsEmpty) current.StopNow(); }
        else (context.GetSystemService(NotificationService) as NotificationManager)?.Cancel(NotificationId);
    }

    public static void Enqueue(Context context, string requestID, string revision)
    {
        if (!UrlPolicy.ValidID(requestID) || !IsRevision(revision)) return;
        var state = SecureState.Read();
        if (!ValidLocalGrant(state) || !HasRequiredLocationPermission(context, true))
        {
            _ = ReportFailureAsync(requestID, revision, "permission");
            return;
        }
        var current = instance;
        if (current is not null && !current.stopping)
        {
            current.QueueRequest(requestID, revision);
            return;
        }
        var intent = new Intent(context, typeof(LocationShareService)).SetAction(ActionRequest);
        intent.PutExtra("request_id", requestID);
        intent.PutExtra("revision", revision);
        try
        {
            if (OperatingSystem.IsAndroidVersionAtLeast(26)) context.StartForegroundService(intent);
            else context.StartService(intent);
        }
        catch { _ = ReportFailureAsync(requestID, revision, "background_restricted"); }
    }

    private static void DisableStateOnly()
    {
        SecureState.Update(s =>
        {
            s.LocationSharingEnabled = false;
            s.LocationShareAllFriends = false;
            s.LocationAllowedUsers = [];
            s.LocationGrantRevision = "";
            s.LocationSharingAccount = "";
            s.LocationSharingRevocationPending = true;
        });
    }

    public static void DisableLocally(Context context)
    {
        DisableStateOnly();
        instance?.measuring?.Cancel();
        instance?.StopNow();
        try
        {
            using var intent = new Intent(context, typeof(LocationShareService));
            context.StopService(intent);
        }
        catch { }
    }

    public static async TTask SyncPendingRevocationAsync()
    {
        var state = SecureState.Read();
        if (!state.LocationSharingRevocationPending || state.Session is not { Active: true } session) return;
        try
        {
            using var api = new ChatApi(new Uri(session.Origin), session.Token);
            await api.JsonAsync("/api/devices/" + state.DeviceId + "/location-sharing", HttpMethod.Patch,
                new { enabled = false, allowed_user_ids = Array.Empty<string>() });
            SecureState.Update(s =>
            {
                if (s.Session?.Token == session.Token) s.LocationSharingRevocationPending = false;
            });
        }
        catch { }
    }

    private static bool HasRequiredLocationPermission(Context context, bool background)
    {
        bool foreground =
            context.CheckSelfPermission(Android.Manifest.Permission.AccessFineLocation) == Permission.Granted ||
            context.CheckSelfPermission(Android.Manifest.Permission.AccessCoarseLocation) == Permission.Granted;
        if (!foreground) return false;
        if (background && OperatingSystem.IsAndroidVersionAtLeast(29) &&
            context.CheckSelfPermission(Android.Manifest.Permission.AccessBackgroundLocation) != Permission.Granted)
            return false;
        return true;
    }

    private static bool ValidLocalGrant(LocalState state)
    {
        if (!state.LocationSharingEnabled || (!state.LocationShareAllFriends && state.LocationAllowedUsers is not { Length: > 0 }) ||
            !IsRevision(state.LocationGrantRevision)) return false;
        if (state.Session is not { Active: true } session) return false;
        return state.LocationSharingAccount == NotificationPolicy.AccountKey(session);
    }

    private async TTask ProcessRequestAsync(string requestID, string revision)
    {
        await Work.WaitAsync();
        try
        {
            if(stopping) return;
            var local = SecureState.Read();
            if (!ValidLocalGrant(local) || local.LocationGrantRevision != revision ||
                local.Session is not { Active: true } session) return;
            using var api = new ChatApi(new Uri(session.Origin), session.Token);
            var q = await api.JsonAsync("/api/location-requests/" + requestID, HttpMethod.Get);
            if (q.GetProperty("target_id").GetString() != session.UserId ||
                q.GetProperty("status").GetString() != "pending") return;
            if (!q.TryGetProperty("auto_response", out var automatic) || !automatic.GetBoolean()) return;
            local = SecureState.Read();
            if (stopping || !ValidLocalGrant(local) || local.LocationGrantRevision != revision ||
                local.Session?.Token != session.Token) return;
            measuring?.Cancel();
            measuring?.Dispose();
            measuring = new CancellationTokenSource(TimeSpan.FromSeconds(30));
            UpdateNotification("현재 위치 측정 중");
            PositionFix fix = await MeasureOnceAsync(measuring.Token);

            local = SecureState.Read();
            if (!ValidLocalGrant(local) || local.LocationGrantRevision != revision ||
                local.Session?.Token != session.Token) return;
            await SubmitApprovedResponseAsync(api, requestID, local, revision, fix, measuring.Token);
            long sent = DateTimeOffset.UtcNow.ToUnixTimeSeconds();
            SecureState.Update(s =>
            {
                if (s.Session?.Token == session.Token && s.LocationGrantRevision == revision)
                    s.LocationSharingLastSent = sent;
            });
            UpdateNotification("최근 전송 " +
                DateTimeOffset.FromUnixTimeSeconds(sent).ToLocalTime().ToString("HH:mm:ss"));
        }
        catch (System.OperationCanceledException) { await ReportFailureAsync(requestID,revision,"timeout"); }
        catch (ApiException ex) when (ex.Status is System.Net.HttpStatusCode.Unauthorized or System.Net.HttpStatusCode.Forbidden)
        {
            DisableLocally(this);
            StopNow();
        }
        catch (Exception ex)
        {
            string code=ex.Message.Contains("꺼져")?"location_off":ex.Message.Contains("권한")?"permission":ex.Message.Contains("위치")?"timeout":"unavailable";
            await ReportFailureAsync(requestID,revision,code);
        }
        finally
        {
            measuring?.Dispose();
            measuring = null;
            Work.Release();
            pending.TryRemove(requestID,out _);
            if(pending.IsEmpty) StopNow();
        }
    }

    private static async TTask SubmitApprovedResponseAsync(
        ChatApi api, string requestID, LocalState local, string revision, PositionFix fix, CancellationToken ct)
    {
        var payload = new Dictionary<string, object?>
        {
            ["latitude"] = fix.Latitude,
            ["longitude"] = fix.Longitude,
            ["accuracy_m"] = fix.Accuracy,
            ["captured_at"] = fix.CapturedAt,
            ["approximate"] = fix.Approximate
        };
        payload["confirmed"] = false;
        payload["auto_response"] = true;
        payload["device_id"] = local.DeviceId;
        payload["grant_revision"] = revision;
        payload["client_id"] = "location-auto-" + Guid.NewGuid().ToString("N");
        await api.JsonAsync("/api/location-requests/" + requestID + "/respond", HttpMethod.Post, payload, ct);
    }

    private System.Threading.Tasks.Task<PositionFix> MeasureOnceAsync(CancellationToken ct) => MeasureHistoryFixAsync(this, ct);

    internal static async System.Threading.Tasks.Task<PositionFix> MeasureHistoryFixAsync(Context context, CancellationToken ct)
    {
        bool fine = context.CheckSelfPermission(Android.Manifest.Permission.AccessFineLocation) == Permission.Granted;
        if (!fine && context.CheckSelfPermission(Android.Manifest.Permission.AccessCoarseLocation) != Permission.Granted)
            throw new IOException("위치 권한이 없습니다.");
        var manager = context.GetSystemService(LocationService) as LocationManager ??
            throw new IOException("위치 서비스가 없습니다.");
        var providers = manager.GetProviders(true) ?? [];
        var selected = providers.Where(p => p is "fused" or "network" || (fine && p == "gps")).ToArray();
        if (selected.Length == 0) throw new IOException("휴대전화 위치 기능이 꺼져 있습니다.");
        var answer = new TaskCompletionSource<PositionFix>(TaskCreationOptions.RunContinuationsAsynchronously);
        using var listener = new OneFixListener(answer, !fine);
        using var cancelled = ct.Register(() => answer.TrySetCanceled(ct));
        try
        {
            int started=0;
            foreach(var provider in selected)
                try { manager.RequestLocationUpdates(provider,0,0,listener,Looper.MainLooper); started++; } catch { }
            if(started==0) throw new IOException("위치 권한 또는 위치 설정을 확인하세요.");
            return await answer.Task.WaitAsync(TimeSpan.FromSeconds(25), ct);
        }
        catch (TimeoutException) { throw new IOException("새 위치를 얻지 못했습니다."); }
        finally { manager.RemoveUpdates(listener); }
    }

    private static void AcceptFix(TaskCompletionSource<PositionFix> answer,
        Android.Locations.Location? value, bool approximate)
    {
        if (value is null) return;
        var fix = new PositionFix(value.Latitude, value.Longitude,
            value.HasAccuracy ? value.Accuracy : 0, value.Time / 1000, approximate);
        var age = SystemClock.ElapsedRealtimeNanos() - value.ElapsedRealtimeNanos;
        long now = DateTimeOffset.UtcNow.ToUnixTimeSeconds();
        if (age >= 0 && age <= 10_000_000_000L && fix.IsUsable(now)) answer.TrySetResult(fix);
        // 오래된 첫 결과는 무시하고 제한 시간 안의 새 위치를 기다립니다.
    }

    private sealed class OneFixConsumer(TaskCompletionSource<PositionFix> answer, bool approximate)
        : Java.Lang.Object, Java.Util.Functions.IConsumer
    {
        public void Accept(Java.Lang.Object? value) =>
            AcceptFix(answer, value as Android.Locations.Location, approximate);
    }

    private sealed class OneFixListener(TaskCompletionSource<PositionFix> answer, bool approximate)
        : Java.Lang.Object, ILocationListener
    {
        public void OnLocationChanged(Android.Locations.Location location) =>
            AcceptFix(answer, location, approximate);
        public void OnProviderDisabled(string provider) { }
        public void OnProviderEnabled(string provider) { }
#pragma warning disable CS0618
        public void OnStatusChanged(string? provider, Availability status, Bundle? extras) { }
#pragma warning restore CS0618
    }

    private static void CreateChannel(Context context)
    {
        if (context.GetSystemService(NotificationService) is not NotificationManager manager) return;
        var channel = new NotificationChannel(Channel, "내 위치 공유", NotificationImportance.Low)
        {
            Description = "위치 조회에 응답하는 동안만 잠시 표시됩니다.",
            LockscreenVisibility = NotificationVisibility.Private
        };
        manager.CreateNotificationChannel(channel);
    }

    private Notification BuildNotification(LocalState state, string status)
    {
        using var open = new Intent(this, typeof(MainActivity));
        open.AddFlags(ActivityFlags.ClearTop | ActivityFlags.SingleTop);
        var openPending = PendingIntent.GetActivity(this, 4301, open,
            PendingIntentFlags.UpdateCurrent | PendingIntentFlags.Immutable);
        using var stop = new Intent(this, typeof(LocationShareService)).SetAction(ActionStop);
        var stopPending = PendingIntent.GetService(this, 4302, stop,
            PendingIntentFlags.UpdateCurrent | PendingIntentFlags.Immutable);
        string sent = state.LocationSharingLastSent > 0
            ? DateTimeOffset.FromUnixTimeSeconds(state.LocationSharingLastSent).ToLocalTime().ToString("HH:mm:ss")
            : "없음";
        string audience = state.LocationShareAllFriends ? "모든 친구" : $"기존 허용 {state.LocationAllowedUsers?.Length ?? 0}명";
        string body = "허용한 친구의 요청에 응답하고 있습니다. 측정이 끝나면 자동으로 닫힙니다.";
        var builder = new Notification.Builder(this, Channel)
            .SetSmallIcon(Resource.Drawable.ic_notification)!
            .SetContentTitle("현재 위치 조회에 응답 중")!
            .SetContentText(body)!
            .SetStyle(new Notification.BigTextStyle().BigText(body))!
            .SetContentIntent(openPending)!
            .SetOngoing(true).SetOnlyAlertOnce(true)
            .SetCategory(Notification.CategoryService)!
            .SetVisibility(NotificationVisibility.Private);
        builder.AddAction(new Notification.Action.Builder(null, "공유 중지", stopPending).Build());
        return builder.Build();
    }

    private void UpdateNotification(string text)
    {
        if (stopping) return;
        var state = SecureState.Read();
        if (!ValidLocalGrant(state)) { StopNow(); return; }
        if (GetSystemService(NotificationService) is NotificationManager manager)
            manager.Notify(NotificationId, BuildNotification(state, text));
    }

    private async TTask RevokeServerGrantAsync()
    {
        var state = SecureState.Read();
        if (state.Session is not { Active: true } session) return;
        try
        {
            using var api = new ChatApi(new Uri(session.Origin), session.Token);
            await api.JsonAsync("/api/devices/" + state.DeviceId + "/location-sharing", HttpMethod.Patch,
                new { enabled = false, allowed_user_ids = Array.Empty<string>() });
            SecureState.Update(s =>
            {
                if (s.Session?.Token == session.Token) s.LocationSharingRevocationPending = false;
            });
        }
        catch { }
    }

    private void StopNow()
    {
        if (stopping) return;
        stopping = true;
        measuring?.Cancel();
        try { StopForeground(StopForegroundFlags.Remove); } catch { }
        StopSelf();
    }
}
