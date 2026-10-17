using Android.App;
using Android.Content;
namespace OnTechChat;

// 채팅 알림과 분리된 기존 GPS 채널을 유지해 사용자의 숨김 설정을 보존합니다.
internal static class LocationHistoryNotification
{
    internal const string Channel = "ontech_location_history";

    internal static void Prepare(Context context)
    {
        var manager = (NotificationManager)context.GetSystemService(Context.NotificationService)!;
        var channel = new NotificationChannel(Channel, "GPS 이동 기록", NotificationImportance.Low)
        {
            Description = "이 항목만 끄면 GPS 알림을 숨깁니다. 이동 기록과 채팅 알림은 유지됩니다.",
            LockscreenVisibility = NotificationVisibility.Private
        };
        channel.SetSound(null, null);
        channel.EnableVibration(false);
        channel.SetShowBadge(false);
        manager.CreateNotificationChannel(channel);
    }

    internal static Intent SettingsIntent(Context context)
    {
        var intent = new Intent(Android.Provider.Settings.ActionChannelNotificationSettings);
        intent.PutExtra(Android.Provider.Settings.ExtraAppPackage, context.PackageName);
        intent.PutExtra(Android.Provider.Settings.ExtraChannelId, Channel);
        return intent;
    }

    internal static void OpenSettings(Activity owner)
    {
        Prepare(owner);
        try
        {
            using var intent = SettingsIntent(owner);
            owner.StartActivity(intent);
        }
        catch (ActivityNotFoundException)
        {
            using var fallback = new Intent(Android.Provider.Settings.ActionAppNotificationSettings);
            fallback.PutExtra(Android.Provider.Settings.ExtraAppPackage, owner.PackageName);
            owner.StartActivity(fallback);
        }
    }

    internal static string DisplayStatus(Context context)
    {
        var manager = (NotificationManager)context.GetSystemService(Context.NotificationService)!;
        if (!manager.AreNotificationsEnabled())
            return "앱 알림 전체 꺼짐 · 채팅 알림도 꺼져 있습니다.";
        if (manager.GetNotificationChannel(Channel)?.Importance == NotificationImportance.None)
            return "GPS 알림 숨김 · 이동 기록과 채팅 알림은 유지됩니다.";
        return "GPS 알림 표시 중 · 아래에서 GPS 알림만 끌 수 있습니다.";
    }
}
