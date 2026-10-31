package com.lunarvoid.proxy;

import android.Manifest;
import android.app.Activity;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.webkit.JavascriptInterface;
import android.webkit.ValueCallback;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Toast;

import androidx.webkit.WebViewAssetLoader;

import java.io.ByteArrayInputStream;
import java.util.HashMap;

public class MainActivity extends Activity {
    private static final int PICK_FILE = 4711;
    private static final String HOME = "https://appassets.androidplatform.net/index.html";

    private WebView web;
    private ValueCallback<Uri[]> fileCallback;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        getWindow().setStatusBarColor(Color.parseColor("#020916"));
        getWindow().setNavigationBarColor(Color.parseColor("#020916"));

        GameEngine.initialize(this);

        final WebViewAssetLoader loader = new WebViewAssetLoader.Builder()
                .addPathHandler("/", new WebViewAssetLoader.AssetsPathHandler(this))
                .build();

        web = new WebView(this);
        web.setBackgroundColor(Color.parseColor("#020916"));
        WebSettings ws = web.getSettings();
        ws.setJavaScriptEnabled(true);
        ws.setDomStorageEnabled(true);
        ws.setAllowFileAccess(false);
        ws.setAllowContentAccess(false);

        web.setWebViewClient(new WebViewClient() {
            @Override
            public WebResourceResponse shouldInterceptRequest(WebView view, WebResourceRequest request) {
                WebResourceResponse local = loader.shouldInterceptRequest(request.getUrl());
                return local != null ? local : blocked();
            }
        });

        // Lets the page's "choose a skin / pack" buttons open Android's file picker.
        web.setWebChromeClient(new WebChromeClient() {
            @Override
            public boolean onShowFileChooser(WebView view, ValueCallback<Uri[]> callback, FileChooserParams params) {
                if (fileCallback != null) {
                    fileCallback.onReceiveValue(null);
                }
                fileCallback = callback;
                try {
                    startActivityForResult(params.createIntent(), PICK_FILE);
                } catch (Exception e) {
                    fileCallback = null;
                    return false;
                }
                return true;
            }
        });

        web.addJavascriptInterface(new Bridge(), "LunarVoidNative");
        web.loadUrl(HOME);
        setContentView(web);

        if (Build.VERSION.SDK_INT >= 33
                && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, 1);
        }
    }

    private WebResourceResponse blocked() {
        return new WebResourceResponse("text/plain", "utf-8", 403, "Blocked", new HashMap<String, String>(),
                new ByteArrayInputStream(new byte[0]));
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        if (requestCode == PICK_FILE) {
            if (fileCallback != null) {
                fileCallback.onReceiveValue(WebChromeClient.FileChooserParams.parseResult(resultCode, data));
                fileCallback = null;
            }
            return;
        }
        super.onActivityResult(requestCode, resultCode, data);
    }

    @Override
    protected void onDestroy() {
        if (web != null) {
            web.removeJavascriptInterface("LunarVoidNative");
        }
        super.onDestroy();
    }

    /** What the app screen can call as window.LunarVoidNative.* */
    private class Bridge {
        /** Every game request from the page: sign-in, start/stop, uploads, status. */
        @JavascriptInterface
        public String gameCall(String json) {
            return GameEngine.request(getApplicationContext(), json);
        }

        /** Opens Microsoft's "enter your code" page in the browser. */
        @JavascriptInterface
        public void openGameSignIn() {
            runOnUiThread(() -> {
                try {
                    startActivity(new Intent(Intent.ACTION_VIEW, Uri.parse("https://www.microsoft.com/link")));
                } catch (Exception e) {
                    Toast.makeText(MainActivity.this, "Could not open the sign-in page.", Toast.LENGTH_SHORT).show();
                }
            });
        }

        // The separate "forwarding without the menu" relay is not part of this build.
        @JavascriptInterface
        public String startProxy(String ip, int port, boolean lan) {
            return "{\"state\":\"stopped\",\"error\":\"Forwarding without the menu is not in this build. Keep the in-game menu switch on.\"}";
        }

        @JavascriptInterface
        public String stopProxy() {
            return "{\"state\":\"stopped\"}";
        }

        @JavascriptInterface
        public String status() {
            return "{\"state\":\"stopped\"}";
        }
    }
}
