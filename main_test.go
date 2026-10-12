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
