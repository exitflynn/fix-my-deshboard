# v1 plan

## Scope
- Landing page: "babu-log" wordmark (monospace text), tagline "The bug tracker for government websites.", buttons **File an Issue** and **Explore Issues**.
- File an Issue: anonymous, no login, no contact field.
- Explore Issues: GitHub-style list with Open/Closed tabs and label filter (`?label=irctc`), plus issue page with comments.
- Admin: approve/reject issues and comments.

Not in v1: Wayback/archiving, hashing, automated checks, reproductions, accounts, search, emails, i18n.

## Stack
- One Go binary, `html/template`, Primer CSS. Postgres.
- Docker Compose (app, postgres, caddy) on one VPS in India.
- Nightly `pg_dump` + `uploads/` copied to an S3-compatible bucket.
- Deploy: `git pull && docker compose up -d --build`.

## Data
`issues`
- `id` internal; `number` assigned on approval, gap-free (1, 2, 3…)
- `title`, `url`, `site_said` (exact text, plain), `body` (markdown), `seen_at`
- `labels text[]` (website label from the form dropdown, the rest added by admin)
- `screenshots text[]` (paths under `uploads/`)
- `status`: pending | rejected | open | closed
- `likely_bot bool` (honeypot filled)
- `created_at`

`comments`
- `id`, `issue_id`, `body` (markdown), `status` (pending | approved | rejected), `by_admin bool`, `likely_bot bool`, `created_at`
- `by_admin` comments skip the queue and show a "Moderator" badge, so nobody can pose as the moderator.

## Rules
- Website dropdown: IRCTC, NTES, Passport Seva, Other → becomes a label.
- Label colours: hard-coded map in Go.
- Closing an issue: admin only, with a comment saying why. Duplicates closed with a link to the original.
- Screenshots: up to 3, ≤5 MB, PNG/JPEG/WebP, re-encoded to strip EXIF, served only for approved issues. Admin can remove one during approval.
- Comments: text only, all reviewed before showing ("Comments appear after review").
- Markdown: goldmark (GFM), raw HTML off, sanitized with bluemonday, `<img>` stripped.
- Spam: honeypot field (flags `likely_bot`, never drops) + in-memory per-IP rate limit (5 issues/hr, 20 comments/hr). IPs never stored.
- Admin: `/admin` behind HTTP Basic Auth, credentials from env vars.
