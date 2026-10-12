package main

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (a *app) admin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(u), []byte(a.adminUser)) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), []byte(a.adminPass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="babu-log admin", charset="UTF-8"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		h(w, r)
	}
}

func (a *app) adminHome(w http.ResponseWriter, r *http.Request) {
	issues, err := a.pendingIssues(r.Context())
	if err != nil {
		a.serverError(w, err)
		return
	}
	comments, err := a.pendingComments(r.Context())
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.render(w, http.StatusOK, "admin", data{"Title": "Moderation", "Issues": issues, "Comments": comments})
}

func (a *app) adminFind(w http.ResponseWriter, r *http.Request) {
	var id int64
	n, err := strconv.Atoi(r.FormValue("number"))
	if err == nil {
		err = a.db.QueryRow(r.Context(), `SELECT id FROM issues WHERE number = $1`, n).Scan(&id)
	}
	if err != nil {
		a.renderError(w, http.StatusNotFound, "No issue with that number.")
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/issues/%d", id), http.StatusSeeOther)
}

func (a *app) loadIssueByID(w http.ResponseWriter, r *http.Request) (Issue, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return Issue{}, false
	}
	issue, err := a.issueByID(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		a.notFound(w, r)
		return Issue{}, false
	}
	if err != nil {
		a.serverError(w, err)
		return Issue{}, false
	}
	return issue, true
}

func (a *app) adminIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := a.loadIssueByID(w, r)
	if !ok {
		return
	}
	a.renderAdminIssue(w, r, issue, http.StatusOK, "")
}

func (a *app) renderAdminIssue(w http.ResponseWriter, r *http.Request, issue Issue, status int, errMsg string) {
	comments, err := a.comments(r.Context(), issue.ID, true)
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.render(w, status, "admin_issue", data{
		"Title": "Review: " + issue.Title, "Issue": issue, "Comments": comments,
		"ShotBase": "/admin/uploads/", "KnownLabels": strings.Join(knownLabels(), ", "), "Error": errMsg,
	})
}

var labelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// parseLabels turns "IRCTC, outage  stale-data" into [irctc outage stale-data].
func parseLabels(s string) []string {
	out := []string{}
	for _, l := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return r == ',' || r == ' ' }) {
		if labelPattern.MatchString(l) && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

func (a *app) adminIssueAction(w http.ResponseWriter, r *http.Request) {
	issue, ok := a.loadIssueByID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	title := strings.TrimSpace(r.FormValue("title"))
	labels := parseLabels(r.FormValue("labels"))
	body := strings.TrimSpace(r.FormValue("body"))
	back := fmt.Sprintf("/admin/issues/%d", issue.ID)

	var err error
	switch action := r.FormValue("action"); action {
	case "approve", "save":
		if title == "" {
			a.renderAdminIssue(w, r, issue, http.StatusUnprocessableEntity, "Title can't be empty.")
			return
		}
		if action == "save" {
			_, err = a.db.Exec(ctx, `UPDATE issues SET title = $2, labels = $3 WHERE id = $1`, issue.ID, title, labels)
			break
		}
		// Numbers are handed out at approval so the public sequence has no gaps.
		err = pgx.BeginFunc(ctx, a.db, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1)`); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				UPDATE issues
				SET status = 'open', title = $2, labels = $3,
				    number = (SELECT coalesce(max(number), 0) + 1 FROM issues)
				WHERE id = $1 AND status IN ('pending', 'rejected')`, issue.ID, title, labels)
			return err
		})
		back = "/admin"
	case "reject":
		_, err = a.db.Exec(ctx, `UPDATE issues SET status = 'rejected' WHERE id = $1 AND status = 'pending'`, issue.ID)
		back = "/admin"
	case "comment", "close", "reopen":
		if !issue.Public() {
			a.renderAdminIssue(w, r, issue, http.StatusUnprocessableEntity, "Approve the issue first.")
			return
		}
		if body == "" && action != "reopen" {
			a.renderAdminIssue(w, r, issue, http.StatusUnprocessableEntity, "Write a comment explaining this.")
			return
		}
		err = pgx.BeginFunc(ctx, a.db, func(tx pgx.Tx) error {
			if body != "" {
				if _, err := tx.Exec(ctx, `
					INSERT INTO comments (issue_id, body, status, by_admin) VALUES ($1, $2, 'approved', true)`,
					issue.ID, body); err != nil {
					return err
				}
			}
			newStatus := map[string]string{"close": "closed", "reopen": "open"}[action]
			if newStatus == "" {
				return nil
			}
			_, err := tx.Exec(ctx, `UPDATE issues SET status = $2 WHERE id = $1`, issue.ID, newStatus)
			return err
		})
	default:
		a.renderError(w, http.StatusBadRequest, "Unknown action.")
		return
	}
	if err != nil {
		a.serverError(w, err)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (a *app) adminRemoveScreenshot(w http.ResponseWriter, r *http.Request) {
	issue, ok := a.loadIssueByID(w, r)
	if !ok {
		return
	}
	name := r.FormValue("name")
	if !screenshotName.MatchString(name) || !slices.Contains(issue.Screenshots, name) {
		a.notFound(w, r)
		return
	}
	_, err := a.db.Exec(r.Context(),
		`UPDATE issues SET screenshots = array_remove(screenshots, $2) WHERE id = $1`, issue.ID, name)
	if err != nil {
		a.serverError(w, err)
		return
	}
	removeScreenshots(a.uploadDir, []string{name})
	http.Redirect(w, r, fmt.Sprintf("/admin/issues/%d", issue.ID), http.StatusSeeOther)
}

func (a *app) adminCommentAction(w http.ResponseWriter, r *http.Request) {
	status := map[string]string{"approve": "approved", "reject": "rejected"}[r.FormValue("action")]
	if status == "" {
		a.renderError(w, http.StatusBadRequest, "Unknown action.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}
	_, err = a.db.Exec(r.Context(),
		`UPDATE comments SET status = $2 WHERE id = $1 AND status = 'pending'`, id, status)
	if err != nil {
		a.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *app) adminServeUpload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !screenshotName.MatchString(name) {
		a.notFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(a.uploadDir, name))
}
