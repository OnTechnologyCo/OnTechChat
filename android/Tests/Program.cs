using OnTechChat;
using System.Net;
using System.Text;
using System.Text.Json;

int passed = 0;
void Check(string name, Action check) { check(); Console.WriteLine("PASS " + name); passed++; }
void Reject(string input) { try { UrlPolicy.Origin(input); throw new Exception("accepted " + input); } catch (ArgumentException) { } }
Check("HTTPS domain normalization", () => { if (UrlPolicy.Origin(" https://CHAT.example.com ").AbsoluteUri != "https://chat.example.com/") throw new Exception(); });
Check("HTTPS IP and nondefault port", () => { if (UrlPolicy.Origin("https://203.0.113.10:9443").Port != 9443) throw new Exception(); });
foreach (string input in new[] { "http://chat.example.com", "https://127.0.0.1", "https://localhost", "https://0.0.0.0", "https://[::1]", "https://[::]", "https://user:secret@chat.example.com", "https://chat.example.com/path", "https://chat.example.com/?token=bad", "https://chat.example.com/#bad", "file:///etc/passwd", "javascript:alert(1)" }) Check("reject " + input, () => Reject(input));
var origin = UrlPolicy.Origin("https://chat.example.com");
Check("same-origin default HTTPS port", () => { if (!UrlPolicy.SameOrigin(origin, new Uri("https://chat.example.com:443/api/me"))) throw new Exception(); });
Check("different port rejected", () => { if (UrlPolicy.SameOrigin(origin, new Uri("https://chat.example.com:8443"))) throw new Exception(); });
Check("hostname suffix rejected", () => { if (UrlPolicy.SameOrigin(origin, new Uri("https://chat.example.com.evil.invalid"))) throw new Exception(); });
Check("attachment URL allowed", () => { if (!UrlPolicy.IsFile(origin, "https://chat.example.com/api/files/abcdef12345678?inline=1", out _)) throw new Exception(); });
foreach (string input in new[] { "https://evil.invalid/api/files/abcdef12345678", "https://chat.example.com/api/files/../../config.json", "https://chat.example.com/api/files/abcdef12345678?token=leak", "file:///data/local/secret" }) Check("file reject " + input, () => { if (UrlPolicy.IsFile(origin, input, out _)) throw new Exception(); });
Check("public map tile subresource", () => { if (!UrlPolicy.IsMapTile("https://tile.openstreetmap.org/19/524287/524287.png", "GET", false) || !UrlPolicy.IsMapTile("https://tile.openstreetmap.org/0/0/0.png", "GET", false)) throw new Exception(); });
Check("map tiles cannot navigate or post", () => { if (UrlPolicy.IsMapTile("https://tile.openstreetmap.org/1/0/0.png", "POST", false) || UrlPolicy.IsMapTile("https://tile.openstreetmap.org/1/0/0.png", "GET", true)) throw new Exception(); });
foreach (var tile in new[] { "http://tile.openstreetmap.org/1/0/0.png", "https://tile.openstreetmap.org.evil.invalid/1/0/0.png", "https://tile.openstreetmap.org:444/1/0/0.png", "https://user@tile.openstreetmap.org/1/0/0.png", "https://tile.openstreetmap.org/1/0/0.png?token=private", "https://tile.openstreetmap.org/1/0/0.png#private", "https://tile.openstreetmap.org/20/0/0.png", "https://tile.openstreetmap.org/1/2/0.png", "https://tile.openstreetmap.org/1/0/2.png", "https://tile.openstreetmap.org/index.html" })
    Check("reject unsafe map resource " + tile, () => { if (UrlPolicy.IsMapTile(tile, "GET", false)) throw new Exception(); });
