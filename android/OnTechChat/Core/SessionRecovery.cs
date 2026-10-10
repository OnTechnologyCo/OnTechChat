namespace OnTechChat;

public sealed class SessionRecoveryException(string code, string message, Exception? inner = null) : IOException(message, inner)
{
    public string Code { get; } = code;
}

public static class WebSessionCookie
{
    public static bool Matches(string? header, string token)
    {
        if (string.IsNullOrEmpty(header) || string.IsNullOrEmpty(token)) return false;
        int count = 0;
        bool matches = false;
        foreach (var part in header.Split(';'))
        {
            var cookie = part.Trim();
            var equals = cookie.IndexOf('=');
            if (equals < 0 || cookie[..equals] != "ontech_session") continue;
            count++;
            matches = string.Equals(cookie[(equals + 1)..], token, StringComparison.Ordinal);
        }
        return count == 1 && matches;
    }
}

public static class SessionRecovery
{
    public static bool NeedsWebRecovery(Uri origin, string? requestUrl, string? method, int? status)
    {
        return status == 401 && method == "GET" &&
            Uri.TryCreate(requestUrl, UriKind.Absolute, out var url) &&
            UrlPolicy.SameOrigin(origin, url) && url.AbsolutePath.StartsWith("/api/", StringComparison.Ordinal);
    }

    public static async Task ConfirmCookieAsync(Task<bool> saved, CancellationToken ct,
        TimeSpan? timeout = null, Func<bool>? stored = null)
    {
        bool accepted;
        try { accepted = await saved.WaitAsync(timeout ?? TimeSpan.FromSeconds(8), ct); }
        catch (TimeoutException error)
        {
            ct.ThrowIfCancellationRequested();
            // Java 콜백이 늦더라도 실제 쿠키가 저장됐으면 안전하게 진행할 수 있습니다.
            if (stored?.Invoke() == true) return;
            throw new SessionRecoveryException("WEB_COOKIE_TIMEOUT",
                "웹 로그인 쿠키 저장 응답이 지연되었습니다. 로그인은 유지하며 자동으로 재시도합니다.", error);
        }
        ct.ThrowIfCancellationRequested();
        if (stored is not null) accepted = stored();
        if (!accepted)
            throw new SessionRecoveryException("WEB_COOKIE_MISSING",
                "저장된 로그인과 웹 쿠키가 일치하지 않습니다. 로그인 정보는 유지하며 다시 연결합니다.");
    }
}
