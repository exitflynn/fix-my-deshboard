package main

import (
	"slices"
	"strings"
	"testing"
)

func TestTemplatesParse(t *testing.T) {
	if _, err := parsePages(); err != nil {
		t.Fatal(err)
	}
}

func TestMarkdownSanitized(t *testing.T) {
	out := string(renderMarkdown("**ok** <script>x()</script> ![t](https://evil.example/t.png) [l](javascript:alert(1))"))
	for _, bad := range []string{"<script", "<img", "javascript:"} {
		if strings.Contains(out, bad) {
			t.Errorf("output contains %q: %s", bad, out)
		}
	}
	if !strings.Contains(out, "<strong>ok</strong>") {
		t.Errorf("markdown not rendered: %s", out)
	}
}

func TestParseLabels(t *testing.T) {
	got := parseLabels("IRCTC, outage  outage,bad label!, stale-data")
	want := []string{"irctc", "outage", "bad", "stale-data"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/issues/3":         "/issues/3",
		"//evil.example":    "/issues",
		"https://evil.test": "/issues",
		"/\\evil.example":   "/issues",
		"":                  "/issues",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseFilter(t *testing.T) {
	f := parseFilter(`is:closed label:Stale-Data label:outage service:"Passport Seva" author:@Rail_Watcher site:"rail connect" sort:seen book now sort:bogus`)
	if f.Closed || !slices.Equal(f.Labels, []string{"stale-data", "outage"}) || f.Service != "Passport Seva" ||
		f.Author != "rail_watcher" || f.Site != "rail connect" || f.Sort != "seen" ||
		!slices.Equal(f.Words, []string{"book", "now", "sort:bogus"}) {
		t.Fatalf("parsed wrong: %+v", f)
	}
	want := `label:stale-data label:outage service:"Passport Seva" author:rail_watcher site:"rail connect" sort:seen book now sort:bogus`
	if got := f.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if again := parseFilter(f.String()).String(); again != want {
		t.Errorf("round trip = %q", again)
	}
}

func TestFilterURL(t *testing.T) {
	if got := (filter{}).URL(1); got != "/issues" {
		t.Errorf("default filter URL = %q", got)
	}
	if got := (filter{Closed: true}).URL(2); got != "/issues?page=2&state=closed" {
		t.Errorf("closed filter URL = %q", got)
	}
	f := parseFilter("label:a")
	if got := f.toggleLabel("a").String(); got != "" {
		t.Errorf("toggle off = %q", got)
	}
	if got := f.toggleLabel("b").String(); got != "label:a label:b" {
		t.Errorf("toggle on = %q", got)
	}
	if !slices.Equal(f.Labels, []string{"a"}) {
		t.Errorf("toggleLabel mutated the original: %v", f.Labels)
	}
	if got := f.withService("IRCTC").withService("irctc").Service; got != "" {
		t.Errorf("picking the same service twice should clear it, got %q", got)
	}
}

func TestGuessService(t *testing.T) {
	for site, want := range map[string]string{
		"https://www.irctc.co.in/nget/train-search": "IRCTC",
		"https://enquiry.indianrail.gov.in/mntes/":  "NTES",
		"https://services1.passportindia.gov.in/x":  "Passport Seva",
		"IRCTC Rail Connect app":                    "IRCTC",
		"https://rajasthan.gov.in/":                 "",
		"https://notirctc.co.in.evil.example/":      "",
	} {
		if got := guessService(site); got != want {
			t.Errorf("guessService(%q) = %q, want %q", site, got, want)
		}
	}
}

func TestLikeEscapes(t *testing.T) {
	if got := like(`50%_off\`); got != `%50\%\_off\\%` {
		t.Errorf("like = %q", got)
	}
}