foreach (var map in new[] {
    "https://tiles.openfreemap.org/planet",
    "https://tiles.openfreemap.org/planet/20261004_113936_pt/14/13972/6348.pbf",
    "https://tiles.openfreemap.org/planet/20261004_113936_pt/0/0/0.pbf",
    "https://tiles.openfreemap.org/fonts/Noto%20Sans%20Regular/44032-44287.pbf",
    "https://tiles.openfreemap.org/fonts/Noto%20Sans%20Bold/0-255.pbf",
    "https://tiles.openfreemap.org/fonts/Noto%20Sans%20Italic/1113856-1114111.pbf"
}) {
    Check("public vector map resource " + map, () => { if (!UrlPolicy.IsMapTile(map, "GET", false)) throw new Exception(); });
    Check("vector map cannot navigate/post " + map, () => { if (UrlPolicy.IsMapTile(map, "GET", true) || UrlPolicy.IsMapTile(map, "POST", false)) throw new Exception(); });
}
foreach (var map in new[] {
    "http://tiles.openfreemap.org/planet", "https://tiles.openfreemap.org.evil.invalid/planet",
    "https://tiles.openfreemap.org:444/planet", "https://user@tiles.openfreemap.org/planet",
    "https://tiles.openfreemap.org/planet?token=secret", "https://tiles.openfreemap.org/planet#secret",
    "https://tiles.openfreemap.org/styles/liberty", "https://tiles.openfreemap.org/evil.js",
    "https://tiles.openfreemap.org/planet/20261004_113936_pt/1/2/0.pbf",
    "https://tiles.openfreemap.org/planet/20261004_113936_pt/1/0/2.pbf",
    "https://tiles.openfreemap.org/planet/20261004_113936_pt/20/0/0.pbf",
    "https://tiles.openfreemap.org/planet/20261004_113936_pt/0/0/0.png",
    "https://tiles.openfreemap.org/fonts/Arbitrary/0-255.pbf",
    "https://tiles.openfreemap.org/fonts/Noto%20Sans%20Regular/1-256.pbf",
    "https://tiles.openfreemap.org/fonts/Noto%20Sans%20Regular/0-256.pbf",
    "https://tiles.openfreemap.org/fonts/Noto%20Sans%20Regular/1114112-1114367.pbf",
    "https://tiles.openfreemap.org/fonts/Noto%20Sans%20Regular/0-255.pbf?token=secret"
}) Check("reject vector resource " + map, () => { if (UrlPolicy.IsMapTile(map, "GET", false)) throw new Exception(); });
foreach (var resource in new[] {
    "https://oapi.map.naver.com/openapi/v3/maps.js?ncpKeyId=public123&callback=onTechNaverMapReady&language=ko",
    "https://oapi.map.naver.com/v3/auth?ncpKeyId=public123&url=https%3A%2F%2Fchat.example.com%2F&time=1790000000000&callback=__naver_maps_callback__0",
    "https://map.pstatic.net/nrb/styles/basic.json?fmt=png",
    "https://map.pstatic.net/nrb/styles/dterrain@2x.json?fmt=png",
    "https://nrbe.pstatic.net/styles/satellite@2x.json?fmt=png",
    "https://nrbe.pstatic.net/styles/basic@2x.json?fmt=png&callback=__naver_maps_callback__1",
    "https://map.pstatic.net/nrb/styles/terrain.json?fmt=png&callback=__naver_maps_callback__12",
    "https://ssl.pstatic.net/static/maps/mantle/2x/openhand.cur",
    "https://nrbe.pstatic.net/styles/basic/1792111328/21/2097151/2097151@2x.png?mt=bg.ol.lko",
    "https://map.pstatic.net/nrb/styles/basic/1792111328/0/0/0.png",
    "https://ssl.pstatic.net/static/maps/mantle/2x/dot.gif"
}) {
    Check("NAVER map subresource " + resource, () => { if (!UrlPolicy.IsNaverMapResource(resource, "GET", false)) throw new Exception(); });
    Check("NAVER blocks navigation and POST " + resource, () => { if (UrlPolicy.IsNaverMapResource(resource, "GET", true) || UrlPolicy.IsNaverMapResource(resource, "POST", false)) throw new Exception(); });
}
foreach (var resource in new[] {
    "http://oapi.map.naver.com/openapi/v3/maps.js?ncpKeyId=public123&callback=onTechNaverMapReady&language=ko",
    "https://oapi.map.naver.com.evil.invalid/openapi/v3/maps.js?ncpKeyId=public123&callback=onTechNaverMapReady&language=ko",
    "https://user@oapi.map.naver.com/openapi/v3/maps.js?ncpKeyId=public123&callback=onTechNaverMapReady&language=ko",
    "https://oapi.map.naver.com:444/openapi/v3/maps.js?ncpKeyId=public123&callback=onTechNaverMapReady&language=ko",
    "https://oapi.map.naver.com/openapi/v3/maps.js?ncpKeyId=public123&callback=onTechNaverMapReady&language=ko#private",
    "https://oapi.map.naver.com/openapi/v3/maps.js?ncpClientId=old",
    "https://oapi.map.naver.com/openapi/v3/maps.js?ncpKeyId=public123&callback=onTechNaverMapReady&language=ko&token=secret",
    "https://oapi.map.naver.com/arbitrary.js",
    "https://map.pstatic.net/nrb/styles/basic.json?fmt=png&token=secret",
    "https://nrbe.pstatic.net/styles/basic@2x.json?fmt=png&callback=arbitraryFunction",
    "https://nrbe.pstatic.net/styles/basic@2x.json?fmt=png&callback=__naver_maps_callback__1&token=secret",
    "https://nrbe.pstatic.net/styles/basic/1792111328/22/0/0.png",
    "https://nrbe.pstatic.net/styles/basic/1792111328/1/2/0.png",
    "https://nrbe.pstatic.net/styles/basic/1792111328/1/0/2.png",
    "https://nrbe.pstatic.net/styles/basic/1792111328/1/0/0.png?token=secret",
    "https://nrbe.pstatic.net/styles/arbitrary.js",
    "https://ssl.pstatic.net/static/maps/mantle/2x/arbitrary.js",
    "https://ssl.pstatic.net/static/other/image.png",
    "https://map.naver.com/",
    "https://example.com/nrb/styles/basic.json?fmt=png"
}) Check("reject unsafe NAVER resource " + resource, () => { if (UrlPolicy.IsNaverMapResource(resource, "GET", false)) throw new Exception(); });
Check("safe filename", () => { if (UrlPolicy.SafeFileName("../x\\bad:name.txt").IndexOfAny(['/', '\\', ':']) != -1) throw new Exception(); });
Check("notification IDs", () => { if (!UrlPolicy.ValidID("room-12345678") || UrlPolicy.ValidID("../bad") || UrlPolicy.ValidID(null)) throw new Exception(); });
Check("persistent session and legacy expiry", () => {
    foreach (long expiry in new long[] { 0, 1, DateTimeOffset.UtcNow.ToUnixTimeSeconds() - 3600 })
        if (!new LoginSession("https://chat.example.com/", "token", "user", "id", expiry).Active) throw new Exception();
    if (new LoginSession("https://chat.example.com/", "", "user", "id", 0).Active) throw new Exception();
});
Check("only confirmed session invalidation clears login", () => {
    if (!new ApiException(HttpStatusCode.Unauthorized, "revoked", "SESSION_INVALID").SessionInvalid) throw new Exception();
    foreach (var error in new[] {
        new ApiException(HttpStatusCode.Unauthorized, "proxy", "HTTP_401"),
        new ApiException(HttpStatusCode.Unauthorized, "wrong current password"),
        new ApiException(HttpStatusCode.Forbidden, "proxy"),
        new ApiException(HttpStatusCode.ServiceUnavailable, "restarting")
    }) if (error.SessionInvalid) throw new Exception();
});
var seen = new List<(string Path, string? Auth, string? Marker, string Body)>();
var handler = new FakeHandler(async req =>
{
    string body = req.Content is null ? "" : await req.Content.ReadAsStringAsync();
    seen.Add((req.RequestUri!.AbsolutePath, req.Headers.Authorization?.ToString(), req.Headers.GetValues("X-OnTech-Request").Single(), body));
    return req.RequestUri.AbsolutePath switch
    {
        "/healthz" => FakeHandler.Json("{\"ok\":true,\"version\":\"1.3.0\"}"),
        "/api/login" => FakeHandler.Json("{\"user\":{\"id\":\"user12345678\",\"username\":\"member\"},\"expires_at\":4102444800,\"token\":\"ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890_abc\"}"),
        "/api/devices" => FakeHandler.Json("{\"ok\":true,\"push_enabled\":true}"),
        "/api/logout" => FakeHandler.Json("{\"ok\":true}"),
        _ => new HttpResponseMessage(HttpStatusCode.NotFound) { Content = new StringContent("{\"error\":\"not found\"}") }
    };
});
LoginSession session;
using (var api = new ChatApi(origin, handler: handler)) session = await api.LoginAsync("member", "not-a-real-password");
Check("native login contract", () => { using var d = JsonDocument.Parse(seen[1].Body); if (d.RootElement.GetProperty("client").GetString() != "native" || seen[1].Auth is not null || seen[1].Marker != "1" || !session.Active) throw new Exception(); });
using (var api = new ChatApi(origin, session.Token, new FakeHandler(req =>
{
    if (req.Headers.Authorization?.ToString() != "Bearer " + session.Token) throw new Exception("Auth contract");
    return Task.FromResult(FakeHandler.Json("{\"push_enabled\":true}"));
})))
{
    if (!await api.RegisterDeviceAsync("android-test123", "test device", "fake-fcm-registration-token")) throw new Exception();
    Console.WriteLine("PASS authenticated device registration"); passed++;
}
using (var api = new ChatApi(origin, session.Token, new FakeHandler(req => Task.FromResult(FakeHandler.Json("{\"error\":\"login required\"}", HttpStatusCode.Unauthorized)))))
{
    try { await api.JsonAsync("/api/me", HttpMethod.Get); throw new Exception("Unauthorized accepted"); }
    catch (ApiException e) when (e.Status == HttpStatusCode.Unauthorized) { Console.WriteLine("PASS unauthorized response"); passed++; }
}
using (var api = new ChatApi(origin, session.Token, new FakeHandler(req => Task.FromResult(new HttpResponseMessage(HttpStatusCode.Redirect) { Content = new StringContent("redirect") }))))
{
    try { await api.JsonAsync("/api/me", HttpMethod.Get); throw new Exception("redirect accepted"); }
    catch (ApiException) { Console.WriteLine("PASS redirects not accepted as login"); passed++; }
}

