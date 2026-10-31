using System.Net;
using System.Security.Cryptography;
using System.Text.Json;
using System.Text.RegularExpressions;

namespace OnTechChat;

// Separate public client: chat bearer tokens/cookies are never attached to GitHub requests.
public sealed class GitHubUpdates : IDisposable
{
    public const string API = "https://api.github.com/repos/OnTechnologyCo/OnTechChat/releases/latest";
    public const string ReleaseBase = "https://github.com/OnTechnologyCo/OnTechChat/releases/";
    private static readonly Regex Versions = new(@"^[0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6}\z");
    private static readonly Regex APKName = new(@"^OnTechChat-v([0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6})-arm64\.apk\z");
    private static readonly object CacheLock = new();
    private static byte[]? cachedLatest;
    private static DateTimeOffset cacheUntil;
    private readonly HttpClient http;
    private readonly bool useCache;
    private sealed record Asset(string Name, string URL, long Size, string Hash);

    public GitHubUpdates(HttpMessageHandler? handler = null)
    {
        useCache = handler is null;
        http = new HttpClient(handler ?? new HttpClientHandler { AllowAutoRedirect = false, UseCookies = false });
        http.Timeout = Timeout.InfiniteTimeSpan;
        http.DefaultRequestHeaders.UserAgent.ParseAdd("OnTechChat-Android-Updater/1");
    }
    public static bool IsNewer(string candidate, string installed) =>
        Versions.IsMatch(candidate) && Versions.IsMatch(installed) &&
        Version.TryParse(candidate, out var a) && Version.TryParse(installed, out var b) && a > b;

