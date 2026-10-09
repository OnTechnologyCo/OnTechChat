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
    private static readonly object Gate = new();
    private static Context Ctx => Android.App.Application.Context;
    private static string PathName => Path.Combine(Ctx.FilesDir!.AbsolutePath, "session-v1.enc");
    private static ISecretKey GetKey()
    {
        using var store = KeyStore.GetInstance("AndroidKeyStore")!;
        store.Load(null);
        var key = store.GetKey(Alias, null);
        if (key is not null) return key.JavaCast<ISecretKey>()!;
        using var generator = KeyGenerator.GetInstance(KeyProperties.KeyAlgorithmAes, "AndroidKeyStore")!;
        using var spec = new KeyGenParameterSpec.Builder(Alias, KeyStorePurpose.Encrypt | KeyStorePurpose.Decrypt)
            .SetBlockModes(KeyProperties.BlockModeGcm)!.SetEncryptionPaddings(KeyProperties.EncryptionPaddingNone)!
            .SetKeySize(256)!.Build();
        generator.Init(spec);
        return generator.GenerateKey()!;
    }
    public static LocalState Read()
    {
        lock (Gate)
        {
            if (!File.Exists(PathName)) return new LocalState();
            try
            {
                byte[] raw = File.ReadAllBytes(PathName);
                if (raw.Length < 29 || raw.Length > 4*1024*1024 || raw[0] != 1) throw new IOException();
                using var cipher = Cipher.GetInstance("AES/GCM/NoPadding")!;
                using var key = GetKey();
                using var gcm = new GCMParameterSpec(128, raw[1..13]);
                cipher.Init(CipherMode.DecryptMode, key, gcm);
                cipher.UpdateAAD(Encoding.UTF8.GetBytes("com.ontechdev.chat/session-v1"));
                var plain = cipher.DoFinal(raw[13..])!;
                try { return JsonSerializer.Deserialize<LocalState>(plain) ?? new LocalState(); }
                finally { System.Security.Cryptography.CryptographicOperations.ZeroMemory(plain); }
            }
            catch
            {
                // 기기 복원·키 무효화 시 비밀번호를 재사용하지 않고 재로그인을 요구합니다.
                return new LocalState();
            }
        }
    }
    public static LocalState Update(Action<LocalState> change)
    {
        lock (Gate)
        {
            var state = Read(); change(state);
            byte[] plain = JsonSerializer.SerializeToUtf8Bytes(state);
            if(plain.Length>4*1024*1024-29)throw new IOException("기기 보관 한도를 초과했습니다.");
            using var cipher = Cipher.GetInstance("AES/GCM/NoPadding")!;
            using var key = GetKey(); cipher.Init(CipherMode.EncryptMode, key);
            cipher.UpdateAAD(Encoding.UTF8.GetBytes("com.ontechdev.chat/session-v1"));
            try
            {
                byte[] iv = cipher.GetIV()!, encrypted = cipher.DoFinal(plain)!;
                if (iv.Length != 12) throw new IOException("Unexpected AES-GCM nonce size");
                byte[] result = new byte[1 + iv.Length + encrypted.Length]; result[0] = 1;
                iv.CopyTo(result, 1); encrypted.CopyTo(result, 13);
                File.WriteAllBytes(PathName + ".tmp", result);
                File.Move(PathName + ".tmp", PathName, true);
                return state;
            }
            finally { System.Security.Cryptography.CryptographicOperations.ZeroMemory(plain); }
        }
    }
}