Check("bare domain gets HTTPS", () => { if(UrlPolicy.Origin("chat.example.com").AbsoluteUri!="https://chat.example.com/")throw new Exception(); });
Check("bare IP gets HTTPS and preserves port", () => { if(UrlPolicy.Origin("203.0.113.8:9443").AbsoluteUri!="https://203.0.113.8:9443/")throw new Exception(); });
Check("bare IPv6 gets HTTPS", () => { if(UrlPolicy.Origin("[2001:db8::8]:9443").Port!=9443)throw new Exception(); });
foreach(var address in new[]{"127.0.0.1:8787","https://[::ffff:127.0.0.1]","https://chat.example.com:0","javascript:alert(1)","user:secret@chat.example.com"})Check("reject unsafe address "+address,()=>Reject(address));
async Task Diagnostic(string name,HttpResponseMessage response,string code)
{
 var calls=0;
 var result=await ConnectionDiagnostics.CheckAsync("chat.example.com",handler:new FakeHandler(req=>{calls++;if(req.RequestUri!.AbsolutePath!="/healthz"||req.Headers.Authorization!=null||req.Content!=null)throw new Exception("diagnosis must not send credentials");return Task.FromResult(response);}));
 if(result.Code!=code||calls!=1)throw new Exception(name+" -> "+result.Code+" expected "+code);
 Console.WriteLine("PASS "+name);passed++;
}
await Diagnostic("new server health marker",FakeHandler.Json("{\"ok\":true,\"service\":\"ontech-chat\",\"version\":\"1.3.1\"}"),"OK");
await Diagnostic("old server health compatibility",FakeHandler.Json("{\"ok\":true,\"version\":\"1.3.0\"}"),"OK");
await Diagnostic("HTML is not chat server",new HttpResponseMessage(HttpStatusCode.OK){Content=new StringContent("<html>router login</html>")},"NON_JSON_RESPONSE");
await Diagnostic("wrong service marker",FakeHandler.Json("{\"ok\":true,\"service\":\"other\"}"),"WRONG_SERVER");
await Diagnostic("JSON primitive rejected",FakeHandler.Json("true"),"INVALID_HEALTH");
await Diagnostic("host rejection code retained",FakeHandler.Json("{\"code\":\"HOST_NOT_ALLOWED\",\"error\":\"host mismatch\"}",HttpStatusCode.Forbidden),"HOST_NOT_ALLOWED");
await Diagnostic("gateway failure distinguished",new HttpResponseMessage(HttpStatusCode.BadGateway){Content=new StringContent("bad gateway")},"HTTP_502");
await Diagnostic("redirect rejected before body JSON parsing",new HttpResponseMessage(HttpStatusCode.Redirect){Content=new StringContent("go elsewhere")},"HTTP_REDIRECT");
foreach(var item in new (string Code,Exception Error)[]{
 ("DNS_FAILURE",new HttpRequestException(HttpRequestError.NameResolutionError,"sensitive-not-for-report")),
 ("TLS_FAILURE",new HttpRequestException(HttpRequestError.SecureConnectionError,"sensitive-not-for-report")),
 ("CONNECTION_REFUSED",new System.Net.Sockets.SocketException((int)System.Net.Sockets.SocketError.ConnectionRefused)),
 ("NETWORK_TIMEOUT",new TaskCanceledException("sensitive-not-for-report")),
 ("NETWORK_CONNECTION",new HttpRequestException("sensitive-not-for-report"))})
 Check("diagnosis "+item.Code,()=>{var r=ConnectionDiagnostics.Failure(item.Error,origin);if(r.Code!=item.Code||r.Text.Contains("sensitive-not-for-report"))throw new Exception(r.Text);});
