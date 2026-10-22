package public

import "embed"

// FS embeds all static assets (CSS, HTMX, Alpine CSP, PWA manifest, Service Worker, and icons)
// into the single standalone Go binary.
//
//go:embed css/* js/* *.png *.svg *.webp manifest.json sw.js
var FS embed.FS
