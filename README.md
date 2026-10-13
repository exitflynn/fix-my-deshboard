# FixMyDeshboard

A public record of what doesn't work on Indian government websites people use every day.
Anyone can file an Issue: one specific broken thing, backed by what the site showed.

Decisions and scope: [docs/PLAN.md](docs/PLAN.md).

Layout: `cmd/fixmydeshboard` (entry point), `internal/server` (all app code, schema, and `web/` templates and static files embedded into the binary), `deploy/` (Dockerfile, Caddyfile), `scripts/` (backup, dev seed).

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

Free hosting on an Oracle Cloud Always Free VM with a DuckDNS subdomain: see
[docs/DEPLOY.md](docs/DEPLOY.md). On the server it's one command:

```sh
DOMAIN=fixmydeshboard.duckdns.org bash scripts/server-setup.sh
```
