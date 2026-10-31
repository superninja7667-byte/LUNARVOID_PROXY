package com.lunarvoid.proxy;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.graphics.drawable.Icon;
import android.net.wifi.WifiManager;
import android.os.Build;
import android.os.Handler;
import android.os.IBinder;
import android.os.Looper;
import android.os.PowerManager;
import android.os.SystemClock;

import org.json.JSONObject;

import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/** Keeps the proxy running in the background while you play Minecraft. */
public class GameService extends Service {
    private static final String CHANNEL = "game";
    private static final String ACTION_STOP = "com.lunarvoid.proxy.STOP";
    private static final int NOTIF_ID = 2;

    private final ExecutorService pool = Executors.newSingleThreadExecutor();
    private final Handler handler = new Handler(Looper.getMainLooper());
    private PowerManager.WakeLock wake;
    private WifiManager.MulticastLock multicast;
    private long startedAt;

    private final Runnable watch = new Runnable() {
        @Override
        public void run() {
            pool.execute(() -> {
                boolean stopped = false;
                try {
                    stopped = new JSONObject(GameEngine.call("{\"action\":\"status\"}")).optString("state").equals("stopped");
                } catch (Exception ignored) {
                }
                if (stopped && SystemClock.elapsedRealtime() - startedAt > 15000) {
                    handler.post(GameService.this::stopSelf); // the proxy ended or never started
                } else {
                    handler.postDelayed(this, 4000);
                }
            });
        }
    };

    static void launch(Context ctx, String requestJson) {
        Intent i = new Intent(ctx, GameService.class).putExtra("request", requestJson);
        ctx.startForegroundService(i);
    }

    static void stop(Context ctx) {
        ctx.stopService(new Intent(ctx, GameService.class));
    }

    @Override
    public void onCreate() {
        super.onCreate();
        NotificationManager nm = getSystemService(NotificationManager.class);
        nm.createNotificationChannel(new NotificationChannel(CHANNEL, "Bedrock game proxy", NotificationManager.IMPORTANCE_LOW));

        PendingIntent open = PendingIntent.getActivity(this, 0, new Intent(this, MainActivity.class), PendingIntent.FLAG_IMMUTABLE);
        PendingIntent stop = PendingIntent.getService(this, 1,
                new Intent(this, GameService.class).setAction(ACTION_STOP), PendingIntent.FLAG_IMMUTABLE);
        Notification n = new Notification.Builder(this, CHANNEL)
                .setSmallIcon(R.drawable.ic_notification)
                .setContentTitle("LUNARVOID game proxy")
                .setContentText("Open LUNARVOID for status.")
                .setOngoing(true)
                .setContentIntent(open)
                .addAction(new Notification.Action.Builder(
                        Icon.createWithResource(this, R.drawable.ic_notification), "Stop", stop).build())
                .build();
        if (Build.VERSION.SDK_INT >= 34) {
            startForeground(NOTIF_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE);
        } else {
            startForeground(NOTIF_ID, n);
        }

        PowerManager pm = (PowerManager) getSystemService(POWER_SERVICE);
        if (pm != null) {
            wake = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "LUNARVOID:game");
            wake.setReferenceCounted(false);
            wake.acquire();
        }
        WifiManager wm = (WifiManager) getApplicationContext().getSystemService(WIFI_SERVICE);
        if (wm != null) {
            multicast = wm.createMulticastLock("LUNARVOID:wifi");
            multicast.setReferenceCounted(false);
            multicast.acquire();
        }
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null && ACTION_STOP.equals(intent.getAction())) {
            pool.execute(() -> GameEngine.call("{\"action\":\"stop\"}"));
            stopSelf();
            return START_NOT_STICKY;
        }
        final String request = intent == null ? null : intent.getStringExtra("request");
        startedAt = SystemClock.elapsedRealtime();
        if (request != null) {
            GameEngine.initialize(this);
            pool.execute(() -> GameEngine.call(request));
        }
        handler.removeCallbacks(watch);
        handler.postDelayed(watch, 5000);
        return START_NOT_STICKY;
    }

    @Override
    public void onDestroy() {
        handler.removeCallbacks(watch);
        if (wake != null && wake.isHeld()) {
            wake.release();
        }
        if (multicast != null && multicast.isHeld()) {
            multicast.release();
        }
        pool.shutdown();
        super.onDestroy();
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }
}
