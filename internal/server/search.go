package server

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
)

// filter is an issues search. The query box takes GitHub-style qualifiers:
// label:x (repeatable)  service:x  author:name|anonymous  site:text  sort:oldest|comments|seen  words…
// Open/closed isn't part of the query: lists show still-broken issues unless the Closed tab
// (?state=closed) is picked.
type filter struct {
	Closed  bool
	Labels  []string
	Service string
	Author  string
	Site    string
	Sort    string // "" (newest), "oldest", "comments", "seen" (broken longest)
	Words   []string
}

var sorts = []struct{ Key, Name string }{
	{"", "Newest"},
	{"oldest", "Oldest"},
	{"comments", "Most commented"},
	{"seen", "Broken longest"},
}

func parseFilter(q string) filter {
	var f filter
	for _, tok := range tokenize(q) {
		key, val, ok := strings.Cut(tok, ":")
		if ok && val != "" {
			switch strings.ToLower(key) {
			case "is": // open/closed is chosen with the tabs, not the query
				continue
			case "service":
				f.Service = val
				continue
			case "label":
				if l := strings.ToLower(val); !slices.Contains(f.Labels, l) {
					f.Labels = append(f.Labels, l)
				}
				continue
			case "author":
				f.Author = strings.ToLower(strings.TrimPrefix(val, "@"))
				continue
			case "site":
				f.Site = val
				continue
			case "sort":
				v := strings.ToLower(val)
				if slices.ContainsFunc(sorts, func(s struct{ Key, Name string }) bool { return s.Key == v }) {
					f.Sort = v
					continue
				}
			}
		}
		f.Words = append(f.Words, tok)
	}
	return f
}

