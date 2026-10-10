using Android.Content;
using Android.Runtime;
using Android.Security.Keystore;
using Java.Security;
using Javax.Crypto;
using Javax.Crypto.Spec;
using System.Text;
using System.Text.Json;

namespace OnTechChat;

// 메일 비밀번호/Firebase 서버 키는 받지 않습니다. 앱 세션은 Android Keystore AES-GCM으로 보관합니다.
public static class SecureState
{
    private const string Alias = "OnTechChat.Session.v1";
    private static readonly LocalStateStore Store = new(Load, Save);
    private static bool storedStateExists;
    private static Context Ctx => Android.App.Application.Context;
    private static string PathName => Path.Combine(Ctx.FilesDir!.AbsolutePath, "session-v1.enc");

    private static ISecretKey GetKey(bool allowCreate)
    {
        using var store = KeyStore.GetInstance("AndroidKeyStore")!;
        store.Load(null);
        var key = store.GetKey(Alias, null);
        if (key is not null) return key.JavaCast<ISecretKey>()!;
        // 저장 파일이 있는데 키를 못 읽었다고 새 키를 만들어 원본을 덮어쓰지 않습니다.
        if (!allowCreate) throw new IOException("저장된 로그인 암호화 키에 접근하지 못했습니다.");
        using var generator = KeyGenerator.GetInstance(KeyProperties.KeyAlgorithmAes, "AndroidKeyStore")!;
        using var spec = new KeyGenParameterSpec.Builder(Alias, KeyStorePurpose.Encrypt | KeyStorePurpose.Decrypt)
            .SetBlockModes(KeyProperties.BlockModeGcm)!.SetEncryptionPaddings(KeyProperties.EncryptionPaddingNone)!
            .SetKeySize(256)!.Build();
        generator.Init(spec);
        return generator.GenerateKey()!;
    }

    public static LocalState Read() => Store.Read();
    public static LocalState Update(Action<LocalState> change) => Store.Update(change);

    private static LocalState? Load()
    {
        try
        {
            byte[] raw;
            try { raw = File.ReadAllBytes(PathName); }
            catch (FileNotFoundException) { return null; }
            catch (DirectoryNotFoundException) { return null; }
            storedStateExists = true;
            if (raw.Length < 29 || raw.Length > 4 * 1024 * 1024 || raw[0] != 1)
                throw new IOException("기기 상태 파일 형식 오류");
            using var cipher = Cipher.GetInstance("AES/GCM/NoPadding")!;
            using var key = GetKey(false);
            using var gcm = new GCMParameterSpec(128, raw[1..13]);
            cipher.Init(CipherMode.DecryptMode, key, gcm);
            cipher.UpdateAAD(Encoding.UTF8.GetBytes("com.ontechdev.chat/session-v1"));
            var plain = cipher.DoFinal(raw[13..])!;
            try { return JsonSerializer.Deserialize<LocalState>(plain) ?? throw new IOException("기기 상태 응답 오류"); }
            finally { System.Security.Cryptography.CryptographicOperations.ZeroMemory(plain); }
        }
        catch (Exception error)
        {
            StartupDiagnostics.Record(Ctx, "SecureState.Read", error);
            throw new StateStorageException("STATE_READ_FAILED",
                "저장된 로그인에 일시적으로 접근하지 못했습니다. 원본은 유지했습니다. 앱을 다시 열거나 앱 실행 진단을 확인하세요.", error);
        }
    }

    private static void Save(LocalState state)
    {
        byte[]? plain = null;
        try
        {
            plain = JsonSerializer.SerializeToUtf8Bytes(state);
            if (plain.Length > 4 * 1024 * 1024 - 29) throw new IOException("기기 보관 한도를 초과했습니다.");
            using var cipher = Cipher.GetInstance("AES/GCM/NoPadding")!;
            using var key = GetKey(!storedStateExists);
            cipher.Init(CipherMode.EncryptMode, key);
            cipher.UpdateAAD(Encoding.UTF8.GetBytes("com.ontechdev.chat/session-v1"));
            byte[] iv = cipher.GetIV()!, encrypted = cipher.DoFinal(plain)!;
            if (iv.Length != 12) throw new IOException("Unexpected AES-GCM nonce size");
            byte[] result = new byte[1 + iv.Length + encrypted.Length];
            result[0] = 1;
            iv.CopyTo(result, 1); encrypted.CopyTo(result, 13);
            File.WriteAllBytes(PathName + ".tmp", result);
            File.Move(PathName + ".tmp", PathName, true);
            storedStateExists = true;
        }
        catch (Exception error)
        {
            StartupDiagnostics.Record(Ctx, "SecureState.Save", error);
            throw new StateStorageException("STATE_SAVE_FAILED",
                "로그인 정보 저장을 완료하지 못했습니다. 이전 로그인과 설정은 유지했습니다. 앱 실행 진단을 확인하세요.", error);
        }
        finally { if (plain is not null) System.Security.Cryptography.CryptographicOperations.ZeroMemory(plain); }
    }
}
