package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type Issue struct {
	ID           int64
	Number       *int
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

// Num is the public issue number, or 0 while the issue is unapproved.
func (i Issue) Num() int {
	if i.Number == nil {
		return 0
	}
	return *i.Number
}

func (i Issue) Public() bool { return i.Status == "open" || i.Status == "closed" }

type Comment struct {
	ID          int64
	IssueID     int64
	Body        string
	Status      string
	ByAdmin     bool
	LikelyBot   bool
	CreatedAt   time.Time
	IssueNumber int    // only set for the admin queue
	IssueTitle  string // only set for the admin queue
}

const issueCols = `id, number, title, url, site_said, body, seen_at, labels, screenshots, status, likely_bot, created_at`

func scanIssue(row pgx.Row, extra ...any) (Issue, error) {
	var i Issue
	dest := append([]any{&i.ID, &i.Number, &i.Title, &i.URL, &i.SiteSaid, &i.Body, &i.SeenAt,
		&i.Labels, &i.Screenshots, &i.Status, &i.LikelyBot, &i.CreatedAt}, extra...)
	return i, row.Scan(dest...)
}

const pageSize = 25

// queryIssues returns one page of public issues, newest first, and whether a next page exists.
func (a *app) queryIssues(ctx context.Context, state, label string, page int) ([]Issue, bool, error) {
	rows, err := a.db.Query(ctx, `
		SELECT `+issueCols+`,
		       (SELECT count(*) FROM comments c WHERE c.issue_id = issues.id AND c.status = 'approved')
		FROM issues
		WHERE status = $1 AND ($2 = '' OR $2 = ANY (labels))
		ORDER BY number DESC
		LIMIT $3 OFFSET $4`,
		state, label, pageSize+1, (page-1)*pageSize)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var issues []Issue
	for rows.Next() {
		var count int
		i, err := scanIssue(rows, &count)
		if err != nil {
			return nil, false, err
		}
		i.CommentCount = count
		issues = append(issues, i)
	}
	if err := rows.Err(); err != nil {
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

func (a *app) publicIssue(ctx context.Context, number int) (Issue, error) {
	return scanIssue(a.db.QueryRow(ctx,
		`SELECT `+issueCols+` FROM issues WHERE number = $1 AND status IN ('open', 'closed')`, number))
}

func (a *app) issueByID(ctx context.Context, id int64) (Issue, error) {
	return scanIssue(a.db.QueryRow(ctx, `SELECT `+issueCols+` FROM issues WHERE id = $1`, id))
}

func (a *app) insertIssue(ctx context.Context, i Issue) error {
	_, err := a.db.Exec(ctx, `
		INSERT INTO issues (title, url, site_said, body, seen_at, labels, screenshots, likely_bot)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		i.Title, i.URL, i.SiteSaid, i.Body, i.SeenAt, i.Labels, i.Screenshots, i.LikelyBot)
	return err
}

// comments returns an issue's comments, oldest first. allStatuses is for the admin view.
func (a *app) comments(ctx context.Context, issueID int64, allStatuses bool) ([]Comment, error) {
	rows, err := a.db.Query(ctx, `
		SELECT id, issue_id, body, status, by_admin, likely_bot, created_at
		FROM comments
		WHERE issue_id = $1 AND ($2 OR status = 'approved')
		ORDER BY created_at`, issueID, allStatuses)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Comment, error) {
		var c Comment
		err := row.Scan(&c.ID, &c.IssueID, &c.Body, &c.Status, &c.ByAdmin, &c.LikelyBot, &c.CreatedAt)
		return c, err
	})
}

func (a *app) insertComment(ctx context.Context, c Comment) error {
	_, err := a.db.Exec(ctx, `
		INSERT INTO comments (issue_id, body, status, by_admin, likely_bot) VALUES ($1, $2, $3, $4, $5)`,
		c.IssueID, c.Body, c.Status, c.ByAdmin, c.LikelyBot)
	return err
}

func (a *app) pendingIssues(ctx context.Context) ([]Issue, error) {
	rows, err := a.db.Query(ctx, `SELECT `+issueCols+` FROM issues WHERE status = 'pending' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Issue, error) { return scanIssue(row) })
}

func (a *app) pendingComments(ctx context.Context) ([]Comment, error) {
	rows, err := a.db.Query(ctx, `
		SELECT c.id, c.issue_id, c.body, c.status, c.by_admin, c.likely_bot, c.created_at, i.number, i.title
		FROM comments c JOIN issues i ON i.id = c.issue_id
		WHERE c.status = 'pending'
		ORDER BY c.created_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Comment, error) {
		var c Comment
		err := row.Scan(&c.ID, &c.IssueID, &c.Body, &c.Status, &c.ByAdmin, &c.LikelyBot, &c.CreatedAt,
			&c.IssueNumber, &c.IssueTitle)
		return c, err
	})
}
