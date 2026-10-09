using Android.App;
using Android.Content;
using System.Net;
using TTask = System.Threading.Tasks.Task;
namespace OnTechChat;
internal static class NotificationCenter
{
    private static readonly SemaphoreSlim Gate = new(1,1);
    private static long retryAfter;
    public static string Status {get;private set;}="읽은 대화의 알림은 서버 확인 후 자동 정리합니다.";
    public static async System.Threading.Tasks.Task<int> ReconcileAsync(Context context,bool force=false,CancellationToken ct=default)
    {
        if(!force&&DateTimeOffset.UtcNow.ToUnixTimeSeconds()<retryAfter)return 0;
        if(!await Gate.WaitAsync(0,ct))return 0;
        try
        {
            var session=SecureState.Read().Session;
            if(session is not {Active:true}||context.GetSystemService(Context.NotificationService) is not NotificationManager manager)return 0;
            var active=manager.GetActiveNotifications()??[];
            var ids=active.Select(n=>NotificationPolicy.MessageID(n.Tag,out long id)?id:0).Where(id=>id>0).Distinct().Take(64).ToArray();
            if(ids.Length==0){Status="남아 있는 채팅 알림이 없습니다.";return 0;}
            using var api=new ChatApi(new Uri(session.Origin),session.Token);
            var reply=await api.JsonAsync("/api/notifications/state",HttpMethod.Post,new{message_ids=ids},ct);
            var remove=NotificationPolicy.ConfirmedDismissals(reply,ids);
            var current=SecureState.Read().Session;
            if(current is null||current.Token!=session.Token||current.Origin!=session.Origin||!current.Active)return 0;
            int count=0;
            foreach(var n in manager.GetActiveNotifications()??[])
                if(NotificationPolicy.MessageID(n.Tag,out long id)&&remove.Contains(id)){manager.Cancel(n.Tag,n.Id);count++;}
            retryAfter=0;Status=count>0?$"읽음·삭제·만료 알림 {count}개 정리 완료":"읽지 않은 대화 알림은 유지합니다.";return count;
        }
        catch(ApiException ex){retryAfter=DateTimeOffset.UtcNow.ToUnixTimeSeconds()+30;Status=ex.Status==HttpStatusCode.NotFound?"서버 1.4.1 이상이 필요합니다.":"읽음 확인 대기 중입니다.";return 0;}
        catch(Exception){retryAfter=DateTimeOffset.UtcNow.ToUnixTimeSeconds()+10;Status="서버 연결 후 다시 확인합니다.";return 0;}
        finally{Gate.Release();}
    }
    public static async System.Threading.Tasks.Task<bool> LoadPreferencesAsync(LoginSession session,string device)
    {
        using var api=new ChatApi(new Uri(session.Origin),session.Token);
        var response=await api.JsonAsync("/api/devices/"+Uri.EscapeDataString(device)+"/notifications",HttpMethod.Get);
        bool enabled=response.GetProperty("preview_enabled").GetBoolean();Remember(session,enabled);return enabled;
    }
    public static async TTask SavePreferencesAsync(LoginSession session,string device,bool enabled)
    {
        using var api=new ChatApi(new Uri(session.Origin),session.Token);
        var response=await api.JsonAsync("/api/devices/"+Uri.EscapeDataString(device)+"/notifications",HttpMethod.Patch,new{preview_enabled=enabled});
        bool actual=response.GetProperty("preview_enabled").GetBoolean();if(actual!=enabled)throw new IOException("서버 설정 반영 실패");
        Remember(session,enabled);if(!enabled)RedactExisting(Android.App.Application.Context);
    }
    private static void Remember(LoginSession session,bool enabled)=>SecureState.Update(s=>{if(s.Session?.Token==session.Token){s.NotificationPreview=enabled;s.NotificationPreferenceAccount=NotificationPolicy.AccountKey(session);}});
    private static void RedactExisting(Context context)
    {
        if(context.GetSystemService(Context.NotificationService) is not NotificationManager manager)return;
        foreach(var item in manager.GetActiveNotifications()??[]){if(!NotificationPolicy.MessageID(item.Tag,out _))continue;try{
            using var builder=Notification.Builder.RecoverBuilder(context,item.Notification);
            using var redacted=builder.SetContentTitle("온테크 채팅")!.SetContentText("새 메시지가 있습니다. 앱에서 확인하세요.")!.SetSubText((string?)null)!.SetStyle(new Notification.BigTextStyle().BigText("새 메시지가 있습니다. 앱에서 확인하세요."))!.SetOnlyAlertOnce(true)!.SetVisibility(NotificationVisibility.Private).Build();
            manager.Notify(item.Tag,item.Id,redacted);
        }catch{}}
    }
    public static void OpenSoundSettings(Activity owner)
    {
        PushService.CreateChannel(owner);try{
            using var intent=new Intent(Android.Provider.Settings.ActionChannelNotificationSettings);
            intent.PutExtra(Android.Provider.Settings.ExtraAppPackage,owner.PackageName);intent.PutExtra(Android.Provider.Settings.ExtraChannelId,PushService.Channel);owner.StartActivity(intent);
        }catch(ActivityNotFoundException){using var fallback=new Intent(Android.Provider.Settings.ActionAppNotificationSettings);fallback.PutExtra(Android.Provider.Settings.ExtraAppPackage,owner.PackageName);owner.StartActivity(fallback);}
    }
}
