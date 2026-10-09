using System.Net;
using System.Net.Http.Headers;
using System.Text;
using System.Text.Json;

namespace OnTechChat;

public sealed class ApiException : Exception
{
    public HttpStatusCode Status { get; }
    public string Code { get; }
    public bool SessionInvalid => Status == HttpStatusCode.Unauthorized && Code == "SESSION_INVALID";
    public ApiException(HttpStatusCode status, string message, string code = "") : base(message) { Status = status; Code = code; }
}
public sealed class ChatApi : IDisposable
{
    public const long MaxDownload = 100L * 1024 * 1024;
    private readonly HttpClient http;
    public Uri Origin { get; }
    public ChatApi(Uri origin, string? token = null, HttpMessageHandler? handler = null)
    {
        Origin = UrlPolicy.Origin(origin.AbsoluteUri);
        // 인증 헤더가 다른 서버로 따라가지 않도록 자동 리디렉션을 금지합니다.
        http = new HttpClient(handler ?? new HttpClientHandler { AllowAutoRedirect = false, UseCookies = false });
        http.BaseAddress = Origin; http.Timeout = TimeSpan.FromSeconds(12);
        http.DefaultRequestHeaders.Add("X-OnTech-Request", "1");
        http.DefaultRequestHeaders.UserAgent.ParseAdd("OnTechChat-Android/1.7.0");
        if (!string.IsNullOrEmpty(token)) http.DefaultRequestHeaders.Authorization = new AuthenticationHeaderValue("Bearer", token);
    }
    public async Task<JsonElement> JsonAsync(string path, HttpMethod method, object? payload = null, CancellationToken ct = default)
    {
        if (!path.StartsWith("/api/", StringComparison.Ordinal) && path != "/healthz") throw new ArgumentException("Unsupported API path");
        // ResponseHeadersRead 뒤의 본문 읽기도 같은 제한 시간에 포함합니다.
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(ct);
        timeout.CancelAfter(TimeSpan.FromSeconds(20)); ct = timeout.Token;
        using var request = new HttpRequestMessage(method, path);
        if (payload is not null) request.Content = new StringContent(JsonSerializer.Serialize(payload), Encoding.UTF8, "application/json");
        using var response = await http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, ct);
        if ((int)response.StatusCode is >= 300 and < 400)
            throw new ApiException(response.StatusCode, "다른 주소로 이동하는 응답입니다. 브라우저에서 최종 HTTPS 주소를 확인해 입력하세요. 인증정보를 다른 주소로 자동 전달하지 않습니다.", "HTTP_REDIRECT");
        await using var input = await response.Content.ReadAsStreamAsync(ct);
        using var output = new MemoryStream();
        var buffer = new byte[8192]; int n;
        while ((n = await input.ReadAsync(buffer, ct)) > 0)
        {
            if (output.Length + n > 2 * 1024 * 1024) throw new IOException("서버 응답이 너무 큽니다.");
            await output.WriteAsync(buffer.AsMemory(0, n), ct);
        }
        JsonElement data;
        try { using var document = JsonDocument.Parse(output.ToArray()); data = document.RootElement.Clone(); }
        catch (JsonException) {
            var code=(int)response.StatusCode;
            string message=code is 502 or 503 or 504 ? "프록시가 채팅 서버에 연결하지 못했습니다. 서버 서비스와 프록시 대상 127.0.0.1:8787을 확인하세요." :
                code==404 ? "채팅 서버의 /healthz 또는 API 경로가 없습니다. 주소·포트가 다른 웹사이트를 가리키는지 확인하세요." :
                code is 401 or 403 ? "서버 앞단에서 접근을 차단했습니다. 프록시·접근 정책과 외부 HTTPS 주소를 확인하세요." :
                "채팅 서버 JSON이 아닌 화면이 응답했습니다. 공유기·다른 웹사이트·별도 로그인 페이지로 연결되는지 확인하세요.";
            throw new ApiException(response.StatusCode,message,code>=400 ? "HTTP_"+code : "NON_JSON_RESPONSE");
        }
        if (!response.IsSuccessStatusCode)
        {
            var message = data.ValueKind == JsonValueKind.Object && data.TryGetProperty("error", out var error) ? error.GetString() : null;
            string code = data.ValueKind == JsonValueKind.Object && data.TryGetProperty("code", out var c) && c.ValueKind == JsonValueKind.String ? c.GetString() ?? "" : "";
            throw new ApiException(response.StatusCode, (message ?? $"서버 요청 실패: {(int)response.StatusCode}")[..Math.Min((message ?? $"서버 요청 실패: {(int)response.StatusCode}").Length, 300)], code);
        }
        return data;
    }
    public static string ValidateHealth(JsonElement health)
    {
        if (health.ValueKind != JsonValueKind.Object || !health.TryGetProperty("ok", out var ok) || ok.ValueKind != JsonValueKind.True)
            throw new ApiException(HttpStatusCode.OK,"응답은 왔지만 채팅 서버의 정상 상태 응답이 아닙니다.","INVALID_HEALTH");
        if (health.TryGetProperty("service", out var service) && (service.ValueKind != JsonValueKind.String || service.GetString() != "ontech-chat"))
            throw new ApiException(HttpStatusCode.OK,"다른 서비스의 응답입니다. OnTech Chat 서버 주소를 확인하세요.","WRONG_SERVER");
        var version = health.TryGetProperty("version", out var v) && v.ValueKind == JsonValueKind.String ? v.GetString() ?? "" : "";
        return System.Text.RegularExpressions.Regex.IsMatch(version,@"^[0-9A-Za-z._-]{1,32}$") ? version : "버전 정보 없음";
    }
    public async Task<LoginSession> LoginAsync(string username, string password, CancellationToken ct = default)
    {
        var health = await JsonAsync("/healthz", HttpMethod.Get, ct: ct);
        ValidateHealth(health);
        var json = await JsonAsync("/api/login", HttpMethod.Post, new { username, password, client = "native" }, ct);
        var user = json.GetProperty("user");
        var token = json.GetProperty("token").GetString() ?? "";
        if (token.Length < 24 || token.Length > 256 || token.Any(c => !char.IsAsciiLetterOrDigit(c) && c != '-' && c != '_'))
            throw new IOException("서버 로그인 토큰 형식 오류");
        return new LoginSession(Origin.AbsoluteUri, token, user.GetProperty("username").GetString() ?? username,
            user.GetProperty("id").GetString() ?? "", json.GetProperty("expires_at").GetInt64());
    }
    public async Task<bool> RegisterDeviceAsync(string device, string name, string token, CancellationToken ct = default)
    {
        var json = await JsonAsync("/api/devices", HttpMethod.Post, new { id = device, name, token, notification_delivery = "app" }, ct);
        return json.TryGetProperty("push_enabled", out var p) && p.GetBoolean();
    }
    public async Task LogoutAsync(string device, CancellationToken ct = default) =>
        _ = await JsonAsync("/api/logout", HttpMethod.Post, new { device_id = device }, ct);
    public async Task<(string Name, string Mime)> DownloadAsync(Uri file, string destination, CancellationToken ct)
    {
        if (!UrlPolicy.IsFile(Origin, file.AbsoluteUri, out _)) throw new IOException("이 채팅 서버의 첨부파일만 받을 수 있습니다.");
        using var request = new HttpRequestMessage(HttpMethod.Get, file);
        using var response = await http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, ct);
        if (!response.IsSuccessStatusCode) throw new ApiException(response.StatusCode, "파일에 접근할 수 없습니다. 로그인·채팅방 참여 상태를 확인하세요.");
        if (response.Content.Headers.ContentLength > MaxDownload) throw new IOException("앱의 다운로드 제한은 100 MB입니다.");
        var name = response.Content.Headers.ContentDisposition?.FileNameStar ?? response.Content.Headers.ContentDisposition?.FileName;
        var mime = response.Content.Headers.ContentType?.MediaType ?? "application/octet-stream";
        await using var input = await response.Content.ReadAsStreamAsync(ct);
        try
        {
            await using var output = new FileStream(destination, FileMode.CreateNew, FileAccess.Write, FileShare.None, 65536, true);
            byte[] b = new byte[65536]; long total = 0; int n;
            while ((n = await input.ReadAsync(b, ct)) > 0)
            {
                total += n; if (total > MaxDownload) throw new IOException("앱의 다운로드 제한은 100 MB입니다.");
                await output.WriteAsync(b.AsMemory(0, n), ct);
            }
        }
        catch { if (File.Exists(destination)) File.Delete(destination); throw; }
        return (UrlPolicy.SafeFileName(name), mime);
    }
    public async Task DownloadUpdateAsync(UpdateRelease release,string destination,CancellationToken ct)
    {
        if(release.DownloadPath!="/api/app-updates/android/"+release.SHA256+".apk")throw new IOException("허용된 APK 경로가 아닙니다.");
        using var timer=CancellationTokenSource.CreateLinkedTokenSource(ct);timer.CancelAfter(TimeSpan.FromMinutes(5));ct=timer.Token;
        using var request=new HttpRequestMessage(HttpMethod.Get,release.DownloadPath);
        using var response=await http.SendAsync(request,HttpCompletionOption.ResponseHeadersRead,ct);
        if(response.StatusCode!=HttpStatusCode.OK)throw new ApiException(response.StatusCode,"업데이트 다운로드 실패. 로그인과 배포 상태를 확인하세요.");
        if(response.Content.Headers.ContentLength is {} length&&length!=release.Size)throw new IOException("배포 크기와 응답 크기가 다릅니다.");
        await using var input=await response.Content.ReadAsStreamAsync(ct);
        try {
            await using var output=new FileStream(destination,FileMode.CreateNew,FileAccess.Write,FileShare.None,65536,true);
            using var hash=System.Security.Cryptography.IncrementalHash.CreateHash(System.Security.Cryptography.HashAlgorithmName.SHA256);
            var buffer=new byte[65536];long total=0;int n;
            while((n=await input.ReadAsync(buffer,ct))>0){total+=n;if(total>release.Size||total>UpdateRelease.MaxBytes)throw new IOException("APK 크기 제한 초과");hash.AppendData(buffer,0,n);await output.WriteAsync(buffer.AsMemory(0,n),ct);}
            if(total!=release.Size||Convert.ToHexString(hash.GetHashAndReset()).ToLowerInvariant()!=release.SHA256)throw new IOException("APK 크기 또는 SHA256 검증 실패");
        }catch{if(File.Exists(destination))File.Delete(destination);throw;}
    }
    public void Dispose() => http.Dispose();
}
