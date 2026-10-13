package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/oauth2"
)

type User struct {
	ID       int64
	Username string // empty until chosen on first login
	IsAdmin  bool
}

type userKey struct{}

// currentUser is the logged-in user who has picked a username, or nil.
func currentUser(r *http.Request) *User {
	if r == nil {
		return nil
	}
	u, _ := r.Context().Value(userKey{}).(*User)
	return u
}

const (
	sessionCookie = "session"
	oauthCookie   = "oauth"
	sessionTTL    = 30 * 24 * time.Hour
)

// googleAuth is nil when GOOGLE_CLIENT_ID isn't set (local dev uses DEV_LOGIN instead).
type googleAuth struct {
	config   oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func newGoogleAuth(ctx context.Context, clientID, secret, baseURL string) (*googleAuth, error) {
	provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, err
	}
	return &googleAuth{
		config: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: secret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  strings.TrimRight(baseURL, "/") + "/auth/callback",
			Scopes:       []string{oidc.ScopeOpenID}, // no "profile" or "email": we only need the account ID
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
	}, nil
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func randomToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// withUser loads the session's user into the request context. Logged-in users who haven't
// picked a username yet are sent to /welcome, so they can't post anonymously by accident.
func (a *app) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		var u User
		var username *string
		err = a.db.QueryRow(r.Context(), `
			SELECT u.id, u.username FROM sessions s JOIN users u ON u.id = s.user_id
			WHERE s.token_hash = $1 AND s.expires_at > now()`, hashToken(c.Value)).Scan(&u.ID, &username)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		if username == nil {
			p := r.URL.Path
			if p != "/welcome" && p != "/logout" && !strings.HasPrefix(p, "/static/") {
				http.Redirect(w, r, "/welcome", http.StatusSeeOther)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		u.Username = *username
		u.IsAdmin = slices.Contains(a.adminUsers, u.Username)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, &u)))
	})
}

func (a *app) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		if u == nil {
			http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusSeeOther)
			return
		}
		if !u.IsAdmin {
			a.notFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		h(w, r)
	}
}

// safeNext keeps post-login redirects on this site.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	return next
}

type oauthState struct {
	State, Nonce, Verifier, Next string
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.FormValue("next"))
	if a.google == nil {
		a.render(w, r, http.StatusOK, "login", data{"Title": "Log in", "Next": next})
		return
	}
	st := oauthState{State: randomToken(), Nonce: randomToken(), Verifier: oauth2.GenerateVerifier(), Next: next}
	raw, _ := json.Marshal(st)
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: base64.RawURLEncoding.EncodeToString(raw), Path: "/auth/callback",
		MaxAge: 600, HttpOnly: true, Secure: a.secureCookies, SameSite: http.SameSiteLaxMode,
	})
	url := a.google.config.AuthCodeURL(st.State, oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.Verifier))
	http.Redirect(w, r, url, http.StatusFound)
}

func (a *app) authCallback(w http.ResponseWriter, r *http.Request) {
	if a.google == nil {
		a.notFound(w, r)
		return
	}
	fail := func() {
		a.renderError(w, r, http.StatusBadRequest, "Login didn't complete. Please try again.")
	}
	c, err := r.Cookie(oauthCookie)
	if err != nil {
		fail()
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Path: "/auth/callback", MaxAge: -1})
	var st oauthState
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || json.Unmarshal(raw, &st) != nil || st.State == "" || r.FormValue("state") != st.State {
		fail()
		return
	}
	tok, err := a.google.config.Exchange(r.Context(), r.FormValue("code"), oauth2.VerifierOption(st.Verifier))
	if err != nil {
		fail()
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	idToken, err := a.google.verifier.Verify(r.Context(), rawID)
	if err != nil || idToken.Nonce != st.Nonce {
		fail()
		return
	}
	a.startSession(w, r, "google:"+idToken.Subject, st.Next)
}

// loginDev lets you log in as any name locally, without Google. Only routed when DEV_LOGIN=1.
func (a *app) loginDev(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		a.renderError(w, r, http.StatusBadRequest, "Enter a name.")
		return
	}
	a.startSession(w, r, "dev:"+name, safeNext(r.FormValue("next")))
}

func (a *app) startSession(w http.ResponseWriter, r *http.Request, sub, next string) {
	ctx := r.Context()
	var userID int64
	err := a.db.QueryRow(ctx, `
		INSERT INTO users (google_sub) VALUES ($1)
		ON CONFLICT (google_sub) DO UPDATE SET google_sub = excluded.google_sub
		RETURNING id`, sub).Scan(&userID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	token := randomToken()
	_, err = a.db.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		hashToken(token), userID, time.Now().Add(sessionTTL))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, Secure: a.secureCookies, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.db.Exec(r.Context(), `DELETE FROM sessions WHERE token_hash = $1`, hashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9_-]{3,30}$`)

var reservedUsernames = []string{"admin", "anonymous", "babu-log", "fixmydeshboard", "deleted-user", "moderator"}

func (a *app) welcome(w http.ResponseWriter, r *http.Request) {
	if currentUser(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if r.Method == http.MethodGet {
		a.render(w, r, http.StatusOK, "welcome", data{"Title": "Choose a username"})
		return
	}

	name := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	fail := func(msg string) {
		a.render(w, r, http.StatusUnprocessableEntity, "welcome",
			data{"Title": "Choose a username", "Error": msg, "Username": name})
	}
	if !usernamePattern.MatchString(name) || slices.Contains(reservedUsernames, name) {
		fail("Use 3–30 lowercase letters, numbers, - or _.")
		return
	}
	tag, err := a.db.Exec(r.Context(), `
		UPDATE users SET username = $2
		WHERE username IS NULL AND id = (SELECT user_id FROM sessions WHERE token_hash = $1 AND expires_at > now())`,
		hashToken(c.Value), name)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		fail("That username is taken.")
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// actorKey is what rate limits count against: the account when logged in, otherwise the IP.
func (a *app) actorKey(r *http.Request) string {
	if u := currentUser(r); u != nil {
		return "user:" + u.Username
	}
	return "ip:" + a.clientIP(r)
}
