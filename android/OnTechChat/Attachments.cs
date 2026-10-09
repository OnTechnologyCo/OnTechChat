using Android.App;
using Android.Content;
using Android.Content.PM;
using Android.Runtime;
using Android.Webkit;
using AndroidX.Core.Content;
using AUri = Android.Net.Uri;

namespace OnTechChat;

[ContentProvider(["com.ontechdev.chat.files"], Exported = false, GrantUriPermissions = true)]
[MetaData("android.support.FILE_PROVIDER_PATHS", Resource = "@xml/file_paths")]
public sealed class ChatFileProvider : FileProvider { }

public sealed partial class MainActivity
{
    private const int UploadRequest = 41, SaveRequest = 42;
    private IValueCallback? uploadCallback;
    private Uri? selectionOrigin;
    private bool downloading;
    private string? savePath;
    private string? saveMime;
    internal void FinishPicker(AUri[]? result)
    {
        var callback = uploadCallback; uploadCallback = null; selectionOrigin = null;
        if (callback is null) return;
        if (result is null) callback.OnReceiveValue(null);
        else { using var array = new JavaArray<AUri>(JavaArray<AUri>.ToLocalJniHandle(result), JniHandleOwnership.TransferLocalRef); callback.OnReceiveValue(array); }
    }
#pragma warning disable CS0618, CS0672
    internal void ChooseUpload(IValueCallback callback, string[]? accepts, bool multiple)
    {
        RunOnUiThread(() =>
        {
            FinishPicker(null); uploadCallback = callback; selectionOrigin = ServerOrigin;
            if (ServerOrigin is null || Web is null) { FinishPicker(null); return; }
            try
            {
                using var intent = new Intent(Intent.ActionOpenDocument);
                intent.AddCategory(Intent.CategoryOpenable); intent.SetType("*/*");
                intent.PutExtra(Intent.ExtraAllowMultiple,multiple);
                var types = accepts?.Where(s => s.Contains('/') && !s.Contains('\n')).Take(10).ToArray();
                if (types?.Length > 0) intent.PutExtra(Intent.ExtraMimeTypes, types);
                intent.AddFlags(ActivityFlags.GrantReadUriPermission);
                StartActivityForResult(intent, UploadRequest);
            }
            catch (ActivityNotFoundException) { FinishPicker(null); Notice("파일 선택 앱을 찾지 못했습니다."); }
        });
    }
    protected override void OnActivityResult(int requestCode, Result resultCode, Intent? data)
    {
        base.OnActivityResult(requestCode, resultCode, data);
        if (requestCode == UploadRequest)
        {
            try
            {
                if(resultCode!=Result.Ok||selectionOrigin is null||ServerOrigin is null||!UrlPolicy.SameOrigin(selectionOrigin,ServerOrigin)){FinishPicker(null);return;}
                var uris=new List<AUri>();
                if(data?.ClipData is {} clip){
                    if(clip.ItemCount>4){FinishPicker(null);Notice("한 번에 파일4개까지 선택하세요.");return;}
                    for(int i=0;i<clip.ItemCount;i++)if(clip.GetItemAt(i)?.Uri is {} item)uris.Add(item);
                }else if(data?.Data is {} single)uris.Add(single);
                if(uris.Count==0){FinishPicker(null);return;}
                foreach(var uri in uris){
                    if(uri.Scheme!="content"){FinishPicker(null);return;}
                    // 모든 선택 파일에 기존 private provider/크기 검사를 적용합니다.
                    var provider=PackageManager?.ResolveContentProvider(uri.Authority!,PackageInfoFlags.MetaData);
                    if(provider?.ApplicationInfo?.Uid==Android.OS.Process.MyUid()){FinishPicker(null);Notice("앱 내부 파일은 첨부할 수 없습니다.");return;}
                    using var cursor=ContentResolver?.Query(uri,[Android.Provider.OpenableColumns.Size],null,null,null);
                    if(cursor?.MoveToFirst()==true&&!cursor.IsNull(0)&&cursor.GetLong(0)>ChatApi.MaxDownload){FinishPicker(null);Notice("파일은100MB까지 선택할 수 있습니다.");return;}
                }
                FinishPicker(uris.ToArray());
            }
            catch { FinishPicker(null); Notice("선택한 파일에 접근하지 못했습니다."); }
        }
        else if (requestCode == SaveRequest)
        {
            var uri = resultCode == Result.Ok ? data?.Data : null;
            var file = savePath; savePath = null;
            if (uri is null || file is null) return;
            Run(async () =>
            {
                using var output = ContentResolver?.OpenOutputStream(uri, "w") ?? throw new IOException("저장 위치를 열지 못했습니다.");
                await using var input = File.OpenRead(file); await input.CopyToAsync(output, Lifetime.Token);
                Notice("선택한 위치에 파일을 저장했습니다.");
            });
        }
    }
#pragma warning restore CS0618, CS0672
    internal void StartDownload(string raw)
    {
        RunOnUiThread(() => Run(async () =>
        {
            if (downloading) { Notice("다른 파일을 받는 중입니다."); return; }
            if (ServerOrigin is not { } origin || !UrlPolicy.IsFile(origin, raw, out var url)) throw new IOException("이 서버의 첨부파일 주소만 열 수 있습니다.");
            var session = SecureState.Read().Session;
            if (session is not { Active: true }) throw new IOException("로그인이 필요합니다.");
            downloading = true;
            string dir = Path.Combine(CacheDir!.AbsolutePath, "chat-downloads", Guid.NewGuid().ToString("N")); Directory.CreateDirectory(dir);
            string target = Path.Combine(dir, "content.part");
            try
            {
                Notice("첨부파일을 받는 중…");
                using var api = new ChatApi(origin, session.Token);
                using var timeout = CancellationTokenSource.CreateLinkedTokenSource(Lifetime.Token); timeout.CancelAfter(TimeSpan.FromMinutes(3));
                var result = await api.DownloadAsync(url!, target, timeout.Token);
                if (SecureState.Read().Session?.Token != session.Token) throw new IOException("로그인 상태가 변경되어 파일 열기를 취소했습니다.");
                string final = Path.Combine(dir, result.Name); if (final != target) File.Move(target, final);
                Notice("다운로드 완료 · " + result.Name);
                new AlertDialog.Builder(this)!.SetTitle(result.Name)!.SetMessage("앱 외부에 저장하거나 다른 앱으로 열면 해당 파일의 사본이 앱 밖에 남습니다.")!
                    .SetPositiveButton("파일 저장", (_, _) => SaveFile(final, result.Mime))!
                    .SetNeutralButton("다른 앱으로 열기", (_, _) => OpenFile(final, result.Mime))!
                    .SetNegativeButton("닫기", (_, _) => { })!.Show();
            }
            catch { if (Directory.Exists(dir)) Directory.Delete(dir, true); throw; }
            finally { downloading = false; }
        }));
    }
#pragma warning disable CS0618
    private void SaveFile(string file, string mime)
    {
        try
        {
            savePath = file; saveMime = mime;
            using var intent = new Intent(Intent.ActionCreateDocument);
            intent.AddCategory(Intent.CategoryOpenable); intent.SetType(mime); intent.PutExtra(Intent.ExtraTitle, Path.GetFileName(file));
            StartActivityForResult(intent, SaveRequest);
        }
        catch (ActivityNotFoundException) { savePath = null; Notice("파일 저장 앱을 찾지 못했습니다."); }
    }
#pragma warning restore CS0618
    private void OpenFile(string file, string mime)
    {
        try
        {
            var uri = FileProvider.GetUriForFile(this, PackageName + ".files", new Java.IO.File(file));
            using var intent = new Intent(Intent.ActionView); intent.SetDataAndType(uri, mime);
            intent.AddFlags(ActivityFlags.GrantReadUriPermission); intent.ClipData = ClipData.NewRawUri("첨부파일", uri);
            StartActivity(Intent.CreateChooser(intent, "첨부파일 열기"));
        }
        catch (ActivityNotFoundException) { Notice("파일 형식을 열 앱이 없습니다. 파일 저장을 사용하세요."); }
    }
    internal void AfterWebLogout(bool revoked)
    {
        CookieManager.Instance!.RemoveAllCookies(null); CookieManager.Instance.Flush(); WebStorage.Instance!.DeleteAllData();
        ShowLogin(revoked ? "로그아웃했습니다." : "앱 로그아웃 완료. 서버 기기 해제는 서버 관리 화면에서 확인하세요.");
    }
}
