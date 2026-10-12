package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

func (a *app) home(w http.ResponseWriter, r *http.Request) {
	latest, _, err := a.queryIssues(r.Context(), "open", "", 1)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, "home", data{"Home": true, "Latest": latest[:min(3, len(latest))]})
}

func (a *app) listIssues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := "open"
	if q.Get("state") == "closed" {
		state = "closed"
	}
	label := strings.TrimSpace(q.Get("label"))
	page, _ := strconv.Atoi(q.Get("page"))
	page = max(page, 1)

	issues, hasNext, err := a.queryIssues(r.Context(), state, label, page)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	open, closed, err := a.countIssues(r.Context(), label)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	d := data{
		"Title": "Issues", "Issues": issues, "State": state, "Label": label,
		"OpenCount": open, "ClosedCount": closed,
	}
	if page > 1 {
		d["PrevURL"] = issuesURL(state, label, page-1)
	}
	if hasNext {
		d["NextURL"] = issuesURL(state, label, page+1)
	}
	a.render(w, r, http.StatusOK, "list", d)
}

type issueForm struct {
	Title, Site, SeenAt, SiteSaid, Body string
}

func (a *app) newIssue(w http.ResponseWriter, r *http.Request) {
	a.renderNewIssue(w, r, http.StatusOK, issueForm{SeenAt: time.Now().In(ist).Format("2006-01-02T15:04")}, nil)
}

func (a *app) renderNewIssue(w http.ResponseWriter, r *http.Request, status int, f issueForm, errs []string) {
	if len(errs) > 0 && r.MultipartForm != nil && len(r.MultipartForm.File["screenshots"]) > 0 {
		errs = append(errs, "Please choose your screenshots again.")
	}
	a.render(w, r, status, "new", data{"Title": "Report a problem", "Form": f, "Errors": errs})
}

func (a *app) createIssue(w http.ResponseWriter, r *http.Request) {
	actor := a.actorKey(r)
	if a.issueLimit.full(actor) {
		a.renderError(w, r, http.StatusTooManyRequests,
			"Too many issues from you in the last hour. Please try again later.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxScreenshots*maxScreenshotBytes+1<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		a.renderError(w, r, http.StatusRequestEntityTooLarge,
			"That upload was too large. Screenshots can be up to 5 MB each, 3 at most.")
		return
	}
	defer r.MultipartForm.RemoveAll()

	f := issueForm{
		Title:    strings.TrimSpace(r.FormValue("title")),
		Site:     strings.TrimSpace(r.FormValue("site")),
		SeenAt:   r.FormValue("seen_at"),
		SiteSaid: strings.TrimSpace(r.FormValue("site_said")),
		Body:     strings.TrimSpace(r.FormValue("body")),
	}
	files := r.MultipartForm.File["screenshots"]
	seenAt, errs := validateIssue(f, len(files))
	if len(errs) > 0 {
		a.renderNewIssue(w, r, http.StatusUnprocessableEntity, f, errs)
		return
	}

	shots := []string{} // non-nil: a nil slice is sent as NULL
	for _, fh := range files {
		name, err := saveScreenshot(a.uploadDir, fh)
		if err != nil {
			removeScreenshots(a.uploadDir, shots)
			if errors.Is(err, errBadImage) {
				a.renderNewIssue(w, r, http.StatusUnprocessableEntity, f, []string{err.Error()})
			} else {
				a.serverError(w, r, err)
			}
			return
		}
		shots = append(shots, name)
	}

	id, err := a.insertIssue(r.Context(), Issue{
		AuthorID: userID(r),
		Title:    f.Title, Site: f.Site, SiteSaid: f.SiteSaid, Body: f.Body, SeenAt: seenAt,
		Labels: []string{"triage"}, Screenshots: shots,
		LikelyBot: r.FormValue("homepage") != "",
	})
	if err != nil {
		removeScreenshots(a.uploadDir, shots)
		a.serverError(w, r, err)
		return
	}
	a.issueLimit.add(actor)
	http.Redirect(w, r, fmt.Sprintf("/issues/%d", id), http.StatusSeeOther)
}

// userID is the logged-in user's id for author_id, or nil for anonymous posts.
func userID(r *http.Request) *int64 {
	if u := currentUser(r); u != nil {
		return &u.ID
	}
	return nil
}

