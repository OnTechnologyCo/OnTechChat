using Android.App;
using Android.Content;
using Android.Graphics;
using Android.Net.Http;
using Android.Webkit;
using Android.Widget;
using System.Text;

namespace OnTechChat;

internal sealed class ChatWebClient(MainActivity owner) : WebViewClient
{
    private static WebResourceResponse Denied() => new("text/plain", "utf-8", 403, "Forbidden", new Dictionary<string, string>(), new MemoryStream(Encoding.UTF8.GetBytes("요청이 허용되지 않습니다.")));
    public override bool ShouldOverrideUrlLoading(WebView? view, IWebResourceRequest? request)
    {
        var raw = request?.Url?.ToString();
        if(raw?.StartsWith("ontechchat://",StringComparison.Ordinal)==true){
            if(request?.IsForMainFrame==true&&request.HasGesture&&owner.ServerOrigin is {} trusted&&
               Uri.TryCreate(view?.Url,UriKind.Absolute,out var page)&&UrlPolicy.SameOrigin(trusted,page)&&page.AbsolutePath=="/"){
                if(raw=="ontechchat://notification-settings")owner.RunOnUiThread(owner.ShowNotificationOptions);
                else if(raw=="ontechchat://app-update")owner.RunOnUiThread(owner.BeginUpdateCheck);
                else if(raw=="ontechchat://location-sharing-settings")owner.RunOnUiThread(owner.ShowLocationHistorySettings);
                else if(raw=="ontechchat://location-history-settings")owner.RunOnUiThread(owner.ShowLocationHistorySettings);
                else if(System.Text.RegularExpressions.Regex.Match(raw,@"^ontechchat://open-location\?request=([a-zA-Z0-9_-]{8,100})\z") is {Success:true} map)owner.RunOnUiThread(()=>owner.OpenLocationMap(map.Groups[1].Value));
                else if(NativeFeaturePolicy.LocationTarget(raw,out var type,out var id))owner.RunOnUiThread(()=>owner.BeginShareLocation(type,id));
            }
            return true;
        }
        if (raw == "about:blank") return false;
        if (!Uri.TryCreate(raw, UriKind.Absolute, out var url) || owner.ServerOrigin is not { } origin) return true;
        if (UrlPolicy.SameOrigin(origin, url))
        {
            if (request?.IsForMainFrame == true && UrlPolicy.IsFile(origin, raw, out var file))
            { owner.StartDownload(file!.AbsoluteUri); return true; }
            return false;
        }
        if (request?.IsForMainFrame == true && request.HasGesture && url.Scheme == "https" && url.UserInfo == "")
            owner.RunOnUiThread(() => new AlertDialog.Builder(owner)!.SetTitle("외부 링크")!.SetMessage($"채팅 앱 밖의 브라우저로 이동할까요?\n{url.GetLeftPart(UriPartial.Authority)}")!
                .SetNegativeButton("취소", (_, _) => { })!.SetPositiveButton("브라우저로 열기", (_, _) =>
                {
                    try { owner.StartActivity(new Intent(Intent.ActionView, Android.Net.Uri.Parse(url.AbsoluteUri))); }
                    catch (ActivityNotFoundException) { owner.Notice("링크를 열 브라우저가 없습니다."); }
                })!.Show());
        return true;
    }
    public override WebResourceResponse? ShouldInterceptRequest(WebView? view, IWebResourceRequest? request)
    {
        string raw = request?.Url?.ToString() ?? "";
        if (raw == "about:blank") return null;
        var origin = owner.ServerOrigin;
        if (origin is null) return Denied();
        if (raw.StartsWith("blob:" + origin.GetLeftPart(UriPartial.Authority) + "/", StringComparison.Ordinal)) return null;
        if (request is not null && UrlPolicy.IsMapTile(raw, request.Method, request.IsForMainFrame)) return null;
        if (!Uri.TryCreate(raw, UriKind.Absolute, out var uri) || !UrlPolicy.SameOrigin(origin, uri)) return Denied();
        if(request?.Method=="POST"&&System.Text.RegularExpressions.Regex.IsMatch(uri.AbsolutePath,@"^/api/rooms/[a-zA-Z0-9_-]{8,100}/read\z"))owner.RunOnUiThread(owner.QueueNotificationCheck);
        if (uri.AbsolutePath == "/api/login")
        {
            owner.Notice("저장된 로그인으로 다시 연결하고 있습니다. 메뉴에서 새로고침을 누르세요.");
            return Denied();
        }
        if (uri.AbsolutePath == "/api/logout" && request?.Method == "POST")
        {
            bool revoked = PushCoordinator.LogoutAsync().GetAwaiter().GetResult();
            owner.RunOnUiThread(() => owner.AfterWebLogout(revoked));
            return new WebResourceResponse("application/json", "utf-8", new MemoryStream(Encoding.UTF8.GetBytes("{\"ok\":true}")));
        }
        return null;
    }
    public override void OnReceivedSslError(WebView? view, SslErrorHandler? handler, SslError? error)
    { handler?.Cancel(); owner.Notice("진단 TLS_FAILURE · 서버 인증서 검증 실패. IP/도메인과 인증서 이름, 유효기간, 중간 인증서를 확인하세요. 검사를 건너뛰지 않습니다."); }
    public override void OnReceivedError(WebView? view, IWebResourceRequest? request, WebResourceError? error)
    { if (request?.IsForMainFrame == true) { owner.ReconnectWebPending = true; owner.UpdatePageFailed(); owner.Notice($"웹 화면 접속 실패 · WebView 코드 {error?.ErrorCode}. 서버 주소·HTTPS 포트를 확인하고 메뉴에서 새로고침하세요."); } }
    public override void OnReceivedHttpError(WebView? view, IWebResourceRequest? request, WebResourceResponse? response)
    { if(request?.IsForMainFrame==true){owner.ReconnectWebPending = true; owner.UpdatePageFailed();owner.Notice($"웹 화면 HTTP {response?.StatusCode} · 서버 외부 주소, 프록시 Host 설정과 서비스 실행 상태를 확인하세요.");} }
    public override void OnPageStarted(WebView? view, string? url, Bitmap? favicon)
    { owner.UpdatePageStarted(); }
    public override void OnPageFinished(WebView? view, string? url)
    { if (view is not null) CookieManager.Instance?.Flush(); owner.QueueNotificationCheck(); owner.UpdatePageFinished(url); }
}

