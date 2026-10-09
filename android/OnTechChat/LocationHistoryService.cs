using Android.App;
using Android.Content;
using Android.Content.PM;
using Android.OS;
using OperationCanceledException=System.OperationCanceledException;
using System.Net;
using TTask=System.Threading.Tasks.Task;
namespace OnTechChat;
[Service(Name="com.ontechdev.chat.LocationHistoryService",Exported=false,ForegroundServiceType=ForegroundService.TypeLocation)]
public sealed class LocationHistoryService:Service
{
 private const string Channel="ontech_location_history";
 private const int NoticeID=4303;
 private static LocationHistoryService? instance;
 private static readonly SemaphoreSlim SyncGate=new(1,1);
 private CancellationTokenSource? lifetime;
 public static string Status {get;private set;}="기록 중지";
 public static void PrepareNotificationChannel(Context context){var manager=(NotificationManager)context.GetSystemService(NotificationService)!;var channel=new NotificationChannel(Channel,"GPS 이동 기록",NotificationImportance.Low){Description="1분 간격 위치 기록 알림. 숨겨도 기록은 계속됩니다.",LockscreenVisibility=NotificationVisibility.Private};channel.SetSound(null,null);channel.EnableVibration(false);channel.SetShowBadge(false);manager.CreateNotificationChannel(channel);}
 public override IBinder? OnBind(Intent? intent)=>null;
 public override void OnCreate(){base.OnCreate();instance=this;}
 public static void Start(Context context)
 {
  if(!LocationHistoryPolicy.CanRecord(SecureState.Read())||instance is not null)return;
  try {context.StartForegroundService(new Intent(context,typeof(LocationHistoryService)));}
  catch {Status="Android 실행 제한 · 앱에서 다시 시작하세요.";SecureState.Update(s=>s.HistoryEnabled=false);}
 }
 public override StartCommandResult OnStartCommand(Intent? intent,StartCommandFlags flags,int startId)
 {
  if(intent?.Action=="stop"){Pause(this);_ = SyncAsync();return StartCommandResult.NotSticky;}
  if(lifetime is not null)return StartCommandResult.NotSticky;
  if(!LocationHistoryPolicy.CanRecord(SecureState.Read())){StopSelf();return StartCommandResult.NotSticky;}
  try {
   PrepareNotificationChannel(this);
   using var open=new Intent(this,typeof(MainActivity));var openPI=PendingIntent.GetActivity(this,4303,open,PendingIntentFlags.UpdateCurrent|PendingIntentFlags.Immutable);
   using var stop=new Intent(this,typeof(LocationHistoryService)).SetAction("stop");var stopPI=PendingIntent.GetService(this,4304,stop,PendingIntentFlags.UpdateCurrent|PendingIntentFlags.Immutable);
   var builder=new Notification.Builder(this,Channel).SetSmallIcon(Resource.Drawable.ic_notification)!.SetContentTitle("GPS 이동 기록 중")!.SetContentText("1분 간격 · 48시간 보관 · 연결되면 서버에 전송")!.SetContentIntent(openPI)!.SetOngoing(true).SetOnlyAlertOnce(true).SetVisibility(NotificationVisibility.Private);
   builder.AddAction(new Notification.Action.Builder(null,"기록 중지",stopPI).Build());StartForeground(NoticeID,builder.Build());
   lifetime=new();_ = RecordLoop(lifetime.Token);
  }catch{Status="위치 권한/서비스 실행 제한 · 다시 시작하세요.";SecureState.Update(s=>s.HistoryEnabled=false);StopSelf();}
  return StartCommandResult.NotSticky;
 }
 public static void Pause(Context context,bool delete=false)
 {
  LocationShareService.DisableLocally(context);_ = LocationShareService.SyncPendingRevocationAsync();
  SecureState.Update(s=>{if(s.Session is {Active:true} login)s.HistoryAccount=NotificationPolicy.AccountKey(login);s.HistoryEnabled=false;s.HistoryShare=false;s.HistoryQueue.Clear();s.HistoryRevokePending=true;s.HistoryDeletePending|=delete;});
  instance?.lifetime?.Cancel();context.StopService(new Intent(context,typeof(LocationHistoryService)));Status=delete?"삭제 요청 대기":"기록 중지";
 }
 public static void Forget(Context context)
 {
  instance?.lifetime?.Cancel();context.StopService(new Intent(context,typeof(LocationHistoryService)));
  SecureState.Update(s=>{LocationHistoryPolicy.Clear(s);s.HistoryAccount="";s.HistoryRevokePending=false;s.HistoryDeletePending=false;});Status="기록 중지";
 }
 public override void OnDestroy(){lifetime?.Cancel();lifetime?.Dispose();lifetime=null;if(ReferenceEquals(instance,this))instance=null;try{StopForeground(StopForegroundFlags.Remove);}catch{}base.OnDestroy();}
 private async TTask RecordLoop(CancellationToken ct)
 {
  try {
   while(!ct.IsCancellationRequested) {
    var s=SecureState.Read();if(!LocationHistoryPolicy.CanRecord(s))break;
    if(CheckSelfPermission(Android.Manifest.Permission.AccessFineLocation)!=Permission.Granted&&CheckSelfPermission(Android.Manifest.Permission.AccessCoarseLocation)!=Permission.Granted){Pause(this);await SyncAsync();break;}
    await SyncAsync(ct);
    s=SecureState.Read();if(!LocationHistoryPolicy.CanRecord(s))break;
    string revision=s.HistoryRevision;
    try {
     using var sample=CancellationTokenSource.CreateLinkedTokenSource(ct);sample.CancelAfter(TimeSpan.FromSeconds(30));
     var fix=await LocationShareService.MeasureHistoryFixAsync(this,sample.Token);
     SecureState.Update(v=>{LocationHistoryPolicy.Add(v,fix,revision,DateTimeOffset.UtcNow.ToUnixTimeSeconds());});
     Status="최근 측정 "+DateTimeOffset.FromUnixTimeSeconds(fix.CapturedAt).ToLocalTime().ToString("HH:mm:ss");
    }catch(OperationCanceledException)when(ct.IsCancellationRequested){break;}catch{Status="GPS 측정 대기 · 실내/위치 설정을 확인하세요.";}
    await SyncAsync(ct);
    var last=SecureState.Read().HistoryLastCaptured;
    int wait=(int)Math.Clamp(last+60-DateTimeOffset.UtcNow.ToUnixTimeSeconds(),5,60);
    await TTask.Delay(TimeSpan.FromSeconds(wait),ct);
   }
  }catch(OperationCanceledException){}finally{StopSelf();}
 }
 public static async TTask SyncAsync(CancellationToken ct=default)
 {
  if(!await SyncGate.WaitAsync(0,ct))return;
  string syncToken="",syncRevision="";
  bool StillCurrent(){var v=SecureState.Read();return v.Session?.Token==syncToken&&v.HistoryRevision==syncRevision;}
  try {
   var s=SecureState.Update(v=>LocationHistoryPolicy.Prune(v,DateTimeOffset.UtcNow.ToUnixTimeSeconds()));
   if(!LocationHistoryPolicy.Belongs(s)||s.Session is not {Active:true} session)return;
   syncToken=session.Token;syncRevision=s.HistoryRevision;
   using var api=new ChatApi(new Uri(session.Origin),session.Token);
   using var deadline=CancellationTokenSource.CreateLinkedTokenSource(ct);deadline.CancelAfter(TimeSpan.FromSeconds(20));
   if(s.HistoryDeletePending||s.HistoryRevokePending) {
    if(s.HistoryDeletePending)await api.JsonAsync("/api/location-history",HttpMethod.Delete,ct:deadline.Token);
    else await api.JsonAsync("/api/devices/"+s.DeviceId+"/location-history",HttpMethod.Patch,new{enabled=false},deadline.Token);
    SecureState.Update(v=>{if(v.Session?.Token==session.Token&&v.HistoryRevision==s.HistoryRevision){v.HistoryRevokePending=false;v.HistoryDeletePending=false;v.HistoryRevision="";}});
    Status="기록 중지 · 서버 반영 완료";return;
   }
   if(!s.HistoryEnabled)return;
   var info=await api.JsonAsync("/api/devices/"+s.DeviceId+"/location-history",HttpMethod.Get,ct:deadline.Token);
   if(!info.GetProperty("enabled").GetBoolean()||info.GetProperty("revision").GetString()!=s.HistoryRevision){if(StillCurrent()){Forget(Android.App.Application.Context);LocationShareService.DisableLocally(Android.App.Application.Context);Status="서버에서 위치 조회/기록이 중지되었습니다.";}return;}
   for(int n=0;n<30;n++) {
    var current=SecureState.Read();if(!LocationHistoryPolicy.CanRecord(current)||current.HistoryRevision!=s.HistoryRevision||current.Session?.Token!=session.Token)return;
    var batch=current.HistoryQueue.Take(100).ToArray();if(batch.Length==0)break;
    await api.JsonAsync("/api/devices/"+s.DeviceId+"/location-history",HttpMethod.Post,new{revision=s.HistoryRevision,points=batch},deadline.Token);
    var ids=batch.Select(p=>p.Id).ToHashSet();
    SecureState.Update(v=>{if(v.Session?.Token==session.Token&&v.HistoryRevision==s.HistoryRevision){v.HistoryQueue.RemoveAll(p=>ids.Contains(p.Id));v.HistoryLastSynced=DateTimeOffset.UtcNow.ToUnixTimeSeconds();}});
   }
  }catch(ApiException e)when(e.Status is HttpStatusCode.Unauthorized or HttpStatusCode.Forbidden or HttpStatusCode.Conflict or HttpStatusCode.NotFound){if(StillCurrent()){Forget(Android.App.Application.Context);LocationShareService.DisableLocally(Android.App.Application.Context);Status="인증 또는 위치 조회 설정이 변경되었습니다.";}}
  catch(OperationCanceledException){}
  catch(ApiException e){Status=e.Status==HttpStatusCode.TooManyRequests?"서버 보관/전송 한도 · 기기에 보관 후 재시도":"위치 전송 보류 · 기기 날짜/시각과 서버 설정을 확인하세요.";}
  catch{Status="서버 미연결 · 기기에 보관 후 재전송";}
  finally{SyncGate.Release();}
 }
}
