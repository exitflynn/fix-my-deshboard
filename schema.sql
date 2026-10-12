CREATE TABLE IF NOT EXISTS issues (
    id          bigserial PRIMARY KEY,
    number      integer UNIQUE,
    title       text NOT NULL,
    url         text NOT NULL,
    site_said   text NOT NULL DEFAULT '',
    body        text NOT NULL DEFAULT '',
    seen_at     timestamptz NOT NULL,
    labels      text[] NOT NULL DEFAULT '{}',
    screenshots text[] NOT NULL DEFAULT '{}',
    status      text NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'rejected', 'open', 'closed')),
    likely_bot  boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS comments (
    id         bigserial PRIMARY KEY,
    issue_id   bigint NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    body       text NOT NULL,
    status     text NOT NULL DEFAULT 'pending'
               CHECK (status IN ('pending', 'approved', 'rejected')),
    by_admin   boolean NOT NULL DEFAULT false,
    likely_bot boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS comments_issue_id ON comments (issue_id);
