# v1 plan

## Scope
- Landing page: "babu-log" wordmark (monospace text), tagline "The bug tracker for government websites.", buttons **File an Issue** and **Explore Issues**.
- File an Issue: anonymous or logged in. No contact field.
- Accounts (optional): Sign in with Google, username chosen once on first login.
- Explore Issues: GitHub-style list with Open/Closed tabs and label filter (`?label=irctc`), plus issue page with comments.
- Issues and comments are public as soon as they're posted. Admin removes things afterwards.
- New issues get the `triage` label; admin swaps it for `accepted` (one button).

Not in v1: Wayback/archiving, hashing, automated checks, reproductions, search, emails, profiles, edit history, i18n.

## Stack
- One Go binary, `html/template`, a small hand-written stylesheet (cream background, Newsreader serif + Instrument Sans, self-hosted fonts). Postgres.
- Docker Compose (app, postgres, caddy) on one VPS in India.
- Nightly `pg_dump` + `uploads/` copied to an S3-compatible bucket.
- Deploy: `git pull && docker compose up -d --build`.

## Data
`users`
- `id`, `google_sub` (Google's opaque ID; no name, email or photo stored), `username` (NULL until chosen)

`sessions`
- `token_hash` (sha256 of the cookie), `user_id`, `expires_at` (30 days)

`issues`
- `id` is the public issue number (removed issues leave gaps)
- `author_id` (NULL = anonymous), `title`, `site` (website link or app name, free text), `site_said` (exact text, plain), `body` (markdown), `seen_at`, `edited_at`
- `labels text[]` (`triage` on creation; admin adds the rest, e.g. `irctc`, `outage`, `accepted`)
- `screenshots text[]` (paths under `uploads/`)
- `status`: open | closed | removed
- `likely_bot bool` (honeypot filled)
- `created_at`

`comments`
- `id`, `issue_id`, `author_id` (NULL = anonymous), `body` (markdown), `status` (visible | removed), `likely_bot bool`, `created_at`, `edited_at`

## Rules
- Any government website or app can be reported; the form takes a free-text "Website link / App name" (links are clickable).
- Label colours and display names: hard-coded maps in Go. The public never sees `triage`; `accepted` shows as "Verified ✓". Closing removes `triage`.
- Public wording: "Report a problem", status "Still broken" / "Closed". Home shows the 3 latest open issues.
- Issue pages have a no-JS "Share on WhatsApp" link; the screenshot is the share preview image.
- Closing an issue: admin only, with a comment saying why. Duplicates closed with a link to the original.
- Screenshots: up to 3, ≤5 MB, PNG/JPEG/WebP, re-encoded to strip EXIF, hidden once the issue is removed. Admin can remove a single screenshot.
- Comments: text only. Removed comments stay in the database and can be restored.
- Markdown: goldmark (GFM), raw HTML off, sanitized with bluemonday, `<img>` stripped.
- Spam: honeypot field (flags `likely_bot` in admin, never drops) + in-memory per-IP rate limit (5 issues/hr, 20 comments/hr). IPs never stored.
- Admin: accounts whose username is in `ADMIN_USERS`. They get `/admin`, Remove buttons on public pages, and a "Moderator" badge on their posts.

## Accounts
- Logged in = your posts carry your username. To post anonymously, log out.
- Usernames: 3–30 of `a-z 0-9 - _`, unique, can't be changed (admin can rename by hand).
- Authors can edit an issue's title and details, and their comments' text. The record (URL, time seen, what the site said, screenshots) is fixed once posted. Edits show an "edited" time; no history kept.
- Authors can delete their own comments (hidden, kept in the DB like admin removals), not issues.
- Account deletion: by hand on request; posts stay and show as removed user.
- Rate limits count per account when logged in, per IP when anonymous.
- `DEV_LOGIN=1` allows logging in as any name for local development; refused with an https `BASE_URL`.
