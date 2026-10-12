# v1 plan

## Scope
- Landing page: "babu-log" wordmark (monospace text), tagline "The bug tracker for government websites.", buttons **File an Issue** and **Explore Issues**.
- File an Issue: anonymous, no login, no contact field.
- Explore Issues: GitHub-style list with Open/Closed tabs and label filter (`?label=irctc`), plus issue page with comments.
- Issues and comments are public as soon as they're posted. Admin removes things afterwards.
- New issues get the `triage` label; admin swaps it for `accepted` (one button).

Not in v1: Wayback/archiving, hashing, automated checks, reproductions, accounts, search, emails, i18n.

## Stack
- One Go binary, `html/template`, Primer CSS. Postgres.
- Docker Compose (app, postgres, caddy) on one VPS in India.
- Nightly `pg_dump` + `uploads/` copied to an S3-compatible bucket.
- Deploy: `git pull && docker compose up -d --build`.

## Data
`issues`
- `id` is the public issue number (removed issues leave gaps)
- `title`, `url`, `site_said` (exact text, plain), `body` (markdown), `seen_at`
- `labels text[]` (website label from the form dropdown + `triage`, the rest set by admin)
- `screenshots text[]` (paths under `uploads/`)
- `status`: open | closed | removed
- `likely_bot bool` (honeypot filled)
- `created_at`

`comments`
- `id`, `issue_id`, `body` (markdown), `status` (visible | removed), `by_admin bool`, `likely_bot bool`, `created_at`
- `by_admin` comments show a "Moderator" badge, so nobody can pose as the moderator.

## Rules
- Website dropdown: IRCTC, NTES, Passport Seva, Other → becomes a label.
- Label colours: hard-coded map in Go.
- Closing an issue: admin only, with a comment saying why. Duplicates closed with a link to the original.
- Screenshots: up to 3, ≤5 MB, PNG/JPEG/WebP, re-encoded to strip EXIF, hidden once the issue is removed. Admin can remove a single screenshot.
- Comments: text only. Removed comments stay in the database and can be restored.
- Markdown: goldmark (GFM), raw HTML off, sanitized with bluemonday, `<img>` stripped.
- Spam: honeypot field (flags `likely_bot` in admin, never drops) + in-memory per-IP rate limit (5 issues/hr, 20 comments/hr). IPs never stored.
- Admin: `/admin` behind HTTP Basic Auth, credentials from env vars.