    public static bool IsAssetURI(Uri u)
    {
        if (u.Scheme != "https" || !u.IsDefaultPort || u.UserInfo.Length != 0 || u.Fragment.Length != 0) return false;
        if (u.Host == "github.com")
            return u.Query.Length == 0 && Regex.IsMatch(u.AbsolutePath,
                @"^/OnTechnologyCo/OnTechChat/releases/download/v[0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6}/(android-update\.json|OnTechChat-v[0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6}-arm64\.apk)\z");
        return u.Host is "release-assets.githubusercontent.com" or "objects.githubusercontent.com" or "github-releases.githubusercontent.com";
    }
    private async Task<HttpResponseMessage> OpenAsync(string url, bool asset, CancellationToken ct)
    {
        var uri = new Uri(url, UriKind.Absolute);
        for (var hop = 0; hop < 6; hop++)
        {
            if (asset ? !IsAssetURI(uri) : uri.AbsoluteUri != API) throw new IOException("허용된 GitHub 배포 주소가 아닙니다.");
            using var request = new HttpRequestMessage(HttpMethod.Get, uri);
            if (!asset)
            {
                request.Headers.Accept.ParseAdd("application/vnd.github+json");
                request.Headers.Add("X-GitHub-Api-Version", "2022-11-28");
            }
            var response = await http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, ct);
            if ((int)response.StatusCode is >= 300 and < 400)
            {
                var location = response.Headers.Location;
                response.Dispose();
                if (!asset || location is null) throw new IOException("허용되지 않은 GitHub 이동 응답입니다.");
                uri = location.IsAbsoluteUri ? location : new Uri(uri, location);
                continue;
            }
            if (response.StatusCode != HttpStatusCode.OK)
            {
                var status = (int)response.StatusCode; response.Dispose();
                throw new IOException($"GitHub 업데이트 조회/다운로드 실패 ({status}). 인터넷 연결 또는 릴리스 파일을 확인하세요.");
            }
            return response;
        }
        throw new IOException("GitHub 다운로드 이동 횟수를 초과했습니다.");
    }
    private async Task<byte[]> ReadSmallAsync(string url, bool asset, long limit, CancellationToken ct)
    {
        using var response = await OpenAsync(url, asset, ct);
        if (response.Content.Headers.ContentLength > limit) throw new IOException("업데이트 정보 크기 제한 초과");
        await using var input = await response.Content.ReadAsStreamAsync(ct);
        using var output = new MemoryStream();
        var buffer = new byte[8192]; int n;
        while ((n = await input.ReadAsync(buffer, ct)) > 0)
        {
            if (output.Length + n > limit) throw new IOException("업데이트 정보 크기 제한 초과");
            await output.WriteAsync(buffer.AsMemory(0, n), ct);
        }
        return output.ToArray();
    }
    private static Asset ReadAsset(JsonElement entry, string tag, long limit)
    {
        var name = entry.GetProperty("name").GetString() ?? "";
        var url = entry.GetProperty("browser_download_url").GetString() ?? "";
        var digest = entry.GetProperty("digest").GetString() ?? "";
        var size = entry.GetProperty("size").GetInt64();
        if (entry.GetProperty("state").GetString() != "uploaded" || size < 1 || size > limit ||
            !Regex.IsMatch(digest, @"^sha256:[0-9a-f]{64}\z") || url != ReleaseBase + "download/" + tag + "/" + name)
            throw new IOException("GitHub 배포 파일의 주소·크기·SHA256 정보가 올바르지 않습니다.");
        return new Asset(name, url, size, digest[7..]);
    }
    public async Task<UpdateRelease?> LatestAsync(string installedVersion, CancellationToken cancellationToken)
    {
        using var deadline = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        deadline.CancelAfter(TimeSpan.FromSeconds(25)); var ct = deadline.Token;
        try
        {
            byte[]? raw = null;
            if (useCache) lock (CacheLock) { if (DateTimeOffset.UtcNow < cacheUntil) raw = cachedLatest; }
            if (raw is null)
            {
                raw = await ReadSmallAsync(API, false, 1 << 20, ct);
                if (useCache) lock (CacheLock) { cachedLatest = raw; cacheUntil = DateTimeOffset.UtcNow.AddMinutes(10); }
            }
            using var doc = JsonDocument.Parse(raw); var data = doc.RootElement;
            var tag = data.GetProperty("tag_name").GetString() ?? "";
            if (!tag.StartsWith('v') || !Versions.IsMatch(tag[1..]) ||
                data.GetProperty("draft").GetBoolean() || data.GetProperty("prerelease").GetBoolean())
                throw new IOException("GitHub 정식 릴리스 정보가 아닙니다.");
            Asset? apk = null, manifest = null; string version = "";
            foreach (var item in data.GetProperty("assets").EnumerateArray())
            {
                var name = item.GetProperty("name").GetString() ?? "";
                var match = APKName.Match(name);
                if (match.Success)
                {
                    if (apk is not null) throw new IOException("릴리스에 APK가 둘 이상 있습니다.");
                    apk = ReadAsset(item, tag, UpdateRelease.MaxBytes); version = match.Groups[1].Value;
                }
                if (name == "android-update.json")
                {
                    if (manifest is not null) throw new IOException("APK 업데이트 정보가 둘 이상 있습니다.");
                    manifest = ReadAsset(item, tag, 16384);
                }
            }
            if (apk is null) throw new IOException("GitHub 릴리스에 ARM64 APK가 없습니다.");
            if (!IsNewer(version, installedVersion)) return null;
            if (manifest is null) throw new IOException("릴리스에 android-update.json을 APK와 함께 게시해야 합니다.");
            var metadata = await ReadSmallAsync(manifest.URL, true, 16384, ct);
            if (metadata.LongLength != manifest.Size || Convert.ToHexString(SHA256.HashData(metadata)).ToLowerInvariant() != manifest.Hash)
                throw new IOException("APK 업데이트 정보의 SHA256 검증 실패");
            using var info = JsonDocument.Parse(metadata);
            var wrapper = JsonSerializer.SerializeToElement(new {
                available = true, release = info.RootElement,
                download_path = "/api/app-updates/android/" + apk.Hash + ".apk"
            });
            var release = UpdateRelease.Parse(wrapper) ?? throw new IOException("APK 업데이트 정보가 없습니다.");
            if (release.VersionName != version || release.SHA256 != apk.Hash || release.Size != apk.Size)
                throw new IOException("APK와 업데이트 정보의 버전·크기·SHA256이 다릅니다.");
            return release with { DownloadPath = apk.URL };
        }
        catch (Exception ex) when (ex is JsonException or KeyNotFoundException or InvalidOperationException or FormatException)
        { throw new IOException("GitHub 업데이트 정보를 해석하지 못했습니다.", ex); }
    }
    public async Task DownloadAsync(UpdateRelease release, string destination, CancellationToken cancellationToken)
    {
        if (!Uri.TryCreate(release.DownloadPath, UriKind.Absolute, out var url) || url.Host != "github.com" || !IsAssetURI(url) ||
            !url.AbsolutePath.EndsWith("/OnTechChat-v" + release.VersionName + "-arm64.apk", StringComparison.Ordinal))
            throw new IOException("허용된 GitHub APK 주소가 아닙니다.");
        using var deadline = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        deadline.CancelAfter(TimeSpan.FromMinutes(5)); var ct = deadline.Token;
        using var response = await OpenAsync(release.DownloadPath, true, ct);
        if (response.Content.Headers.ContentLength is {} size && size != release.Size) throw new IOException("APK 응답 크기가 다릅니다.");
        await using var input = await response.Content.ReadAsStreamAsync(ct);
        var created = false;
        try
        {
            await using var output = new FileStream(destination, FileMode.CreateNew, FileAccess.Write, FileShare.None, 65536, true);
            created = true;
            using var hash = IncrementalHash.CreateHash(HashAlgorithmName.SHA256);
            var buffer = new byte[65536]; long total = 0; int n;
            while ((n = await input.ReadAsync(buffer, ct)) > 0)
            {
                total += n;
                if (total > release.Size || total > UpdateRelease.MaxBytes) throw new IOException("APK 크기 제한 초과");
                hash.AppendData(buffer, 0, n); await output.WriteAsync(buffer.AsMemory(0, n), ct);
            }
            if (total != release.Size || Convert.ToHexString(hash.GetHashAndReset()).ToLowerInvariant() != release.SHA256)
                throw new IOException("APK 크기 또는 SHA256 검증 실패");
        }
        catch { if (created && File.Exists(destination)) File.Delete(destination); throw; }
    }
    public void Dispose() => http.Dispose();
}
