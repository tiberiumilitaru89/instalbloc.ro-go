package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

type contextKey string

const nonceContextKey contextKey = "csp_nonce"

// NonceFromContext extracts the per-request cryptographic CSP nonce.
func NonceFromContext(ctx context.Context) string {
	if val, ok := ctx.Value(nonceContextKey).(string); ok {
		return val
	}
	return ""
}

// GenerateNonce produces a 128-bit CSPRNG hex string for Content-Security-Policy script tags.
func GenerateNonce() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "fallback-deterministic-nonce"
	}
	return hex.EncodeToString(buf)
}

// SecurityHeaders applies Regime 2 Dynamic SSR Content-Security-Policy with per-request nonce
// and canonical X-Robots-Tag indexing directives.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonce := GenerateNonce()
		ctx := context.WithValue(r.Context(), nonceContextKey, nonce)

		csp := fmt.Sprintf(
			"default-src 'self'; script-src 'self' 'nonce-%s'; worker-src 'self'; manifest-src 'self'; style-src 'self'; img-src 'self' data: https://media.instalbloc.ro; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
			nonce,
		)

		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		path := r.URL.Path
		if isNoIndexRoute(path) {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		} else {
			w.Header().Set("X-Robots-Tag", "index, follow, max-image-preview:large, max-snippet:-1, max-video-preview:-1")
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isNoIndexRoute(path string) bool {
	privatePrefixes := []string{
		"/platform-admin",
		"/api/",
		"/reset-password",
		"/verify-otp",
		"/dashboard",
	}
	for _, prefix := range privatePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
