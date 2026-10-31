package com.lunarvoid.proxy;

/**
 * The old LUNARVOID engine (liblunarvoid50.so), unchanged. Its native functions
 * are named after this exact class, so the name and package must not change.
 */
final class GameEngine50 {
    static {
        System.loadLibrary("lunarvoid50");
    }

    private GameEngine50() {
    }

    static native String callNative(String request);

    static native boolean configureNative(String sessionFile, String sessionKey);
}
