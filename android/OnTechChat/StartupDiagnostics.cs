using Android.Content;
using Android.OS;
using System.Text;

namespace OnTechChat;

internal static class StartupDiagnostics
{
    private static readonly object Gate = new();
    private static readonly List<string> Failures = new();
    internal static void Record(Context context, string stage, Exception error)
    {
        // 토큰·사용자 상태·예외 메시지는 저장하지 않습니다. 시작 단계와 오류 종류만 기록합니다.
        var entry = DateTimeOffset.UtcNow.ToString("o") + " · " + stage + "\n" +
            (error.GetType().FullName ?? "Exception") + "\n" + (error.StackTrace ?? "");
        if (entry.Length > 4000) entry = entry[..4000];
        lock (Gate)
        {
            Failures.Add(entry);
            if (Failures.Count > 8) Failures.RemoveAt(0);
            try { File.WriteAllText(Path.Combine(context.FilesDir!.AbsolutePath, "startup-diagnostics.txt"), string.Join("\n\n", Failures)); }
            catch { }
        }
    }
    internal static string Read(Context context)
    {
        string header = "앱 1.8.7 · Android " + Build.VERSION.Release + "\n";
        lock (Gate)
        {
            if (Failures.Count > 0) return header + string.Join("\n\n", Failures);
            try
            {
                var path = Path.Combine(context.FilesDir!.AbsolutePath, "startup-diagnostics.txt");
                if (File.Exists(path)) return header + File.ReadAllText(path);
            }
            catch { }
        }
        return header + "기록된 시작 오류가 없습니다.";
    }
}
