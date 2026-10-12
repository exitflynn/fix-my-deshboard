CREATE TABLE IF NOT EXISTS users (
    id         bigserial PRIMARY KEY,
    google_sub text NOT NULL UNIQUE, -- Google's opaque account ID; no name or email is stored
    username   text UNIQUE,          -- NULL until chosen on first login
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash bytea PRIMARY KEY, -- sha256 of the cookie value
    user_id    bigint NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS issues (
    id          bigserial PRIMARY KEY, -- also the public issue number
    author_id   bigint REFERENCES users (id), -- NULL = anonymous
    title       text NOT NULL,
    site        text NOT NULL, -- website link or app name, as the reporter wrote it
    service     text NOT NULL DEFAULT '', -- e.g. IRCTC; guessed from the site, set by admins
    site_said   text NOT NULL DEFAULT '',
    body        text NOT NULL DEFAULT '',
    seen_at     timestamptz NOT NULL,
    labels      text[] NOT NULL DEFAULT '{}',
    screenshots text[] NOT NULL DEFAULT '{}',
    status      text NOT NULL DEFAULT 'open'
                CHECK (status IN ('open', 'closed', 'removed')),
    likely_bot  boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    edited_at   timestamptz
);

CREATE TABLE IF NOT EXISTS comments (
    id         bigserial PRIMARY KEY,
    issue_id   bigint NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    author_id  bigint REFERENCES users (id), -- NULL = anonymous
    body       text NOT NULL,
    status     text NOT NULL DEFAULT 'visible'
               CHECK (status IN ('visible', 'removed')),
    likely_bot boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    edited_at  timestamptz
);

CREATE INDEX IF NOT EXISTS comments_issue_id ON comments (issue_id);

-- Columns added after the first deploy. Keep these idempotent.
ALTER TABLE issues ADD COLUMN IF NOT EXISTS service text NOT NULL DEFAULT '';
