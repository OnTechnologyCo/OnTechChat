using Android.App;
using Android.Content;
using Android.Content.PM;
using Android.OS;
using System.Net;
using System.Security.Cryptography;
using TTask=System.Threading.Tasks.Task;
namespace OnTechChat;
public sealed partial class MainActivity
{
 private readonly UpdateCheckGate updateGate=new();
 private bool checkingUpdate=>updateGate.Busy;
 private bool waitingInstallPermission,updatePageReady,updatePageFailed;
 private (UpdateRelease Release,string Path,LoginSession Session)? pendingInstall;
 internal void BeginUpdateCheck()=>Run(()=>CheckUpdateAsync(true));
 internal void UpdatePageStarted(){updatePageReady=false;updatePageFailed=false;ui.RemoveCallbacks(QueueAutomaticUpdateCheck);}
 internal void UpdatePageFailed(){updatePageReady=false;updatePageFailed=true;ui.RemoveCallbacks(QueueAutomaticUpdateCheck);}
 internal void UpdatePageFinished(string? url){
  updatePageReady=!updatePageFailed&&ServerOrigin is {} origin&&Uri.TryCreate(url,UriKind.Absolute,out var page)&&UrlPolicy.SameOrigin(origin,page)&&page.AbsolutePath=="/";
  QueueUpdateCheck();
 }
 internal void QueueUpdateCheck(){ui.RemoveCallbacks(QueueAutomaticUpdateCheck);if(!destroyed&&resumed&&updatePageReady)ui.PostDelayed(QueueAutomaticUpdateCheck,8000);}
 private void QueueAutomaticUpdateCheck(){
  if(destroyed||!resumed||Web is null||!updatePageReady||checkingUpdate||sharingLocation||waitingInstallPermission)return;
  Run(()=>CheckUpdateAsync(false));
 }
 private async TTask CheckUpdateAsync(bool manual){
  if(destroyed||!resumed||sharingLocation||waitingInstallPermission)return;
  var session=SecureState.Read().Session;if(session is not{Active:true}){if(manual)Notice("로그인 후 업데이트를 확인하세요.");return;}
  if(!updateGate.TryBegin(NotificationPolicy.AccountKey(session),SystemClock.ElapsedRealtime(),manual,manual||updatePageReady))return;
  try{
   RequireSameSession(session);
   if(pendingInstall is not null){await OfferInstallAsync();return;}
   using var api=new ChatApi(new Uri(session.Origin),session.Token);
   using var deadline=CancellationTokenSource.CreateLinkedTokenSource(Lifetime.Token);deadline.CancelAfter(TimeSpan.FromSeconds(8));
   var data=await api.JsonAsync("/api/app-updates/android",HttpMethod.Get,ct:deadline.Token);var r=UpdateRelease.Parse(data);var installed=InstalledPackage();
   if(r is null||r.VersionCode<=VersionCodeOf(installed)){if(manual)Notice("설치 가능한 더 최신 APK가 없습니다.");return;}
   ValidateRelease(r,installed);RequireSameSession(session);
   var saved=SecureState.Read();
   if(!updateGate.ShouldOffer(r.VersionCode,VersionCodeOf(installed),manual,saved.UpdateDeferredAccount,saved.UpdateDeferredVersion))return;
   updateGate.Offered(r.VersionCode);
   Notice($"새 버전 {r.VersionName}을 자동으로 다운로드합니다. 완료 후 Android 설치 화면이 열립니다.");
   var dir=System.IO.Path.Combine(CacheDir!.AbsolutePath,"app-updates");Directory.CreateDirectory(dir);
   foreach(var old in Directory.EnumerateFiles(dir,"*.apk"))try{File.Delete(old);}catch(IOException){}
   var path=System.IO.Path.Combine(dir,Guid.NewGuid().ToString("N")+".apk");Notice("업데이트 APK 다운로드 중…");await api.DownloadUpdateAsync(r,path,Lifetime.Token);
   await TTask.Run(()=>VerifyArchive(path,r),Lifetime.Token);
   RequireSameSession(session);pendingInstall=(r,path,session);await OfferInstallAsync();
  }catch(ApiException ex)when(ex.Status==HttpStatusCode.NotFound){if(manual)Notice("서버 1.5.0 이상에서 APK 배포를 지원합니다.");}
  catch(Exception){pendingInstall=null;waitingInstallPermission=false;if(manual)throw;Notice("업데이트 확인을 건너뛰었습니다. 채팅은 계속 사용할 수 있습니다. 메뉴에서 다시 확인하세요.");}
  finally{updateGate.Finish();}
 }
#pragma warning disable CS0618
 private PackageInfo InstalledPackage()=>PackageManager!.GetPackageInfo(PackageName!,OperatingSystem.IsAndroidVersionAtLeast(28)?PackageInfoFlags.SigningCertificates:PackageInfoFlags.Signatures) ?? throw new IOException("현재 앱 패키지 정보가 없습니다.");
 private static long VersionCodeOf(PackageInfo p)=>OperatingSystem.IsAndroidVersionAtLeast(28)?p.LongVersionCode:p.VersionCode;
 private static string PackageSigner(PackageInfo p){
  var all=(OperatingSystem.IsAndroidVersionAtLeast(28)?p.SigningInfo?.GetApkContentsSigners():p.Signatures)?.ToArray();
  if(all is null||all.Length!=1)throw new IOException("앱의 단일 서명 인증서를 확인하지 못했습니다.");return Convert.ToHexString(SHA256.HashData(all[0].ToByteArray()!)).ToLowerInvariant();
 }
 private void ValidateRelease(UpdateRelease r,PackageInfo current){
  if(r.Package!=PackageName||r.VersionCode<=VersionCodeOf(current)||r.MinSDK>(int)Build.VERSION.SdkInt)throw new IOException("이 기기에 설치할 수 없는 APK 버전입니다.");
  if(PackageSigner(current)!=r.Signer)throw new IOException("기존 앱과 서명이 다릅니다. 앱을 삭제하지 말고 동일 서명 파일을 요청하세요.");
 }
 private void VerifyArchive(string path,UpdateRelease r){
  ValidateRelease(r,InstalledPackage());using(var f=File.OpenRead(path))if(f.Length!=r.Size||Convert.ToHexString(SHA256.HashData(f)).ToLowerInvariant()!=r.SHA256)throw new IOException("APK 파일이 배포 정보와 다릅니다.");
  var p=PackageManager!.GetPackageArchiveInfo(path,OperatingSystem.IsAndroidVersionAtLeast(28)?PackageInfoFlags.SigningCertificates:PackageInfoFlags.Signatures)??throw new IOException("Android가 APK를 해석하지 못했습니다.");
  if(p.PackageName!=PackageName||VersionCodeOf(p)!=r.VersionCode||PackageSigner(p)!=r.Signer||p.ApplicationInfo is null||p.ApplicationInfo.MinSdkVersion>(int)Build.VERSION.SdkInt)throw new IOException("APK 패키지·버전·서명·지원 Android 검사 실패");
 }
#pragma warning restore CS0618
 private async TTask OfferInstallAsync(){
  if(pendingInstall is not{} p||destroyed||!resumed)return;RequireSameSession(p.Session);
  if(p.Release.VersionCode<=VersionCodeOf(InstalledPackage())){pendingInstall=null;return;}
  if(!PackageManager!.CanRequestPackageInstalls()){
   if(!await ConfirmAsync("이 앱에서 업데이트 설치 허용","Android의 '이 출처 허용' 화면을 엽니다. 취소하면 채팅을 계속 사용하며 같은 버전의 자동 안내는 다시 열지 않습니다.","설정 열기",Lifetime.Token)){DeferPendingUpdate();return;}
   waitingInstallPermission=true;StartActivity(new Intent(Android.Provider.Settings.ActionManageUnknownAppSources,Android.Net.Uri.Parse("package:"+PackageName)));return;
  }
  // User requested automatic download/install flow; Android owns final installation consent.
  await TTask.Run(()=>VerifyArchive(p.Path,p.Release),Lifetime.Token);RequireSameSession(p.Session);
  var uri=AndroidX.Core.Content.FileProvider.GetUriForFile(this,PackageName+".files",new Java.IO.File(p.Path));
  using var intent=new Intent(Intent.ActionView);intent.SetDataAndType(uri,"application/vnd.android.package-archive");
  intent.AddFlags(ActivityFlags.GrantReadUriPermission);intent.ClipData=ClipData.NewRawUri("온테크 앱 업데이트",uri);
  RequireSameSession(p.Session);StartActivity(intent);DeferPendingUpdate();Notice("Android 설치 확인창에서 승인하세요. 취소해도 채팅은 계속 사용할 수 있습니다.");
 }
 private void DeferPendingUpdate(){
  var pending=pendingInstall;pendingInstall=null;waitingInstallPermission=false;
  if(pending is not{} p)return;
  SecureState.Update(s=>{if(s.Session?.Token==p.Session.Token){s.UpdateDeferredAccount=NotificationPolicy.AccountKey(p.Session);s.UpdateDeferredVersion=p.Release.VersionCode;}});
 }
 private void ResumeUpdateInstall(){
  if(!waitingInstallPermission)return;waitingInstallPermission=false;
  Run(async()=>{if(PackageManager!.CanRequestPackageInstalls())await CheckUpdateAsync(true);else{DeferPendingUpdate();Notice("설치를 취소했습니다. 채팅을 계속 사용하고 메뉴에서 업데이트를 다시 시도할 수 있습니다.");}});
 }
}