var invalid=await ConnectionDiagnostics.CheckAsync("https://user:secret@chat.example.com",handler:new FakeHandler(_=>throw new Exception("must not call HTTP")));
Check("invalid credential URL is not sent or echoed",()=>{if(invalid.Code!="ADDRESS_INVALID"||invalid.Text.Contains("secret"))throw new Exception();});

Check("deployment IP HTTPS origin", () => { if (UrlPolicy.Origin("https://175.119.187.91:49333").AbsoluteUri != "https://175.119.187.91:49333/") throw new Exception(); });
Check("deployment bare IP normalized", () => { if (UrlPolicy.Origin("175.119.187.91:49333").Port != 49333) throw new Exception(); });
Check("deployment no HTTP downgrade", () => Reject("http://175.119.187.91:49333"));
Check("diagnostic code displayed", () => { if (!new ConnectionReport(true,"OK","","").Text.Contains("진단 OK")) throw new Exception(); });
Check("one-shot target route",()=>{if(!NativeFeaturePolicy.LocationTarget("ontechchat://answer-location?request=abcd1234",out var type,out var id)||type!="request"||id!="abcd1234")throw new Exception();});
foreach(var uri in new[]{"ontechchat://answer-location?request=abcd1234\n","ontechchat://answer-location?request=abcd1234&user=admin","ontechchat://answer-location?request=../../file","ontechchat://track?user=abcd1234","ontechchat://share-location?user=abcd1234","ontechchat://share-location?room=abcd1234"})Check("unsafe request link rejected",()=>{if(NativeFeaturePolicy.LocationTarget(uri,out _,out _))throw new Exception();});
Check("fresh measured location",()=>{if(!new PositionFix(37,127,30,1000,false).IsUsable(1010))throw new Exception();});
foreach(var p in new[]{new PositionFix(91,1,30,1000,false),new PositionFix(1,181,30,1000,false),new PositionFix(1,1,0,1000,false),new PositionFix(double.NaN,1,10,1000,false),new PositionFix(1,1,10,969,false),new PositionFix(1,1,10,1016,false)})Check("invalid or stale fix rejected",()=>{if(p.IsUsable(1000))throw new Exception();});
foreach(var tag in new[]{"local-test","message-0","message--1","message-1\n","message-99999999999999999999"})Check("unrelated notification retained",()=>{if(NotificationPolicy.MessageID(tag,out _))throw new Exception();});
Check("actual Firebase notification tag",()=>{if(!NotificationPolicy.MessageID("message-123",out var id)||id!=123)throw new Exception();});
Check("server-confirmed exact notifications only",()=>{using var j=JsonDocument.Parse("{\"version\":1,\"dismiss_message_ids\":[2]}");if(!NotificationPolicy.ConfirmedDismissals(j.RootElement,new long[]{1,2,3}).SetEquals(new long[]{2}))throw new Exception();});
Check("unexpected dismissal rejected",()=>{using var j=JsonDocument.Parse("{\"version\":1,\"dismiss_message_ids\":[99]}");try{NotificationPolicy.ConfirmedDismissals(j.RootElement,new long[]{1});throw new Exception();}catch(IOException){}});
foreach(var kind in new[]{"auth","location","location_request","unknown"})Check("sensitive payload redacted",()=>{var x=NotificationPolicy.Display(kind,true,"secret","secret 012345 37.123");if(x.Title.Contains("secret")||x.Body.Contains("012345")||x.Body.Contains("37.123"))throw new Exception();});
Check("preview opt-in",()=>{if(NotificationPolicy.Display("text",true,"친구","안녕하세요").Body!="안녕하세요")throw new Exception();});
Check("preview opt-out",()=>{if(NotificationPolicy.Display("text",false,"secret","secret").Body.Contains("secret"))throw new Exception();});
var signer=new string('b',64);var apkBytes=Encoding.UTF8.GetBytes("PK\u0003\u0004"+new string('x',2044));var apkHash=Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(apkBytes)).ToLowerInvariant();var release=new UpdateRelease("com.ontechdev.chat",131,"1.3.1",apkHash,apkBytes.Length,26,signer,"test","/api/app-updates/android/"+apkHash+".apk");
var apkPath=Path.Combine(Path.GetTempPath(),Guid.NewGuid()+".apk");
try{using var a=new ChatApi(origin,"test-token",new FakeHandler(r=>{if(r.RequestUri?.AbsolutePath!=release.DownloadPath||r.Headers.Authorization?.Parameter!="test-token")throw new Exception();return Task.FromResult(new HttpResponseMessage(HttpStatusCode.OK){Content=new ByteArrayContent(apkBytes)});}));await a.DownloadUpdateAsync(release,apkPath,CancellationToken.None);Check("authenticated update download size hash verified",()=>{if(!File.ReadAllBytes(apkPath).SequenceEqual(apkBytes))throw new Exception();});}finally{File.Delete(apkPath);}
try{using var a=new ChatApi(origin,"test-token",new FakeHandler(_=>Task.FromResult(new HttpResponseMessage(HttpStatusCode.OK){Content=new ByteArrayContent(apkBytes.Select(x=>(byte)(x^1)).ToArray())})));try{await a.DownloadUpdateAsync(release,apkPath,CancellationToken.None);throw new Exception();}catch(IOException){Check("corrupted update removed",()=>{if(File.Exists(apkPath))throw new Exception();});}}finally{File.Delete(apkPath);}
Check("all-friends sharing defaults off",()=>{var s=new LocalState();if(s.LocationSharingEnabled||s.LocationShareAllFriends)throw new Exception();});
Check("all-friends sharing persists without snapshot",()=>{var s=new LocalState{LocationSharingEnabled=true,LocationShareAllFriends=true};var restored=JsonSerializer.Deserialize<LocalState>(JsonSerializer.Serialize(s))!;if(!restored.LocationSharingEnabled||!restored.LocationShareAllFriends||restored.LocationAllowedUsers.Length!=0)throw new Exception();});
Check("history defaults off and one-minute 48-hour policy",()=>{var s=new LocalState();if(s.HistoryEnabled||s.HistoryShare||LocationHistoryPolicy.IntervalSeconds!=60||LocationHistoryPolicy.RetentionSeconds!=172800)throw new Exception();});
Check("history consent revision account and retention",()=>{
 var now=DateTimeOffset.UtcNow.ToUnixTimeSeconds();var login=new LoginSession("https://example.com","test-token","user","user-0001",now+3600);
 var s=new LocalState{Session=login,HistoryEnabled=true,HistoryAccount=NotificationPolicy.AccountKey(login),HistoryRevision=new string('a',32)};
 if(!LocationHistoryPolicy.Add(s,new PositionFix(37,127,10,now,false),s.HistoryRevision,now))throw new Exception("first fix");
 if(LocationHistoryPolicy.Add(s,new PositionFix(37,127,10,now+30,false),s.HistoryRevision,now+30))throw new Exception("too frequent");
 if(LocationHistoryPolicy.Add(s,new PositionFix(37,127,10,now+60,false),"wrong",now+60))throw new Exception("wrong revision");
 if(!LocationHistoryPolicy.Add(s,new PositionFix(37,127,10,now+60,false),s.HistoryRevision,now+60))throw new Exception("one minute");
 s.HistoryQueue.Add(new("old",37,127,10,now-172800,false));LocationHistoryPolicy.Prune(s,now);
 if(s.HistoryQueue.Any(p=>p.Id=="old"))throw new Exception("retention");
 s.HistoryAccount="other";if(LocationHistoryPolicy.CanRecord(s))throw new Exception("account boundary");
 LocationHistoryPolicy.Clear(s);if(s.HistoryEnabled||s.HistoryQueue.Count!=0)throw new Exception("clear");
});
Check("update waits for chat and serializes pending install",()=>{
 var gate=new UpdateCheckGate();
 if(gate.TryBegin("account",100,false,false)||gate.Busy)throw new Exception("started before page ready");
 if(!gate.TryBegin("account",100,false,true)||gate.TryBegin("account",101,true,true))throw new Exception("overlapping check/install");
 gate.Finish();if(gate.TryBegin("account",200,false,true))throw new Exception("duplicate resume check");
 if(!gate.TryBegin("account",200,true,true))throw new Exception("manual retry blocked");gate.Finish();
});
Check("update failure releases gate without immediate automatic retry",()=>{
 var gate=new UpdateCheckGate();if(!gate.TryBegin("account",1,false,true))throw new Exception();
 try{throw new IOException("mock update failure");}catch(IOException){}finally{gate.Finish();}
 if(gate.Busy||gate.TryBegin("account",2,false,true)||!gate.TryBegin("account",60001,false,true))throw new Exception();
});
Check("same installed version never triggers installer",()=>{
 var gate=new UpdateCheckGate();gate.TryBegin("account",1,false,true);
 if(gate.ShouldOffer(181,181,true,"",0)||gate.ShouldOffer(180,181,false,"",0))throw new Exception();
 if(!gate.ShouldOffer(182,181,false,"",0))throw new Exception();
 gate.Offered(182);if(gate.ShouldOffer(182,181,false,"",0)||!gate.ShouldOffer(182,181,true,"",0))throw new Exception();
});
Check("canceled update persists across launch and allows explicit retry",()=>{
 var state=JsonSerializer.Deserialize<LocalState>(JsonSerializer.Serialize(new LocalState{UpdateDeferredAccount="account",UpdateDeferredVersion=182}))!;
 var gate=new UpdateCheckGate();gate.TryBegin("account",1,false,true);
 if(gate.ShouldOffer(182,181,false,state.UpdateDeferredAccount,state.UpdateDeferredVersion)||!gate.ShouldOffer(182,181,true,state.UpdateDeferredAccount,state.UpdateDeferredVersion)||!gate.ShouldOffer(183,181,false,state.UpdateDeferredAccount,state.UpdateDeferredVersion))throw new Exception();
 gate.Finish();if(!gate.TryBegin("other-account",2,false,true)||!gate.ShouldOffer(182,181,false,state.UpdateDeferredAccount,state.UpdateDeferredVersion))throw new Exception("account boundary");
});

