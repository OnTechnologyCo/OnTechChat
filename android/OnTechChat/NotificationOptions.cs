using Android.App;
using Android.Widget;
namespace OnTechChat;
public sealed partial class MainActivity
{
 private bool notificationChecking;
 internal void QueueNotificationCheck(){if(destroyed||!resumed||ui is null)return;ui.RemoveCallbacks(NotificationTick);ui.PostDelayed(NotificationTick,400);}
 private async void NotificationTick(){
  if(destroyed||!resumed||notificationChecking)return;notificationChecking=true;
  try{await NotificationCenter.ReconcileAsync(this,ct:Lifetime.Token);}catch{}
  try{await PollPendingLocationsAsync();}catch{}
  finally{notificationChecking=false;if(!destroyed&&resumed){ui.RemoveCallbacks(NotificationTick);ui.PostDelayed(NotificationTick,5000);}}
 }
 internal void ShowNotificationOptions(){
  if(destroyed)return;var state=SecureState.Read();if(state.Session is not{Active:true} session){Notice("로그인 후 설정하세요.");return;}
  var scroll=new ScrollView(this);var form=new LinearLayout(this){Orientation=Orientation.Vertical};form.SetPadding(Dp(22),Dp(12),Dp(22),Dp(12));scroll.AddView(form);
  var preview=new Switch(this){Text="일반 채팅 내용 미리보기",Enabled=false,TextSize=16};
  var info=new TextView(this){TextSize=13,Text="기기별 설정 확인 중…"};
  var note=new TextView(this){TextSize=13,Text="켜면 보낸 사람과 일반 메시지 일부가 FCM 알림에 표시됩니다. 인증번호·위치 좌표는 숨깁니다. 이 휴대전화에만 적용됩니다."};
  var save=new Button(this){Text="미리보기 설정 저장",Enabled=false};var sound=new Button(this){Text="알림 소리·진동 변경"};
  var clear=new Button(this){Text="읽은 알림 지금 정리"};var test=new Button(this){Text="선택한 소리로 로컬 알림 테스트"};
  foreach(var v in new Android.Views.View[]{preview,note,save,sound,clear,test,info})form.AddView(v);
  var dialog=new AlertDialog.Builder(this)!.SetTitle("알림 미리보기 · 소리")!.SetView(scroll)!.SetNegativeButton("닫기",(_,_)=>{})!.Create()!;dialog.Show();
  sound.Click+=(_,_)=>Run(()=>{NotificationCenter.OpenSoundSettings(this);return System.Threading.Tasks.Task.CompletedTask;});
  test.Click+=(_,_)=>{AskNotificationPermission(false);PushService.Show(this,"","","",true);info.Text="휴대전화 로컬 알림입니다. 서버 FCM 수신 시험은 아닙니다.";};
  clear.Click+=(_,_)=>Run(async()=>{clear.Enabled=false;try{await NotificationCenter.ReconcileAsync(this,true,Lifetime.Token);if(!destroyed)info.Text=NotificationCenter.Status;}finally{if(!destroyed)clear.Enabled=true;}});
  save.Click+=(_,_)=>Run(async()=>{
   save.Enabled=preview.Enabled=false;try{
    if(SecureState.Read().Session?.Token!=session.Token)throw new IOException("로그인이 바뀌었습니다.");
    await NotificationCenter.SavePreferencesAsync(session,state.DeviceId,preview.Checked);
    if(!destroyed)info.Text=preview.Checked?"저장 완료 · 일반 메시지 미리보기 켜짐":"저장 완료 · 본문 숨김";
   }catch{if(!destroyed)info.Text="저장 실패. 서버 버전·기기 등록·네트워크를 확인하세요.";}finally{if(!destroyed)save.Enabled=preview.Enabled=true;}
  });
  Run(async()=>{try{await PushCoordinator.RefreshAsync(true);bool enabled=await NotificationCenter.LoadPreferencesAsync(session,state.DeviceId);if(!destroyed&&dialog.IsShowing){preview.Checked=enabled;save.Enabled=preview.Enabled=true;info.Text="설정 불러오기 완료\n"+NotificationCenter.Status;}}
   catch{if(!destroyed&&dialog.IsShowing)info.Text="서버 1.4.1 이상 및 기기 등록이 필요합니다. 소리 설정은 먼저 바꿀 수 있습니다.";}});
 }
}
