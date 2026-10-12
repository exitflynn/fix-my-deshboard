package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"regexp"
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
	"other":         "#6e7781",
	"triage":        "#ededed",
	"accepted":      "#0e8a16",
	"outage":        "#d73a4a",
	"misleading-ui": "#fbca04",
	"stale-data":    "#c5def5",
	"broken-table":  "#f9d0c4",
}

func knownLabels() []string {
	var out []string
	for _, w := range websites {
		out = append(out, w.Label)
	}
	return append(out, "triage", "accepted", "outage", "misleading-ui", "stale-data", "broken-table")
}

func labelStyle(name string) template.CSS {
	bg, ok := labelColors[name]
	if !ok {
		bg = "#6e7781"
	}
	return template.CSS(fmt.Sprintf("background-color:%s;color:%s", bg, textOn(bg)))
}

// textOn picks dark or light text for a #rrggbb background.
func textOn(hex string) string {
	v, _ := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	r, g, b := float64(v>>16&0xff), float64(v>>8&0xff), float64(v&0xff)
	if (0.299*r+0.587*g+0.114*b)/255 > 0.6 {
		return "#1f2328"
	}
	return "#ffffff"
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

var funcs = template.FuncMap{
	"md":         renderMarkdown,
	"labelStyle": labelStyle,
	"issuesURL":  issuesURL,
	"join":       strings.Join,
	"date":       func(t time.Time) string { return t.In(ist).Format("2 Jan 2006, 15:04 IST") },
	"day":        func(t time.Time) string { return t.In(ist).Format("2 Jan 2006") },
}

var pageNames = []string{"home", "list", "new", "issue", "error", "admin", "admin_issue"}

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

func (a *app) render(w http.ResponseWriter, status int, page string, d data) {
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

func (a *app) renderError(w http.ResponseWriter, status int, msg string) {
	a.render(w, status, "error", data{"Title": http.StatusText(status), "Message": msg})
}

func (a *app) serverError(w http.ResponseWriter, err error) {
	log.Print(err)
	a.renderError(w, http.StatusInternalServerError, "Something went wrong on our side. Please try again.")
}

func (a *app) notFound(w http.ResponseWriter, r *http.Request) {
	a.renderError(w, http.StatusNotFound, "That page doesn't exist.")
}