Check("web read authentication failure requests native recovery", () => {
    foreach (var path in new[] { "/api/me", "/api/rooms", "/api/events" })
        if (!SessionRecovery.NeedsWebRecovery(origin, new Uri(origin, path).AbsoluteUri, "GET", 401)) throw new Exception(path);
});
Check("web recovery excludes password errors and foreign requests", () => {
    foreach (var request in new (string Url, string Method, int Status)[] {
        (new Uri(origin, "/api/password").AbsoluteUri, "POST", 401),
        ("https://other.example.com/api/me", "GET", 401),
        (new Uri(origin, "/").AbsoluteUri, "GET", 401),
        (new Uri(origin, "/api/me").AbsoluteUri, "GET", 403),
        (new Uri(origin, "/api/me").AbsoluteUri, "GET", 503)
    }) if (SessionRecovery.NeedsWebRecovery(origin, request.Url, request.Method, request.Status)) throw new Exception();
});
await SessionRecovery.ConfirmCookieAsync(Task.FromResult(true), CancellationToken.None);
Check("cookie acceptance completes recovery", () => {});
try { await SessionRecovery.ConfirmCookieAsync(Task.FromResult(false), CancellationToken.None); throw new Exception("cookie rejection accepted"); }
catch (IOException) { Check("cookie rejection fails without waiting forever", () => {}); }
var pendingCookie = new TaskCompletionSource<bool>(TaskCreationOptions.RunContinuationsAsynchronously);
bool openedAfterCookie = false;
async Task OpenAfterCookie() { await SessionRecovery.ConfirmCookieAsync(pendingCookie.Task, CancellationToken.None, TimeSpan.FromMilliseconds(30)); openedAfterCookie = true; }
try { await OpenAfterCookie(); throw new Exception("missing cookie callback never timed out"); }
catch (IOException) { Check("missing cookie callback has bounded wait", () => {}); }
pendingCookie.TrySetResult(true);
await Task.Yield();
Check("late cookie callback cannot reopen timed-out session", () => { if (openedAfterCookie) throw new Exception(); });
using (var canceledCookie = new CancellationTokenSource())
{
    canceledCookie.Cancel();
    try { await SessionRecovery.ConfirmCookieAsync(new TaskCompletionSource<bool>().Task, canceledCookie.Token); throw new Exception("destroyed activity continued"); }
    catch (OperationCanceledException) { Check("activity cancellation releases cookie wait", () => {}); }
}

