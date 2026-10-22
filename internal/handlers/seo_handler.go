package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"instalbloc/internal/domain"
	"instalbloc/internal/middleware"
	"instalbloc/internal/views/layouts"
	"instalbloc/internal/views/pages"
)

const canonicalBaseURL = "https://www.instalbloc.ro"

type SEOHandler struct {
	SessionResolver func(r *http.Request) domain.SessionView
}

func NewSEOHandler(resolver func(r *http.Request) domain.SessionView) *SEOHandler {
	return &SEOHandler{
		SessionResolver: resolver,
	}
}

func (h *SEOHandler) Home(w http.ResponseWriter, r *http.Request) {
	var session domain.SessionView
	if h.SessionResolver != nil {
		session = h.SessionResolver(r)
	}

	meta := layouts.PageMeta{
		Title:        "Reparații Sanitare, Termice și Lucrări Blocuri România",
		Description:  "Platforma națională dedicată asociațiilor de proprietari, administratorilor de bloc și locatarilor din România. Conectăm rapid blocurile cu instalatori autorizați, firme de hidroizolații, centrale termice, interfoane și intervenții de urgență.",
		CanonicalURL: canonicalBaseURL + "/",
		NoIndex:      false,
		CSPNonce:     middleware.NonceFromContext(r.Context()),
		CurrentYear:  time.Now().Year(),
		Session:      session,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.Home(meta).Render(r.Context(), w)
}

func (h *SEOHandler) RobotsTXT(w http.ResponseWriter, _ *http.Request) {
	var sb strings.Builder
	sb.WriteString("User-agent: *\n")
	sb.WriteString("Allow: /\n")
	sb.WriteString("Disallow: /platform-admin/\n")
	sb.WriteString("Disallow: /dashboard/\n")
	sb.WriteString("Disallow: /associations/\n")
	sb.WriteString("Disallow: /api/\n")
	sb.WriteString("Disallow: /requests/new\n\n")
	sb.WriteString("Sitemap: " + canonicalBaseURL + "/sitemap.xml\n")

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(sb.String()))
}

type sitemapEntry struct {
	Path       string
	ChangeFreq string
	Priority   string
}

func (h *SEOHandler) SitemapXML(w http.ResponseWriter, _ *http.Request) {
	entries := []sitemapEntry{
		{Path: "/", ChangeFreq: "daily", Priority: "1.0"},
		{Path: "/onboarding", ChangeFreq: "weekly", Priority: "0.9"},
		{Path: "/login", ChangeFreq: "monthly", Priority: "0.7"},
		{Path: "/garantie", ChangeFreq: "monthly", Priority: "0.8"},
		{Path: "/termeni", ChangeFreq: "monthly", Priority: "0.5"},
		{Path: "/gdpr", ChangeFreq: "monthly", Priority: "0.5"},
		{Path: "/cookies", ChangeFreq: "monthly", Priority: "0.5"},
	}

	lastMod := time.Now().UTC().Format("2006-01-02")
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, entry := range entries {
		sb.WriteString(fmt.Sprintf(
			"  <url>\n    <loc>%s%s</loc>\n    <lastmod>%s</lastmod>\n    <changefreq>%s</changefreq>\n    <priority>%s</priority>\n  </url>\n",
			canonicalBaseURL,
			entry.Path,
			lastMod,
			entry.ChangeFreq,
			entry.Priority,
		))
	}
	sb.WriteString(`</urlset>` + "\n")

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(sb.String()))
}
