package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

func (a *app) home(w http.ResponseWriter, r *http.Request) {
	a.render(w, http.StatusOK, "home", data{"Home": true})
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
		a.serverError(w, err)
		return
	}
	open, closed, err := a.countIssues(r.Context(), label)
	if err != nil {
		a.serverError(w, err)
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
	a.render(w, http.StatusOK, "list", d)
}

type issueForm struct {
	Title, Website, URL, SeenAt, SiteSaid, Body string
}

func (a *app) newIssue(w http.ResponseWriter, r *http.Request) {
	a.renderNewIssue(w, http.StatusOK, issueForm{SeenAt: time.Now().In(ist).Format("2006-01-02T15:04")}, nil)
}

func (a *app) renderNewIssue(w http.ResponseWriter, status int, f issueForm, errs []string) {
	a.render(w, status, "new", data{"Title": "File an Issue", "Form": f, "Errors": errs, "Websites": websites})
}

func (a *app) createIssue(w http.ResponseWriter, r *http.Request) {
	ip := a.clientIP(r)
	if a.issueLimit.full(ip) {
		a.renderError(w, http.StatusTooManyRequests,
			"Too many issues from your network in the last hour. Please try again later.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxScreenshots*maxScreenshotBytes+1<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		a.renderError(w, http.StatusRequestEntityTooLarge,
			"That upload was too large. Screenshots can be up to 5 MB each, 3 at most.")
		return
	}
	defer r.MultipartForm.RemoveAll()

	f := issueForm{
		Title:    strings.TrimSpace(r.FormValue("title")),
		Website:  r.FormValue("website"),
		URL:      strings.TrimSpace(r.FormValue("url")),
		SeenAt:   r.FormValue("seen_at"),
		SiteSaid: strings.TrimSpace(r.FormValue("site_said")),
		Body:     strings.TrimSpace(r.FormValue("body")),
	}
	files := r.MultipartForm.File["screenshots"]
	seenAt, errs := validateIssue(f, len(files))
	if len(errs) > 0 {
		a.renderNewIssue(w, http.StatusUnprocessableEntity, f, errs)
		return
	}

	shots := []string{} // non-nil: a nil slice is sent as NULL
	for _, fh := range files {
		name, err := saveScreenshot(a.uploadDir, fh)
		if err != nil {
			removeScreenshots(a.uploadDir, shots)
			if errors.Is(err, errBadImage) {
				a.renderNewIssue(w, http.StatusUnprocessableEntity, f, []string{err.Error()})
			} else {
				a.serverError(w, err)
			}
			return
		}
		shots = append(shots, name)
	}

	err := a.insertIssue(r.Context(), Issue{
		Title: f.Title, URL: f.URL, SiteSaid: f.SiteSaid, Body: f.Body, SeenAt: seenAt,
		Labels: []string{f.Website}, Screenshots: shots,
		LikelyBot: r.FormValue("homepage") != "",
	})
	if err != nil {
		removeScreenshots(a.uploadDir, shots)
		a.serverError(w, err)
		return
	}
	a.issueLimit.add(ip)
	a.render(w, http.StatusOK, "thanks", data{"Title": "Thanks"})
}

func validateIssue(f issueForm, nFiles int) (time.Time, []string) {
	var errs []string
	if f.Title == "" || utf8.RuneCountInString(f.Title) > 200 {
		errs = append(errs, "Add a title (up to 200 characters).")
	}
	if !slices.ContainsFunc(websites, func(w struct{ Label, Name string }) bool { return w.Label == f.Website }) {
		errs = append(errs, "Pick which website this is about.")
	}
	if u, err := url.Parse(f.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(f.URL) > 2000 {
		errs = append(errs, "Add the page's full address, starting with http:// or https://.")
	}
	seenAt, err := time.ParseInLocation("2006-01-02T15:04", f.SeenAt, ist)
	if err != nil || seenAt.After(time.Now().Add(10*time.Minute)) {
		errs = append(errs, "Add when you saw it (not in the future).")
	}
	if utf8.RuneCountInString(f.SiteSaid) > 5000 || utf8.RuneCountInString(f.Body) > 20000 {
		errs = append(errs, "That's too long. Keep 'What the site said' under 5,000 characters and details under 20,000.")
	}
	if nFiles > maxScreenshots {
		errs = append(errs, fmt.Sprintf("Attach at most %d screenshots.", maxScreenshots))
	}
	if f.SiteSaid == "" && nFiles == 0 {
		errs = append(errs, "Add what the site said, a screenshot, or both.")
	}
	return seenAt, errs
}

func (a *app) showIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := a.loadPublicIssue(w, r)
	if !ok {
		return
	}
	a.renderIssue(w, r, issue, http.StatusOK, data{"Commented": r.URL.Query().Get("commented") == "1"})
}

func (a *app) loadPublicIssue(w http.ResponseWriter, r *http.Request) (Issue, bool) {
	n, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		a.notFound(w, r)
		return Issue{}, false
	}
	issue, err := a.publicIssue(r.Context(), n)
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

func (a *app) renderIssue(w http.ResponseWriter, r *http.Request, issue Issue, status int, d data) {
	comments, err := a.comments(r.Context(), issue.ID, false)
	if err != nil {
		a.serverError(w, err)
		return
	}
	d["Title"] = fmt.Sprintf("%s · Issue #%d", issue.Title, issue.Num())
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
	a.render(w, status, "issue", d)
}

func (a *app) createComment(w http.ResponseWriter, r *http.Request) {
	issue, ok := a.loadPublicIssue(w, r)
	if !ok {
		return
	}
	ip := a.clientIP(r)
	if a.commentLimit.full(ip) {
		a.renderError(w, http.StatusTooManyRequests,
			"Too many comments from your network in the last hour. Please try again later.")
		return
	}
	body := strings.TrimSpace(r.FormValue("body"))
	if body == "" || utf8.RuneCountInString(body) > 10000 {
		a.renderIssue(w, r, issue, http.StatusUnprocessableEntity, data{
			"CommentError": "Write a comment (up to 10,000 characters).", "CommentBody": body,
		})
		return
	}
	err := a.insertComment(r.Context(), Comment{
		IssueID: issue.ID, Body: body, Status: "pending", LikelyBot: r.FormValue("homepage") != "",
	})
	if err != nil {
		a.serverError(w, err)
		return
	}
	a.commentLimit.add(ip)
	http.Redirect(w, r, fmt.Sprintf("/issues/%d?commented=1#comment-form", issue.Num()), http.StatusSeeOther)
}

// serveUpload serves a screenshot only once its issue has been approved.
func (a *app) serveUpload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !screenshotName.MatchString(name) {
		a.notFound(w, r)
		return
	}
	var public bool
	err := a.db.QueryRow(r.Context(), `
		SELECT EXISTS (SELECT 1 FROM issues WHERE $1 = ANY (screenshots) AND status IN ('open', 'closed'))`,
		name).Scan(&public)
	if err != nil {
		a.serverError(w, err)
		return
	}
	if !public {
		a.notFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, filepath.Join(a.uploadDir, name))
}
