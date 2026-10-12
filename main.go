package main

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql templates static
var embedded embed.FS

// India has no DST, so a fixed zone is enough and avoids shipping tzdata.
var ist = time.FixedZone("IST", 5*3600+30*60)

type app struct {
	db           *pgxpool.Pool
	pages        map[string]*template.Template
	uploadDir    string
	baseURL      string
	adminUser    string
	adminPass    string
	behindProxy  bool
	issueLimit   *limiter
	commentLimit *limiter
}

func main() {
	a := &app{
		uploadDir:    env("UPLOAD_DIR", "uploads"),
		baseURL:      os.Getenv("BASE_URL"),
		adminUser:    env("ADMIN_USER", "admin"),
		adminPass:    os.Getenv("ADMIN_PASSWORD"),
		behindProxy:  os.Getenv("BEHIND_PROXY") == "1",
		issueLimit:   newLimiter(5, time.Hour),
		commentLimit: newLimiter(20, time.Hour),
	}
	if a.adminPass == "" {
		log.Fatal("ADMIN_PASSWORD must be set")
	}

	ctx := context.Background()
	db, err := connect(ctx, env("DATABASE_URL", "postgres://babu:babu@localhost:5432/babu?sslmode=disable"))
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

	static, _ := fs.Sub(embedded, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))

	mux.HandleFunc("GET /{$}", a.home)
	mux.HandleFunc("GET /issues", a.listIssues)
	mux.HandleFunc("GET /issues/new", a.newIssue)
	mux.HandleFunc("POST /issues", a.createIssue)
	mux.HandleFunc("GET /issues/{number}", a.showIssue)
	mux.HandleFunc("POST /issues/{number}/comments", a.createComment)
	mux.HandleFunc("GET /uploads/{name}", a.serveUpload)

	mux.HandleFunc("GET /admin", a.admin(a.adminHome))
	mux.HandleFunc("GET /admin/find", a.admin(a.adminFind))
	mux.HandleFunc("GET /admin/issues/{id}", a.admin(a.adminIssue))
	mux.HandleFunc("POST /admin/issues/{id}", a.admin(a.adminIssueAction))
	mux.HandleFunc("POST /admin/issues/{id}/screenshots/remove", a.admin(a.adminRemoveScreenshot))
	mux.HandleFunc("POST /admin/comments/{id}", a.admin(a.adminCommentAction))
	mux.HandleFunc("GET /admin/uploads/{name}", a.admin(a.adminServeUpload))

	mux.HandleFunc("/", a.notFound)

	return securityHeaders(http.NewCrossOriginProtection().Handler(mux))
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
