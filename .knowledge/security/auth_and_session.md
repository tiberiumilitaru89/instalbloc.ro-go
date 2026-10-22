---
id: instalbloc-security-auth
domain: security
last_verified: 2026-10-21
dependencies: ["instalbloc-root-index"]
---

# Securitate, Autentificare, CSP, SEO & PWA: InstalBloc (Go Edition)

## 1. Modelul de Sesiune HMAC-SHA256 & Izolarea Exclusivă Super Admin (Implementat în Etapa 3)
- **Format Token (`internal/auth/crypto.go`):** `uid.role.timestamp.signature` semnat cu `crypto/hmac` (`sha256`) și verificat în timp constant prin `hmac.Equal`. Expirare implicită: 30 zile (`720h`).
- **Flag-uri Cookie (`internal/auth/middleware.go`):** `Name: instalbloc_session`, `HttpOnly`, `Secure` (în producție), `SameSite=Lax`, `Path=/`, `MaxAge: 2592000` (30 zile).
- **Izolare Strictă Super Admin (`internal/auth/middleware.go`):** Lista `SUPER_ADMIN_EMAILS` conține exclusiv `oana.becheanu@yahoo.com` și `george.becheanu11@yahoo.com`. Aceștia primesc strict `role: "platform_admin"`, sunt redirecționați automat către `/platform-admin` pe rutele operaționale de chiriaș și sunt blocați la înregistrarea în `/onboarding`.

## 2. Hashing Parole PBKDF2-HMAC-SHA256 & Coduri OTP CSPRNG (Implementat în Etapa 3)
- **Parole Utilizatori (`internal/auth/crypto.go`):** Compatibilitate 100% la nivel de octet cu formatul `pbkdf2$100000$<saltHex>$<derivedHex>` (RFC 2898 PBKDF2-HMAC-SHA256, 32 bytes, 100.000 iterații) și comparare în timp constant prin `crypto/subtle.ConstantTimeCompare`.
- **Generare & Hashing OTP (`internal/auth/crypto.go`):** `crypto/rand.Int` pentru generarea codurilor de 6 cifre (`000000` - `999999`), digest `hmac$sha256$<digest>`, limită de 5 încercări (`maxOTPAttempts = 5`) și fereastră de 10 minute.
- **Serviciu Email Rezilient (`internal/services/email.go`):** API HTTP Resend cu `context.WithTimeout` (8s), retry cu exponential backoff și mascare PII (`config.MaskEmail`) în logurile structurate `slog`.
- **Onboarding & Anti-Duplicat (`internal/handlers/auth_handler.go`):** Normalizare deterministă (`NormalizeEntityKey`, `NormalizeCUI`, `NormalizePhone`) și tranzacții SQLite atomice (`BEGIN` / `COMMIT`) pentru înregistrarea `locatar`, `asociatie` și `prestator` + pagini legale complete (`/termeni`, `/gdpr`, `/cookies`, `/garantie`).

## 3. Politica CSP, SEO & PWA (Implementat în Etapa 2)
- **CSP Middleware (`internal/middleware/security.go`):** Regim 2 Dynamic SSR cu nonce CSPRNG 128-bit (`crypto/rand`, 16 bytes hex) pe fiecare request: `script-src 'self' 'nonce-${nonce}'; style-src 'self'; worker-src 'self'; manifest-src 'self';`. Zero `'unsafe-inline'`, zero `'unsafe-eval'`, zero `'strict-dynamic'`, zero `innerHTML`.
- **Runtime Client CSP-Safe (`public/embed.go`):** HTMX 2.0.4 (`public/js/htmx.min.js`) + `@alpinejs/csp` v3.14.8 (`public/js/alpine.min.js`) + `public/js/pwa-init.js` + `public/sw.js` încorporate în binarul Go prin `//go:embed`.
- **Headers Indexare (`X-Robots-Tag`) & SEO (`internal/handlers/seo_handler.go`):** Rutele publice (`/`, `/robots.txt`, `/sitemap.xml`) primesc `index, follow`, iar rutele private (`/platform-admin`, `/dashboard`, `/associations`, `/requests/new`, `/api/`, `/reset-password`, `/verify-otp`) primesc `noindex, nofollow`.


## 4. Servicii Externe Reziliente: Cloudflare R2, Netopia IPN & SLA Cron (Implementat în Etapa 6)
- **Cloudflare R2 (`internal/services/r2_storage.go`):** Semnare nativă AWS Signature Version 4 (`AWS4-HMAC-SHA256`) cu `crypto/hmac` și `crypto/sha256`, whitelist strict MIME (`image/jpeg`, `image/png`, `image/webp`, `application/pdf`), plafon `10 MB` (`MaxMediaUploadBytes`), chei anti-path-traversal (`BuildSafeObjectKey`) și upload cu timeout `8s` + retry exponential backoff către `media.instalbloc.ro`.
- **Netopia Payments & Webhook (`internal/services/netopia.go`):** Generare sesiuni checkout în subdiviziuni întregi `int64` bani și verificare în timp constant (`hmac.Equal`) a semnăturii `X-Netopia-Signature` pe `/webhooks/netopia`.
- **SLA 96h & Subscripții 30 Zile Cron (`internal/services/sla_cron.go`):** Verificare periodică la 15 minute a subscripțiilor expirate (`ExpireOverdueSubscriptions` + sincronizare `is_subscribed = 0` + alertă email) și a tichetelor în `accepted`/`in_progress` care depășesc 96h fără prelungire activă (`extended_until`).
