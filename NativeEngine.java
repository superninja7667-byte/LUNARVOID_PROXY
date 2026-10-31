package com.lunarvoid.proxy;

import org.json.JSONObject;

import java.io.File;

import engine.Engine;

/** The rebuilt engine (Go code in /engine, built into an .aar by the workflow). */
final class NativeEngine {
    private NativeEngine() {
    }

    static String call(String json) {
        return Engine.call(json);
    }

    static boolean configure(String sessionFile, String sessionKey) {
        Engine.call("{\"action\":\"configure\",\"key\":" + JSONObject.quote(sessionKey) + "}");
        File dir = new File(new File(sessionFile).getParentFile(), "lunarvoid");
        Engine.call("{\"action\":\"init\",\"dir\":" + JSONObject.quote(dir.getAbsolutePath()) + "}");
        return true;
    }
}
