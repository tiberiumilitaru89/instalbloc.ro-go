---
id: instalbloc-business-roles
domain: business-rules
last_verified: 2026-10-21
dependencies: ["instalbloc-root-index"]
---

# Reguli de Business, Tarifare & Flux de Lucru: InstalBloc

## 1. Terminologie Canonică: „Panou Lucrări”
- Toate referințele din interfața publică și privată utilizează exclusiv termenul **„Panou Lucrări”** / **„Panou / Lucrări”** / **„Lucrări”** (termenul vechi „Bursă” este eliminat integral).

## 2. Matricea Rolurilor & Izolarea Dual-Role
1. **`visitor`**: Utilizator neautentificat; acces la pagina de prezentare, login, onboarding și verificare QR cod avizier.
2. **`locatar` / `client` (Client Independent)**:
   - Persoane fizice sau juridice, case, apartamente, spații comerciale, birouri, sedii de firmă.
   - Raportează avarii și solicită oferte de preț din contul propriu.
   - **Tarif:** 10 RON / lună (1000 bani subdiviziuni întregi).
3. **`asociatie`**:
   - Administratori de bloc / Asociații de proprietari.
   - Gestionează tichetele, bugete de reparații, acceptă oferte prestatori și generează PV de recepție.
   - **Tarif:** **3 RON / apartament / lună** (calcul dinamic: `Număr Apartamente × 300 bani`).
4. **`prestator`**:
   - Instalatori autorizați, meșteri și firme specializate (cu CUI).
   - **Filtrare Strictă pe Domeniu:** În `/dashboard` (Panou Lucrări), prestatorii văd exclusiv lucrările care corespund domeniilor lor înregistrate la profil, putând filtra manual doar județul.
   - **Regula de Ofertare, Plafon de 3 Modificări Lunare & Retragere Liberă:** Orice prestator cu abonament activ poate depune o ofertă inițială pe orice lucrare deschisă (`open` sau `pending_offers`) din aria/domeniul său fără consum din cotă. Cronometrarea celor **3 modificări lunare** (`monthly_modifications < 3`) se aplică strict la **actualizarea/modificarea** unei oferte deja trimise. Retragerea ofertei este **100% liberă** (nu consumă din cota lunară) și repune tichetul pe liber (`open` dacă nu mai există alte oferte).
   - **Tarif:** 15 RON / lună (1500 bani, ciclu de 30 zile, cu opțiune de Reînnoire Automată).
5. **`platform_admin` (Izolare Exclusivă Super Admin)**:
   - Administratori generali unici desemnați strict prin `SUPER_ADMIN_EMAILS`: `oana.becheanu@yahoo.com` și `george.becheanu11@yahoo.com`.
   - **Fără Roluri de Platformă:** Conturile de Super Admin au exclusiv `primary_role: "platform_admin"`, nu își pot crea conturi de `locatar`, `asociatie` sau `prestator`, nu pot comuta roluri și sunt redirecționate automat după autentificare direct către `/platform-admin`. Orice acces spre `/dashboard`, `/associations`, `/onboarding`, `/requests/new`, `/dashboard/oferte` sau `/dashboard/facturi` îi redirecționează la `/platform-admin`.

## 3. Automatul Finit de Stare (FSM) în 4 Faze pentru Tichete & Regula de Aur
Fiecare sesizare tehnică parcurge un ciclu determinist în 4 faze vizuale:
1. **Faza 1 — „1. Deschis” (`open`, `pending_offers`)**: Lucrare deschisă în Panoul de Lucrări. Prima ofertă depusă comută tichetul în `pending_offers`; retragerea sau respingerea tuturor ofertelor îl repune pe `open`.
2. **Faza 2 — „2. Ofertă Acceptată” (`claimed`, `accepted`)**: Beneficiarul acceptă o ofertă (`accepted_offer_id`, `claimed_by_provider_id`, `accepted_at`), respingând automat celelalte oferte în așteptare. Numărul de telefon al clientului este demascat exclusiv pentru prestatorul desemnat cu abonament activ.
3. **Faza 3 — „3. În Lucru” (`in_progress`) — Regula de Aur**:
   - **Strict doar beneficiarul** (proprietarul tichetului, administratorul asociației aferente sau deținătorul tokenului QR `tracking_token`) poate confirma începerea execuției (`POST /requests/{id}/start-work`). Prestatorul este blocat determinist (`403 Forbidden`) de la pornirea lucrării.
   - Din momentul intrării în Faza 3 (`in_progress`) sau Faza 4 (`completed`), tichetul dispare din Panoul de Lucrări pentru ceilalți prestatori și este vizibil exclusiv creatorului, asociației administratoare și prestatorului desemnat.
4. **Faza 4 — „4. Finalizat” (`completed`)**:
   - Închidere de către beneficiar (`POST /requests/{id}/complete`) cu acordarea obligatorie a unei recenzii (1–5 stele în `reviews`) și recalcularea mediei `rating` / `total_reviews` / `total_jobs` a prestatorului.
   - Alternativ, **Recepție Tacită la 72h** (`POST /requests/{id}/tacit-complete`) după depunerea raportului tehnic final (`reported_completed_at`), cu recenzie automată de 5 stele.
   - Bariera SLA 96h: un beneficiar cu o lucrare acceptată de peste 96h nefinalizată și fără prelungire activă (`extended_until`) nu poate deschide o lucrare nouă până la finalizarea, prelungirea sau repunerea pe liber a celei curente.


## 4. Rutele Panourilor Dedicate & Platform Admin (Etapa 5 — Conectate în `cmd/server/main.go`)
- **Asociații & Scări (`/associations*`):** Gestiune portofoliu asociații, verificare anti-duplicat CUI/denumire, actualizare dinamică apartamente (`300 bani × apartamente` cu swap HTMX) și adăugare scări/clădiri.
- **Avizier QR & Sesizare Publică (`/qr-avizier*`, `/avizier/{code}`):** Generare/dezactivare coduri scurte `AVZ-XXXXXXXX` și formular public cu rate-limit de maximum 5 sesizări/oră per hash IP HMAC-SHA256 în `qr_rate_limits`.
- **Facturare & Ledger Imutabil (`/billing*`, `/webhooks/netopia`):** Emitere facturi în subdiviziuni întregi (`int64` bani), procesare tranzacțională cu `idempotency_key UNIQUE` în tabelul append-only `payments` și activare abonament pe 30 de zile cu ștampilă `DD.MM.YYYY, HH:mm:ss`.
- **Consola Super Admin (`/platform-admin*`):** Acces exclusiv pentru `oana.becheanu@yahoo.com` și `george.becheanu11@yahoo.com`, comutare subscripții 30 zile, reînnoire automată, suspendare/activare conturi și trimitere rapoarte executive multi-destinatar.
