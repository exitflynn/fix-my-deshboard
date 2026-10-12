package main

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

// filter is a parsed issues search, GitHub style:
// is:open|closed  label:x (repeatable)  author:name|anonymous  site:text  sort:oldest|comments|seen  words…
type filter struct {
	State  string // "open", "closed", or "" for both
	Labels []string
	Author string
	Site   string
	Sort   string // "" (newest), "oldest", "comments", "seen" (broken longest)
	Words  []string
}

const defaultQuery = "is:open"

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
			case "is":
				if v := strings.ToLower(val); v == "open" || v == "closed" {
					f.State = v
					continue
				}
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
	if f.State != "" {
		parts = append(parts, "is:"+f.State)
	}
	for _, l := range f.Labels {
		parts = append(parts, "label:"+l)
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
	if q := f.String(); q != defaultQuery {
		v.Set("q", q)
	}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if len(v) == 0 {
		return "/issues"
	}
	return "/issues?" + v.Encode()
}

func (f filter) withState(s string) filter { f.State = s; return f }
func (f filter) withSort(s string) filter  { f.Sort = s; return f }

func (f filter) toggleLabel(l string) filter {
	if slices.Contains(f.Labels, l) {
		f.Labels = slices.DeleteFunc(slices.Clone(f.Labels), func(x string) bool { return x == l })
	} else {
		f.Labels = append(slices.Clone(f.Labels), l)
	}
	return f
}

// Links used around the site to jump into a filtered list.
func labelURL(l string) string  { return filter{State: "open", Labels: []string{l}}.URL(1) }
func siteURL(s string) string   { return filter{State: "open", Site: siteName(s)}.URL(1) }
func authorURL(a string) string { return filter{Author: a}.URL(1) }

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
	if withState && f.State != "" {
		conds = append(conds, "i.status = "+arg(f.State))
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

// countByState counts matching issues per state, ignoring the filter's own is: qualifier.
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
