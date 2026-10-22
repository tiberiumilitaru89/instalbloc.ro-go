package handlers

import (
	"database/sql"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"instalbloc/internal/auth"
	"instalbloc/internal/config"
	"instalbloc/internal/db/sqlc"
	"instalbloc/internal/domain"
	"instalbloc/internal/middleware"
	"instalbloc/internal/services"
	"instalbloc/internal/views/layouts"
	"instalbloc/internal/views/pages"
)

type AuthHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
	cfg     *config.Config
	email   *services.EmailService
}

func NewAuthHandler(conn *sql.DB, queries *sqlc.Queries, cfg *config.Config, emailSvc *services.EmailService) *AuthHandler {
	return &AuthHandler{
		db:      conn,
		queries: queries,
		cfg:     cfg,
		email:   emailSvc,
	}
}

func (h *AuthHandler) pageMeta(r *http.Request, title string, noIndex bool) layouts.PageMeta {
	return layouts.PageMeta{
		Title:        title,
		CanonicalURL: canonicalBaseURL + r.URL.Path,
		NoIndex:      noIndex,
		CSPNonce:     middleware.NonceFromContext(r.Context()),
		CurrentYear:  time.Now().Year(),
		Session:      auth.SessionFromContext(r.Context()),
	}
}

func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	session := auth.SessionFromContext(r.Context())
	if session.IsLoggedIn() {
		if session.IsSuperAdmin {
			http.Redirect(w, r, "/platform-admin", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	infoMsg := ""
	if r.URL.Query().Get("reset") == "success" {
		infoMsg = "Parola a fost actualizată cu succes. Te poți autentifica acum."
	}
	meta := h.pageMeta(r, "Autentificare în Cont", false)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.LoginPage(meta, "", infoMsg, "").Render(r.Context(), w)
}

func (h *AuthHandler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Cerere invalidă", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	password := r.FormValue("password")
	meta := h.pageMeta(r, "Autentificare în Cont", true)

	user, err := h.queries.GetUserByEmail(r.Context(), email)
	if err != nil || user.IsActive == 0 || !user.PasswordHash.Valid || !auth.VerifyPassword(password, user.PasswordHash.String) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = pages.LoginPage(meta, "Email sau parolă incorectă.", "", email).Render(r.Context(), w)
		return
	}

	now := time.Now().Unix()
	_ = h.queries.UpdateUserLastLogin(r.Context(), sqlc.UpdateUserLastLoginParams{
		LastLoginAt: sql.NullInt64{Int64: now, Valid: true},
		UpdatedAt:   now,
		ID:          user.ID,
	})

	isSuper := config.IsSuperAdminEmail(user.Email)
	role := domain.RoleType(user.PrimaryRole)
	if isSuper {
		role = domain.RolePlatformAdmin
	}

	if !isSuper && user.IsVerified == 0 {
		code, otpErr := auth.GenerateOTPCode()
		if otpErr == nil {
			codeHash := auth.HashOTPCode(code, h.cfg.HMACSecret)
			_, _ = h.queries.CreateOTPVerification(r.Context(), sqlc.CreateOTPVerificationParams{
				UserID:    sql.NullInt64{Int64: user.ID, Valid: true},
				Email:     user.Email,
				CodeHash:  codeHash,
				Purpose:   "login",
				ExpiresAt: now + 600,
				CreatedAt: now,
			})
			if h.email != nil {
				_ = h.email.SendOTP(r.Context(), user.Email, code, "login")
			}
			http.Redirect(w, r, "/verify-otp?email="+url.QueryEscape(user.Email)+"&purpose=login", http.StatusSeeOther)
			return
		}
	}

	token := auth.SignSessionToken(user.ID, role, now, h.cfg.HMACSecret)
	auth.SetSessionCookie(w, token, h.cfg.Env == config.EnvProduction)

	if isSuper {
		http.Redirect(w, r, "/platform-admin", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *AuthHandler) VerifyOTPPage(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("email")))
	purpose := strings.TrimSpace(r.URL.Query().Get("purpose"))
	if purpose == "" {
		purpose = "login"
	}
	meta := h.pageMeta(r, "Verificare Cod 2FA", true)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.VerifyOTPPage(meta, email, purpose, "").Render(r.Context(), w)
}

func (h *AuthHandler) VerifyOTPSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Cerere invalidă", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	purpose := strings.TrimSpace(r.FormValue("purpose"))
	if purpose == "" {
		purpose = "login"
	}
	code := strings.TrimSpace(r.FormValue("code"))
	now := time.Now().Unix()
	meta := h.pageMeta(r, "Verificare Cod 2FA", true)

	otp, err := h.queries.GetLatestActiveOTP(r.Context(), sqlc.GetLatestActiveOTPParams{
		Email:     email,
		Purpose:   purpose,
		ExpiresAt: now,
	})
	if err != nil || otp.Attempts >= 5 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = pages.VerifyOTPPage(meta, email, purpose, "Codul a expirat sau a depășit numărul maxim de încercări.").Render(r.Context(), w)
		return
	}

	if !auth.VerifyOTPCode(code, otp.CodeHash, h.cfg.HMACSecret) {
		_ = h.queries.IncrementOTPAttempts(r.Context(), otp.ID)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = pages.VerifyOTPPage(meta, email, purpose, "Cod de verificare incorect.").Render(r.Context(), w)
		return
	}

	_ = h.queries.ConsumeOTP(r.Context(), sqlc.ConsumeOTPParams{
		ConsumedAt: sql.NullInt64{Int64: now, Valid: true},
		ID:         otp.ID,
	})

	user, err := h.queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	_ = h.queries.MarkUserVerified(r.Context(), sqlc.MarkUserVerifiedParams{
		UpdatedAt: now,
		ID:        user.ID,
	})

	isSuper := config.IsSuperAdminEmail(user.Email)
	role := domain.RoleType(user.PrimaryRole)
	if isSuper {
		role = domain.RolePlatformAdmin
	}

	token := auth.SignSessionToken(user.ID, role, now, h.cfg.HMACSecret)
	auth.SetSessionCookie(w, token, h.cfg.Env == config.EnvProduction)

	if isSuper {
		http.Redirect(w, r, "/platform-admin", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	auth.ClearSessionCookie(w, h.cfg.Env == config.EnvProduction)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *AuthHandler) ForgotPasswordPage(w http.ResponseWriter, r *http.Request) {
	meta := h.pageMeta(r, "Recuperare Parolă", true)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.ForgotPasswordPage(meta, "", "").Render(r.Context(), w)
}

func (h *AuthHandler) ForgotPasswordSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Cerere invalidă", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	now := time.Now().Unix()

	if user, err := h.queries.GetUserByEmail(r.Context(), email); err == nil && user.IsActive == 1 {
		if code, otpErr := auth.GenerateOTPCode(); otpErr == nil {
			codeHash := auth.HashOTPCode(code, h.cfg.HMACSecret)
			_, _ = h.queries.CreateOTPVerification(r.Context(), sqlc.CreateOTPVerificationParams{
				UserID:    sql.NullInt64{Int64: user.ID, Valid: true},
				Email:     user.Email,
				CodeHash:  codeHash,
				Purpose:   "reset_password",
				ExpiresAt: now + 600,
				CreatedAt: now,
			})
			if h.email != nil {
				_ = h.email.SendOTP(r.Context(), user.Email, code, "reset_password")
			}
		}
	}

	http.Redirect(w, r, "/reset-password?email="+url.QueryEscape(email), http.StatusSeeOther)
}

