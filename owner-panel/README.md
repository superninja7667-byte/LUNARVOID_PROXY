# LunarVoid Owner Panel

Standalone admin web console for LunarVoid Proxy.

## Run the API
Set `OWNER_KEY` to a long random secret and `PORT` (defaults to 8080), then:

```sh
cd owner-panel
go run .
```

The SQLite database is stored at `owner-panel-data.sqlite` (or `DATABASE_PATH`). Serve `index.html` from the same origin as the API, or configure `localStorage.lv_api` to the API base path before opening it.

## API
- `POST /api/owner/login` — exchange owner key for a short-lived signed token.
- `GET /api/owner/users` — list known installs.
- `PUT /api/owner/users/{id}/access` — set member, beta_tester or owner.
- `GET/POST /api/owner/releases` — view/publish early-access release announcements.
- `POST /api/app/check-in` — record an app install check-in. Accepts an install ID and optional display label.

This first version tracks install IDs, not verified Microsoft identities. Do not enter Microsoft passwords or tokens in this panel. Use HTTPS when exposing it publicly.
