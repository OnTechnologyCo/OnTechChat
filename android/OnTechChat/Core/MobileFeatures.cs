using System.Globalization;
using System.Text.Json;
using System.Text.RegularExpressions;
namespace OnTechChat;
public sealed record PositionFix(double Latitude,double Longitude,double Accuracy,long CapturedAt,bool Approximate)
{
 public bool IsUsable(long now)=>double.IsFinite(Latitude)&&double.IsFinite(Longitude)&&double.IsFinite(Accuracy)&&Latitude is >=-90 and <=90&&Longitude is >=-180 and <=180&&Accuracy is >0 and <=20000&&CapturedAt>=now-30&&CapturedAt<=now+15;
}
public static class NativeFeaturePolicy
{
 public static bool LocationTarget(string raw,out string type,out string id)
 {
  type=id="";
  var answer=Regex.Match(raw,@"^ontechchat://answer-location\?request=([a-zA-Z0-9_-]{8,100})\z");
  if(!answer.Success)return false;
  type="request";id=answer.Groups[1].Value;return true;
 }
}
public sealed record UpdateRelease(string Package,long VersionCode,string VersionName,string SHA256,long Size,int MinSDK,string Signer,string Notes,string DownloadPath)
{
 public const long MaxBytes=150L*1024*1024;
 public static UpdateRelease? Parse(JsonElement data){
  if(data.ValueKind!=JsonValueKind.Object||!data.TryGetProperty("available",out var av)||av.ValueKind is not(JsonValueKind.True or JsonValueKind.False))throw new IOException("업데이트 응답 형식 오류");if(!av.GetBoolean())return null;
  try{var r=data.GetProperty("release");var v=new UpdateRelease(r.GetProperty("package_name").GetString()??"",r.GetProperty("version_code").GetInt64(),r.GetProperty("version_name").GetString()??"",r.GetProperty("sha256").GetString()??"",r.GetProperty("size").GetInt64(),r.GetProperty("min_sdk").GetInt32(),r.GetProperty("signing_cert_sha256").GetString()??"",r.GetProperty("notes").GetString()??"",data.GetProperty("download_path").GetString()??"");
   if(r.GetProperty("schema").GetInt32()!=1||v.Package!="com.ontechdev.chat"||v.VersionCode is <1 or >2100000000||v.Size is <1024 or >MaxBytes||v.MinSDK is <26 or >100||v.VersionName.Length is <1 or >80||v.Notes.Length>4000||!Regex.IsMatch(v.SHA256,@"^[0-9a-f]{64}\z")||!Regex.IsMatch(v.Signer,@"^[0-9a-f]{64}\z")||v.DownloadPath!="/api/app-updates/android/"+v.SHA256+".apk"||r.GetProperty("filename").GetString()!=v.SHA256+".apk")throw new IOException("업데이트 정보 검증 실패");return v;
  }catch(Exception ex)when(ex is KeyNotFoundException or InvalidOperationException or FormatException){throw new IOException("업데이트 응답을 해석하지 못했습니다.");}
 }
}
