package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type Issue struct {
	ID           int64
	AuthorID     *int64
	AuthorName   *string // nil = anonymous
	Title        string
	Site         string // website link or app name
	SiteSaid     string
	Body         string
	SeenAt       time.Time
	Labels       []string
	Screenshots  []string
	Status       string
	LikelyBot    bool
	CreatedAt    time.Time
	EditedAt     *time.Time
	CommentCount int
}

func (i Issue) Removed() bool { return i.Status == "removed" }

type Comment struct {
	ID         int64
	IssueID    int64
	AuthorID   *int64
	AuthorName *string // nil = anonymous
	Body       string
	Status     string
	LikelyBot  bool
	CreatedAt  time.Time
	EditedAt   *time.Time
	IssueTitle string // only set for the admin list
}

const issueFrom = ` FROM issues i LEFT JOIN users u ON u.id = i.author_id `

const issueCols = `i.id, i.author_id, u.username, i.title, i.site, i.site_said, i.body, i.seen_at,
	i.labels, i.screenshots, i.status, i.likely_bot, i.created_at, i.edited_at`

func scanIssue(row pgx.Row, extra ...any) (Issue, error) {
	var i Issue
	dest := append([]any{&i.ID, &i.AuthorID, &i.AuthorName, &i.Title, &i.Site, &i.SiteSaid, &i.Body, &i.SeenAt,
		&i.Labels, &i.Screenshots, &i.Status, &i.LikelyBot, &i.CreatedAt, &i.EditedAt}, extra...)
	return i, row.Scan(dest...)
}

const commentFrom = ` FROM comments c LEFT JOIN users u ON u.id = c.author_id `

const commentCols = `c.id, c.issue_id, c.author_id, u.username, c.body, c.status, c.likely_bot, c.created_at, c.edited_at`

func scanComment(row pgx.Row, extra ...any) (Comment, error) {
	var c Comment
	dest := append([]any{&c.ID, &c.IssueID, &c.AuthorID, &c.AuthorName, &c.Body, &c.Status, &c.LikelyBot,
		&c.CreatedAt, &c.EditedAt}, extra...)
	return c, row.Scan(dest...)
}

const pageSize = 25

// issue returns any issue by id. Callers decide whether a removed one may be shown.
func (a *app) issue(ctx context.Context, id int64) (Issue, error) {
	return scanIssue(a.db.QueryRow(ctx, `SELECT `+issueCols+issueFrom+`WHERE i.id = $1`, id))
}

func (a *app) insertIssue(ctx context.Context, i Issue) (int64, error) {
	var id int64
	err := a.db.QueryRow(ctx, `
		INSERT INTO issues (author_id, title, site, site_said, body, seen_at, labels, screenshots, likely_bot)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`,
		i.AuthorID, i.Title, i.Site, i.SiteSaid, i.Body, i.SeenAt, i.Labels, i.Screenshots, i.LikelyBot).Scan(&id)
	return id, err
}

func (a *app) comment(ctx context.Context, id int64) (Comment, error) {
	return scanComment(a.db.QueryRow(ctx, `SELECT `+commentCols+commentFrom+`WHERE c.id = $1`, id))
}

// comments returns an issue's comments, oldest first. withRemoved is for the admin view.
func (a *app) comments(ctx context.Context, issueID int64, withRemoved bool) ([]Comment, error) {
	rows, err := a.db.Query(ctx, `SELECT `+commentCols+commentFrom+`
		WHERE c.issue_id = $1 AND ($2 OR c.status = 'visible')
		ORDER BY c.id`, issueID, withRemoved)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Comment, error) { return scanComment(row) })
}

func (a *app) insertComment(ctx context.Context, c Comment) (int64, error) {
	var id int64
	err := a.db.QueryRow(ctx, `
		INSERT INTO comments (issue_id, author_id, body, likely_bot) VALUES ($1, $2, $3, $4) RETURNING id`,
		c.IssueID, c.AuthorID, c.Body, c.LikelyBot).Scan(&id)
	return id, err
}

func (a *app) triageIssues(ctx context.Context) ([]Issue, error) {
	rows, err := a.db.Query(ctx, `SELECT `+issueCols+issueFrom+`
		WHERE i.status <> 'removed' AND 'triage' = ANY (i.labels)
		ORDER BY i.id DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Issue, error) { return scanIssue(row) })
}

func (a *app) recentComments(ctx context.Context, limit int) ([]Comment, error) {
	rows, err := a.db.Query(ctx, `SELECT `+commentCols+`, i.title`+commentFrom+`
		JOIN issues i ON i.id = c.issue_id
		ORDER BY c.id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Comment, error) {
		var title string
		c, err := scanComment(row, &title)
		c.IssueTitle = title
		return c, err
	})
}
