package main

import (
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

func (a *app) adminHome(w http.ResponseWriter, r *http.Request) {
	issues, err := a.triageIssues(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	comments, err := a.recentComments(r.Context(), 50)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "admin", data{"Title": "Admin", "Issues": issues, "Comments": comments})
}

func (a *app) adminFind(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/issues/"+strings.TrimPrefix(r.FormValue("number"), "#"), http.StatusSeeOther)
}

func (a *app) loadIssueByID(w http.ResponseWriter, r *http.Request) (Issue, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return Issue{}, false
	}
	issue, err := a.issue(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		a.notFound(w, r)
		return Issue{}, false
	}
	if err != nil {
		a.serverError(w, r, err)
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
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, status, "admin_issue", data{
		"Title": fmt.Sprintf("Admin: #%d", issue.ID), "Issue": issue, "Comments": comments,
		"ShotBase": "/admin/uploads/", "KnownLabels": strings.Join(knownLabels, ", "), "Error": errMsg,
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
	body := strings.TrimSpace(r.FormValue("body"))

	var err error
	switch action := r.FormValue("action"); action {
	case "save":
		title := strings.TrimSpace(r.FormValue("title"))
		if title == "" {
			a.renderAdminIssue(w, r, issue, http.StatusUnprocessableEntity, "Title can't be empty.")
			return
		}
		_, err = a.db.Exec(ctx, `UPDATE issues SET title = $2, labels = $3 WHERE id = $1`,
			issue.ID, title, parseLabels(r.FormValue("labels")))
	case "accept":
		_, err = a.db.Exec(ctx, `
			UPDATE issues SET labels = array_append(array_remove(array_remove(labels, 'triage'), 'accepted'), 'accepted')
			WHERE id = $1`, issue.ID)
	case "remove", "restore":
		status := map[string]string{"remove": "removed", "restore": "open"}[action]
		_, err = a.db.Exec(ctx, `UPDATE issues SET status = $2 WHERE id = $1`, issue.ID, status)
	case "comment", "close", "reopen":
		if body == "" && action != "reopen" {
			a.renderAdminIssue(w, r, issue, http.StatusUnprocessableEntity, "Write a comment explaining this.")
			return
		}
		err = pgx.BeginFunc(ctx, a.db, func(tx pgx.Tx) error {
			if body != "" {
				if _, err := tx.Exec(ctx, `INSERT INTO comments (issue_id, author_id, body) VALUES ($1, $2, $3)`,
					issue.ID, currentUser(r).ID, body); err != nil {
					return err
				}
			}
			switch action {
			case "close": // a closed issue no longer needs triage
				_, err := tx.Exec(ctx, `UPDATE issues SET status = 'closed', labels = array_remove(labels, 'triage') WHERE id = $1`, issue.ID)
				return err
			case "reopen":
				_, err := tx.Exec(ctx, `UPDATE issues SET status = 'open' WHERE id = $1`, issue.ID)
				return err
			}
			return nil
		})
	default:
		a.renderError(w, r, http.StatusBadRequest, "Unknown action.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, adminBack(r, fmt.Sprintf("/admin/issues/%d", issue.ID)), http.StatusSeeOther)
}

// adminBack is where to return after an admin action: the page the button was on
// (admin pages or a public issue page), or fallback.
func adminBack(r *http.Request, fallback string) string {
	back := r.FormValue("back")
	if strings.HasPrefix(back, "/admin") || strings.HasPrefix(back, "/issues") {
		return safeNext(back)
	}
	return fallback
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
		a.serverError(w, r, err)
		return
	}
	removeScreenshots(a.uploadDir, []string{name})
	http.Redirect(w, r, fmt.Sprintf("/admin/issues/%d", issue.ID), http.StatusSeeOther)
}

func (a *app) adminCommentAction(w http.ResponseWriter, r *http.Request) {
	status := map[string]string{"remove": "removed", "restore": "visible"}[r.FormValue("action")]
	if status == "" {
		a.renderError(w, r, http.StatusBadRequest, "Unknown action.")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}
	if _, err = a.db.Exec(r.Context(), `UPDATE comments SET status = $2 WHERE id = $1`, id, status); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, adminBack(r, "/admin"), http.StatusSeeOther)
}

func (a *app) adminServeUpload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !screenshotName.MatchString(name) {
		a.notFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(a.uploadDir, name))
}
