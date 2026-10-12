CREATE TABLE IF NOT EXISTS issues (
    id          bigserial PRIMARY KEY, -- also the public issue number
    title       text NOT NULL,
    url         text NOT NULL,
    site_said   text NOT NULL DEFAULT '',
    body        text NOT NULL DEFAULT '',
    seen_at     timestamptz NOT NULL,
    labels      text[] NOT NULL DEFAULT '{}',
    screenshots text[] NOT NULL DEFAULT '{}',
    status      text NOT NULL DEFAULT 'open'
                CHECK (status IN ('open', 'closed', 'removed')),
    likely_bot  boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS comments (
    id         bigserial PRIMARY KEY,
    issue_id   bigint NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    body       text NOT NULL,
    status     text NOT NULL DEFAULT 'visible'
               CHECK (status IN ('visible', 'removed')),
    by_admin   boolean NOT NULL DEFAULT false,
    likely_bot boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS comments_issue_id ON comments (issue_id);
