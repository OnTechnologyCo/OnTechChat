using System.Net;
using System.Text.RegularExpressions;

namespace OnTechChat;

// 사용자 입력 주소를 한 HTTPS 원점으로 제한합니다. 토큰을 URL로 전달하지 않습니다.
public static class UrlPolicy
{
    public static Uri Origin(string input)
    {
        var value = input.Trim();
        if (!value.Contains("://", StringComparison.Ordinal))
        {
            // 접두사 없는 도메인/IP만 HTTPS로 보완합니다. HTTP나 임의 경로는 자동 우회하지 않습니다.
            if (!Regex.IsMatch(value, @"^(?:[^:\s/?#@\\]+|\[[0-9a-fA-F:.]+\])(?::[0-9]{1,5})?/?$"))
                throw new ArgumentException("서버 주소를 입력하세요. 예: chat.example.com 또는 https://공인IP:포트");
            value = "https://" + value;
        }
        if (value.StartsWith("http://", StringComparison.OrdinalIgnoreCase))
            throw new ArgumentException("HTTP 주소는 LTE·5G 로그인에 사용할 수 없습니다. 서버 PC의 127.0.0.1:8787은 내부 HTTP 주소입니다. 외부 HTTPS 주소와 인증서를 먼저 구성하세요.");
        if (!Uri.TryCreate(value, UriKind.Absolute, out var uri) || uri.Scheme != Uri.UriSchemeHttps ||
            string.IsNullOrEmpty(uri.Host) || uri.Port < 1 || uri.UserInfo.Length != 0 || uri.Query.Length != 0 ||
            uri.Fragment.Length != 0 || (uri.AbsolutePath != "/" && uri.AbsolutePath != "") ||
            value.Any(char.IsControl) || value.Contains('\\'))
            throw new ArgumentException("서버의 HTTPS 주소만 입력하세요. 예: https://chat.example.com");
        var host = uri.Host.Trim('[', ']');
        if (host.Equals("localhost", StringComparison.OrdinalIgnoreCase) ||
            (IPAddress.TryParse(host, out var ip) && (IPAddress.IsLoopback(ip) || (ip.IsIPv4MappedToIPv6 && IPAddress.IsLoopback(ip.MapToIPv4())) || ip.Equals(IPAddress.Any) || ip.Equals(IPAddress.IPv6Any))))
            throw new ArgumentException("127.0.0.1/localhost는 이 휴대전화 자신입니다. 외부에서 접속 가능한 서버 주소를 입력하세요.");
        return new Uri(uri.GetLeftPart(UriPartial.Authority) + "/");
    }
    public static bool SameOrigin(Uri origin, Uri uri) => uri.Scheme == "https" && uri.UserInfo.Length == 0 &&
        string.Equals(origin.IdnHost, uri.IdnHost, StringComparison.OrdinalIgnoreCase) && origin.Port == uri.Port;
    // Only the fixed public background-map resources may leave the server origin.
    // Fallback rendering scripts stay local. NAVER has a separate, explicit policy below.
    public static bool IsMapTile(string? target, string? method, bool isMainFrame)
    {
        if (isMainFrame || method != "GET" || target is null || target.Contains('\\') || target.Any(char.IsControl) ||
            !Uri.TryCreate(target, UriKind.Absolute, out var uri) || uri.Scheme != "https" ||
            uri.Port != 443 || uri.UserInfo.Length != 0 || uri.Query.Length != 0 || uri.Fragment.Length != 0) return false;
        if (uri.IdnHost == "tile.openstreetmap.org")
            return MapCoordinates(Regex.Match(uri.AbsolutePath, @"^/([0-9]{1,2})/([0-9]{1,6})/([0-9]{1,6})\.png\z", RegexOptions.CultureInvariant));
        if (uri.IdnHost != "tiles.openfreemap.org") return false;
        if (uri.AbsolutePath == "/planet") return true;
        var tile = Regex.Match(uri.AbsolutePath, @"^/planet/[0-9]{8}_[0-9]{6}_pt/([0-9]{1,2})/([0-9]{1,6})/([0-9]{1,6})\.pbf\z", RegexOptions.CultureInvariant);
        if (tile.Success) return MapCoordinates(tile);
        var font = Regex.Match(uri.AbsolutePath, @"^/fonts/Noto%20Sans%20(?:Regular|Italic|Bold)/([0-9]{1,7})-([0-9]{1,7})\.pbf\z", RegexOptions.CultureInvariant);
        if (!font.Success) return false;
        int first = int.Parse(font.Groups[1].Value), last = int.Parse(font.Groups[2].Value);
        return first % 256 == 0 && last == first + 255 && last <= 1114111;
    }
    // NAVER Dynamic Map: HTTPS GET subresources only, exact hosts and map paths.
    // Never attach the chat token/cookies or permit navigation to these resources.
    public static bool IsNaverMapResource(string? target, string? method, bool isMainFrame)
    {
        if (isMainFrame || method != "GET" || target is null || target.Contains('\\') || target.Any(char.IsControl) ||
            !Uri.TryCreate(target, UriKind.Absolute, out var uri) || uri.Scheme != "https" ||
            uri.Port != 443 || uri.UserInfo.Length != 0 || uri.Fragment.Length != 0) return false;
        var path = uri.AbsolutePath;
        if (uri.IdnHost == "oapi.map.naver.com")
        {
            if (path == "/openapi/v3/maps.js")
                return Regex.IsMatch(uri.Query, @"^\?ncpKeyId=[a-zA-Z0-9]+&callback=onTechNaverMapReady&language=ko\z", RegexOptions.CultureInvariant);
            if (path == "/v3/auth")
                return Regex.IsMatch(uri.Query, @"^\?ncpKeyId=[a-zA-Z0-9]+&url=[^&]+&time=[0-9]+&callback=__naver_maps_callback__[0-9]+\z", RegexOptions.CultureInvariant);
            return false;
        }
        if (uri.IdnHost == "ssl.pstatic.net")
            return uri.Query.Length == 0 && Regex.IsMatch(path, @"^/static/maps/mantle/[12]x/[a-zA-Z0-9_/-]+\.(?:png|gif|svg|cur)\z", RegexOptions.CultureInvariant);
        string prefix;
        if (uri.IdnHost == "map.pstatic.net") prefix = "/nrb/styles/";
        else if (uri.IdnHost == "nrbe.pstatic.net") prefix = "/styles/";
        else return false;
        if (!path.StartsWith(prefix, StringComparison.Ordinal)) return false;
        path = path[prefix.Length..];
        if (Regex.IsMatch(path, @"^(?:basic|terrain|satellite|dbasic|dterrain)(?:@2x)?\.json\z", RegexOptions.CultureInvariant))
            return Regex.IsMatch(uri.Query, @"^\?fmt=png(?:&callback=__naver_maps_callback__[0-9]+)?\z", RegexOptions.CultureInvariant);
        var tile = Regex.Match(path, @"^(?:basic|terrain|satellite|dbasic|dterrain)/[0-9]+/([0-9]{1,2})/([0-9]{1,7})/([0-9]{1,7})(?:@2x)?\.png\z", RegexOptions.CultureInvariant);
        if (!tile.Success || (uri.Query.Length != 0 && !Regex.IsMatch(uri.Query, @"^\?mt=[a-z.]+\z", RegexOptions.CultureInvariant))) return false;
        int z = int.Parse(tile.Groups[1].Value), x = int.Parse(tile.Groups[2].Value), y = int.Parse(tile.Groups[3].Value);
        return z <= 21 && x < (1 << z) && y < (1 << z);
    }
    private static bool MapCoordinates(Match tile)
    {
        if (!tile.Success) return false;
        int z = int.Parse(tile.Groups[1].Value), x = int.Parse(tile.Groups[2].Value), y = int.Parse(tile.Groups[3].Value);
        return z <= 19 && x < (1 << z) && y < (1 << z);
    }
    public static bool ValidID(string? id) => id is not null && Regex.IsMatch(id, "^[a-zA-Z0-9_-]{8,100}$", RegexOptions.CultureInvariant);
    public static bool IsFile(Uri origin, string? target, out Uri? file)
    {
        file = null;
        if (!Uri.TryCreate(target, UriKind.Absolute, out var uri) || !SameOrigin(origin, uri) ||
            !Regex.IsMatch(uri.AbsolutePath, "^/api/files/[a-zA-Z0-9_-]{8,100}$") ||
            (uri.Query != "" && uri.Query != "?inline=1") || uri.Fragment.Length != 0) return false;
        file = uri; return true;
    }
    public static string SafeFileName(string? name)
    {
        name = (name ?? "첨부파일").Trim('"', ' ', '.');
        name = Regex.Replace(name, "[\\\\/:*?\"<>|\\x00-\\x1f\\x7f]", "_");
        if (name.Length > 160) name = name[..160];
        return string.IsNullOrWhiteSpace(name) ? "첨부파일" : name;
    }
}
public sealed record LoginSession(string Origin, string Token, string Username, string UserId, long ExpiresAt)
{
    // 유효 여부는 서버가 판단합니다. 서버 재시작/기기 시각/이전 만료 시각으로 로컬 로그인을 지우지 않습니다.
    public bool Active => !string.IsNullOrWhiteSpace(Token);
}
public sealed class LocalState
{
    public bool HistoryEnabled { get; set; }
    public bool HistoryShare { get; set; }
    public string HistoryAccount { get; set; } = "";
    public string HistoryRevision { get; set; } = "";
    public bool HistoryRevokePending { get; set; }
    public bool HistoryDeletePending { get; set; }
    public long HistoryLastCaptured { get; set; }
    public long HistoryLastSynced { get; set; }
    public List<HistoryPoint> HistoryQueue { get; set; } = [];
    public string Origin { get; set; } = "";
    public string DeviceId { get; set; } = "android-" + Guid.NewGuid().ToString("N");
    public LoginSession? Session { get; set; }
    public string FcmToken { get; set; } = "";
    public string RegistrationKey { get; set; } = "";
    public bool NotificationPrompted { get; set; }
    public bool NotificationPreview { get; set; }
    public string NotificationPreferenceAccount { get; set; } = "";
    public long DeviceRegisteredAt { get; set; }
    public string UpdateDeferredAccount { get; set; } = "";
    public long UpdateDeferredVersion { get; set; }
    public long LastUpdateCheck { get; set; }
    public string LastUpdateOrigin { get; set; } = "";
    public bool LocationSharingEnabled { get; set; }
    public bool LocationShareAllFriends { get; set; }
    public string[] LocationAllowedUsers { get; set; } = [];
    public string LocationGrantRevision { get; set; } = "";
    public string LocationSharingAccount { get; set; } = "";
    public long LocationSharingLastSent { get; set; }
    public bool LocationSharingRevocationPending { get; set; }
}
