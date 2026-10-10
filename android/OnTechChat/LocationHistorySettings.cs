using Android.App;
using Android.Content.PM;
using Android.Widget;
using TTask=System.Threading.Tasks.Task;
namespace OnTechChat;
public sealed partial class MainActivity
{
 private bool preparingHistory;
 private string historyConnectionError = "";
 internal void ShowLocationHistorySettings()
 {
  var s=SecureState.Read();
  if(s.Session is not {Active:true}){Notice("로그인 후 사용할 수 있습니다.");return;}
  var box=new LinearLayout(this){Orientation=Orientation.Vertical};box.SetPadding(Dp(20),Dp(8),Dp(20),0);
  var enabled=new CheckBox(this){Text="내 위치 조회 허용",Checked=s.LocationSharingEnabled};
  box.AddView(enabled);
  box.AddView(new TextView(this){Text="켜면 1분 간격으로 위치를 기록하고 친구가 지도에서 이동 경로를 조회할 수 있습니다. 기록은 최근 48시간 보관합니다. 별도의 기록 시작 설정은 없습니다.\n\n새로 허용하면 모든 친구(이후 추가되는 친구 포함)에게 공유합니다. 기존 선택 공유는 자동으로 범위를 넓히지 않습니다.\n\nAndroid 기록 알림은 아래 알림 표시 설정에서 숨길 수 있습니다. 시스템의 실행 중인 앱 표시는 유지됩니다. 인터넷이 끊기면 기기에 암호화 보관하고 연결 후 전송합니다. 강제 종료·재부팅 후 앱을 열면 재개합니다."});
  box.AddView(new TextView(this){Text="\n"+LocationHistoryService.Status+(historyConnectionError.Length>0?"\n"+historyConnectionError:"")+" · 미전송 "+s.HistoryQueue.Count+"개"+(s.HistoryDeletePending||s.HistoryRevokePending?"\n서버 중지/삭제 반영 대기":"")});
  var delete=new Button(this){Text="내 모든 이동 기록 삭제"};
  delete.Click+=(_,_)=>new AlertDialog.Builder(this)!.SetTitle("이동 기록 삭제")!.SetMessage("이 계정의 모든 기기 기록을 삭제하고 위치 조회 허용을 끕니다. 연결 전에는 서버 삭제가 대기됩니다. 복구할 수 없습니다.")!.SetNegativeButton("취소",(_,_)=>{})!.SetPositiveButton("삭제",(_,_)=>{LocationHistoryService.Pause(this,true);Run(async()=>{await LocationHistoryService.SyncAsync();Notice(LocationHistoryService.Status);});})!.Show();
  box.AddView(delete);
  var alerts=new Button(this){Text="기록 알림 표시 설정"};alerts.Click+=(_,_)=>{LocationHistoryService.PrepareNotificationChannel(this);var intent=new Android.Content.Intent(Android.Provider.Settings.ActionChannelNotificationSettings);intent.PutExtra(Android.Provider.Settings.ExtraAppPackage,PackageName);intent.PutExtra(Android.Provider.Settings.ExtraChannelId,"ontech_location_history");StartActivity(intent);};box.AddView(alerts);
  new AlertDialog.Builder(this)!.SetTitle("내 위치 조회 허용 · 이동경로")!.SetView(box)!.SetNegativeButton("닫기",(_,_)=>{})!
   .SetPositiveButton("적용",(_,_)=>Run(async()=>{
    if(!enabled.Checked){LocationHistoryService.Pause(this);await LocationHistoryService.SyncAsync();Notice("위치 기록과 친구 조회를 중지했습니다.");return;}
    if(!HistoryPermissions())return;
    if(!SecureState.Read().LocationSharingEnabled){await PushCoordinator.RefreshAsync(true);await SaveLocationSharingAsync();}
    await EnableHistoryFromSharingAsync();
   }))!.Show();
 }
 private bool HistoryPermissions()
 {
  if(CheckSelfPermission(Android.Manifest.Permission.AccessFineLocation)!=Permission.Granted&&CheckSelfPermission(Android.Manifest.Permission.AccessCoarseLocation)!=Permission.Granted){RequestPermissions([Android.Manifest.Permission.AccessFineLocation,Android.Manifest.Permission.AccessCoarseLocation],LocationPermissionRequest);Notice("위치 권한을 허용하면 이동 기록을 시작할 수 있습니다.");return false;}
  return true;
 }
 private async TTask EnableHistoryFromSharingAsync()
 {
  if(preparingHistory||!resumed||destroyed)return;preparingHistory=true;
  try{
   var s=SecureState.Read();if(s.Session is not {Active:true} session||!s.LocationSharingEnabled||s.LocationSharingAccount!=NotificationPolicy.AccountKey(session))return;
   if(LocationHistoryPolicy.CanRecord(s)){LocationHistoryService.Start(this);return;}
   if(s.HistoryRevokePending||s.HistoryDeletePending||!HistoryPermissions())return;
   await PushCoordinator.RefreshAsync(true);s=SecureState.Read();
   using var api=new ChatApi(new Uri(session.Origin),session.Token);
   var grant=await api.JsonAsync("/api/devices/"+s.DeviceId+"/location-sharing",HttpMethod.Get,ct:Lifetime.Token);
   if(!grant.GetProperty("enabled").GetBoolean()||grant.GetProperty("revision").GetString()!=s.LocationGrantRevision){LocationShareService.DisableLocally(this);return;}
   var info=await api.JsonAsync("/api/devices/"+s.DeviceId+"/location-history",HttpMethod.Patch,new{enabled=true,share_with_friends=true,follow_sharing=true,interval_seconds=60,confirmed=true},Lifetime.Token);
   bool saved=false;SecureState.Update(v=>{if(v.Session?.Token!=session.Token||!v.LocationSharingEnabled)return;LocationHistoryPolicy.Clear(v);v.HistoryAccount=NotificationPolicy.AccountKey(session);v.HistoryRevision=info.GetProperty("revision").GetString()??"";v.HistoryEnabled=true;v.HistoryShare=true;saved=true;});
   if(saved){LocationHistoryService.Start(this);Notice("위치 조회 허용에 따라 1분 간격 기록을 시작했습니다. 최근 48시간 보관합니다.");}
  }finally{preparingHistory=false;}
 }
 internal async TTask ResumeHistoryAsync()
 {
  try{
   var s=SecureState.Read();if(s.HistoryAccount!=""&&!LocationHistoryPolicy.Belongs(s)){LocationHistoryService.Forget(this);return;}
   if(s.HistoryEnabled&&!s.LocationSharingEnabled){LocationHistoryService.Pause(this);}
   await LocationHistoryService.SyncAsync();
   if(resumed&&!destroyed)await EnableHistoryFromSharingAsync();
   historyConnectionError = "";
  }catch{historyConnectionError = "위치 기록 연결 대기 · 서버 연결 후 다시 시도합니다.";}
 }
}
