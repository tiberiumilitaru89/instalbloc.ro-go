---
id: instalbloc-root-index
title: "Harta de Cunoaștere Canonică: InstalBloc (Go + Templ + HTMX)"
domain: instalbloc-architecture
stack: go-templ-htmx
tenancy: multi-tenant
rendering: hybrid-ssr
ledger_active: true
last_verified: 2026-10-21
dependencies: []
---

# Harta de Cunoaștere Canonică (OKF) — InstalBloc (Go Edition)

Acest document reprezintă Sursa Unică de Adevăr (SSOT) pentru platforma de management asociații de proprietari, intervenții tehnice și avarii `InstalBloc`, rescrisă în arhitectura **Go + Templ + HTMX + Alpine.js**:

| Criteriu | Specificație Arhitecturală Canonică |
| :--- | :--- |
| **Stack & Framework** | Go 1.23+, Chi Router v5, Templ (Type-Safe HTML), HTMX 2.x, Alpine.js 3.x, `sqlc`, SQLite (`modernc.org/sqlite`, zero CGO), Tailwind CSS v4 CLI |
| **Profil Arhitectural** | `stack: go-templ-htmx`, `tenancy: multi-tenant`, `rendering: hybrid-ssr`, `ledger_active: true` |
| **Găzduire & Runtime** | Binar unic Go (`go:embed`) + SQLite în modul WAL (`busy_timeout=5000`, `foreign_keys=ON`), protejat prin Cloudflare CDN & Proxy |
| **PWA (Android & iOS)** | `manifest.json` + Service Worker `/sw.js` + pictograma oficială picătura portocalie (`#f97316`) în `/icon.svg`, `/icon-192.png`, `/icon-512.png`, `/icon-maskable-512.png`, `/apple-touch-icon.png` |
| **SEO & Indexare** | Indexare publică activă (`/robots.txt`, `/sitemap.xml`, Schema.org JSON-LD cu `nonce`) |
| **Stocare Media (Object Storage)** | **Cloudflare R2** (S3 compatible via AWS SDK Go v2), domeniu dedicat `media.instalbloc.ro` |
| **Izolare Exclusivă Super Admin** | Super Adminii (`oana.becheanu@yahoo.com` și `george.becheanu11@yahoo.com`) au profil unic `primaryRole: "platform_admin"`, nu pot deține sau crea roluri de platformă (`locatar`, `asociatie`, `prestator`) și sunt direcționați exclusiv în `/platform-admin` |
| **Subscripții & Rapoarte** | Ciclu de abonare de 1 lună (30 zile) cu ștampilă exactă de dată și oră (`DD.MM.YYYY, HH:mm:ss`), buton de Reînnoire Automată Subscripție în panoul fiecărui utilizator și în `/platform-admin`, plus rapoarte multi-destinatar |
| **Anti-Duplicat Entități** | `internal/services/normalizer.go`: o singură adresă de email permisă per Firmă / Asociație / Client Independent (verificare case-insensitive și fără diacritice pe denumire/CUI/telefon) |
| **Politici CSP** | Regimul 2 (SSR Dinamic) cu Nonce criptografic (`script-src 'self' 'nonce-${nonce}'; worker-src 'self'; manifest-src 'self'`), zero `'unsafe-inline'` pe scripturi, zero `innerHTML` |
| **Bariere Mecanice** | Gatekeeper strict: `go vet ./... && go test ./...` |

---

## Noduri de Cunoaștere Active

1. **Business, Roluri & Flux de Lucrări (Panou Lucrări):**
   - [`business-rules/roles_and_workflow.md`](./business-rules/roles_and_workflow.md)
2. **Model de Date, Anti-Duplicat & Integritate Relațională:**
   - [`database/schema_and_fsm.md`](./database/schema_and_fsm.md)
3. **Securitate, Sesiuni, CSP, SEO & PWA:**
   - [`security/auth_and_session.md`](./security/auth_and_session.md)
