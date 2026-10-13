# FixMyDeshboard

A public record of what doesn't work on Indian government websites people use every day.
Anyone can file an Issue: one specific broken thing, backed by what the site showed.

Decisions and scope: [PLAN.md](PLAN.md).

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

On a VPS in India with Docker installed:

```sh
git clone git@github.com:exitflynn/fix-my-deshboard.git && cd fix-my-deshboard
cp .env.example .env   # DOMAIN, BASE_URL=https://<domain>, POSTGRES_PASSWORD, BACKUP_REMOTE,
                       # GOOGLE_CLIENT_ID/SECRET, ADMIN_USERS; remove DEV_LOGIN
docker compose up -d --build
```

Point the domain's DNS at the VPS first; Caddy fetches the HTTPS certificate on its own.

Google sign-in: in Google Cloud Console, create an OAuth client of type "Web application" with
redirect URI `https://<domain>/auth/callback`. Only the `openid` scope is requested, so the
consent screen asks for nothing beyond "sign in".

Update with `git pull && docker compose up -d --build`.

Backups: install [rclone](https://rclone.org), configure a remote (e.g. Cloudflare R2), set
`BACKUP_REMOTE`, and add `scripts/backup.sh` to cron (example in the script).
