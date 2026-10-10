using Android.App;
using Android.Runtime;
using Firebase;

namespace OnTechChat;

[Application]
public sealed class ChatApplication : Application
{
    public ChatApplication(nint handle, JniHandleOwnership ownership) : base(handle, ownership) { }
    public override void OnCreate()
    {
        base.OnCreate();
        try { FirebaseApp.InitializeApp(this); }
        catch (Exception error) { StartupDiagnostics.Record(this, "Firebase.Initialize", error); }
        try { PushService.CreateChannel(this); }
        catch (Exception error) { StartupDiagnostics.Record(this, "Notifications.CreateChannel", error); }
        try
        {
            var folder = Path.Combine(CacheDir!.AbsolutePath, "chat-downloads");
            if (Directory.Exists(folder))
                foreach (var dir in Directory.GetDirectories(folder))
                    if (Directory.GetLastWriteTimeUtc(dir) < DateTime.UtcNow.AddDays(-1)) Directory.Delete(dir, true);
        }
        catch { /* 정리 실패로 앱의 로그인/채팅을 막지 않습니다. */ }
    }
}
