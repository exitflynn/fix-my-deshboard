# FixMyDeshboard

A public record of what doesn't work on Indian government websites people use every day.
Anyone can file an Issue: one specific broken thing, backed by what the site showed.

Layout: `cmd/fixmydeshboard` (entry point), `internal/server` (all app code, schema, and `web/` templates and static files embedded into the binary), `deploy/` (Dockerfile, Render blueprint, Caddyfile for self-hosting), `scripts/` (backup, dev seed).

## Run locally

```sh
cp .env.example .env   # set POSTGRES_PASSWORD; DEV_LOGIN=1 is on for local use
docker compose up -d --build
```

- Site: http://localhost:8080 (or https://localhost through Caddy)
- Log in with any name (dev login), pick a username, and put it in `ADMIN_USERS` to get `/admin`.
- Tests: `go test ./...`

Templates, CSS and the schema are embedded in the binary, so rebuild after changing them.

## Deploy

On [Render](https://render.com): **New → Blueprint**, pick this repo, Blueprint path
`deploy/render.yaml`. It creates the web service and a Postgres database (free plans,
Singapore). Screenshots are stored in Postgres, so no disk is needed.

Settings (environment variables):

| Variable | |
|---|---|
| `DATABASE_URL` | Postgres connection string (set by the blueprint) |
| `BASE_URL` | Public URL; on Render it's detected automatically |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | Optional. Turns on login; redirect URI is `<BASE_URL>/auth/callback` |
| `ADMIN_USERS` | Comma-separated usernames with admin rights |
| `BEHIND_PROXY` | `1` when behind a proxy that sets `X-Forwarded-For` |

Self-hosting instead: `docker compose up -d` with `DOMAIN` set in `.env` (Caddy gets HTTPS).
