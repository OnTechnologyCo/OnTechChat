using Android.App;
using Android.Content;
using Android.OS;
using System.Globalization;
using TTask = System.Threading.Tasks.Task;

namespace OnTechChat;

public sealed partial class LocationShareService
{
    internal static async TTask ReportFailureAsync(string id,string revision,string code)
    {
        var state=SecureState.Read();
        if(!UrlPolicy.ValidID(id)||state.Session is not {Active:true} session)return;
        try
        {
            using var timeout=new CancellationTokenSource(TimeSpan.FromSeconds(8));
            using var api=new ChatApi(new Uri(session.Origin),session.Token);
            await api.JsonAsync("/api/location-requests/"+id+"/fail",HttpMethod.Post,
                new {device_id=state.DeviceId,grant_revision=revision,code},timeout.Token);
        }
        catch { /* 수신자 팝업의 서버 만료 처리로 종료됩니다. */ }
    }
}

public sealed partial class MainActivity
{
    private readonly HashSet<string> promptedLocationRequests=new();
    private async TTask PollPendingLocationsAsync()
    {
        if(!resumed||destroyed)return;
        var state=SecureState.Read();
        if(state.Session is not {Active:true} session)return;
        using var api=new ChatApi(new Uri(session.Origin),session.Token);
        var result=await api.JsonAsync("/api/location-requests?device_id="+Uri.EscapeDataString(state.DeviceId),HttpMethod.Get,ct:Lifetime.Token);
        if(SecureState.Read().Session?.Token!=session.Token||!resumed)return;
        foreach(var q in result.GetProperty("requests").EnumerateArray())
        {
            string id=q.GetProperty("id").GetString()??"";
            if(!UrlPolicy.ValidID(id))continue;
            if(q.GetProperty("auto_response").GetBoolean())
            {
                string revision=q.GetProperty("grant_revision").GetString()??"";
                if(state.LocationSharingEnabled&&state.LocationGrantRevision==revision)
                    LocationShareService.Enqueue(this,id,revision);
            }
            else if(!sharingLocation&&!checkingUpdate&&promptedLocationRequests.Add(id))
            {
                (GetSystemService(NotificationService) as NotificationManager)?.Cancel("location-request-"+id,0);
                BeginShareLocation("request",id);
                break;
            }
        }
    }

    internal void OpenLocationMap(string id)
    {
        Run(async()=>
        {
            var session=SecureState.Read().Session;
            if(session is not {Active:true}||!UrlPolicy.ValidID(id))return;
            using var api=new ChatApi(new Uri(session.Origin),session.Token);
            var q=await api.JsonAsync("/api/location-requests/"+id,HttpMethod.Get,ct:Lifetime.Token);
            RequireSameSession(session);
            if(q.GetProperty("status").GetString()!="responded"||!q.TryGetProperty("location",out var point))
                throw new IOException("조회 위치가 없거나 만료되었습니다. 다시 확인해 주세요.");
            double lat=point.GetProperty("latitude").GetDouble(),lon=point.GetProperty("longitude").GetDouble();
            if(!double.IsFinite(lat)||!double.IsFinite(lon)||Math.Abs(lat)>90||Math.Abs(lon)>180)return;
            string coords=string.Create(CultureInfo.InvariantCulture,$"{lat:F7},{lon:F7}");
            string label=q.GetProperty("target_name").GetString()??"조회한 위치";
            var geo=Android.Net.Uri.Parse("geo:"+coords+"?q="+Uri.EscapeDataString(coords+" ("+label+")"));
            try { StartActivity(new Intent(Intent.ActionView,geo)); }
            catch(ActivityNotFoundException)
            {
                try { StartActivity(new Intent(Intent.ActionView,Android.Net.Uri.Parse("https://www.google.com/maps/search/?api=1&query="+coords))); }
                catch(ActivityNotFoundException) { Notice("지도 앱 또는 브라우저를 설치해 주세요."); }
            }
        });
    }
}
