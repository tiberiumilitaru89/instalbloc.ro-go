package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"instalbloc/internal/domain"
	"instalbloc/internal/middleware"
)

func TestHomeAndCSPHeaders(t *testing.T) {
	seo := NewSEOHandler(func(_ *http.Request) domain.SessionView {
		return domain.SessionView{}
	})

	handler := middleware.SecurityHeaders(http.HandlerFunc(seo.Home))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self' 'nonce-") {
		t.Fatalf("expected CSP to contain script-src 'self' 'nonce-...', got %q", csp)
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") || strings.Contains(csp, "strict-dynamic") {
		t.Fatalf("CSP must not contain unsafe-inline, unsafe-eval, or strict-dynamic: %q", csp)
	}

	body := rec.Body.String()
	for _, cat := range domain.ServiceCategories {
		if !strings.Contains(body, cat) {
			t.Errorf("expected rendered home page to contain category %q", cat)
		}
	}
}

func TestRobotsAndSitemap(t *testing.T) {
	seo := NewSEOHandler(nil)

	reqRobots := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	recRobots := httptest.NewRecorder()
	seo.RobotsTXT(recRobots, reqRobots)

	if recRobots.Code != http.StatusOK {
		t.Fatalf("expected status 200 for robots.txt, got %d", recRobots.Code)
	}
	if !strings.Contains(recRobots.Body.String(), "Disallow: /platform-admin/") {
		t.Fatalf("expected robots.txt to disallow /platform-admin/")
	}

	reqSitemap := httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil)
	recSitemap := httptest.NewRecorder()
	seo.SitemapXML(recSitemap, reqSitemap)

	if recSitemap.Code != http.StatusOK {
		t.Fatalf("expected status 200 for sitemap.xml, got %d", recSitemap.Code)
	}
	if !strings.Contains(recSitemap.Body.String(), "<urlset") {
		t.Fatalf("expected valid XML urlset in sitemap.xml")
	}
}