// isAuthor reports whether the logged-in user wrote something with this author_id.
func isAuthor(u *User, authorID *int64) bool {
	return u != nil && authorID != nil && *authorID == u.ID
}

func validateIssue(f issueForm, nFiles int) (time.Time, []string) {
	var errs []string
	errs = append(errs, validateTitleBody(f.Title, f.Body)...)
	if f.Site == "" || utf8.RuneCountInString(f.Site) > 2000 {
		errs = append(errs, "Add the website link or the app's name.")
	} else if looksLikeURL(f.Site) && siteHref(f.Site) == "" {
		errs = append(errs, "That link doesn't look right. Copy it from the browser's address bar, or write the app's name.")
	}
	seenAt, err := time.ParseInLocation("2006-01-02T15:04", f.SeenAt, ist)
	if err != nil || seenAt.After(time.Now().Add(10*time.Minute)) {
		errs = append(errs, "Add when you saw it (not in the future).")
	}
	if utf8.RuneCountInString(f.SiteSaid) > 5000 {
		errs = append(errs, "Keep 'What the site said' under 5,000 characters.")
	}
	if nFiles > maxScreenshots {
		errs = append(errs, fmt.Sprintf("Attach at most %d screenshots.", maxScreenshots))
	}
	if f.SiteSaid == "" && nFiles == 0 {
		errs = append(errs, "Add what the site said, a screenshot, or both.")
	}
	return seenAt, errs
}

func validateTitleBody(title, body string) []string {
	var errs []string
	if title == "" || utf8.RuneCountInString(title) > 200 {
		errs = append(errs, "Add a title (up to 200 characters).")
	}
	if utf8.RuneCountInString(body) > 20000 {
		errs = append(errs, "Keep the details under 20,000 characters.")
	}
	return errs
}

func (a *app) showIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := a.loadPublicIssue(w, r)
	if !ok {
		return
	}
	a.renderIssue(w, r, issue, http.StatusOK, data{})
}

func (a *app) loadPublicIssue(w http.ResponseWriter, r *http.Request) (Issue, bool) {
	id, err := strconv.ParseInt(r.PathValue("number"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return Issue{}, false
	}
	issue, err := a.issue(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && issue.Removed()) {
		a.notFound(w, r)
		return Issue{}, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return Issue{}, false
	}
	return issue, true
}

func (a *app) renderIssue(w http.ResponseWriter, r *http.Request, issue Issue, status int, d data) {
	comments, err := a.comments(r.Context(), issue.ID, false)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	d["Title"] = fmt.Sprintf("%s · Issue #%d", issue.Title, issue.ID)
	d["OGTitle"] = issue.Title // WhatsApp truncates long preview titles
	link := fmt.Sprintf("%s/issues/%d", strings.TrimRight(a.baseURL, "/"), issue.ID)
	d["WhatsApp"] = "https://wa.me/?text=" + strings.ReplaceAll(url.QueryEscape(issue.Title+"\n"+link), "+", "%20")
	d["Issue"] = issue
	d["Comments"] = comments
	d["ShotBase"] = "/uploads/"
	desc := issue.SiteSaid
	if desc == "" {
		desc = issue.Body
	}
	if utf8.RuneCountInString(desc) > 200 {
		desc = string([]rune(desc)[:200]) + "…"
	}
	d["Description"] = desc
	if a.baseURL != "" && len(issue.Screenshots) > 0 {
		d["Image"] = strings.TrimRight(a.baseURL, "/") + "/uploads/" + issue.Screenshots[0]
	}
	a.render(w, r, status, "issue", d)
}

// editIssue lets an issue's author change the title and details. The evidence
// (website/app, time seen, what the site said, screenshots) is fixed once posted.
func (a *app) editIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := a.loadPublicIssue(w, r)
	if !ok {
		return
	}
	if !isAuthor(currentUser(r), issue.AuthorID) {
		a.notFound(w, r)
		return
	}
	title, body := issue.Title, issue.Body
	if r.Method == http.MethodPost {
		title, body = strings.TrimSpace(r.FormValue("title")), strings.TrimSpace(r.FormValue("body"))
		errs := validateTitleBody(title, body)
		if len(errs) == 0 {
			_, err := a.db.Exec(r.Context(),
				`UPDATE issues SET title = $2, body = $3, edited_at = now() WHERE id = $1`, issue.ID, title, body)
			if err != nil {
				a.serverError(w, r, err)
				return
			}
			http.Redirect(w, r, fmt.Sprintf("/issues/%d", issue.ID), http.StatusSeeOther)
			return
		}
		a.render(w, r, http.StatusUnprocessableEntity, "edit_issue",
			data{"Title": "Edit issue", "Issue": issue, "EditTitle": title, "EditBody": body, "Errors": errs})
		return
	}
	a.render(w, r, http.StatusOK, "edit_issue",
		data{"Title": "Edit issue", "Issue": issue, "EditTitle": title, "EditBody": body})
}

