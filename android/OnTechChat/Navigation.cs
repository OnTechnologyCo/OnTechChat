using Android.Webkit;
namespace OnTechChat;

public sealed partial class MainActivity
{
    private BackCallback? backCallback;
    private bool navigatingBack;
    private void RegisterBackNavigation()
    {
        if (!OperatingSystem.IsAndroidVersionAtLeast(33)) return;
        backCallback = new BackCallback(this);
        OnBackInvokedDispatcher.RegisterOnBackInvokedCallback(0, backCallback);
    }
    private void UnregisterBackNavigation()
    {
        if (OperatingSystem.IsAndroidVersionAtLeast(33) && backCallback is not null)
            OnBackInvokedDispatcher.UnregisterOnBackInvokedCallback(backCallback);
        backCallback?.Dispose(); backCallback = null;
    }
    private void NavigateBack()
    {
        if (destroyed || !resumed || navigatingBack) return;
        var web = Web;
        if (web is null || ServerOrigin is null || !Uri.TryCreate(web.Url, UriKind.Absolute, out var uri) ||
            uri.GetLeftPart(UriPartial.Authority) != ServerOrigin.GetLeftPart(UriPartial.Authority) || uri.AbsolutePath != "/")
        { MoveTaskToBack(true); return; }
        navigatingBack = true;
        try
        {
            web.EvaluateJavascript("typeof ontechNavigateBack==='function'?ontechNavigateBack():null", new Callback(value =>
            {
                navigatingBack = false;
                if (destroyed || !resumed || Web != web || web.Url != uri.AbsoluteUri) return;
                if (value?.ToString() == "true") return;
                if (value?.ToString() == "null" && web.CanGoBack()) web.GoBack();
                else MoveTaskToBack(true);
            }));
        }
        catch (Java.Lang.IllegalStateException) { navigatingBack = false; }
    }
    private sealed class BackCallback(MainActivity owner) : Java.Lang.Object, Android.Window.IOnBackInvokedCallback
    {
        public void OnBackInvoked() => owner.NavigateBack();
    }
}
