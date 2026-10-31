using System.Net;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using OnTechChat;

internal static class GitHubUpdateTests
{
    public static async Task Run(Action<string, Action> check)
    {
        var bytes=Encoding.UTF8.GetBytes("PK\u0003\u0004"+new string('x',2044));
        var hash=Convert.ToHexString(SHA256.HashData(bytes)).ToLowerInvariant();
        const string tag="v1.15.99",version="1.8.12";
        const string name="OnTechChat-v1.8.12-arm64.apk";
        var url=GitHubUpdates.ReleaseBase+"download/"+tag+"/"+name;
        var metadata=JsonSerializer.SerializeToUtf8Bytes(new{schema=1,package_name="com.ontechdev.chat",version_code=192,version_name=version,filename=hash+".apk",sha256=hash,size=bytes.Length,min_sdk=26,signing_cert_sha256=new string('b',64),notes="test"});
        object Asset(string n,byte[] content)=>new{name=n,state="uploaded",size=content.Length,digest="sha256:"+Convert.ToHexString(SHA256.HashData(content)).ToLowerInvariant(),browser_download_url=GitHubUpdates.ReleaseBase+"download/"+tag+"/"+n};
        var listing=JsonSerializer.Serialize(new{tag_name=tag,draft=false,prerelease=false,assets=new[]{Asset(name,bytes),Asset("android-update.json",metadata)}});
        var download=Path.Combine(Path.GetTempPath(),Guid.NewGuid()+".apk");int requests=0;
        var fixture=new FakeHandler(r=>{
            requests++;
            if(r.Headers.Authorization is not null||r.Headers.Contains("Cookie")||r.Headers.Contains("X-OnTech-Request"))throw new Exception("chat credential sent to GitHub");
            if(r.RequestUri!.AbsoluteUri==GitHubUpdates.API)return Task.FromResult(FakeHandler.Json(listing));
            if(r.RequestUri.AbsoluteUri.EndsWith("/android-update.json"))return Task.FromResult(new HttpResponseMessage(HttpStatusCode.OK){Content=new ByteArrayContent(metadata)});
            if(r.RequestUri.AbsoluteUri==url){var response=new HttpResponseMessage(HttpStatusCode.Found);response.Headers.Location=new Uri("https://release-assets.githubusercontent.com/fixture?token=public");return Task.FromResult(response);}
            if(r.RequestUri.Host=="release-assets.githubusercontent.com")return Task.FromResult(new HttpResponseMessage(HttpStatusCode.OK){Content=new ByteArrayContent(bytes)});
            throw new Exception("unexpected request "+r.RequestUri);
        });
        try{
            using var updates=new GitHubUpdates(fixture);
            var release=await updates.LatestAsync("1.8.11",CancellationToken.None)??throw new Exception("release missing");
            check("GitHub release selects matching APK metadata",()=>{if(release.VersionCode!=192||release.DownloadPath!=url||release.SHA256!=hash)throw new Exception();});
            await updates.DownloadAsync(release,download,CancellationToken.None);
            check("GitHub redirects download without chat credentials and verify hash",()=>{if(!File.ReadAllBytes(download).SequenceEqual(bytes)||requests!=4)throw new Exception();});
            if(await updates.LatestAsync("1.8.12",CancellationToken.None)is not null)throw new Exception("same version offered");
            check("GitHub ignores same or older APK",()=>{if(GitHubUpdates.IsNewer("1.8.9","1.8.11")||!GitHubUpdates.IsNewer("1.8.12","1.8.11"))throw new Exception();});
            File.Delete(download);
            using var corrupt=new GitHubUpdates(new FakeHandler(_=>Task.FromResult(new HttpResponseMessage(HttpStatusCode.OK){Content=new ByteArrayContent(bytes.Select(x=>(byte)(x^1)).ToArray())})));
            try{await corrupt.DownloadAsync(release,download,CancellationToken.None);throw new Exception("corrupt APK accepted");}
            catch(IOException){check("corrupt GitHub APK removed",()=>{if(File.Exists(download))throw new Exception();});}
            using var wrongRedirect=new GitHubUpdates(new FakeHandler(_=>{var response=new HttpResponseMessage(HttpStatusCode.Found);response.Headers.Location=new Uri("https://attacker.invalid/file.apk");return Task.FromResult(response);}));
            try{await wrongRedirect.DownloadAsync(release,download,CancellationToken.None);throw new Exception("foreign redirect accepted");}
            catch(IOException){check("foreign GitHub redirect rejected",()=>{});}
        }finally{File.Delete(download);}
        foreach(var value in new[]{"http://github.com/OnTechnologyCo/OnTechChat/releases/download/v1.15.99/"+name,"https://github.com/Other/Repo/releases/download/v1.15.99/"+name,"https://github.com@attacker.invalid/"+name,"https://release-assets.githubusercontent.com.attacker.invalid/file.apk"})
            check("untrusted APK URL rejected",()=>{if(GitHubUpdates.IsAssetURI(new Uri(value)))throw new Exception();});
        using var missing=new GitHubUpdates(new FakeHandler(_=>Task.FromResult(FakeHandler.Json(JsonSerializer.Serialize(new{tag_name=tag,draft=false,prerelease=false,assets=new[]{Asset(name,bytes)}})))));
        try{await missing.LatestAsync("1.8.11",CancellationToken.None);throw new Exception("missing metadata accepted");}
        catch(IOException){check("new APK requires version and signature metadata",()=>{});}
    }
}