func (a *app) createComment(w http.ResponseWriter, r *http.Request) {
	issue, ok := a.loadPublicIssue(w, r)
	if !ok {
		return
	}
	actor := a.actorKey(r)
	if a.commentLimit.full(actor) {
		a.renderError(w, r, http.StatusTooManyRequests,
			"Too many comments from you in the last hour. Please try again later.")
		return
	}
	body := strings.TrimSpace(r.FormValue("body"))
	if body == "" || utf8.RuneCountInString(body) > 10000 {
		a.renderIssue(w, r, issue, http.StatusUnprocessableEntity, data{
			"CommentError": "Write a comment (up to 10,000 characters).", "CommentBody": body,
		})
		return
	}
	id, err := a.insertComment(r.Context(), Comment{
		IssueID: issue.ID, AuthorID: userID(r), Body: body, LikelyBot: r.FormValue("homepage") != "",
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.commentLimit.add(actor)
	http.Redirect(w, r, fmt.Sprintf("/issues/%d#comment-%d", issue.ID, id), http.StatusSeeOther)
}

// loadOwnComment returns a visible comment written by the logged-in user, or responds 404.
func (a *app) loadOwnComment(w http.ResponseWriter, r *http.Request) (Comment, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return Comment{}, false
	}
	c, err := a.comment(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (c.Status != "visible" || !isAuthor(currentUser(r), c.AuthorID))) {
		a.notFound(w, r)
		return Comment{}, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return Comment{}, false
	}
	return c, true
}

func (a *app) editComment(w http.ResponseWriter, r *http.Request) {
	c, ok := a.loadOwnComment(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		body := strings.TrimSpace(r.FormValue("body"))
		if body == "" || utf8.RuneCountInString(body) > 10000 {
			c.Body = body
			a.render(w, r, http.StatusUnprocessableEntity, "edit_comment",
				data{"Title": "Edit comment", "Comment": c, "Error": "Write a comment (up to 10,000 characters)."})
			return
		}
		_, err := a.db.Exec(r.Context(), `UPDATE comments SET body = $2, edited_at = now() WHERE id = $1`, c.ID, body)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/issues/%d#comment-%d", c.IssueID, c.ID), http.StatusSeeOther)
		return
	}
	a.render(w, r, http.StatusOK, "edit_comment", data{"Title": "Edit comment", "Comment": c})
}

// deleteComment hides the author's own comment. It stays in the database, like an admin removal.
func (a *app) deleteComment(w http.ResponseWriter, r *http.Request) {
	c, ok := a.loadOwnComment(w, r)
	if !ok {
		return
	}
	if _, err := a.db.Exec(r.Context(), `UPDATE comments SET status = 'removed' WHERE id = $1`, c.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/issues/%d", c.IssueID), http.StatusSeeOther)
}

// serveUpload serves a screenshot unless its issue has been removed.
func (a *app) serveUpload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !screenshotName.MatchString(name) {
		a.notFound(w, r)
		return
	}
	var public bool
	err := a.db.QueryRow(r.Context(), `
		SELECT EXISTS (SELECT 1 FROM issues WHERE $1 = ANY (screenshots) AND status <> 'removed')`,
		name).Scan(&public)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !public {
		a.notFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, filepath.Join(a.uploadDir, name))
}

func looksLikeURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// siteHref returns the site as a safe link if it's a valid http(s) URL, or "".
func siteHref(s string) string {
	if !looksLikeURL(s) {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.String()
}

// siteName is a short form for lists: the host for links ("irctc.co.in"), otherwise the text as written.
func siteName(s string) string {
	if href := siteHref(s); href != "" {
		u, _ := url.Parse(href)
		return strings.TrimPrefix(u.Hostname(), "www.")
	}
	if r := []rune(s); len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}
