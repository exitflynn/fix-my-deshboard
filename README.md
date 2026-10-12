# babu-log

A public record of what doesn't work on Indian government websites people use every day.
Anyone can file an Issue: one specific broken thing, backed by what the site showed.

Decisions and scope: [PLAN.md](PLAN.md).

## Run locally

```sh
cp .env.example .env   # set POSTGRES_PASSWORD and ADMIN_PASSWORD
docker compose up -d --build
```

- Site: http://localhost:8080 (or https://localhost through Caddy)
- Moderation: http://localhost:8080/admin (ADMIN_USER / ADMIN_PASSWORD)

Templates, CSS and the schema are embedded in the binary, so rebuild after changing them.

## Deploy

On a VPS in India with Docker installed:

```sh
git clone <repo> && cd babu-log
cp .env.example .env   # DOMAIN, BASE_URL=https://<domain>, strong passwords, BACKUP_REMOTE
docker compose up -d --build
```

Point the domain's DNS at the VPS first; Caddy fetches the HTTPS certificate on its own.
Update with `git pull && docker compose up -d --build`.

Backups: install [rclone](https://rclone.org), configure a remote (e.g. Cloudflare R2), set
`BACKUP_REMOTE`, and add `scripts/backup.sh` to cron (example in the script).
