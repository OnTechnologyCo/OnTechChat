using Android.App;
using Android.Content;
using Android.Content.PM;
using Android.Locations;
using Android.OS;
using System.Globalization;
using System.Text.Json;
using TTask=System.Threading.Tasks.Task;
namespace OnTechChat;

public sealed partial class MainActivity
{
    private const int LocationPermissionRequest=53;
    private TaskCompletionSource<bool>? locationPermission;
    private CancellationTokenSource? measuringLocation;
    private bool sharingLocation;
    private static long NowSeconds=>DateTimeOffset.UtcNow.ToUnixTimeSeconds();
    private void RequireSameSession(LoginSession session)
    {
        if(destroyed||!resumed||SecureState.Read().Session is not {Active:true} s||s.Token!=session.Token||s.Origin!=session.Origin)
            throw new IOException("화면 또는 로그인 상태가 바뀌었습니다. 원래 대화에서 다시 시도해 주세요.");
    }
    private async System.Threading.Tasks.Task<bool> ConfirmAsync(string titleText,string messageText,string accept,CancellationToken ct)
    {
        if(destroyed||!resumed||ct.IsCancellationRequested)return false;
        var answer=new TaskCompletionSource<bool>(TaskCreationOptions.RunContinuationsAsynchronously);
        var dialog=new AlertDialog.Builder(this)!.SetTitle(titleText)!.SetMessage(messageText)!.SetNegativeButton("취소",(_,_)=>answer.TrySetResult(false))!.SetPositiveButton(accept,(_,_)=>answer.TrySetResult(true))!.Create()!;
        dialog.CancelEvent+=(_,_)=>answer.TrySetResult(false);
        using var cancel=ct.Register(()=>RunOnUiThread(()=>{answer.TrySetResult(false);dialog.Dismiss();}));
        dialog.Show();return await answer.Task;
    }
    internal void BeginShareLocation(string type,string id)
    {
        Run(async()=>
        {
            if(sharingLocation||checkingUpdate){Notice("위치 확인 또는 앱 업데이트 작업을 마친 뒤 다시 눌러주세요.");return;}
            var session=SecureState.Read().Session;
            if(session is not {Active:true}||!UrlPolicy.ValidID(id)||type!="request"){Notice("로그인 후 대화 화면에서 다시 시도하세요.");return;}
            sharingLocation=true;
            try
            {
                RequireSameSession(session);
                using var api=new ChatApi(new Uri(session.Origin),session.Token);
                var q=await api.JsonAsync("/api/location-requests/"+id,HttpMethod.Get,ct:Lifetime.Token);
                if(q.GetProperty("target_id").GetString()!=session.UserId||q.GetProperty("status").GetString()!="pending"||q.GetProperty("expires_at").GetInt64()<=NowSeconds)throw new IOException("나에게 온 유효한 위치 요청이 아닙니다. 취소되거나 만료됐을 수 있습니다.");
                string name=q.GetProperty("requester_name").GetString()??"요청자";
                string? roomID=q.GetProperty("room_id").GetString();
                long requestCreated=q.GetProperty("created_at").GetInt64();
                RequireSameSession(session);
                var consent=$"{name}님이 현재 위치를 요청했습니다.\n\n승인하면 내 위치를 한 번 측정한 뒤 {name}님의 조회에 응답합니다. 취소해도 다른 채팅 기능은 사용할 수 있습니다. 서버 보관은 최대 24시간입니다.";
                bool measurementApproved=await ConfirmAsync("위치 확인 요청에 응답",consent,"동의하고 위치 한 번 측정",Lifetime.Token);
                if(!measurementApproved)return;
                if(!await EnsureLocationPermissionAsync())return;
                for(var i=0;!resumed&&i<15;i++)await TTask.Delay(100,Lifetime.Token);
                RequireSameSession(session);
                measuringLocation=CancellationTokenSource.CreateLinkedTokenSource(Lifetime.Token);measuringLocation.CancelAfter(TimeSpan.FromSeconds(30));
                PositionFix fix;
                try{Notice("현재 위치 측정 중… 최대 25초. 앱을 벗어나면 취소됩니다.");fix=await MeasureOnceAsync(measurementApproved,measuringLocation.Token);}
                finally{measuringLocation.Dispose();measuringLocation=null;}
                RequireSameSession(session);
                var coords=string.Create(CultureInfo.InvariantCulture,$"{fix.Latitude:F5}, {fix.Longitude:F5}");
                bool consentApproved=await ConfirmAsync("이 위치를 전송할까요?",$"수신 대상: {name}\n내 위치: {coords}\n정확도: 약 {Math.Ceiling(fix.Accuracy):N0}m\n\n이 전송 한 번에만 동의하며, 이후 자동 추적하지 않습니다. 서버 보관은 최대 24시간입니다.","동의하고 한 번 전송",Lifetime.Token);
                if(!consentApproved)return;
                RequireSameSession(session);if(!fix.IsUsable(NowSeconds))throw new IOException("위치 측정 후 시간이 지났습니다. 다시 측정해 주세요.");
                RequireSameSession(session);
                if(fix.CapturedAt<requestCreated-5)throw new IOException("요청 후 측정한 새 위치만 응답할 수 있습니다.");
                var endpoint="/api/location-requests/"+id+"/respond";
                // 화면의 최종 동의와 현재 계정을 다시 확인한 한 건만 전송합니다.
                if(!consentApproved||!resumed||SecureState.Read().Session?.Token!=session.Token)return;
                await api.JsonAsync(endpoint,HttpMethod.Post,new{
                    latitude=fix.Latitude,longitude=fix.Longitude,accuracy_m=fix.Accuracy,captured_at=fix.CapturedAt,
                    approximate=fix.Approximate,confirmed=consentApproved,client_id="location-"+Guid.NewGuid().ToString("N")},Lifetime.Token);
                if(SecureState.Read().Session?.Token==session.Token) Notice("상대의 위치 조회 팝업으로 응답했습니다. 채팅에는 메시지를 보내지 않았습니다.");
            }
            catch(System.OperationCanceledException){await LocationShareService.ReportFailureAsync(id,"","timeout");Notice("위치 측정이 취소되거나 시간이 초과되었습니다.");}
            catch(Exception){await LocationShareService.ReportFailureAsync(id,"","unavailable");throw;}
            finally{measuringLocation?.Cancel();measuringLocation?.Dispose();measuringLocation=null;sharingLocation=false;}
        });
    }
    private async System.Threading.Tasks.Task<bool> EnsureLocationPermissionAsync()
    {
        if(CheckSelfPermission(Android.Manifest.Permission.AccessCoarseLocation)==Permission.Granted||CheckSelfPermission(Android.Manifest.Permission.AccessFineLocation)==Permission.Granted)return true;
        locationPermission=new(TaskCreationOptions.RunContinuationsAsynchronously);
        RequestPermissions([Android.Manifest.Permission.AccessFineLocation,Android.Manifest.Permission.AccessCoarseLocation],LocationPermissionRequest);
        try{return await locationPermission.Task.WaitAsync(TimeSpan.FromMinutes(2),Lifetime.Token);}
        finally{locationPermission=null;}
    }
    public override void OnRequestPermissionsResult(int requestCode,string[] permissions,Permission[] grantResults)
    {
        base.OnRequestPermissionsResult(requestCode,permissions,grantResults);
        if(requestCode!=LocationPermissionRequest)return;
        bool ok=CheckSelfPermission(Android.Manifest.Permission.AccessCoarseLocation)==Permission.Granted||CheckSelfPermission(Android.Manifest.Permission.AccessFineLocation)==Permission.Granted;
        locationPermission?.TrySetResult(ok);if(!ok)Notice("위치 권한을 허용하지 않아 전송하지 않았습니다.");
    }
#pragma warning disable CS0618,CS0672
    // 반복 위치 구독 대신 Android의 1회 측정 API만 사용합니다.
    private async System.Threading.Tasks.Task<PositionFix> MeasureOnceAsync(bool userApproved,CancellationToken ct)
    {
        if(!userApproved||!resumed||destroyed)throw new IOException("화면에서 위치 측정에 동의한 경우만 가능합니다.");
        var manager=GetSystemService(LocationService) as LocationManager??throw new IOException("위치 서비스가 없습니다.");
        bool fine=CheckSelfPermission(Android.Manifest.Permission.AccessFineLocation)==Permission.Granted;
        if(!fine&&CheckSelfPermission(Android.Manifest.Permission.AccessCoarseLocation)!=Permission.Granted)throw new IOException("위치 권한이 없습니다.");
        var available=manager.GetProviders(true)??[];
        string? provider=available.Contains("fused")?"fused":available.Contains("network")?"network":fine&&available.Contains("gps")?"gps":null;
        if(provider is null)throw new IOException("휴대전화의 위치 설정을 켠 뒤 다시 요청에 응답하세요.");
        var answer=new TaskCompletionSource<PositionFix>(TaskCreationOptions.RunContinuationsAsynchronously);
        using var signal=new Android.OS.CancellationSignal();
        using var consumer=new OneFixConsumer(answer,!fine);
        using var listener=new OneFixListener(answer,!fine);
        using var cancelled=ct.Register(()=>{signal.Cancel();answer.TrySetCanceled(ct);});
        try
        {
            if(OperatingSystem.IsAndroidVersionAtLeast(30))manager.GetCurrentLocation(provider,signal,MainExecutor!,consumer);
            else manager.RequestSingleUpdate(provider,listener,Looper.MainLooper);
            return await answer.Task.WaitAsync(TimeSpan.FromSeconds(25),ct);
        }
        catch(TimeoutException){throw new IOException("새 위치를 얻지 못했습니다. 과거 위치를 대신 보내지 않습니다.");}
        finally{signal.Cancel();manager.RemoveUpdates(listener);}
    }
    private static void AcceptFix(TaskCompletionSource<PositionFix> answer,Android.Locations.Location? value,bool approximate)
    {
        if(value is null){answer.TrySetException(new IOException("현재 위치가 반환되지 않았습니다."));return;}
        var fix=new PositionFix(value.Latitude,value.Longitude,value.HasAccuracy?value.Accuracy:0,value.Time/1000,approximate);
        var age=SystemClock.ElapsedRealtimeNanos()-value.ElapsedRealtimeNanos;
        if(age>=0&&age<=10_000_000_000L&&fix.IsUsable(NowSeconds))answer.TrySetResult(fix);
        else answer.TrySetException(new IOException("새 위치가 아니거나 정확도를 확인하지 못했습니다. 다시 시도하세요."));
    }
    private sealed class OneFixConsumer(TaskCompletionSource<PositionFix> answer,bool approximate):Java.Lang.Object,Java.Util.Functions.IConsumer
    {public void Accept(Java.Lang.Object? value)=>AcceptFix(answer,value as Android.Locations.Location,approximate);}
    private sealed class OneFixListener(TaskCompletionSource<PositionFix> answer,bool approximate):Java.Lang.Object,ILocationListener
    {
        public void OnLocationChanged(Android.Locations.Location location)=>AcceptFix(answer,location,approximate);
        public void OnProviderDisabled(string provider){}public void OnProviderEnabled(string provider){}
        public void OnStatusChanged(string? provider,Availability status,Bundle? extras){}
    }
#pragma warning restore CS0618,CS0672
}
