---
id: instalbloc-database-schema
domain: database
last_verified: 2026-10-21
dependencies: ["instalbloc-root-index"]
---

# Bază de Date, Anti-Duplicat & Disciplină Financiară: InstalBloc (Go + SQLite)

## 1. Arhitectură Stocare & Pragme Concurență
- **Motor:** SQLite 3 accesat prin driverul nativ pur Go `modernc.org/sqlite` (zero CGO).
- **Generare Cod Tipizat:** `sqlc` v1.27+ din `internal/db/schema.sql` și `internal/db/queries/*.sql`.
- **Pragme Obligatorii la Inițializare:**
  - `PRAGMA journal_mode = WAL;`
  - `PRAGMA busy_timeout = 5000;`
  - `PRAGMA synchronous = NORMAL;`
  - `PRAGMA foreign_keys = ON;`
  - `PRAGMA temp_store = MEMORY;`

## 2. Regula Anti-Duplicat Entități (`internal/services/normalizer.go`)
- Orice **Firmă Prestatoare**, **Asociație de Proprietari** sau **Client Independent** poate fi înregistrat(ă) pe platformă cu **o singură adresă de email**.
- Sistemul normalizează denumirea firmei/asociației (lowercase, fără diacritice, fără sufixe juridice `S.R.L.`, `SRL`, `P.F.A.`, `I.I.`, `S.A.`), CUI/CIF (fără `RO` și spații) și telefonul (cifre canonice).
- Dacă entitatea există deja sub o altă adresă de email, operațiunea este blocată determinist și returnează:
  `"Există deja acest utilizator/firmă înregistrat(ă) cu adresa de email: [email_existent]"`

## 3. Disciplină Financiară & Integritate Sume
- Conform Modulului 2.2 (`ledger_active: true`), calculele monetare (sume manoperă, materiale, abonamente, facturi) operează intern în subdiviziuni întregi (`int64` bani: `1 RON = 100 bani`), eliminând erorile de virgulă mobilă IEEE 754.

## 4. Rezoluția Proprietății Asociației pentru Avarii QR Publice
- Sesizările depuse prin Avizierul QR public (`/avizier-qr/{assocID}`) rezolvă automat `user_id`-ul administratorului titular prin interogarea parametrizată `GetPrimaryAssociationAdminUserID` (`user_associations` cu prioritate pe `member_role = 'admin'`), atașând sesizarea în tabela `reports` fără a permite orfani relaționali (`FOREIGN KEY (user_id) REFERENCES users(id)`).
