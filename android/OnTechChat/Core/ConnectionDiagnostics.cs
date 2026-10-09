using System.Net;
using System.Net.Sockets;
using System.Security.Authentication;

namespace OnTechChat;

public sealed record ConnectionReport(bool Success, string Code, string Address, string Detail)
{
    // 로그인 입력값·토큰·비밀번호·서버 원문 HTML을 보고서에 포함하지 않습니다.
    public string Text => $"앱 1.7.0 · 진단 {Code}\n{(Address.Length > 0 ? "주소: " + Address + "\n" : "")}{Detail}";
}
public static class ConnectionDiagnostics
{
    public static async Task<ConnectionReport> CheckAsync(string input, CancellationToken ct = default, HttpMessageHandler? handler = null)
    {
        Uri? origin = null;
        try
        {
            origin=UrlPolicy.Origin(input);
            using var api=new ChatApi(origin,handler:handler);
            var data=await api.JsonAsync("/healthz",HttpMethod.Get,ct:ct);
            var version=ChatApi.ValidateHealth(data);
            return new(true,"OK",origin.GetLeftPart(UriPartial.Authority),$"HTTPS 서버 응답 정상 · 서버 {version}\n이제 채팅 서버 계정으로 로그인하세요. FCM 수신까지 확인한 결과는 아닙니다.");
        }
        catch (Exception e) { return Failure(e,origin); }
    }
    public static ConnectionReport Failure(Exception exception, Uri? origin=null)
    {
        var code="UNEXPECTED";var detail="작업을 완료하지 못했습니다. 진단코드와 연결 주소를 확인해 주세요.";
        if (exception is ArgumentException) {code="ADDRESS_INVALID";detail=exception.Message;}
        else if(exception is ApiException api)
        {
            code=string.IsNullOrEmpty(api.Code)?"HTTP_"+(int)api.Status:api.Code;
            // 서버가 준 코드는 제한된 문자만 사용합니다.
            if(!System.Text.RegularExpressions.Regex.IsMatch(code,"^[A-Z0-9_]{1,50}$"))code="HTTP_"+(int)api.Status;
            detail=$"HTTP {(int)api.Status}\n{api.Message}";
        }
        else if(exception is OperationCanceledException) {code="NETWORK_TIMEOUT";detail="제한 시간 안에 연결 또는 응답 읽기를 완료하지 못했습니다. 포트 포워딩·방화벽·서버 실행 상태를 확인하세요.";}
        else
        {
            var chain=new List<Exception>();for(Exception? e=exception;e is not null&&chain.Count<8;e=e.InnerException)chain.Add(e);
            bool TypeHas(string term)=>chain.Any(e=>(e.GetType().FullName??"").Contains(term,StringComparison.OrdinalIgnoreCase));
            var http=chain.OfType<HttpRequestException>().FirstOrDefault();
            var socket=chain.OfType<SocketException>().FirstOrDefault();
            if(http?.HttpRequestError==HttpRequestError.NameResolutionError || socket?.SocketErrorCode is SocketError.HostNotFound or SocketError.NoData || TypeHas("UnknownHostException"))
            {code="DNS_FAILURE";detail="도메인을 IP로 찾지 못했습니다. 주소 오타·DNS 등록·휴대전화 인터넷을 확인하세요.";}
            else if(http?.HttpRequestError==HttpRequestError.SecureConnectionError || chain.Any(e=>e is AuthenticationException) || TypeHas("SSL") || TypeHas("Certificate"))
            {code="TLS_FAILURE";detail="HTTPS/TLS 연결 또는 인증서 검증에 실패했습니다. HTTP 포트에 HTTPS로 연결했는지, IP/도메인과 인증서가 일치하는지, 인증서 기한·중간 인증서를 확인하세요. 인증서 검사는 끄지 않습니다.";}
            else if(socket?.SocketErrorCode==SocketError.ConnectionRefused)
            {code="CONNECTION_REFUSED";detail="대상 포트가 연결을 거부했습니다. 서버/HTTPS 프록시가 해당 포트에서 실행 중인지 확인하세요.";}
            else if(socket?.SocketErrorCode==SocketError.TimedOut || TypeHas("SocketTimeoutException"))
            {code="NETWORK_TIMEOUT";detail="대상 포트에서 응답이 없습니다. 포트 포워딩·방화벽·공인 IP를 확인하세요.";}
            else if(http is not null || socket is not null || TypeHas("ConnectException"))
            {code="NETWORK_CONNECTION";detail="서버에 연결하지 못했습니다. LTE·5G에서는 공유기 내부 IP가 아닌 외부 접속 주소가 필요합니다. HTTPS 포트·방화벽·공유기 설정을 확인하세요.";}
            else if(exception is IOException){code="RESPONSE_READ";detail=exception.Message;}
        }
        return new(false,code,origin?.GetLeftPart(UriPartial.Authority)??"",detail);
    }
}