var persistenceSession = new LoginSession(origin.AbsoluteUri, "ABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890_native", "member", "user12345678", 0);
var loads = 0;
LocalState? persisted = new() { Session = persistenceSession, Origin = origin.AbsoluteUri, DeviceId = "android-stable-device" };
var stateStore = new LocalStateStore(
    () => { if (++loads > 1) throw new IOException("temporary keystore failure"); return persisted; },
    state => persisted = JsonSerializer.Deserialize<LocalState>(JsonSerializer.Serialize(state)));
stateStore.Read();
stateStore.Update(state => state.FcmToken = "fcm-refreshed");
stateStore.Update(state => { state.RegistrationKey = "registration"; state.NotificationPrompted = true; });
Check("background token and settings writes retain native login", () => {
    var state = stateStore.Read();
    if (state.Session != persistenceSession || state.DeviceId != "android-stable-device" || state.FcmToken != "fcm-refreshed" ||
        state.RegistrationKey != "registration" || persisted?.Session != persistenceSession || loads != 1) throw new Exception();
});
Check("read snapshots cannot mutate the authoritative login", () => {
    var snapshot = stateStore.Read(); snapshot.Session = null; snapshot.HistoryQueue.Clear();
    if (stateStore.Read().Session != persistenceSession) throw new Exception();
});
Check("update result cannot mutate the authoritative login", () => {
    var snapshot = stateStore.Update(state => state.NotificationPrompted = false); snapshot.Session = null;
    if (stateStore.Read().Session != persistenceSession) throw new Exception();
});
var firstLoadFailing = true; var recoveryWrites = 0;
var unavailableStore = new LocalStateStore(
    () => firstLoadFailing ? throw new IOException("key unavailable") : new LocalState { Session = persistenceSession },
    _ => recoveryWrites++);