func (h *AuthHandler) ResetPasswordPage(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("email")))
	meta := h.pageMeta(r, "Setare Parolă Nouă", true)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.ResetPasswordPage(meta, email, "").Render(r.Context(), w)
}

func (h *AuthHandler) ResetPasswordSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Cerere invalidă", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	code := strings.TrimSpace(r.FormValue("code"))
	newPassword := r.FormValue("new_password")
	now := time.Now().Unix()
	meta := h.pageMeta(r, "Setare Parolă Nouă", true)

	if len(newPassword) < 8 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = pages.ResetPasswordPage(meta, email, "Parola nouă trebuie să conțină cel puțin 8 caractere.").Render(r.Context(), w)
		return
	}

	otp, err := h.queries.GetLatestActiveOTP(r.Context(), sqlc.GetLatestActiveOTPParams{
		Email:     email,
		Purpose:   "reset_password",
		ExpiresAt: now,
	})
	if err != nil || otp.Attempts >= 5 || !auth.VerifyOTPCode(code, otp.CodeHash, h.cfg.HMACSecret) {
		if err == nil {
			_ = h.queries.IncrementOTPAttempts(r.Context(), otp.ID)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = pages.ResetPasswordPage(meta, email, "Codul de resetare este invalid sau a expirat.").Render(r.Context(), w)
		return
	}

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_ = pages.ResetPasswordPage(meta, email, "Eroare la procesarea parolei.").Render(r.Context(), w)
		return
	}

	user, err := h.queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	_ = h.queries.ConsumeOTP(r.Context(), sqlc.ConsumeOTPParams{
		ConsumedAt: sql.NullInt64{Int64: now, Valid: true},
		ID:         otp.ID,
	})
	_ = h.queries.UpdateUserPassword(r.Context(), sqlc.UpdateUserPasswordParams{
		PasswordHash: sql.NullString{String: hash, Valid: true},
		UpdatedAt:    now,
		ID:           user.ID,
	})

	http.Redirect(w, r, "/login?reset=success", http.StatusSeeOther)
}