internal sealed class ChatChrome(MainActivity owner) : WebChromeClient
{
    public override bool OnShowFileChooser(WebView? webView, IValueCallback? callback, FileChooserParams? parameters)
    {
        if (callback is null) return false;
        owner.ChooseUpload(callback, parameters?.GetAcceptTypes(), parameters?.Mode==ChromeFileChooserMode.OpenMultiple); return true;
    }
    public override void OnPermissionRequest(PermissionRequest? request) => request?.Deny();
    public override bool OnJsAlert(WebView? view, string? url, string? message, JsResult? result)
    {
        owner.RunOnUiThread(() => new AlertDialog.Builder(owner)!.SetMessage(message)!.SetPositiveButton("확인", (_, _) => result?.Confirm())!.SetCancelable(false)!.Show()); return true;
    }
    public override bool OnJsConfirm(WebView? view, string? url, string? message, JsResult? result)
    {
        owner.RunOnUiThread(() => new AlertDialog.Builder(owner)!.SetMessage(message)!.SetPositiveButton("확인", (_, _) => result?.Confirm())!
            .SetNegativeButton("취소", (_, _) => result?.Cancel())!.SetCancelable(false)!.Show()); return true;
    }
    public override bool OnJsPrompt(WebView? view, string? url, string? message, string? defaultValue, JsPromptResult? result)
    {
        owner.RunOnUiThread(() =>
        {
            var input = new EditText(owner) { Text = defaultValue, InputType = Android.Text.InputTypes.ClassText | Android.Text.InputTypes.TextVariationPassword };
            new AlertDialog.Builder(owner)!.SetMessage(message)!.SetView(input)!.SetPositiveButton("확인", (_, _) => { var v = input.Text ?? ""; input.Text = ""; result?.Confirm(v); })!
                .SetNegativeButton("취소", (_, _) => { input.Text = ""; result?.Cancel(); })!.SetCancelable(false)!.Show();
        }); return true;
    }
}
internal sealed class ChatDownload(MainActivity owner) : Java.Lang.Object, IDownloadListener
{
    public void OnDownloadStart(string? url, string? userAgent, string? contentDisposition, string? mimetype, long contentLength)
        => owner.StartDownload(url ?? "");
}