try { unavailableStore.Update(state => state.FcmToken = "background"); throw new Exception("failed read replaced login"); }
catch (IOException) { Check("unreadable encrypted state never reaches background save", () => { if (recoveryWrites != 0) throw new Exception(); }); }
firstLoadFailing = false;
unavailableStore.Update(state => state.FcmToken = "background");
Check("temporary first read failure can recover without state reset", () => {
    if (unavailableStore.Read().Session != persistenceSession || recoveryWrites != 1) throw new Exception();
});
var saveFailing = false;
var writeStore = new LocalStateStore(() => new LocalState { Session = persistenceSession, FcmToken = "original" },
    _ => { if (saveFailing) throw new IOException("write unavailable"); });
writeStore.Read(); saveFailing = true;
try { writeStore.Update(state => { state.Session = null; state.FcmToken = "changed"; }); throw new Exception("failed write committed"); }
catch (IOException) { Check("failed disk write preserves both cached login and settings", () => {
    var state = writeStore.Read(); if (state.Session != persistenceSession || state.FcmToken != "original") throw new Exception();
}); }
try { writeStore.Update(state => { state.Session = null; throw new ArgumentException("cancel change"); }); }
catch (ArgumentException) { Check("failed change cannot clear existing login", () => { if (writeStore.Read().Session != persistenceSession) throw new Exception(); }); }
saveFailing = false;
writeStore.Update(state => state.FcmToken = "new");
Check("write recovery continues with original credentials", () => {
    if (writeStore.Read().Session != persistenceSession || writeStore.Read().FcmToken != "new") throw new Exception();
});
Parallel.For(0, 40, _ => writeStore.Update(state => state.HistoryLastSynced++));
Check("concurrent background updates retain login and every update", () => {
    if (writeStore.Read().Session != persistenceSession || writeStore.Read().HistoryLastSynced != 40) throw new Exception();
});
writeStore.Update(state => state.Session = null);
Check("explicit logout still clears the cached login", () => { if (writeStore.Read().Session != null) throw new Exception(); });
var emptyStore = new LocalStateStore(() => null, _ => {});
emptyStore.Update(state => state.Session = persistenceSession);
Check("new installation can save its first login", () => { if (emptyStore.Read().Session != persistenceSession) throw new Exception(); });
Check("state diagnostics omit underlying key or token data", () => {
    var report = ConnectionDiagnostics.Failure(new StateStorageException("STATE_READ_FAILED", "로그인 정보 접근 대기", new IOException("private-key-token")), origin);
    if (report.Code != "STATE_READ_FAILED" || report.Text.Contains("private-key-token")) throw new Exception();
});
Check("web cookie matches only the exact session", () => {
    if (!WebSessionCookie.Matches("theme=dark; ontech_session=" + persistenceSession.Token + "; other=value", persistenceSession.Token)) throw new Exception();
    foreach (var cookie in new[] { "", "ontech_session=wrong", "ontech_session_extra=" + persistenceSession.Token,
        "ontech_session=" + persistenceSession.Token + "x", "ontech_session=" + persistenceSession.Token + "; ontech_session=old",
        "ontech_session=old; ontech_session=" + persistenceSession.Token })
        if (WebSessionCookie.Matches(cookie, persistenceSession.Token)) throw new Exception();
});
await SessionRecovery.ConfirmCookieAsync(Task.FromResult(false), CancellationToken.None, stored: () => true);
Check("actual stored cookie controls success despite callback representation", () => {});
try { await SessionRecovery.ConfirmCookieAsync(Task.FromResult(true), CancellationToken.None, stored: () => false); throw new Exception("missing actual cookie accepted"); }
catch (SessionRecoveryException e) when (e.Code == "WEB_COOKIE_MISSING") { Check("successful callback cannot bypass actual cookie mismatch", () => {}); }
await SessionRecovery.ConfirmCookieAsync(new TaskCompletionSource<bool>().Task, CancellationToken.None, TimeSpan.FromMilliseconds(20), () => true);
Check("missing callback can use verified cookie without reconnect loop", () => {});
using (var canceledVerification = new CancellationTokenSource())
{
    canceledVerification.Cancel();
    try { await SessionRecovery.ConfirmCookieAsync(new TaskCompletionSource<bool>().Task, canceledVerification.Token, stored: () => true); throw new Exception("cancellation bypassed by cookie"); }
    catch (OperationCanceledException) { Check("stored cookie cannot bypass activity cancellation", () => {}); }
}

Console.WriteLine($"TOTAL PASSED: {passed}");
sealed class FakeHandler(Func<HttpRequestMessage, Task<HttpResponseMessage>> send) : HttpMessageHandler
{
    protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken) => send(request);
    public static HttpResponseMessage Json(string json, HttpStatusCode status = HttpStatusCode.OK) => new(status) { Content = new StringContent(json, Encoding.UTF8, "application/json") };
}
