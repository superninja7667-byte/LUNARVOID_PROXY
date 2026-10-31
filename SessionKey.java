package com.lunarvoid.proxy;

import android.content.Context;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;

import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.security.KeyStore;
import java.security.SecureRandom;

import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/**
 * Keeps the secret that encrypts your Microsoft sign-in. The secret is random,
 * stored only in encrypted form, and the key that protects it never leaves the
 * phone's Android Keystore. Updating the app keeps it; uninstalling removes it.
 */
final class SessionKey {
    private static final String ALIAS = "lunarvoid.session.key";
    private static final String FILE = "lunarvoid.session.v1";

    private SessionKey() {
    }

    /** Returns the 32-byte secret as 64 hex characters, or "" if the keystore is unavailable. */
    static String load(Context ctx) {
        try {
            KeyStore ks = KeyStore.getInstance("AndroidKeyStore");
            ks.load(null);
            if (!ks.containsAlias(ALIAS)) {
                KeyGenerator kg = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore");
                kg.init(new KeyGenParameterSpec.Builder(ALIAS,
                        KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
                        .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                        .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                        .setKeySize(256)
                        .build());
                kg.generateKey();
            }
            SecretKey wrap = (SecretKey) ks.getKey(ALIAS, null);
            File f = new File(ctx.getNoBackupFilesDir(), FILE);

            if (f.exists()) {
                byte[] blob = new byte[(int) f.length()];
                try (FileInputStream in = new FileInputStream(f)) {
                    int off = 0;
                    while (off < blob.length) {
                        int n = in.read(blob, off, blob.length - off);
                        if (n < 0) break;
                        off += n;
                    }
                }
                if (blob.length > 12) {
                    try {
                        Cipher c = Cipher.getInstance("AES/GCM/NoPadding");
                        c.init(Cipher.DECRYPT_MODE, wrap, new GCMParameterSpec(128, blob, 0, 12));
                        return hex(c.doFinal(blob, 12, blob.length - 12));
                    } catch (Exception broken) {
                        // unreadable (for example the keystore was reset): make a new one below
                        // the old encrypted sign-in cannot be opened any more, so the user signs in again
                        //noinspection ResultOfMethodCallIgnored
                        f.delete();
                    }
                }
            }

            byte[] secret = new byte[32];
            new SecureRandom().nextBytes(secret);
            Cipher c = Cipher.getInstance("AES/GCM/NoPadding");
            c.init(Cipher.ENCRYPT_MODE, wrap);
            byte[] iv = c.getIV();
            byte[] ct = c.doFinal(secret);
            try (FileOutputStream out = new FileOutputStream(f)) {
                out.write(iv);
                out.write(ct);
            }
            return hex(secret);
        } catch (Exception e) {
            return "";
        }
    }

    private static String hex(byte[] b) {
        char[] d = "0123456789abcdef".toCharArray();
        char[] out = new char[b.length * 2];
        for (int i = 0; i < b.length; i++) {
            out[i * 2] = d[(b[i] >> 4) & 0xF];
            out[i * 2 + 1] = d[b[i] & 0xF];
        }
        return new String(out);
    }
}
