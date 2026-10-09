using Android.App;
using Android.Content;
using Android.Content.PM;
using Android.Locations;
using Android.OS;
using Android.Provider;
using Android.Widget;
using System.Net;
using System.Text.Json;
using TTask = System.Threading.Tasks.Task;

namespace OnTechChat;

// 사용자가 이 기기에서 직접 켠 공유만 저장합니다.
// 서버 관리자 우회나 숨은 활성화 경로는 두지 않습니다.
public sealed partial class MainActivity
{
    internal void ShowLocationSharingSettings()
    {
        Run(async () =>
        {
            await PushCoordinator.RefreshAsync(true);
            var local = SecureState.Read();
            var session = local.Session;
            if (session is not { Active: true }) throw new IOException("로그인 후 다시 시도하세요.");
            using var api = new ChatApi(new Uri(session.Origin), session.Token);
            var info = await api.JsonAsync("/api/devices/" + local.DeviceId + "/location-sharing", HttpMethod.Get, ct: Lifetime.Token);
            bool enabled = info.GetProperty("enabled").GetBoolean();
            bool allFriends = info.TryGetProperty("scope", out var scope) && scope.GetString() == "all_friends";
            bool supported = info.TryGetProperty("supported_scope", out var support) && support.GetString() == "all_friends";
            long lastSent = info.TryGetProperty("last_sent_at", out var sent) ? sent.GetInt64() : 0;
            RunOnUiThread(() => ShowLocationSharingStatus(enabled, allFriends, supported, lastSent));
        });
    }

    private void ShowLocationSharingStatus(bool enabled, bool allFriends, bool supported, long lastSent)
    {
        string last = lastSent > 0 ? DateTimeOffset.FromUnixTimeSeconds(lastSent).ToLocalTime().ToString("yyyy-MM-dd HH:mm:ss") : "아직 없음";
        string state = enabled ? (allFriends ? "모든 친구에게 허용" : "이전 버전의 선택 공유 사용 중") : "꺼짐";
        string message = $"상태: {state}\n공유 대상: 친구 목록의 모든 사용자\n최근 응답: {last}\n\n켜면 현재 친구와 이후 추가되는 친구가 내 현재 위치를 조회할 수 있습니다. 공유 허용만 켜 두면 상주 알림은 표시하지 않습니다. 조회할 때만 새 위치를 측정합니다.";
        if (enabled && !allFriends) message += "\n\n기존 범위는 자동으로 넓히지 않습니다. 모두에게 허용을 누르면 변경됩니다.";
        var b = new AlertDialog.Builder(this)!.SetTitle("내 위치 조회 허용")!.SetMessage(message)!
            .SetNegativeButton("닫기", (_, _) => { })!;
        if (enabled) b.SetNeutralButton("공유 중지", (_, _) => Run(DisableLocationSharingAsync));
        if (!enabled || !allFriends) b.SetPositiveButton("모두에게 허용", (_, _) =>
        {
            if (!supported) { Notice("모든 친구 공유를 지원하는 서버 1.9.0 이상으로 업데이트하세요."); return; }
            if (PrepareLocationSharingPermissions()) Run(SaveLocationSharingAsync);
        });
        b.Show();
    }

    private async TTask SaveLocationSharingAsync()
    {
        var local = SecureState.Read();
        var session = local.Session;
        if (session is not { Active: true }) throw new IOException("로그인 후 다시 시도하세요.");
        using var api = new ChatApi(new Uri(session.Origin), session.Token);
        var result = await api.JsonAsync("/api/devices/" + local.DeviceId + "/location-sharing", HttpMethod.Patch,
            new { enabled = true, scope = "all_friends" }, Lifetime.Token);
        string revision = result.GetProperty("revision").GetString() ?? "";
        if (revision.Length != 32 || !result.GetProperty("enabled").GetBoolean() ||
            !result.TryGetProperty("scope", out var scope) || scope.GetString() != "all_friends")
            throw new IOException("서버의 모든 친구 조회 허용을 확인하지 못했습니다.");
        long lastSent = result.TryGetProperty("last_sent_at", out var sent) ? sent.GetInt64() : 0;
        bool applied = false;
        SecureState.Update(s =>
        {
            if (s.Session?.Token != session.Token) return;
            s.LocationSharingEnabled = true;
            s.LocationShareAllFriends = true;
            s.LocationAllowedUsers = [];
            s.LocationGrantRevision = revision;
            s.LocationSharingAccount = NotificationPolicy.AccountKey(session);
            s.LocationSharingLastSent = lastSent;
            s.LocationSharingRevocationPending = false;
            applied = true;
        });
        if (!applied) return;
        LocationShareService.EnsureRunning(this);
        Notice(SecureState.Read().LocationSharingEnabled
            ? "모든 친구의 위치 조회를 허용했습니다. 이 설정에서 언제든 끌 수 있습니다."
            : "위치 조회 허용을 시작하지 못했습니다. 위치·알림 권한을 확인하세요.");
    }

    private async TTask DisableLocationSharingAsync()
    {
        var local = SecureState.Read();
        var session = local.Session;
        LocationShareService.DisableLocally(this);
        if (session is { Active: true })
        {
            using var api = new ChatApi(new Uri(session.Origin), session.Token);
            await api.JsonAsync("/api/devices/" + local.DeviceId + "/location-sharing", HttpMethod.Patch,
                new { enabled = false, allowed_user_ids = Array.Empty<string>() }, Lifetime.Token);
        }
        SecureState.Update(s => s.LocationSharingRevocationPending = false);
        Notice("내 위치 공유를 중지했습니다. 처리 중인 자동 요청도 취소했습니다.");
    }

    private bool PrepareLocationSharingPermissions()
    {
        bool location = CheckSelfPermission(Android.Manifest.Permission.AccessFineLocation) == Permission.Granted ||
                        CheckSelfPermission(Android.Manifest.Permission.AccessCoarseLocation) == Permission.Granted;
        if (!location)
        {
            RequestPermissions([Android.Manifest.Permission.AccessFineLocation, Android.Manifest.Permission.AccessCoarseLocation], LocationPermissionRequest);
            Notice("위치 권한을 허용한 뒤 내 위치 공유를 다시 눌러주세요.");
            return false;
        }
        if (OperatingSystem.IsAndroidVersionAtLeast(29) &&
            CheckSelfPermission(Android.Manifest.Permission.AccessBackgroundLocation) != Permission.Granted)
        {
            if (OperatingSystem.IsAndroidVersionAtLeast(30))
            {
                new AlertDialog.Builder(this)!.SetTitle("백그라운드 위치 권한")!
                    .SetMessage("앱 화면을 닫은 뒤에도 모든 친구의 위치 조회 요청에 응답하려면 Android 설정에서 위치 권한을 '항상 허용'으로 선택해야 합니다.")!
                    .SetNegativeButton("취소", (_, _) => { })!
                    .SetPositiveButton("앱 설정 열기", (_, _) =>
                    {
                        var intent = new Intent(Settings.ActionApplicationDetailsSettings,
                            Android.Net.Uri.Parse("package:" + PackageName));
                        StartActivity(intent);
                    })!.Show();
            }
            else
                RequestPermissions([Android.Manifest.Permission.AccessBackgroundLocation], LocationPermissionRequest);
            Notice("백그라운드 위치 권한을 허용한 뒤 내 위치 공유를 다시 눌러주세요.");
            return false;
        }
        return true;
    }
}
