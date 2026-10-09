using System.Text.Json.Serialization;
namespace OnTechChat;
public sealed record HistoryPoint(
 [property:JsonPropertyName("id")] string Id,
 [property:JsonPropertyName("latitude")] double Latitude,
 [property:JsonPropertyName("longitude")] double Longitude,
 [property:JsonPropertyName("accuracy_m")] double Accuracy,
 [property:JsonPropertyName("captured_at")] long CapturedAt,
 [property:JsonPropertyName("approximate")] bool Approximate);
public static class LocationHistoryPolicy
{
 public const int IntervalSeconds=60;
 public const long RetentionSeconds=48*60*60;
 public static bool Belongs(LocalState s)=>s.Session is {Active:true} session&&s.HistoryAccount==NotificationPolicy.AccountKey(session);
 public static bool CanRecord(LocalState s)=>s.HistoryEnabled&&Belongs(s)&&s.HistoryRevision.Length==32&&!s.HistoryDeletePending&&!s.HistoryRevokePending;
 public static void Prune(LocalState s,long now)=>s.HistoryQueue=s.HistoryQueue.Where(p=>p.CapturedAt>now-RetentionSeconds&&p.CapturedAt<=now+30).OrderBy(p=>p.CapturedAt).TakeLast(3000).ToList();
 public static bool Add(LocalState s,PositionFix fix,string revision,long now)
 {
  Prune(s,now);
  if(!CanRecord(s)||s.HistoryRevision!=revision||!fix.IsUsable(now)||fix.CapturedAt-s.HistoryLastCaptured<IntervalSeconds)return false;
  s.HistoryQueue.Add(new(Guid.NewGuid().ToString("N"),fix.Latitude,fix.Longitude,fix.Accuracy,fix.CapturedAt,fix.Approximate));
  s.HistoryLastCaptured=fix.CapturedAt;Prune(s,now);return true;
 }
 public static void Clear(LocalState s)
 {
  s.HistoryEnabled=false;s.HistoryShare=false;s.HistoryRevision="";s.HistoryQueue.Clear();s.HistoryLastCaptured=0;s.HistoryLastSynced=0;
 }
}
