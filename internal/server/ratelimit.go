package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// limiter is an in-memory sliding-window counter per key. It resets on restart, which is fine.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	l := &limiter{max: max, window: window, hits: map[string][]time.Time{}}
	go func() {
		for range time.Tick(10 * time.Minute) {
			l.mu.Lock()
			for k := range l.hits {
				l.prune(k)
			}
			l.mu.Unlock()
		}
	}()
	return l
}

func (l *limiter) prune(key string) {
	cutoff := time.Now().Add(-l.window)
	h := l.hits[key]
	i := 0
	for i < len(h) && h[i].Before(cutoff) {
		i++
	}
	if i == len(h) {
		delete(l.hits, key)
	} else {
		l.hits[key] = h[i:]
	}
}

// full reports whether key has used up its allowance.
func (l *limiter) full(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key)
	return len(l.hits[key]) >= l.max
}

// add records one use. Called only after a successful submission.
func (l *limiter) add(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hits[key] = append(l.hits[key], time.Now())
}

// clientIP is used only for rate limiting and is never stored.
func (a *app) clientIP(r *http.Request) string {
	if a.behindProxy {
		// Caddy replaces X-Forwarded-For with the real client IP for untrusted clients.
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
