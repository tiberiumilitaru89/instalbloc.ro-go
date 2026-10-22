package auth

import (
	"context"
	"net/http"
	"strings"

	"instalbloc/internal/config"
	"instalbloc/internal/db/sqlc"
	"instalbloc/internal/domain"
)

type sessionContextKey string

const (
	sessionKey        sessionContextKey = "instalbloc_session_ctx"
	SessionCookieName string            = "instalbloc_session"
)

// SessionFromContext retrieves the authenticated SessionView from the request context.
func SessionFromContext(ctx context.Context) domain.SessionView {
	if s, ok := ctx.Value(sessionKey).(domain.SessionView); ok {
		return s
	}
	return domain.SessionView{Role: domain.RoleVisitor}
}

// WithSession injects a SessionView into a context (useful for testing and middleware).
func WithSession(ctx context.Context, session domain.SessionView) context.Context {
	return context.WithValue(ctx, sessionKey, session)
}

// SetSessionCookie writes the signed HMAC-SHA256 session cookie with strict HttpOnly and SameSite flags.
func SetSessionCookie(w http.ResponseWriter, token string, isProd bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   SessionMaxAgeSec,
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie invalidates the session cookie on logout.
func ClearSessionCookie(w http.ResponseWriter, isProd bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteLaxMode,
	})
}

// SessionMiddleware hydrates the user session from the HMAC-signed cookie and enforces
// Exclusive Super Admin isolation across all routes.
func SessionMiddleware(queries *sqlc.Queries, cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := domain.SessionView{Role: domain.RoleVisitor}

			if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
				uid, tokenRole, verifyErr := VerifySessionToken(cookie.Value, cfg.HMACSecret, SessionMaxAgeSec)
				if verifyErr == nil && queries != nil {
					user, dbErr := queries.GetUserByID(r.Context(), uid)
					if dbErr == nil && user.IsActive == 1 {
						isSuper := config.IsSuperAdminEmail(user.Email)
						resolvedRole := tokenRole
						if isSuper {
							resolvedRole = domain.RolePlatformAdmin
						} else if resolvedRole == "" || resolvedRole == domain.RolePlatformAdmin {
							resolvedRole = domain.RoleType(user.PrimaryRole)
						}
						displayName := strings.TrimSpace(user.Name)
						if displayName == "" {
							displayName = user.Email
						}
						session = domain.SessionView{
							UserID:       user.ID,
							Email:        user.Email,
							Name:         displayName,
							Role:         resolvedRole,
							IsSuperAdmin: isSuper,
						}
					}
				}
			}

			path := r.URL.Path

			// Exclusive Super Admin isolation: Super Admins are redirected away from standard platform workflows
			if session.IsSuperAdmin {
				if strings.HasPrefix(path, "/dashboard") ||
					strings.HasPrefix(path, "/associations") ||
					strings.HasPrefix(path, "/onboarding") ||
					strings.HasPrefix(path, "/requests/new") {
					http.Redirect(w, r, "/platform-admin", http.StatusSeeOther)
					return
				}
			}

			// Protect /platform-admin exclusively for Super Admins
			if strings.HasPrefix(path, "/platform-admin") && !session.IsSuperAdmin {
				if session.IsLoggedIn() {
					http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
				} else {
					http.Redirect(w, r, "/login", http.StatusSeeOther)
				}
				return
			}

			ctx := WithSession(r.Context(), session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth ensures the request belongs to an authenticated user; redirects to /login otherwise.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := SessionFromContext(r.Context())
		if !session.IsLoggedIn() {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
