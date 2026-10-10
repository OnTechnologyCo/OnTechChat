using System.Text.Json;
using System.Security.Cryptography;

namespace OnTechChat;

public sealed class StateStorageException(string code, string message, Exception? inner = null) : IOException(message, inner)
{
    public string Code { get; } = code;
}

// 유효하게 읽은 상태를 프로세스 안에서 유지합니다. 읽기/쓰기 오류를 빈 로그인으로 바꾸지 않습니다.
public sealed class LocalStateStore(Func<LocalState?> load, Action<LocalState> save)
{
    private readonly object gate = new();
    private LocalState? current;

    public LocalState Read()
    {
        lock (gate) return Clone(Load());
    }

    public LocalState Update(Action<LocalState> change)
    {
        lock (gate)
        {
            var next = Clone(Load());
            change(next);
            var committed = Clone(next);
            // 디스크 저장이 성공한 뒤에만 메모리의 로그인/설정을 교체합니다.
            save(committed);
            current = committed;
            return Clone(current);
        }
    }

    private LocalState Load()
    {
        if (current is null) current = Clone(load() ?? new LocalState());
        return current;
    }

    private static LocalState Clone(LocalState state)
    {
        var bytes = JsonSerializer.SerializeToUtf8Bytes(state);
        try { return JsonSerializer.Deserialize<LocalState>(bytes) ?? throw new IOException("기기 상태를 읽지 못했습니다."); }
        finally { CryptographicOperations.ZeroMemory(bytes); }
    }
}
