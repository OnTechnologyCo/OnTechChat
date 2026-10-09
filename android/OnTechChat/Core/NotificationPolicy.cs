using System.Globalization;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;

namespace OnTechChat;

public readonly record struct NotificationText(string Title, string Body);
public static class NotificationPolicy
{
    public static bool MessageID(string? tag, out long id)
    {
        id = 0;
        if (tag is null || !tag.StartsWith("message-", StringComparison.Ordinal)) return false;
        var digits = tag[8..];
        return digits.Length is > 0 and <= 19 && digits.All(c => c is >= '0' and <= '9') &&
            long.TryParse(digits, NumberStyles.None, CultureInfo.InvariantCulture, out id) && id > 0;
    }
    public static HashSet<long> ConfirmedDismissals(JsonElement response, IEnumerable<long> requested)
    {
        if (!response.TryGetProperty("version", out var version) || !version.TryGetInt32(out int n) || n != 1 ||
            !response.TryGetProperty("dismiss_message_ids", out var ids) || ids.ValueKind != JsonValueKind.Array || ids.GetArrayLength() > 64)
            throw new IOException("알림 읽음 확인 응답 형식이 맞지 않습니다.");
        var allowed = requested.ToHashSet(); var result = new HashSet<long>();
        foreach (var item in ids.EnumerateArray())
            if (item.TryGetInt64(out long id) && id > 0 && allowed.Contains(id)) result.Add(id);
            else throw new IOException("요청하지 않은 알림 ID를 반환했습니다.");
        return result;
    }
    public static string AccountKey(LoginSession s) => Convert.ToHexString(SHA256.HashData(
        Encoding.UTF8.GetBytes(UrlPolicy.Origin(s.Origin).AbsoluteUri + "|" + s.UserId)));
    public static string Clean(string? text, int limit)
    {
        var filtered = new StringBuilder();
        foreach (var rune in (text ?? "").EnumerateRunes())
            filtered.Append(Rune.GetUnicodeCategory(rune) is UnicodeCategory.Control or UnicodeCategory.Format ? " " : rune.ToString());
        var value = string.Join(' ', filtered.ToString().Split((char[]?)null, StringSplitOptions.RemoveEmptyEntries));
        var runes = value.EnumerateRunes().ToArray();
        return runes.Length > limit ? string.Concat(runes.Take(limit).Select(r => r.ToString())) + "…" : value;
    }
    public static NotificationText Display(string kind, bool preview, string? title, string? body, bool test = false)
    {
        if (test) return new("로컬 알림 테스트", "설정한 소리·진동 확인용입니다. 서버 FCM 시험은 아닙니다.");
        if(kind=="location_request")return new("위치 확인 요청","다른 사용자가 현재 위치를 요청했습니다. 앱에서 승인하거나 거절할 수 있습니다.");
        if(kind=="location")return new("위치 응답 도착","동의한 위치가 도착했습니다. 앱에서 확인하세요.");
        if (kind == "auth") return new("온테크 인증함", "로그인 인증번호가 도착했습니다. 앱에서 확인해 주세요.");
        if (preview && (kind is "text" or "file") && !string.IsNullOrWhiteSpace(body))
            return new(string.IsNullOrWhiteSpace(title) ? "온테크 채팅" : Clean(title, 40), Clean(body, 100));
        return new("온테크 채팅", "새 채팅 메시지가 도착했습니다.");
    }
}
