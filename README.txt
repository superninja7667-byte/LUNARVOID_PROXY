LUNARVOID PROXY - source (build 6)

app/                       Android app (shared code) + app screen (assets/index.html = your v4 page)
  src/old/                 APK OLD: your old engine (liblunarvoid50.so), unchanged. Known to join servers.
  src/next/                APK NEXT: the rebuilt engine below
  lunarvoid.jks            fixed signing key. Keep your repository PRIVATE.
engine/                    rebuilt Go engine (new in-game menu, passes the server's resource packs on)
  pack/                    the in-game menu resource pack (JSON UI)
.github/workflows/build.yml  (you add/edit it on GitHub)
