package main

import (
	"bytes"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// Websites offered in the File an Issue form. The chosen one becomes the issue's first label.
var websites = []struct{ Label, Name string }{
	{"irctc", "IRCTC"},
	{"ntes", "NTES"},
	{"passport-seva", "Passport Seva"},
	{"other", "Other"},
}

var labelColors = map[string]string{
	"irctc":         "#1d76db",
	"ntes":          "#0e8a16",
	"passport-seva": "#5319e7",
	"other":         "#a8a396",
	"triage":        "#d4a017",
	"accepted":      "#0e8a16",
	"outage":        "#d73a4a",
	"misleading-ui": "#e0a800",
	"stale-data":    "#6aa6d8",
	"broken-table":  "#e07b5f",
}

func knownLabels() []string {
	var out []string
	for _, w := range websites {
		out = append(out, w.Label)
	}
	return append(out, "triage", "accepted", "outage", "misleading-ui", "stale-data", "broken-table")
}

// labelStyle sets the colour of the label's dot.
func labelStyle(name string) template.CSS {
	c, ok := labelColors[name]
	if !ok {
		c = "#a8a396"
	}
	return template.CSS("--c:" + c)
}

var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM)) // raw HTML is dropped by default

// Explicit allow-list. <img> is deliberately absent: remote images leak viewers' IPs
// and can be swapped after approval. Screenshots go through uploads instead.
var sanitizer = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowStandardURLs()
	p.AllowAttrs("href").OnElements("a")
	p.RequireNoFollowOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.AllowElements("p", "br", "hr", "strong", "em", "del", "code", "pre", "blockquote",
		"ul", "ol", "li", "h1", "h2", "h3", "h4", "h5", "h6",
		"table", "thead", "tbody", "tr", "th", "td")
	p.AllowAttrs("start").Matching(bluemonday.Integer).OnElements("ol")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").OnElements("input")
	return p
}()

func renderMarkdown(s string) template.HTML {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(s), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(s))
	}
	return template.HTML(sanitizer.SanitizeBytes(buf.Bytes()))
}

func issuesURL(state, label string, page int) string {
	q := url.Values{}
	if state == "closed" {
		q.Set("state", "closed")
	}
	if label != "" {
		q.Set("label", label)
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if len(q) == 0 {
		return "/issues"
	}
	return "/issues?" + q.Encode()
}

// moderators are the ADMIN_USERS usernames; their posts get a "Moderator" badge.
var moderators []string

var funcs = template.FuncMap{
	"md":         renderMarkdown,
	"labelStyle": labelStyle,
	"issuesURL":  issuesURL,
	"join":       strings.Join,
	"hasPrefix":  strings.HasPrefix,
	"isAuthor":   isAuthor,
	"dict": func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	},
	"isModerator": func(name *string) bool { return name != nil && slices.Contains(moderators, *name) },
	"date":        func(t time.Time) string { return t.In(ist).Format("2 Jan 2006, 15:04 IST") },
	"day":         func(t time.Time) string { return t.In(ist).Format("2 Jan 2006") },
}

var pageNames = []string{"home", "list", "new", "issue", "edit_issue", "edit_comment", "login", "welcome",
	"error", "admin", "admin_issue"}

func parsePages() (map[string]*template.Template, error) {
	pages := map[string]*template.Template{}
	for _, name := range pageNames {
		t, err := template.New("").Funcs(funcs).ParseFS(embedded,
			"templates/layout.html", "templates/partials.html", "templates/"+name+".html")
		if err != nil {
			return nil, err
		}
		pages[name] = t
	}
	return pages, nil
}

type data map[string]any

// render executes a page. r may be nil; when set, the logged-in user is passed as .User.
func (a *app) render(w http.ResponseWriter, r *http.Request, status int, page string, d data) {
	d["User"] = currentUser(r)
	if r != nil {
		d["Path"] = r.URL.RequestURI()
	}
	var buf bytes.Buffer
	if err := a.pages[page].ExecuteTemplate(&buf, "layout", d); err != nil {
		log.Printf("render %s: %v", page, err)
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (a *app) renderError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	a.render(w, r, status, "error", data{"Title": http.StatusText(status), "Message": msg})
}

func (a *app) serverError(w http.ResponseWriter, r *http.Request, err error) {
	log.Print(err)
	a.renderError(w, r, http.StatusInternalServerError, "Something went wrong on our side. Please try again.")
}

func (a *app) notFound(w http.ResponseWriter, r *http.Request) {
	a.renderError(w, r, http.StatusNotFound, "That page doesn't exist.")
}
