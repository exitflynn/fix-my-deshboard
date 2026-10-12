package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type Issue struct {
	ID           int64
	Title        string
	URL          string
	SiteSaid     string
	Body         string
	SeenAt       time.Time
	Labels       []string
	Screenshots  []string
	Status       string
	LikelyBot    bool
	CreatedAt    time.Time
	CommentCount int
}

func (i Issue) Removed() bool { return i.Status == "removed" }

type Comment struct {
	ID         int64
	IssueID    int64
	Body       string
	Status     string
	ByAdmin    bool
	LikelyBot  bool
	CreatedAt  time.Time
	IssueTitle string // only set for the admin list
}

const issueCols = `id, title, url, site_said, body, seen_at, labels, screenshots, status, likely_bot, created_at`

func scanIssue(row pgx.Row, extra ...any) (Issue, error) {
	var i Issue
	dest := append([]any{&i.ID, &i.Title, &i.URL, &i.SiteSaid, &i.Body, &i.SeenAt,
		&i.Labels, &i.Screenshots, &i.Status, &i.LikelyBot, &i.CreatedAt}, extra...)
	return i, row.Scan(dest...)
}

const pageSize = 25

// queryIssues returns one page of issues, newest first, and whether a next page exists.
func (a *app) queryIssues(ctx context.Context, state, label string, page int) ([]Issue, bool, error) {
	rows, err := a.db.Query(ctx, `
		SELECT `+issueCols+`,
		       (SELECT count(*) FROM comments c WHERE c.issue_id = issues.id AND c.status = 'visible')
		FROM issues
		WHERE status = $1 AND ($2 = '' OR $2 = ANY (labels))
		ORDER BY id DESC
		LIMIT $3 OFFSET $4`,
		state, label, pageSize+1, (page-1)*pageSize)
	if err != nil {
		return nil, false, err
	}
	issues, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Issue, error) {
		var count int
		i, err := scanIssue(row, &count)
		i.CommentCount = count
		return i, err
	})
	if err != nil {
		return nil, false, err
	}
	if len(issues) > pageSize {
		return issues[:pageSize], true, nil
	}
	return issues, false, nil
}

func (a *app) countIssues(ctx context.Context, label string) (open, closed int, err error) {
	err = a.db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'open'), count(*) FILTER (WHERE status = 'closed')
		FROM issues WHERE $1 = '' OR $1 = ANY (labels)`, label).Scan(&open, &closed)
	return
}

// issue returns any issue by id. Callers decide whether a removed one may be shown.
func (a *app) issue(ctx context.Context, id int64) (Issue, error) {
	return scanIssue(a.db.QueryRow(ctx, `SELECT `+issueCols+` FROM issues WHERE id = $1`, id))
}

func (a *app) insertIssue(ctx context.Context, i Issue) (int64, error) {
	var id int64
	err := a.db.QueryRow(ctx, `
		INSERT INTO issues (title, url, site_said, body, seen_at, labels, screenshots, likely_bot)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		i.Title, i.URL, i.SiteSaid, i.Body, i.SeenAt, i.Labels, i.Screenshots, i.LikelyBot).Scan(&id)
	return id, err
}

// comments returns an issue's comments, oldest first. withRemoved is for the admin view.
func (a *app) comments(ctx context.Context, issueID int64, withRemoved bool) ([]Comment, error) {
	rows, err := a.db.Query(ctx, `
		SELECT id, issue_id, body, status, by_admin, likely_bot, created_at
		FROM comments
		WHERE issue_id = $1 AND ($2 OR status = 'visible')
		ORDER BY id`, issueID, withRemoved)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Comment, error) {
		var c Comment
		err := row.Scan(&c.ID, &c.IssueID, &c.Body, &c.Status, &c.ByAdmin, &c.LikelyBot, &c.CreatedAt)
		return c, err
	})
}

func (a *app) insertComment(ctx context.Context, c Comment) (int64, error) {
	var id int64
	err := a.db.QueryRow(ctx, `
		INSERT INTO comments (issue_id, body, by_admin, likely_bot) VALUES ($1, $2, $3, $4) RETURNING id`,
		c.IssueID, c.Body, c.ByAdmin, c.LikelyBot).Scan(&id)
	return id, err
}

func (a *app) triageIssues(ctx context.Context) ([]Issue, error) {
	rows, err := a.db.Query(ctx, `
		SELECT `+issueCols+` FROM issues
		WHERE status <> 'removed' AND 'triage' = ANY (labels)
		ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Issue, error) { return scanIssue(row) })
}

func (a *app) recentComments(ctx context.Context, limit int) ([]Comment, error) {
	rows, err := a.db.Query(ctx, `
		SELECT c.id, c.issue_id, c.body, c.status, c.by_admin, c.likely_bot, c.created_at, i.title
		FROM comments c JOIN issues i ON i.id = c.issue_id
		WHERE NOT c.by_admin
		ORDER BY c.id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Comment, error) {
		var c Comment
		err := row.Scan(&c.ID, &c.IssueID, &c.Body, &c.Status, &c.ByAdmin, &c.LikelyBot, &c.CreatedAt, &c.IssueTitle)
		return c, err
	})
}
