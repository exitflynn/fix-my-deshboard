package server

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql web
var embedded embed.FS

// India has no DST, so a fixed zone is enough and avoids shipping tzdata.
var ist = time.FixedZone("IST", 5*3600+30*60)

type app struct {
	db            *pgxpool.Pool
	pages         map[string]*template.Template
	uploadDir     string
	baseURL       string
	adminUsers    []string
	google        *googleAuth
	devLogin      bool
	secureCookies bool
	behindProxy   bool
	issueLimit    *limiter
	commentLimit  *limiter
}

// Run reads configuration from the environment and serves until it fails.
func Run() {
	a := &app{
		uploadDir:    env("UPLOAD_DIR", "uploads"),
		baseURL:      env("BASE_URL", "http://localhost:8080"),
		devLogin:     os.Getenv("DEV_LOGIN") == "1",
		behindProxy:  os.Getenv("BEHIND_PROXY") == "1",
		issueLimit:   newLimiter(5, time.Hour),
		commentLimit: newLimiter(20, time.Hour),
	}
	a.secureCookies = strings.HasPrefix(a.baseURL, "https://")
	for _, u := range strings.Split(os.Getenv("ADMIN_USERS"), ",") {
		if u = strings.TrimSpace(strings.ToLower(u)); u != "" {
			a.adminUsers = append(a.adminUsers, u)
		}
	}
	moderators = a.adminUsers

	ctx := context.Background()
	if id := os.Getenv("GOOGLE_CLIENT_ID"); id != "" {
		g, err := newGoogleAuth(ctx, id, os.Getenv("GOOGLE_CLIENT_SECRET"), a.baseURL)
		if err != nil {
			log.Fatalf("google login: %v", err)
		}
		a.google = g
	}
	switch {
	case a.devLogin && a.secureCookies:
		log.Fatal("DEV_LOGIN is for local development only; it can't be used with an https BASE_URL")
	case a.devLogin:
		log.Print("DEV_LOGIN is on: anyone can log in as any name")
	case a.google == nil:
		log.Fatal("set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET, or DEV_LOGIN=1 for local development")
	}

	db, err := connect(ctx, env("DATABASE_URL", "postgres://fixmydeshboard:fixmydeshboard@localhost:5432/fixmydeshboard?sslmode=disable"))
	if err != nil {
		log.Fatal(err)
	}
	a.db = db

	if err := os.MkdirAll(a.uploadDir, 0o755); err != nil {
		log.Fatal(err)
	}
	if a.pages, err = parsePages(); err != nil {
		log.Fatal(err)
	}

	addr := env("ADDR", ":8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           a.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
	}
	log.Printf("listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()

	static, _ := fs.Sub(embedded, "web/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))

	mux.HandleFunc("GET /{$}", a.listIssues)
	mux.HandleFunc("GET /issues", a.oldIssuesList)
	mux.HandleFunc("GET /services", a.services)
	mux.HandleFunc("GET /issues/new", a.newIssue)
	mux.HandleFunc("POST /issues", a.createIssue)
	mux.HandleFunc("GET /issues/{number}", a.showIssue)
	mux.HandleFunc("GET /issues/{number}/edit", a.editIssue)
	mux.HandleFunc("POST /issues/{number}/edit", a.editIssue)
	mux.HandleFunc("POST /issues/{number}/comments", a.createComment)
	mux.HandleFunc("GET /comments/{id}/edit", a.editComment)
	mux.HandleFunc("POST /comments/{id}/edit", a.editComment)
	mux.HandleFunc("POST /comments/{id}/delete", a.deleteComment)
	mux.HandleFunc("GET /uploads/{name}", a.serveUpload)
	mux.HandleFunc("POST /theme", a.setTheme)

	mux.HandleFunc("GET /login", a.login)
	mux.HandleFunc("GET /auth/callback", a.authCallback)
	mux.HandleFunc("GET /welcome", a.welcome)
	mux.HandleFunc("POST /welcome", a.welcome)
	mux.HandleFunc("POST /logout", a.logout)
	if a.devLogin {
		mux.HandleFunc("POST /login/dev", a.loginDev)
	}

	mux.HandleFunc("GET /admin", a.requireAdmin(a.adminHome))
	mux.HandleFunc("GET /admin/find", a.requireAdmin(a.adminFind))
	mux.HandleFunc("GET /admin/issues/{id}", a.requireAdmin(a.adminIssue))
	mux.HandleFunc("POST /admin/issues/{id}", a.requireAdmin(a.adminIssueAction))
	mux.HandleFunc("POST /admin/issues/{id}/screenshots/remove", a.requireAdmin(a.adminRemoveScreenshot))
	mux.HandleFunc("POST /admin/comments/{id}", a.requireAdmin(a.adminCommentAction))
	mux.HandleFunc("GET /admin/uploads/{name}", a.requireAdmin(a.adminServeUpload))

	mux.HandleFunc("/", a.notFound)

	return securityHeaders(http.NewCrossOriginProtection().Handler(a.withUser(mux)))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
				"form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}

func connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	// Postgres may still be starting when the app boots.
	for i := 0; ; i++ {
		if err = db.Ping(ctx); err == nil || i == 15 {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return nil, err
	}
	schema, _ := embedded.ReadFile("schema.sql")
	_, err = db.Exec(ctx, string(schema))
	return db, err
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