func resolveSelectedRole(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "asociatie":
		return "asociatie"
	case "prestator":
		return "prestator"
	default:
		return "locatar"
	}
}

func (h *AuthHandler) OnboardingPage(w http.ResponseWriter, r *http.Request) {
	role := resolveSelectedRole(r.URL.Query().Get("role"))
	meta := h.pageMeta(r, "Înregistrare Cont Nou", false)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.OnboardingPage(meta, role, "").Render(r.Context(), w)
}

func (h *AuthHandler) OnboardingSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Cerere invalidă", http.StatusBadRequest)
		return
	}

	roleStr := resolveSelectedRole(r.FormValue("role"))
	name := strings.TrimSpace(r.FormValue("name"))
	phone := strings.TrimSpace(r.FormValue("phone"))
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	password := r.FormValue("password")
	entityName := strings.TrimSpace(r.FormValue("entity_name"))
	cui := strings.TrimSpace(r.FormValue("cui"))
	county := strings.TrimSpace(r.FormValue("county"))
	city := strings.TrimSpace(r.FormValue("city"))
	address := strings.TrimSpace(r.FormValue("address"))
	description := strings.TrimSpace(r.FormValue("description"))
	aptCountStr := strings.TrimSpace(r.FormValue("apartment_count"))

	meta := h.pageMeta(r, "Înregistrare Cont Nou", true)
	renderErr := func(status int, msg string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_ = pages.OnboardingPage(meta, roleStr, msg).Render(r.Context(), w)
	}

	// Exclusive Super Admin isolation guard: Super Admins cannot register standard platform roles
	if config.IsSuperAdminEmail(email) {
		renderErr(http.StatusForbidden, "Conturile de Super Admin au acces exclusiv la panoul administrativ și nu pot înregistra roluri standard.")
		return
	}

	if len(password) < 8 {
		renderErr(http.StatusBadRequest, "Parola trebuie să conțină cel puțin 8 caractere.")
		return
	}

	normPhone := services.NormalizePhone(phone)
	if len(normPhone) < 10 {
		renderErr(http.StatusBadRequest, "Numărul de telefon introdus nu este valid.")
		return
	}

	// 1. Check duplicate Email or Phone across active users
	existingUsers, err := h.queries.ListUsersForDuplicateCheck(r.Context())
	if err == nil {
		for _, u := range existingUsers {
			if strings.EqualFold(u.Email, email) {
				renderErr(http.StatusConflict, services.FormatDuplicateEntityError(u.Email).Error())
				return
			}
			if u.Phone.Valid && services.NormalizePhone(u.Phone.String) == normPhone {
				renderErr(http.StatusConflict, services.FormatDuplicateEntityError(u.Email).Error())
				return
			}
		}
	}

	normCUI := services.NormalizeFiscalCode(cui)
	normCoreEntity := services.NormalizeCoreCompanyName(entityName)

	// 2. Check duplicate CUI or Company Name for Providers
	if roleStr == "prestator" {
		if normCUI == "" || normCoreEntity == "" {
			renderErr(http.StatusBadRequest, "Denumirea firmei și CUI-ul sunt obligatorii pentru prestatori.")
			return
		}
		providers, pErr := h.queries.ListProvidersForDuplicateCheck(r.Context())
		if pErr == nil {
			for _, p := range providers {
				if p.Cui.Valid && services.NormalizeFiscalCode(p.Cui.String) == normCUI {
					renderErr(http.StatusConflict, services.FormatDuplicateEntityError(p.Email).Error())
					return
				}
				if p.CompanyName.Valid && services.NormalizeCoreCompanyName(p.CompanyName.String) == normCoreEntity {
					renderErr(http.StatusConflict, services.FormatDuplicateEntityError(p.Email).Error())
					return
				}
			}
		}
	}

	// 3. Check duplicate CUI or Association Name for Associations
	var aptCount int64 = 40
	if roleStr == "asociatie" {
		if normCoreEntity == "" {
			renderErr(http.StatusBadRequest, "Denumirea asociației de proprietari este obligatorie.")
			return
		}
		if parsedApt, parseErr := strconv.ParseInt(aptCountStr, 10, 64); parseErr == nil && parsedApt > 0 {
			aptCount = parsedApt
		}
		assocs, aErr := h.queries.ListAssociationsForDuplicateCheck(r.Context())
		if aErr == nil {
			for _, a := range assocs {
				if normCUI != "" && a.Cui.Valid && services.NormalizeFiscalCode(a.Cui.String) == normCUI {
					renderErr(http.StatusConflict, services.FormatDuplicateEntityError(a.Email).Error())
					return
				}
				if services.NormalizeCoreCompanyName(a.Name) == normCoreEntity && strings.EqualFold(strings.TrimSpace(a.Address), address) {
					renderErr(http.StatusConflict, services.FormatDuplicateEntityError(a.Email).Error())
					return
				}
			}
		}
	}

	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		renderErr(http.StatusInternalServerError, "Eroare la criptarea parolei.")
		return
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		renderErr(http.StatusInternalServerError, "Eroare la inițierea tranzacției.")
		return
	}
	defer func() { _ = tx.Rollback() }()

	qtx := h.queries.WithTx(tx)
	now := time.Now().Unix()

	createdUser, err := qtx.CreateUser(r.Context(), sqlc.CreateUserParams{
		Email:        email,
		PasswordHash: sql.NullString{String: passwordHash, Valid: true},
		Name:         name,
		FirstName:    sql.NullString{String: name, Valid: true},
		LastName:     sql.NullString{},
		PrimaryRole:  roleStr,
		IsSubscribed: 1,
		IsVerified:   1,
		Phone:        sql.NullString{String: normPhone, Valid: true},
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		renderErr(http.StatusConflict, "Adresa de email este deja utilizată.")
		return
	}

	_, err = qtx.AddUserRole(r.Context(), sqlc.AddUserRoleParams{
		UserID:    createdUser.ID,
		Role:      roleStr,
		CreatedAt: now,
	})
	if err != nil {
		renderErr(http.StatusInternalServerError, "Eroare la atribuirea rolului.")
		return
	}

	switch roleStr {
	case "asociatie":
		assoc, assocErr := qtx.CreateAssociation(r.Context(), sqlc.CreateAssociationParams{
			Name:               entityName,
			Address:            address,
			Cui:                sql.NullString{String: normCUI, Valid: normCUI != ""},
			City:               sql.NullString{String: city, Valid: city != ""},
			County:             sql.NullString{String: county, Valid: county != ""},
			ApartmentCount:     aptCount,
			RepresentativeName: sql.NullString{String: name, Valid: true},
			IsSubscribed:       1,
			CreatedAt:          now,
			UpdatedAt:          now,
		})
		if assocErr != nil {
			renderErr(http.StatusInternalServerError, "Eroare la înregistrarea asociației.")
			return
		}
		_, err = qtx.CreateAssociationAdmin(r.Context(), sqlc.CreateAssociationAdminParams{
			UserID:        createdUser.ID,
			AssociationID: assoc.ID,
			Role:          "asociatie",
			CreatedAt:     now,
		})
	case "prestator":
		_, err = qtx.CreateProviderProfile(r.Context(), sqlc.CreateProviderProfileParams{
			UserID:             createdUser.ID,
			CompanyName:        sql.NullString{String: entityName, Valid: true},
			Cui:                sql.NullString{String: normCUI, Valid: true},
			County:             sql.NullString{String: county, Valid: true},
			City:               sql.NullString{String: city, Valid: true},
			Description:        sql.NullString{String: description, Valid: description != ""},
			PhonePublic:        sql.NullString{String: normPhone, Valid: true},
			EmailPublic:        sql.NullString{String: email, Valid: true},
			AcceptsEmergencies: 1,
			TravelRadiusKm:     25,
			CreatedAt:          now,
			UpdatedAt:          now,
		})
	default:
		_, err = qtx.CreateLocatarProfile(r.Context(), sqlc.CreateLocatarProfileParams{
			UserID:         createdUser.ID,
			TipLocuinta:    "bloc",
			EsteProprietar: 1,
			AssociationID:  sql.NullInt64{},
			Address:        sql.NullString{String: address, Valid: address != ""},
			City:           sql.NullString{String: city, Valid: city != ""},
			County:         sql.NullString{String: county, Valid: county != ""},
			CreatedAt:      now,
			UpdatedAt:      now,
		})
	}
	if err != nil {
		renderErr(http.StatusInternalServerError, "Eroare la salvarea profilului.")
		return
	}

	// Activate 30-day cycle subscription
	_, err = qtx.CreateSubscription(r.Context(), sqlc.CreateSubscriptionParams{
		UserID:    createdUser.ID,
		PlanID:    roleStr + "_monthly",
		Status:    "active",
		StartedAt: now,
		ExpiresAt: now + 30*24*3600,
		AutoRenew: 1,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		renderErr(http.StatusInternalServerError, "Eroare la activarea abonamentului.")
		return
	}

	if err := tx.Commit(); err != nil {
		renderErr(http.StatusInternalServerError, "Eroare la finalizarea înregistrării.")
		return
	}

	token := auth.SignSessionToken(createdUser.ID, domain.RoleType(roleStr), now, h.cfg.HMACSecret)
	auth.SetSessionCookie(w, token, h.cfg.Env == config.EnvProduction)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *AuthHandler) Termeni(w http.ResponseWriter, r *http.Request) {
	meta := h.pageMeta(r, "Termeni și Condiții", false)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.TermeniPage(meta).Render(r.Context(), w)
}

func (h *AuthHandler) GDPR(w http.ResponseWriter, r *http.Request) {
	meta := h.pageMeta(r, "Politica de Confidențialitate GDPR", false)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.GDPRPage(meta).Render(r.Context(), w)
}

func (h *AuthHandler) Cookies(w http.ResponseWriter, r *http.Request) {
	meta := h.pageMeta(r, "Politica de Cookie-uri", false)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.CookiesPage(meta).Render(r.Context(), w)
}

func (h *AuthHandler) Garantie(w http.ResponseWriter, r *http.Request) {
	meta := h.pageMeta(r, "Cum Funcționează Garanția Lucrărilor", false)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.GarantiePage(meta).Render(r.Context(), w)
}
