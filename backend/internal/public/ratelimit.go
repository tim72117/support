package public

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// limiter is a small fixed-window counter keyed by an arbitrary string (an IP
// or a conversation id). In-memory and per-process: good enough to blunt a
// single abusive client, NOT a distributed limit (several backend instances
// each keep their own counts) and not a substitute for edge protection.
type limiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*window
	now    func() time.Time
}

type window struct {
	start time.Time
	count int
}

func newLimiter(limit int, w time.Duration) *limiter {
	return &limiter{limit: limit, window: w, hits: map[string]*window{}, now: time.Now}
}

// allow reports whether key may make one more request in its current window.
func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.hits) > 20000 { // sweep expired entries so the map cannot grow without bound
		for k, w := range l.hits {
			if now.Sub(w.start) >= l.window {
				delete(l.hits, k)
			}
		}
	}
	w, ok := l.hits[key]
	if !ok || now.Sub(w.start) >= l.window {
		l.hits[key] = &window{start: now, count: 1}
		return true
	}
	if w.count >= l.limit {
		return false
	}
	w.count++
	return true
}

// clientIP is the caller's address. Behind a reverse proxy every request
// shares the proxy's RemoteAddr, so with trustProxy the right-most
// X-Forwarded-For entry (the one our own proxy appended; left-most entries are
// client-controlled and spoofable) is used instead.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