// tokenize splits on spaces, keeping "quoted phrases" (and key:"quoted values") together.
func tokenize(q string) []string {
	var toks []string
	var cur strings.Builder
	inQuote := false
	for _, r := range q {
		switch {
		case r == '"':
			inQuote = !inQuote
		case unicode.IsSpace(r) && !inQuote:
			if cur.Len() > 0 {
				toks = append(toks, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		toks = append(toks, cur.String())
	}
	return toks
}

func quoteIfSpaced(s string) string {
	if strings.ContainsFunc(s, unicode.IsSpace) {
		return `"` + s + `"`
	}
	return s
}

// String is the canonical query, shown in the search box.
func (f filter) String() string {
	var parts []string
	for _, l := range f.Labels {
		parts = append(parts, "label:"+l)
	}
	if f.Service != "" {
		parts = append(parts, "service:"+quoteIfSpaced(f.Service))
	}
	if f.Author != "" {
		parts = append(parts, "author:"+f.Author)
	}
	if f.Site != "" {
		parts = append(parts, "site:"+quoteIfSpaced(f.Site))
	}
	if f.Sort != "" {
		parts = append(parts, "sort:"+f.Sort)
	}
	for _, w := range f.Words {
		parts = append(parts, quoteIfSpaced(w))
	}
	return strings.Join(parts, " ")
}

func (f filter) URL(page int) string {
	v := url.Values{}
	if q := f.String(); q != "" {
		v.Set("q", q)
	}
	if f.Closed {
		v.Set("state", "closed")
	}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if len(v) == 0 {
		return "/"
	}
	return "/?" + v.Encode()
}

func (f filter) withClosed(c bool) filter { f.Closed = c; return f }
func (f filter) withSort(s string) filter { f.Sort = s; return f }

// withService picks a service, or clears it if it's already the one picked.
func (f filter) withService(s string) filter {
	if strings.EqualFold(f.Service, s) {
		s = ""
	}
	f.Service = s
	return f
}

func (f filter) toggleLabel(l string) filter {
	if slices.Contains(f.Labels, l) {
		f.Labels = slices.DeleteFunc(slices.Clone(f.Labels), func(x string) bool { return x == l })
	} else {
		f.Labels = append(slices.Clone(f.Labels), l)
	}
	return f
}

// Links used around the site to jump into a filtered list.
func labelURL(l string) string   { return filter{Labels: []string{l}}.URL(1) }
func serviceURL(s string) string { return filter{Service: s}.URL(1) }
func siteURL(s string) string    { return filter{Site: siteName(s)}.URL(1) }
func authorURL(a string) string  { return filter{Author: a}.URL(1) }

// like turns user text into an ILIKE pattern matching it anywhere, with wildcards escaped.
func like(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
}

// where builds the SQL condition and args. Removed issues are never included.
func (f filter) where(withState bool) (string, []any) {
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	conds := []string{"i.status <> 'removed'"}
	if withState {
		conds = append(conds, map[bool]string{false: "i.status = 'open'", true: "i.status = 'closed'"}[f.Closed])
	}
	if f.Service != "" {
		conds = append(conds, "lower(i.service) = lower("+arg(f.Service)+")")
	}
	for _, l := range f.Labels {
		conds = append(conds, arg(l)+" = ANY (i.labels)")
	}
	switch f.Author {
	case "":
	case "anonymous":
		conds = append(conds, "i.author_id IS NULL")
	default:
		conds = append(conds, "u.username = "+arg(f.Author))
	}
	if f.Site != "" {
		conds = append(conds, "i.site ILIKE "+arg(like(f.Site)))
	}
	for _, w := range f.Words {
		p := arg(like(w))
		conds = append(conds, fmt.Sprintf("(i.title ILIKE %[1]s OR i.site ILIKE %[1]s OR i.site_said ILIKE %[1]s OR i.body ILIKE %[1]s)", p))
	}
	return strings.Join(conds, " AND "), args
}

func (f filter) orderBy() string {
	switch f.Sort {
	case "oldest":
		return "i.id ASC"
	case "comments":
		return "comment_count DESC, i.id DESC"
	case "seen":
		return "i.seen_at ASC, i.id ASC"
	}
	return "i.id DESC"
}

// searchIssues returns one page of matching issues and whether a next page exists.
func (a *app) searchIssues(ctx context.Context, f filter, page int) ([]Issue, bool, error) {
	where, args := f.where(true)
	args = append(args, pageSize+1, (page-1)*pageSize)
	rows, err := a.db.Query(ctx, `
		SELECT `+issueCols+`,
		       (SELECT count(*) FROM comments c WHERE c.issue_id = i.id AND c.status = 'visible') AS comment_count
		`+issueFrom+`
		WHERE `+where+`
		ORDER BY `+f.orderBy()+fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
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

// countByState counts matching issues per state (still broken and closed).
func (a *app) countByState(ctx context.Context, f filter) (open, closed int, err error) {
	where, args := f.where(false)
	err = a.db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE i.status = 'open'), count(*) FILTER (WHERE i.status = 'closed')
		`+issueFrom+` WHERE `+where, args...).Scan(&open, &closed)
	return
}

// usedLabels lists labels on public issues, for the filter menu. triage is internal.
func (a *app) usedLabels(ctx context.Context) ([]string, error) {
	rows, err := a.db.Query(ctx, `
		SELECT DISTINCT l FROM issues, unnest(labels) AS l
		WHERE status <> 'removed' AND l <> 'triage'
		ORDER BY l`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// usedServices lists services on public issues, for the filter menu and the admin form.
func (a *app) usedServices(ctx context.Context) ([]string, error) {
	rows, err := a.db.Query(ctx, `
		SELECT DISTINCT service FROM issues
		WHERE status <> 'removed' AND service <> ''
		ORDER BY service`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// serviceHosts maps well-known domains to a service, so most issues arrive pre-filled.
// Admins set or correct the service for everything else.
var serviceHosts = map[string]string{
	"irctc.co.in":               "IRCTC",
	"enquiry.indianrail.gov.in": "NTES",
	"passportindia.gov.in":      "Passport Seva",
	"incometax.gov.in":          "Income Tax",
	"uidai.gov.in":              "Aadhaar",
}

func guessService(site string) string {
	host := strings.ToLower(siteName(site))
	for h, s := range serviceHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return s
		}
	}
	if looksLikeURL(site) {
		return ""
	}
	// App names like "IRCTC Rail Connect app".
	lower := strings.ToLower(site)
	for _, s := range []string{"IRCTC", "NTES"} {
		if strings.Contains(lower, strings.ToLower(s)) {
			return s
		}
	}
	return ""
}

type serviceCount struct {
	Name               string
	Open, Closed       int
	OpenURL, ClosedURL string
}

// serviceCounts lists every service with its still-broken and closed counts, most broken first,
// plus how many issues have no service yet.
func (a *app) serviceCounts(ctx context.Context) ([]serviceCount, int, error) {
	rows, err := a.db.Query(ctx, `
		SELECT service, count(*) FILTER (WHERE status = 'open'), count(*) FILTER (WHERE status = 'closed')
		FROM issues WHERE status <> 'removed' AND service <> ''
		GROUP BY service
		ORDER BY 2 DESC, lower(service)`)
	if err != nil {
		return nil, 0, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (serviceCount, error) {
		var s serviceCount
		err := row.Scan(&s.Name, &s.Open, &s.Closed)
		s.OpenURL = filter{Service: s.Name}.URL(1)
		s.ClosedURL = filter{Service: s.Name, Closed: true}.URL(1)
		return s, err
	})
	if err != nil {
		return nil, 0, err
	}
	var unassigned int
	err = a.db.QueryRow(ctx, `SELECT count(*) FROM issues WHERE status = 'open' AND service = ''`).Scan(&unassigned)
	return list, unassigned, err
}
