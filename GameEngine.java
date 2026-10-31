package com.lunarvoid.proxy;

import android.content.Context;
import android.net.ConnectivityManager;
import android.net.LinkAddress;
import android.net.LinkProperties;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.os.SystemClock;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.File;
import java.net.Inet4Address;
import java.net.InetAddress;

/** Sits between the app screen and the old engine. */
final class GameEngine {
    static final String VERSION = "26.50";
    private static final String UNAVAILABLE =
            "{\"available\":false,\"state\":\"stopped\",\"error\":\"Game mode requires ARM64 Android and its bundled native engine.\"}";

    private static boolean ready;
    private static boolean keyOk;
    private static boolean lan;
    private static long startingUntil;

    private GameEngine() {
    }

    static synchronized void initialize(Context ctx) {
        if (ready) {
            return;
        }
        try {
            File session = new File(ctx.getApplicationContext().getNoBackupFilesDir(), "session.bin");
            String key = SessionKey.load(ctx.getApplicationContext());
            keyOk = !key.isEmpty();
            NativeEngine.configure(session.getAbsolutePath(), key);
            ready = true;
        } catch (Throwable t) {
            ready = false;
        }
    }

    /** One raw request to the engine. */
    static String call(String json) {
        try {
            return NativeEngine.call(json);
        } catch (Throwable t) {
            return UNAVAILABLE;
        }
    }

    static String wifiAddress(Context ctx) {
        try {
            ConnectivityManager cm = (ConnectivityManager) ctx.getSystemService(Context.CONNECTIVITY_SERVICE);
            for (Network n : cm.getAllNetworks()) {
                NetworkCapabilities caps = cm.getNetworkCapabilities(n);
                if (caps == null || !caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)) {
                    continue;
                }
                LinkProperties lp = cm.getLinkProperties(n);
                if (lp == null) {
                    continue;
                }
                for (LinkAddress la : lp.getLinkAddresses()) {
                    InetAddress a = la.getAddress();
                    if (a instanceof Inet4Address && !a.isLoopbackAddress()) {
                        return a.getHostAddress();
                    }
                }
            }
        } catch (Exception ignored) {
        }
        return "";
    }

    /** The engine's status plus the few fields the app screen expects from the Android side. */
    static String withExtras(Context ctx, String raw) {
        try {
            JSONObject o = new JSONObject(raw);
            o.put("selectedVersion", VERSION);
            o.put("supportedVersions", new JSONArray().put(VERSION));
            o.put("lan", lan);
            if (!o.has("address") || o.optString("address").isEmpty() || lan) {
                String addr = lan ? wifiAddress(ctx) : "";
                o.put("address", addr.isEmpty() ? "127.0.0.1" : addr);
            }
            if (!keyOk && o.optString("sessionWarning").isEmpty()) {
                o.put("sessionWarning", "Protected sign-in storage is unavailable. This session will not survive an app restart.");
            }
            String state = o.optString("state", "stopped");
            if (!state.equals("stopped") || !o.optString("error").isEmpty()) {
                startingUntil = 0;
            } else if (SystemClock.elapsedRealtime() < startingUntil) {
                o.put("state", "starting");
            }
            return o.toString();
        } catch (Exception e) {
            return raw;
        }
    }

    static String status(Context ctx) {
        return withExtras(ctx, call("{\"action\":\"status\"}"));
    }

    /** Every request that comes from the app screen. */
    static String request(Context ctx, String json) {
        Context app = ctx.getApplicationContext();
        initialize(app);
        JSONObject req;
        String action;
        try {
            req = new JSONObject(json);
            action = req.optString("action");
        } catch (Exception e) {
            return "{\"error\":\"Bad request.\"}";
        }
        switch (action) {
            case "selectVersion":
                if (!VERSION.equals(req.optString("version"))) {
                    return "{\"error\":\"This build supports Minecraft " + VERSION + " only.\"}";
                }
                return status(app);
            case "start":
                lan = req.optBoolean("lan", false);
                if (lan && wifiAddress(app).isEmpty()) {
                    return withExtras(app, "{\"available\":true,\"state\":\"stopped\",\"error\":\"Connect to Wi-Fi to use same Wi-Fi mode.\"}");
                }
                startingUntil = SystemClock.elapsedRealtime() + 12000;
                GameService.launch(app, json);
                return withExtras(app, "{\"available\":true,\"state\":\"starting\"}");
            case "stop":
                startingUntil = 0;
                GameService.stop(app);
                call("{\"action\":\"stop\"}");
                return status(app);
            default:
                return withExtras(app, call(json));
        }
    }
}
