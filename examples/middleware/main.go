// HTTP middleware: extract user → Get → Allow → 200 or 429 with rate-limit headers.
//
// Run: go run ./examples/middleware/
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"time"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
	"github.com/swasthikshetty10/go-ratelimiter/limiter/inmemory"
	"github.com/swasthikshetty10/go-ratelimiter/manager"
)

func rateLimitMiddleware(mgr *manager.Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("X-User-ID")
		if userID == "" {
			http.Error(w, "missing X-User-ID", http.StatusBadRequest)
			return
		}

		lim, err := mgr.Get(userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		result := lim.Allow()
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(result.Limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))

		if !result.Allowed {
			if result.RetryAfter > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())))
			}
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, "rate limit exceeded")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	mgr, err := manager.New(manager.Config{
		NewLimiter: func(_ string) (limiter.Limiter, error) {
			return inmemory.New(
				limiter.AlgorithmSlidingWindowCounter,
				limiter.WithLimit(2),
				limiter.WithWindow(time.Minute),
			)
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})

	srv := httptest.NewServer(rateLimitMiddleware(mgr, api))
	defer srv.Close()

	client := srv.Client()
	do := func(userID string) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api", nil)
		req.Header.Set("X-User-ID", userID)
		resp, err := client.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("user=%-6s status=%d remaining=%s retry-after=%s body=%s\n",
			userID,
			resp.StatusCode,
			resp.Header.Get("X-RateLimit-Remaining"),
			resp.Header.Get("Retry-After"),
			body,
		)
	}

	do("alice")
	do("alice")
	do("alice") // 429
	do("bob")   // separate bucket
}
