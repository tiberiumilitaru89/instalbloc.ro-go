package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"instalbloc/internal/auth"
	"instalbloc/internal/config"
	"instalbloc/internal/db/sqlc"
	"instalbloc/internal/domain"
	"instalbloc/internal/middleware"
	"instalbloc/internal/services"
	"instalbloc/internal/views/layouts"
	"instalbloc/internal/views/pages"
)

const (
	locatarPlanFeeBani    int64 = 1000 // 10.00 RON / 30 days
	prestatorPlanFeeBani  int64 = 1500 // 15.00 RON / 30 days
	asociatiePerAptBani   int64 = 300  // 3.00 RON / apartment / 30 days
	subscriptionCycleSecs int64 = 30 * 24 * 3600
	qrRateWindowSecs      int64 = 3600
	qrMaxRequestsPerHour  int64 = 5
)

type PlatformHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
	cfg     *config.Config
	email   *services.EmailService
	cron    *services.SLACronWorker
	r2      *services.R2StorageService
	netopia *services.NetopiaService
}

func NewPlatformHandler(
	conn *sql.DB,
	queries *sqlc.Queries,
	cfg *config.Config,
	emailSvc *services.EmailService,
	cronWorker *services.SLACronWorker,
) *PlatformHandler {
	return &PlatformHandler{
		db:      conn,
		queries: queries,
		cfg:     cfg,
		email:   emailSvc,
		cron:    cronWorker,
		r2:      services.NewR2StorageService(cfg, nil),
		netopia: services.NewNetopiaService(cfg),
	}
}

func (h *PlatformHandler) pageMeta(r *http.Request, title string, noIndex bool) layouts.PageMeta {
	return layouts.PageMeta{
		Title:        title,
		CanonicalURL: canonicalBaseURL + r.URL.Path,
		NoIndex:      noIndex,
		CSPNonce:     middleware.NonceFromContext(r.Context()),
		CurrentYear:  time.Now().Year(),
		Session:      auth.SessionFromContext(r.Context()),
	}
}

func formatBaniDecimal(bani int64) string {
	if bani < 0 {
		bani = 0
	}
	whole := bani / 100
	rem := bani % 100
	return fmt.Sprintf("%d.%02d", whole, rem)
}

func generateShortQRCode() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("AVZ-%d", time.Now().Unix()%100000)
	}
	return "AVZ-" + strings.ToUpper(hex.EncodeToString(b[:]))
}

func extractClientIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func hashClientIP(r *http.Request, secret string) string {
	ip := extractClientIP(r)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(ip))
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *PlatformHandler) requireExclusiveSuperAdmin(w http.ResponseWriter, r *http.Request) (domain.SessionView, bool) {
	sess := auth.SessionFromContext(r.Context())
	if !sess.IsLoggedIn() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return sess, false
	}
	if !sess.IsSuperAdmin || !config.IsSuperAdminEmail(sess.Email) {
		http.Error(w, "Acces interzis: această secțiune este rezervată exclusiv Super Adminilor platformei.", http.StatusForbidden)
		return sess, false
	}
	return sess, true
}

// ============================================================================
// 1. ASSOCIATIONS & BUILDINGS MANAGEMENT (/associations)
// ============================================================================

func (h *PlatformHandler) AssociationsPage(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if !sess.IsLoggedIn() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if sess.IsSuperAdmin {
		http.Redirect(w, r, "/platform-admin", http.StatusSeeOther)
		return
	}
	if sess.Role != domain.RoleAsociatie {
		http.Error(w, "Acces permis doar administratorilor și președinților de asociații.", http.StatusForbidden)
		return
	}

	assocs, err := h.queries.ListUserAssociations(r.Context(), sess.UserID)
	if err != nil {
		assocs = []sqlc.Association{}
	}

	cards := make([]pages.AssociationCardView, 0, len(assocs))
	for _, a := range assocs {
		aptCount := a.ApartmentCount
		if aptCount <= 0 {
			aptCount = 1
		}
		feeBani := aptCount * asociatiePerAptBani

		bRows, _ := h.queries.ListBuildingsByAssociation(r.Context(), a.ID)
		bViews := make([]pages.BuildingView, 0, len(bRows))
		for _, b := range bRows {
			bViews = append(bViews, pages.BuildingView{
				ID:   b.ID,
				Name: b.Name,
			})
		}

		cards = append(cards, pages.AssociationCardView{
			ID:                  a.ID,
			Name:                a.Name,
			Address:             a.Address,
			CUI:                 a.Cui.String,
			City:                a.City.String,
			County:              a.County.String,
			ApartmentCount:      aptCount,
			RepresentativeName:  a.RepresentativeName.String,
			IsSubscribed:        a.IsSubscribed == 1,
			MonthlyFeeBani:      feeBani,
			MonthlyFeeFormatted: formatBaniDecimal(feeBani),
			Buildings:           bViews,
		})
	}

	var flashSuccess, flashError string
	switch r.URL.Query().Get("success") {
	case "created":
		flashSuccess = "Asociația de proprietari a fost înregistrată cu succes în portofoliul tău."
	case "apartments":
		flashSuccess = "Numărul de apartamente și tariful lunar (3 RON / apartament) au fost actualizate."
	case "building":
		flashSuccess = "Scara / corpul de clădire a fost adăugat în structura asociației."
	}
	if errMsg := strings.TrimSpace(r.URL.Query().Get("error")); errMsg != "" {
		flashError = errMsg
	}

	meta := h.pageMeta(r, "Asociațiile Mele de Proprietari", true)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.AssociationsPage(meta, pages.AssociationsPageData{
		Associations: cards,
		FlashSuccess: flashSuccess,
		FlashError:   flashError,
	}).Render(r.Context(), w)
}

func (h *PlatformHandler) CreateAssociationSubmit(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if !sess.IsLoggedIn() || sess.IsSuperAdmin || sess.Role != domain.RoleAsociatie {
		http.Error(w, "Acces neautorizat.", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Formular invalid.", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	cui := strings.TrimSpace(r.FormValue("cui"))
	aptStr := strings.TrimSpace(r.FormValue("apartment_count"))
	county := strings.TrimSpace(r.FormValue("county"))
	city := strings.TrimSpace(r.FormValue("city"))
	address := strings.TrimSpace(r.FormValue("address"))
	repName := strings.TrimSpace(r.FormValue("representative_name"))
	if repName == "" {
		repName = sess.Name
	}

	normCore := services.NormalizeCoreCompanyName(name)
	normCUI := services.NormalizeFiscalCode(cui)
	if normCore == "" || address == "" || city == "" || !domain.IsValidCounty(county) {
		http.Error(w, "Datele asociației sunt incomplete sau județul este invalid.", http.StatusBadRequest)
		return
	}

	aptCount, err := strconv.ParseInt(aptStr, 10, 64)
	if err != nil || aptCount < 1 || aptCount > 2000 {
		aptCount = 40
	}

	// Anti-Duplicate verification across existing associations
	existingAssocs, err := h.queries.ListAssociationsForDuplicateCheck(r.Context())
	if err == nil {
		for _, existing := range existingAssocs {
			if normCUI != "" && existing.Cui.Valid && services.NormalizeFiscalCode(existing.Cui.String) == normCUI {
				http.Error(w, services.FormatDuplicateEntityError(existing.Email).Error(), http.StatusConflict)
				return
			}
			if services.NormalizeCoreCompanyName(existing.Name) == normCore && strings.EqualFold(strings.TrimSpace(existing.Address), address) {
				http.Error(w, services.FormatDuplicateEntityError(existing.Email).Error(), http.StatusConflict)
				return
			}
		}
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "Eroare la inițierea tranzacției.", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback() }()

	qtx := h.queries.WithTx(tx)
	now := time.Now().Unix()

	subStatus := int64(0)
	if dbUser, uErr := qtx.GetUserByID(r.Context(), sess.UserID); uErr == nil && dbUser.IsSubscribed == 1 {
		subStatus = 1
	}

	assoc, err := qtx.CreateAssociation(r.Context(), sqlc.CreateAssociationParams{
		Name:               name,
		Address:            address,
		Cui:                sql.NullString{String: normCUI, Valid: normCUI != ""},
		City:               sql.NullString{String: city, Valid: true},
		County:             sql.NullString{String: county, Valid: true},
		ApartmentCount:     aptCount,
		RepresentativeName: sql.NullString{String: repName, Valid: repName != ""},
		IsSubscribed:       subStatus,
		CreatedAt:          now,
		UpdatedAt:          now,
	})
	if err != nil {
		http.Error(w, "Eroare la crearea asociației.", http.StatusInternalServerError)
		return
	}

	_, err = qtx.CreateAssociationAdmin(r.Context(), sqlc.CreateAssociationAdminParams{
		UserID:        sess.UserID,
		AssociationID: assoc.ID,
		Role:          "asociatie",
		CreatedAt:     now,
	})
	if err != nil {
		http.Error(w, "Eroare la asocierea administratorului.", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Eroare la salvarea tranzacției.", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/associations?success=created", http.StatusSeeOther)
}

func (h *PlatformHandler) UpdateAssociationApartmentsSubmit(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if !sess.IsLoggedIn() || sess.IsSuperAdmin || sess.Role != domain.RoleAsociatie {
		http.Error(w, "Acces neautorizat.", http.StatusForbidden)
		return
	}
	assocID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || assocID <= 0 {
		http.Error(w, "ID asociație invalid.", http.StatusBadRequest)
		return
	}

	// Anti-IDOR ownership guard
	if _, err := h.queries.GetAssociationAdmin(r.Context(), sqlc.GetAssociationAdminParams{
		UserID:        sess.UserID,
		AssociationID: assocID,
	}); err != nil {
		http.Error(w, "Nu ai drepturi de administrare asupra acestei asociații.", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Formular invalid.", http.StatusBadRequest)
		return
	}
	aptCount, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("apartment_count")), 10, 64)
	if err != nil || aptCount < 1 || aptCount > 2000 {
		http.Error(w, "Numărul de apartamente trebuie să fie între 1 și 2000.", http.StatusBadRequest)
		return
	}

	now := time.Now().Unix()
	if err := h.queries.UpdateAssociationApartmentCount(r.Context(), sqlc.UpdateAssociationApartmentCountParams{
		ApartmentCount: aptCount,
		UpdatedAt:      now,
		ID:             assocID,
	}); err != nil {
		http.Error(w, "Eroare la actualizarea numărului de apartamente.", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/associations?success=apartments", http.StatusSeeOther)
}

func (h *PlatformHandler) CreateBuildingSubmit(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if !sess.IsLoggedIn() || sess.IsSuperAdmin || sess.Role != domain.RoleAsociatie {
		http.Error(w, "Acces neautorizat.", http.StatusForbidden)
		return
	}
	assocID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || assocID <= 0 {
		http.Error(w, "ID asociație invalid.", http.StatusBadRequest)
		return
	}

	// Anti-IDOR ownership guard
	if _, err := h.queries.GetAssociationAdmin(r.Context(), sqlc.GetAssociationAdminParams{
		UserID:        sess.UserID,
		AssociationID: assocID,
	}); err != nil {
		http.Error(w, "Nu ai drepturi de administrare asupra acestei asociații.", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Formular invalid.", http.StatusBadRequest)
		return
	}
	buildingName := strings.TrimSpace(r.FormValue("building_name"))
	if len(buildingName) < 2 {
		http.Error(w, "Denumirea blocului/scării este prea scurtă.", http.StatusBadRequest)
		return
	}

	if _, err := h.queries.CreateBuilding(r.Context(), sqlc.CreateBuildingParams{
		AssociationID: assocID,
		Name:          buildingName,
	}); err != nil {
		http.Error(w, "Eroare la adăugarea scării.", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/associations?success=building", http.StatusSeeOther)
}

// ============================================================================
// 2. DIGITAL QR NOTICEBOARD & PASSWORDLESS EMERGENCY REPORTING
// ============================================================================

func (h *PlatformHandler) QRAvizierPage(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if !sess.IsLoggedIn() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if sess.IsSuperAdmin {
		http.Redirect(w, r, "/platform-admin", http.StatusSeeOther)
		return
	}
	if sess.Role != domain.RoleAsociatie {
		http.Error(w, "Doar asociațiile de proprietari pot genera afișe QR de avizier.", http.StatusForbidden)
		return
	}

	assocs, _ := h.queries.ListUserAssociations(r.Context(), sess.UserID)
	options := make([]pages.AssociationOptionView, 0, len(assocs))
	var allInvites []pages.QRInviteView
	var activeInvite *pages.QRInviteView
	selectedCode := strings.TrimSpace(r.URL.Query().Get("code"))

	for _, a := range assocs {
		options = append(options, pages.AssociationOptionView{
			ID:      a.ID,
			Name:    a.Name,
			Address: a.Address,
			County:  a.County.String,
			City:    a.City.String,
		})
		invRows, _ := h.queries.ListQrInvitesByAssociation(r.Context(), a.ID)
		for _, inv := range invRows {
			view := pages.QRInviteView{
				ID:              inv.ID,
				AssociationID:   a.ID,
				AssociationName: a.Name,
				Code:            inv.Code,
				PublicURL:       canonicalBaseURL + "/avarie/" + inv.Code,
				UsedCount:       inv.UsedCount,
				CreatedAtLabel:  formatTimestamp(inv.CreatedAt),
				IsActive:        inv.IsActive == 1,
			}
			allInvites = append(allInvites, view)
			if view.IsActive {
				if selectedCode != "" && strings.EqualFold(view.Code, selectedCode) {
					vCopy := view
					activeInvite = &vCopy
				} else if activeInvite == nil {
					vCopy := view
					activeInvite = &vCopy
				}
			}
		}
	}

	var flashSuccess string
	switch r.URL.Query().Get("success") {
	case "qr_created":
		flashSuccess = "Codul QR pentru avizierul scării a fost generat cu succes."
	case "qr_deactivated":
		flashSuccess = "Codul QR selectat a fost dezactivat."
	}

	meta := h.pageMeta(r, "Cod QR Avizier Digital", true)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.QRAvizierPage(meta, pages.QRAvizierPageData{
		Associations: options,
		ActiveInvite: activeInvite,
		Invites:      allInvites,
		FlashSuccess: flashSuccess,
	}).Render(r.Context(), w)
}

func (h *PlatformHandler) GenerateQRInviteSubmit(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if !sess.IsLoggedIn() || sess.IsSuperAdmin || sess.Role != domain.RoleAsociatie {
		http.Error(w, "Acces neautorizat.", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Formular invalid.", http.StatusBadRequest)
		return
	}

	assocID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("association_id")), 10, 64)
	if err != nil || assocID <= 0 {
		http.Error(w, "Selectează o asociație validă.", http.StatusBadRequest)
		return
	}

	// Anti-IDOR ownership guard
	if _, err := h.queries.GetAssociationAdmin(r.Context(), sqlc.GetAssociationAdminParams{
		UserID:        sess.UserID,
		AssociationID: assocID,
	}); err != nil {
		http.Error(w, "Nu ai drepturi asupra acestei asociații.", http.StatusForbidden)
		return
	}

	code := generateShortQRCode()
	now := time.Now().Unix()

	created, err := h.queries.CreateQrInvite(r.Context(), sqlc.CreateQrInviteParams{
		AssociationID: assocID,
		BuildingID:    sql.NullInt64{},
		ApartmentID:   sql.NullInt64{},
		Type:          "emergency_report",
		Code:          code,
		MaxUses:       sql.NullInt64{},
		ExpiresAt:     sql.NullInt64{},
		CreatedAt:     now,
	})
	if err != nil {
		http.Error(w, "Eroare la generarea codului QR.", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/dashboard/qr?code="+created.Code+"&success=qr_created", http.StatusSeeOther)
}

func (h *PlatformHandler) DeactivateQRInviteSubmit(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if !sess.IsLoggedIn() || sess.IsSuperAdmin || sess.Role != domain.RoleAsociatie {
		http.Error(w, "Acces neautorizat.", http.StatusForbidden)
		return
	}
	inviteID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || inviteID <= 0 {
		http.Error(w, "ID QR invalid.", http.StatusBadRequest)
		return
	}

	assocs, _ := h.queries.ListUserAssociations(r.Context(), sess.UserID)
	for _, a := range assocs {
		_ = h.queries.DeactivateQrInvite(r.Context(), sqlc.DeactivateQrInviteParams{
			ID:            inviteID,
			AssociationID: a.ID,
		})
	}

	http.Redirect(w, r, "/dashboard/qr?success=qr_deactivated", http.StatusSeeOther)
}

func (h *PlatformHandler) PublicQRReportPage(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	if code == "" {
		http.NotFound(w, r)
		return
	}

	inv, err := h.queries.GetQrInviteByCode(r.Context(), code)
	if err != nil || inv.IsActive != 1 {
		http.NotFound(w, r)
		return
	}
	if inv.ExpiresAt.Valid && inv.ExpiresAt.Int64 > 0 && inv.ExpiresAt.Int64 < time.Now().Unix() {
		http.NotFound(w, r)
		return
	}

	assoc, err := h.queries.GetAssociationByID(r.Context(), inv.AssociationID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	meta := h.pageMeta(r, "Raportare Rapidă Avarie — "+assoc.Name, true)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.PublicQRReportPage(meta, pages.PublicQRReportPageData{
		Code:            inv.Code,
		AssociationID:   assoc.ID,
		AssociationName: assoc.Name,
		Address:         assoc.Address,
		County:          assoc.County.String,
		City:            assoc.City.String,
	}).Render(r.Context(), w)
}

func (h *PlatformHandler) PublicQRReportSubmit(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	if code == "" {
		http.NotFound(w, r)
		return
	}

	inv, err := h.queries.GetQrInviteByCode(r.Context(), code)
	if err != nil || inv.IsActive != 1 {
		http.NotFound(w, r)
		return
	}
	assoc, err := h.queries.GetAssociationByID(r.Context(), inv.AssociationID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	meta := h.pageMeta(r, "Raportare Rapidă Avarie — "+assoc.Name, true)
	renderFormErr := func(status int, msg string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_ = pages.PublicQRReportPage(meta, pages.PublicQRReportPageData{
			Code:            inv.Code,
			AssociationID:   assoc.ID,
			AssociationName: assoc.Name,
			Address:         assoc.Address,
			County:          assoc.County.String,
			City:            assoc.City.String,
			ErrorMsg:        msg,
		}).Render(r.Context(), w)
	}

	now := time.Now().Unix()
	ipHash := hashClientIP(r, h.cfg.HMACSecret)

	// SQLite Rate-Limiting: max 5 QR emergency submissions per hour per IP + Association
	rl, rlErr := h.queries.GetQrRateLimit(r.Context(), sqlc.GetQrRateLimitParams{
		IpHash:        ipHash,
		AssociationID: assoc.ID,
	})
	if rlErr == nil {
		if now-rl.WindowStart < qrRateWindowSecs {
			if rl.RequestCount >= qrMaxRequestsPerHour {
				renderFormErr(http.StatusTooManyRequests, "Limita de securitate depășită: ai trimis deja 5 sesizări în ultima oră pentru această asociație.")
				return
			}
			_ = h.queries.UpdateQrRateLimitWindow(r.Context(), sqlc.UpdateQrRateLimitWindowParams{
				RequestCount:  rl.RequestCount + 1,
				WindowStart:   rl.WindowStart,
				LastRequestAt: now,
				ID:            rl.ID,
			})
		} else {
			_ = h.queries.UpdateQrRateLimitWindow(r.Context(), sqlc.UpdateQrRateLimitWindowParams{
				RequestCount:  1,
				WindowStart:   now,
				LastRequestAt: now,
				ID:            rl.ID,
			})
		}
	} else {
		_, _ = h.queries.CreateQrRateLimit(r.Context(), sqlc.CreateQrRateLimitParams{
			IpHash:        ipHash,
			AssociationID: assoc.ID,
			WindowStart:   now,
			LastRequestAt: now,
		})
	}

	if err := r.ParseForm(); err != nil {
		renderFormErr(http.StatusBadRequest, "Formular invalid.")
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	category := strings.TrimSpace(r.FormValue("category"))
	urgency := strings.TrimSpace(r.FormValue("urgency"))
	apartament := strings.TrimSpace(r.FormValue("apartament"))
	clientName := strings.TrimSpace(r.FormValue("client_name"))
	clientPhone := services.NormalizePhone(r.FormValue("client_phone"))
	description := strings.TrimSpace(r.FormValue("description"))

	if len(title) < 4 || !domain.IsValidCategory(category) || len(clientName) < 2 || len(clientPhone) < 10 || len(description) < 5 {
		renderFormErr(http.StatusBadRequest, "Te rugăm să completezi toate câmpurile obligatorii și un număr de telefon valid.")
		return
	}
	if urgency != "high" && urgency != "medium" && urgency != "low" {
		urgency = "high"
	}

	trackingToken := generateTrackingToken()
	county := assoc.County.String
	if county == "" {
		county = "București"
	}
	city := assoc.City.String
	if city == "" {
		city = "București"
	}

	ownerUserID, ownerErr := h.queries.GetPrimaryAssociationAdminUserID(r.Context(), assoc.ID)
	if ownerErr != nil || ownerUserID <= 0 {
		if sess := auth.SessionFromContext(r.Context()); sess.IsLoggedIn() {
			ownerUserID = sess.UserID
		} else if allUsers, uErr := h.queries.ListAllUsersForAdmin(r.Context()); uErr == nil && len(allUsers) > 0 {
			ownerUserID = allUsers[0].ID
		}
	}

	tipInterventie := "programat"
	if urgency == "high" {
		tipInterventie = "urgenta_imediata"
	}

	createdReq, err := h.queries.CreateReport(r.Context(), sqlc.CreateReportParams{
		Title:                title,
		Description:          description + " (Raportat prin Avizier QR — " + apartament + ")",
		Category:             category,
		Status:               "open",
		TipInterventie:       tipInterventie,
		Urgency:              urgency,
		UserID:               ownerUserID,
		AssociationID:        sql.NullInt64{Int64: assoc.ID, Valid: true},
		County:               county,
		Zone:                 city,
		Address:              sql.NullString{String: assoc.Address, Valid: true},
		City:                 sql.NullString{String: city, Valid: true},
		ClientName:           sql.NullString{String: clientName, Valid: true},
		ClientPhone:          sql.NullString{String: clientPhone, Valid: true},
		RequesterType:        "locatar",
		Asociatie:            sql.NullString{String: assoc.Name, Valid: true},
		Apartament:           sql.NullString{String: apartament, Valid: apartament != ""},
		TipSpatiu:            sql.NullString{String: "comun", Valid: true},
		SursaFonduri:         "in_asteptare_verificare",
		AprobatAdministrator: 0,
		TrackingToken:        sql.NullString{String: trackingToken, Valid: true},
		CreatedAt:            now,
		UpdatedAt:            now,
	})
	if err != nil {
		renderFormErr(http.StatusInternalServerError, "Eroare la înregistrarea sesizării.")
		return
	}

	_ = h.queries.IncrementQrInviteUsedCount(r.Context(), inv.ID)

	http.Redirect(w, r, fmt.Sprintf("/requests/%d?token=%s", createdReq.ID, trackingToken), http.StatusSeeOther)
}

// ============================================================================
// 3. 30-DAY SUBSCRIPTIONS, INVOICES & APPEND-ONLY FINANCIAL LEDGER
// ============================================================================

func (h *PlatformHandler) computeUserPlan(ctx context.Context, userID int64, role domain.RoleType) (int64, string, string, string) {
	switch role {
	case domain.RoleAsociatie:
		assocs, _ := h.queries.ListUserAssociations(ctx, userID)
		var totalApts int64
		for _, a := range assocs {
			if a.ApartmentCount > 0 {
				totalApts += a.ApartmentCount
			}
		}
		if totalApts <= 0 {
			totalApts = 40
		}
		feeBani := totalApts * asociatiePerAptBani
		return feeBani,
			"Asociație de Proprietari",
			fmt.Sprintf("Plan Asociație (%d apartamente × 3 RON)", totalApts),
			"Tarif dinamic calculat la 3 RON / apartament / 30 zile pentru administrarea lucrărilor și avizierul QR."
	case domain.RolePrestator:
		return prestatorPlanFeeBani,
			"Firmă / Prestator Autorizat",
			"Plan Firmă / Prestator (15 RON / 30 zile)",
			"Acces la lucrările din Panou Lucrări, transmitere oferte și deblocare contact beneficiar."
	default:
		return locatarPlanFeeBani,
			"Locatar / Proprietar",
			"Plan Locatar / Proprietar (10 RON / 30 zile)",
			"Publicare lucrări individuale și de bloc, selecție oferte și proces-verbal digital cu garanție."
	}
}

func (h *PlatformHandler) BillingPage(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if !sess.IsLoggedIn() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if sess.IsSuperAdmin {
		http.Redirect(w, r, "/platform-admin", http.StatusSeeOther)
		return
	}

	feeBani, roleLabel, planTitle, planDesc := h.computeUserPlan(r.Context(), sess.UserID, sess.Role)
	now := time.Now().Unix()

	subView := pages.SubscriptionSummaryView{
		ID:                 0,
		IsSubscribed:       false,
		PlanLabel:          string(sess.Role) + "_monthly",
		Status:             "inactive",
		StartedAtFormatted: "—",
		ExpiresAtFormatted: "—",
		AutoRenew:          false,
	}
	if sub, err := h.queries.GetActiveSubscriptionByUserID(r.Context(), sess.UserID); err == nil {
		subView.ID = sub.ID
		subView.IsSubscribed = sub.Status == "active" && sub.ExpiresAt > now
		subView.PlanLabel = sub.PlanID
		subView.Status = sub.Status
		subView.StartedAtFormatted = formatTimestamp(sub.StartedAt)
		subView.ExpiresAtFormatted = formatTimestamp(sub.ExpiresAt)
		subView.AutoRenew = sub.AutoRenew == 1
	}

	invRows, _ := h.queries.ListInvoicesByUserID(r.Context(), sess.UserID)
	invViews := make([]pages.InvoiceRowView, 0, len(invRows))
	invNumberByID := make(map[int64]string, len(invRows))
	for _, inv := range invRows {
		invNumberByID[inv.ID] = inv.InvoiceNumber
		paidLabel := "—"
		if inv.PaidAt.Valid && inv.PaidAt.Int64 > 0 {
			paidLabel = formatTimestamp(inv.PaidAt.Int64)
		}
		invViews = append(invViews, pages.InvoiceRowView{
			ID:              inv.ID,
			InvoiceNumber:   inv.InvoiceNumber,
			AmountBani:      inv.AmountBani,
			AmountFormatted: formatBaniDecimal(inv.AmountBani),
			Status:          inv.Status,
			StatusLabel:     inv.Status,
			DueDateLabel:    formatTimestamp(inv.DueDate),
			PaidAtLabel:     paidLabel,
			CreatedAtLabel:  formatTimestamp(inv.CreatedAt),
		})
	}

	payRows, _ := h.queries.ListPaymentsByUserID(r.Context(), sess.UserID)
	payViews := make([]pages.PaymentRowView, 0, len(payRows))
	for _, p := range payRows {
		paidLabel := formatTimestamp(p.CreatedAt)
		if p.PaidAt.Valid && p.PaidAt.Int64 > 0 {
			paidLabel = formatTimestamp(p.PaidAt.Int64)
		}
		payViews = append(payViews, pages.PaymentRowView{
			ID:              p.ID,
			AmountBani:      p.AmountBani,
			AmountFormatted: formatBaniDecimal(p.AmountBani),
			Provider:        p.Provider,
			ExternalID:      p.ExternalID,
			IdempotencyKey:  p.IdempotencyKey,
			PaidAtLabel:     paidLabel,
		})
	}

	var flashSuccess, flashError string
	switch r.URL.Query().Get("success") {
	case "invoice_generated":
		flashSuccess = "Factura proformă pentru ciclul de 30 de zile a fost emisă."
	case "invoice_paid":
		flashSuccess = "Plata a fost înregistrată în registrul imutabil (Ledger), iar abonamentul de 30 de zile a fost activat/prelungit."
	}

	meta := h.pageMeta(r, "Abonament 30 Zile & Facturi", true)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.BillingPage(meta, pages.BillingPageData{
		RoleLabel:        roleLabel,
		PlanTitle:        planTitle,
		PlanFeeBani:      feeBani,
		PlanFeeFormatted: formatBaniDecimal(feeBani),
		PlanDescription:  planDesc,
		Subscription:     subView,
		Invoices:         invViews,
		Payments:         payViews,
		FlashSuccess:     flashSuccess,
		FlashError:       flashError,
	}).Render(r.Context(), w)
}

func (h *PlatformHandler) GenerateInvoiceSubmit(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if !sess.IsLoggedIn() || sess.IsSuperAdmin {
		http.Error(w, "Acces neautorizat.", http.StatusForbidden)
		return
	}

	feeBani, _, _, _ := h.computeUserPlan(r.Context(), sess.UserID, sess.Role)
	now := time.Now().Unix()

	var randSuffix [2]byte
	_, _ = rand.Read(randSuffix[:])
	invNum := fmt.Sprintf("IB-%s-%d-%s", time.Now().Format("200601"), sess.UserID, strings.ToUpper(hex.EncodeToString(randSuffix[:])))

	_, err := h.queries.CreateInvoice(r.Context(), sqlc.CreateInvoiceParams{
		UserID:        sess.UserID,
		Amount:        float64(feeBani) / 100.0,
		AmountBani:    feeBani,
		Currency:      "RON",
		Status:        "unpaid",
		Type:          "subscription_30d",
		InvoiceNumber: invNum,
		DueDate:       now + 7*24*3600,
		PaidAt:        sql.NullInt64{},
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		http.Error(w, "Eroare la emiterea facturii.", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/dashboard/facturi?success=invoice_generated", http.StatusSeeOther)
}

func (h *PlatformHandler) processInvoicePaymentTx(
	ctx context.Context,
	inv sqlc.Invoice,
	provider string,
	externalID string,
	idempotencyKey string,
	rawPayload string,
) (sqlc.Payment, error) {
	if existing, err := h.queries.GetPaymentByIdempotencyKey(ctx, idempotencyKey); err == nil {
		return existing, nil
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return sqlc.Payment{}, err
	}
	defer func() { _ = tx.Rollback() }()

	qtx := h.queries.WithTx(tx)
	now := time.Now().Unix()

	if _, err := qtx.MarkInvoicePaid(ctx, sqlc.MarkInvoicePaidParams{
		PaidAt:    sql.NullInt64{Int64: now, Valid: true},
		UpdatedAt: now,
		ID:        inv.ID,
		UserID:    inv.UserID,
	}); err != nil {
		return sqlc.Payment{}, err
	}

	var subID int64
	if existingSub, subErr := qtx.GetActiveSubscriptionByUserID(ctx, inv.UserID); subErr == nil {
		baseExpiry := now
		if existingSub.Status == "active" && existingSub.ExpiresAt > now {
			baseExpiry = existingSub.ExpiresAt
		}
		updatedSub, extErr := qtx.ExtendSubscriptionExpiry(ctx, sqlc.ExtendSubscriptionExpiryParams{
			ExpiresAt: baseExpiry + subscriptionCycleSecs,
			UpdatedAt: now,
			ID:        existingSub.ID,
		})
		if extErr != nil {
			return sqlc.Payment{}, extErr
		}
		subID = updatedSub.ID
	} else {
		user, uErr := qtx.GetUserByID(ctx, inv.UserID)
		planID := "locatar_monthly"
		if uErr == nil {
			planID = user.PrimaryRole + "_monthly"
		}
		createdSub, createErr := qtx.CreateSubscription(ctx, sqlc.CreateSubscriptionParams{
			UserID:    inv.UserID,
			PlanID:    planID,
			Status:    "active",
			StartedAt: now,
			ExpiresAt: now + subscriptionCycleSecs,
			AutoRenew: 1,
			CreatedAt: now,
			UpdatedAt: now,
		})
		if createErr != nil {
			return sqlc.Payment{}, createErr
		}
		subID = createdSub.ID
	}

	if err := qtx.UpdateUserSubscriptionStatus(ctx, sqlc.UpdateUserSubscriptionStatusParams{
		IsSubscribed: 1,
		UpdatedAt:    now,
		ID:           inv.UserID,
	}); err != nil {
		return sqlc.Payment{}, err
	}

	userAssocs, _ := qtx.ListUserAssociations(ctx, inv.UserID)
	for _, a := range userAssocs {
		_ = qtx.UpdateAssociationSubscription(ctx, sqlc.UpdateAssociationSubscriptionParams{
			IsSubscribed: 1,
			UpdatedAt:    now,
			ID:           a.ID,
		})
	}

	payment, err := qtx.CreatePaymentIdempotent(ctx, sqlc.CreatePaymentIdempotentParams{
		SubscriptionID: subID,
		UserID:         inv.UserID,
		Amount:         float64(inv.AmountBani) / 100.0,
		AmountBani:     inv.AmountBani,
		Currency:       "RON",
		Status:         "succeeded",
		Provider:       provider,
		ExternalID:     externalID,
		IdempotencyKey: idempotencyKey,
		RawPayload:     rawPayload,
		PaidAt:         sql.NullInt64{Int64: now, Valid: true},
		CreatedAt:      now,
	})
	if err != nil {
		return sqlc.Payment{}, err
	}

	if err := tx.Commit(); err != nil {
		return sqlc.Payment{}, err
	}
	return payment, nil
}

func (h *PlatformHandler) PayInvoiceSubmit(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if !sess.IsLoggedIn() || sess.IsSuperAdmin {
		http.Error(w, "Acces neautorizat.", http.StatusForbidden)
		return
	}

	invID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || invID <= 0 {
		http.Error(w, "ID factură invalid.", http.StatusBadRequest)
		return
	}

	inv, err := h.queries.GetInvoiceByID(r.Context(), invID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Anti-IDOR ownership guard
	if inv.UserID != sess.UserID {
		http.Error(w, "Nu ai permisiunea de a achita această factură.", http.StatusForbidden)
		return
	}

	idempotencyKey := fmt.Sprintf("idem-inv-%d-u-%d", inv.ID, sess.UserID)
	externalID := fmt.Sprintf("NETOPIA-%s-%d", inv.InvoiceNumber, inv.ID)
	rawPayload := fmt.Sprintf(`{"invoice_id":%d,"invoice_number":%q,"amount_bani":%d}`, inv.ID, inv.InvoiceNumber, inv.AmountBani)

	if _, err := h.processInvoicePaymentTx(r.Context(), inv, "netopia", externalID, idempotencyKey, rawPayload); err != nil {
		http.Error(w, "Eroare la procesarea plății în registrul imutabil.", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/dashboard/facturi?success=invoice_paid", http.StatusSeeOther)
}

type netopiaWebhookPayload struct {
	InvoiceID      int64  `json:"invoice_id"`
	ExternalID     string `json:"external_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type netopiaWebhookResponse struct {
	Status         string `json:"status"`
	PaymentID      int64  `json:"payment_id"`
	AmountBani     int64  `json:"amount_bani"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (h *PlatformHandler) NetopiaWebhook(w http.ResponseWriter, r *http.Request) {
	var payload netopiaWebhookPayload
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		rawBody, readErr := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if readErr != nil {
			http.Error(w, "Eroare citire payload", http.StatusBadRequest)
			return
		}
		sigHeader := strings.TrimSpace(r.Header.Get("X-Netopia-Signature"))
		if sigHeader != "" || (h.cfg != nil && h.cfg.Env == config.EnvProduction) {
			if h.netopia == nil || !h.netopia.VerifyWebhookSignature(rawBody, sigHeader) {
				http.Error(w, "Semnătură HMAC-SHA256 Netopia invalidă", http.StatusUnauthorized)
				return
			}
		}
		if err := json.Unmarshal(rawBody, &payload); err != nil {
			http.Error(w, "JSON invalid", http.StatusBadRequest)
			return
		}
	} else {
		_ = r.ParseForm()
		payload.InvoiceID, _ = strconv.ParseInt(strings.TrimSpace(r.FormValue("invoice_id")), 10, 64)
		payload.ExternalID = strings.TrimSpace(r.FormValue("external_id"))
		payload.IdempotencyKey = strings.TrimSpace(r.FormValue("idempotency_key"))
	}

	if payload.InvoiceID <= 0 || payload.IdempotencyKey == "" {
		http.Error(w, "Parametri webhook incompleți", http.StatusBadRequest)
		return
	}
	if payload.ExternalID == "" {
		payload.ExternalID = "NTP-WH-" + strconv.FormatInt(payload.InvoiceID, 10)
	}

	inv, err := h.queries.GetInvoiceByID(r.Context(), payload.InvoiceID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	rawBytes, _ := json.Marshal(payload)
	payment, err := h.processInvoicePaymentTx(r.Context(), inv, "netopia", payload.ExternalID, payload.IdempotencyKey, string(rawBytes))
	if err != nil {
		http.Error(w, "Eroare procesare plată", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(netopiaWebhookResponse{
		Status:         "ok",
		PaymentID:      payment.ID,
		AmountBani:     payment.AmountBani,
		IdempotencyKey: payment.IdempotencyKey,
	})
}

type mediaUploadResponse struct {
	Status       string `json:"status"`
	ObjectKey    string `json:"object_key"`
	PublicURL    string `json:"public_url"`
	PresignedURL string `json:"presigned_url,omitempty"`
}

func (h *PlatformHandler) UploadMediaSubmit(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if !sess.IsLoggedIn() || sess.IsSuperAdmin {
		http.Error(w, "Acces neautorizat pentru încărcare media.", http.StatusForbidden)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, services.MaxMediaUploadBytes+64*1024)
	if err := r.ParseMultipartForm(services.MaxMediaUploadBytes); err != nil {
		http.Error(w, "Fișierul depășește limita de 10 MB sau formularul multipart este invalid.", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Fișierul atașat lipsește.", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, services.MaxMediaUploadBytes+1))
	if err != nil {
		http.Error(w, "Eroare la citirea fișierului.", http.StatusBadRequest)
		return
	}

	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	if _, valErr := services.ValidateMediaFile(contentType, int64(len(data))); valErr != nil {
		http.Error(w, valErr.Error(), http.StatusUnsupportedMediaType)
		return
	}

	objectKey, keyErr := services.BuildSafeObjectKey("tickets", sess.UserID, contentType)
	if keyErr != nil {
		http.Error(w, keyErr.Error(), http.StatusBadRequest)
		return
	}

	publicURL, upErr := h.r2.UploadObject(r.Context(), objectKey, contentType, data)
	if upErr != nil {
		http.Error(w, "Eroare la încărcarea în Cloudflare R2.", http.StatusBadGateway)
		return
	}
	presignedURL, _ := h.r2.GeneratePresignedPutURL(objectKey, 15*time.Minute, time.Now())

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(mediaUploadResponse{
		Status:       "ok",
		ObjectKey:    objectKey,
		PublicURL:    publicURL,
		PresignedURL: presignedURL,
	})
}

// ============================================================================
// 4. EXCLUSIVE SUPER ADMIN CONTROL PANEL & MULTI-RECIPIENT REPORTS
// ============================================================================

func roleDisplayName(role string) string {
	switch role {
	case "platform_admin":
		return "Super Admin"
	case "asociatie":
		return "Asociație Proprietari"
	case "prestator":
		return "Firmă / Prestator"
	default:
		return "Locatar / Proprietar"
	}
}

func (h *PlatformHandler) buildAdminUserRows(ctx context.Context) ([]pages.AdminUserRowView, int, int, int, int) {
	users, _ := h.queries.ListAllUsersForAdmin(ctx)
	subs, _ := h.queries.ListAllSubscriptionsForAdmin(ctx)

	latestSubByUser := make(map[int64]sqlc.Subscription, len(subs))
	for _, s := range subs {
		if _, exists := latestSubByUser[s.UserID]; !exists {
			latestSubByUser[s.UserID] = s
		}
	}

	now := time.Now().Unix()
	rows := make([]pages.AdminUserRowView, 0, len(users))
	var locatariCount, asociatiiCount, prestatoriCount, activeSubCount int

	for _, u := range users {
		role := u.PrimaryRole
		if config.IsSuperAdminEmail(u.Email) {
			role = "platform_admin"
		}
		switch role {
		case "asociatie":
			asociatiiCount++
		case "prestator":
			prestatoriCount++
		case "locatar":
			locatariCount++
		}

		sub, hasSub := latestSubByUser[u.ID]
		isSubActive := u.IsSubscribed == 1
		startedLabel := "—"
		expiresLabel := "—"
		autoRenew := false
		var subID int64

		if hasSub {
			subID = sub.ID
			startedLabel = formatTimestamp(sub.StartedAt)
			expiresLabel = formatTimestamp(sub.ExpiresAt)
			autoRenew = sub.AutoRenew == 1
			isSubActive = sub.Status == "active" && sub.ExpiresAt > now && u.IsSubscribed == 1
		}
		if isSubActive {
			activeSubCount++
		}

		var entityDetails string
		if role == "asociatie" {
			assocs, _ := h.queries.ListUserAssociations(ctx, u.ID)
			if len(assocs) > 0 {
				entityDetails = fmt.Sprintf("%s (%d ap. • %s RON/lună)", assocs[0].Name, assocs[0].ApartmentCount, formatBaniDecimal(assocs[0].ApartmentCount*asociatiePerAptBani))
			}
		} else if role == "prestator" {
			if prof, err := h.queries.GetProviderProfileByUserID(ctx, u.ID); err == nil && prof.CompanyName.Valid {
				entityDetails = fmt.Sprintf("%s (CUI: %s)", prof.CompanyName.String, prof.Cui.String)
			}
		}

		rows = append(rows, pages.AdminUserRowView{
			ID:             u.ID,
			Name:           u.Name,
			Email:          u.Email,
			Phone:          u.Phone.String,
			Role:           role,
			RoleLabel:      roleDisplayName(role),
			EntityDetails:  entityDetails,
			IsActive:       u.IsActive == 1,
			IsSubscribed:   isSubActive,
			SubscriptionID: subID,
			StartedAtLabel: startedLabel,
			ExpiresAtLabel: expiresLabel,
			AutoRenew:      autoRenew,
			CreatedAtLabel: formatTimestamp(u.CreatedAt),
		})
	}

	return rows, locatariCount, asociatiiCount, prestatoriCount, activeSubCount
}

func (h *PlatformHandler) PlatformAdminPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireExclusiveSuperAdmin(w, r); !ok {
		return
	}

	if h.cron != nil {
		_, _, _ = h.cron.RunOnce(r.Context())
	}

	userRows, locatariCount, asociatiiCount, prestatoriCount, activeSubCount := h.buildAdminUserRows(r.Context())

	allPayments, _ := h.queries.ListAllPaymentsForAdmin(r.Context())
	var totalRevenueBani int64
	recentPayments := make([]pages.PaymentRowView, 0, len(allPayments))
	for _, p := range allPayments {
		if p.Status == "succeeded" {
			totalRevenueBani += p.AmountBani
		}
		if len(recentPayments) < 15 {
			paidLabel := formatTimestamp(p.CreatedAt)
			if p.PaidAt.Valid && p.PaidAt.Int64 > 0 {
				paidLabel = formatTimestamp(p.PaidAt.Int64)
			}
			recentPayments = append(recentPayments, pages.PaymentRowView{
				ID:              p.ID,
				AmountBani:      p.AmountBani,
				AmountFormatted: formatBaniDecimal(p.AmountBani),
				Provider:        p.Provider,
				ExternalID:      p.ExternalID,
				IdempotencyKey:  p.IdempotencyKey,
				PaidAtLabel:     paidLabel,
			})
		}
	}

	allTickets, _ := h.queries.ListActiveReports(r.Context())
	now := time.Now().Unix()
	var activeTicketsCount, completedTicketsCount, overdueSLACount int
	recentTickets := make([]pages.AdminTicketRowView, 0, len(allTickets))
	for _, t := range allTickets {
		if t.Status == "completed" {
			completedTicketsCount++
		} else {
			activeTicketsCount++
		}
		isOverdue := (t.Status == "claimed" || t.Status == "accepted" || t.Status == "in_progress") && (now-t.UpdatedAt > 96*3600)
		if isOverdue {
			overdueSLACount++
		}
		if len(recentTickets) < 25 {
			phaseNum, statusLabel := statusPhase(t.Status)
			recentTickets = append(recentTickets, pages.AdminTicketRowView{
				ID:             t.ID,
				Title:          t.Title,
				Category:       t.Category,
				County:         t.County,
				Status:         t.Status,
				StatusLabel:    statusLabel,
				PhaseNumber:    phaseNum,
				Overdue96h:     isOverdue,
				CreatedAtLabel: formatTimestamp(t.CreatedAt),
			})
		}
	}

	var flashSuccess, flashError string
	switch r.URL.Query().Get("success") {
	case "subscription_toggled":
		flashSuccess = "Statusul abonamentului de 30 de zile a fost actualizat."
	case "autorenew_toggled":
		flashSuccess = "Opțiunea de reînnoire automată a fost comutată."
	case "active_toggled":
		flashSuccess = "Starea contului utilizatorului a fost actualizată."
	case "report_sent":
		flashSuccess = "Raportul executiv multi-destinatar a fost expediat pe email."
	}

	meta := h.pageMeta(r, "Panou Super Admin — InstalBloc", true)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.PlatformAdminPage(meta, pages.PlatformAdminPageData{
		TotalUsers:            len(userRows),
		TotalLocatari:         locatariCount,
		TotalAsociatii:        asociatiiCount,
		TotalPrestatori:       prestatoriCount,
		ActiveSubscriptions:   activeSubCount,
		TotalRevenueBani:      totalRevenueBani,
		TotalRevenueRON:       formatBaniDecimal(totalRevenueBani),
		ActiveTicketsCount:    activeTicketsCount,
		CompletedTicketsCount: completedTicketsCount,
		OverdueSLACount:       overdueSLACount,
		Users:                 userRows,
		RecentTickets:         recentTickets,
		RecentPayments:        recentPayments,
		FlashSuccess:          flashSuccess,
		FlashError:            flashError,
	}).Render(r.Context(), w)
}

func (h *PlatformHandler) AdminToggleUserSubscription(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireExclusiveSuperAdmin(w, r); !ok {
		return
	}

	targetUserID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || targetUserID <= 0 {
		http.Error(w, "ID utilizator invalid.", http.StatusBadRequest)
		return
	}

	targetUser, err := h.queries.GetUserByID(r.Context(), targetUserID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	now := time.Now().Unix()
	existingSub, subErr := h.queries.GetActiveSubscriptionByUserID(r.Context(), targetUserID)
	currentlyActive := targetUser.IsSubscribed == 1 && (subErr != nil || (existingSub.Status == "active" && existingSub.ExpiresAt > now))

	var newSubFlag int64 = 1
	if currentlyActive {
		newSubFlag = 0
		if subErr == nil {
			_, _ = h.queries.ExtendSubscriptionExpiry(r.Context(), sqlc.ExtendSubscriptionExpiryParams{
				ExpiresAt: now - 60,
				UpdatedAt: now,
				ID:        existingSub.ID,
			})
			_ = h.queries.ExpireOverdueSubscriptions(r.Context(), sqlc.ExpireOverdueSubscriptionsParams{
				UpdatedAt: now,
				ExpiresAt: now,
			})
		}
	} else {
		if subErr == nil {
			_, _ = h.queries.ExtendSubscriptionExpiry(r.Context(), sqlc.ExtendSubscriptionExpiryParams{
				ExpiresAt: now + subscriptionCycleSecs,
				UpdatedAt: now,
				ID:        existingSub.ID,
			})
		} else {
			_, _ = h.queries.CreateSubscription(r.Context(), sqlc.CreateSubscriptionParams{
				UserID:    targetUserID,
				PlanID:    targetUser.PrimaryRole + "_monthly",
				Status:    "active",
				StartedAt: now,
				ExpiresAt: now + subscriptionCycleSecs,
				AutoRenew: 1,
				CreatedAt: now,
				UpdatedAt: now,
			})
		}
	}

	_ = h.queries.UpdateUserSubscriptionStatus(r.Context(), sqlc.UpdateUserSubscriptionStatusParams{
		IsSubscribed: newSubFlag,
		UpdatedAt:    now,
		ID:           targetUserID,
	})

	assocs, _ := h.queries.ListUserAssociations(r.Context(), targetUserID)
	for _, a := range assocs {
		_ = h.queries.UpdateAssociationSubscription(r.Context(), sqlc.UpdateAssociationSubscriptionParams{
			IsSubscribed: newSubFlag,
			UpdatedAt:    now,
			ID:           a.ID,
		})
	}

	if isHTMX(r) {
		rows, _, _, _, _ := h.buildAdminUserRows(r.Context())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = pages.PlatformAdminUsersPartial(rows).Render(r.Context(), w)
		return
	}

	http.Redirect(w, r, "/platform-admin?success=subscription_toggled", http.StatusSeeOther)
}

func (h *PlatformHandler) AdminToggleUserAutoRenew(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireExclusiveSuperAdmin(w, r); !ok {
		return
	}

	targetUserID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || targetUserID <= 0 {
		http.Error(w, "ID utilizator invalid.", http.StatusBadRequest)
		return
	}

	targetUser, err := h.queries.GetUserByID(r.Context(), targetUserID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	now := time.Now().Unix()
	if existingSub, subErr := h.queries.GetActiveSubscriptionByUserID(r.Context(), targetUserID); subErr == nil {
		var nextVal int64 = 1
		if existingSub.AutoRenew == 1 {
			nextVal = 0
		}
		_, _ = h.queries.AdminToggleSubscriptionAutoRenew(r.Context(), sqlc.AdminToggleSubscriptionAutoRenewParams{
			AutoRenew: nextVal,
			UpdatedAt: now,
			ID:        existingSub.ID,
		})
	} else {
		_, _ = h.queries.CreateSubscription(r.Context(), sqlc.CreateSubscriptionParams{
			UserID:    targetUserID,
			PlanID:    targetUser.PrimaryRole + "_monthly",
			Status:    "active",
			StartedAt: now,
			ExpiresAt: now + subscriptionCycleSecs,
			AutoRenew: 1,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	if isHTMX(r) {
		rows, _, _, _, _ := h.buildAdminUserRows(r.Context())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = pages.PlatformAdminUsersPartial(rows).Render(r.Context(), w)
		return
	}

	http.Redirect(w, r, "/platform-admin?success=autorenew_toggled", http.StatusSeeOther)
}

func (h *PlatformHandler) AdminToggleUserActive(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireExclusiveSuperAdmin(w, r); !ok {
		return
	}

	targetUserID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || targetUserID <= 0 {
		http.Error(w, "ID utilizator invalid.", http.StatusBadRequest)
		return
	}

	targetUser, err := h.queries.GetUserByID(r.Context(), targetUserID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if config.IsSuperAdminEmail(targetUser.Email) {
		http.Error(w, "Conturile canonice de Super Admin nu pot fi suspendate.", http.StatusForbidden)
		return
	}

	var nextActive int64 = 1
	if targetUser.IsActive == 1 {
		nextActive = 0
	}
	now := time.Now().Unix()
	_ = h.queries.UpdateUserActiveStatus(r.Context(), sqlc.UpdateUserActiveStatusParams{
		IsActive:  nextActive,
		UpdatedAt: now,
		ID:        targetUserID,
	})

	if isHTMX(r) {
		rows, _, _, _, _ := h.buildAdminUserRows(r.Context())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = pages.PlatformAdminUsersPartial(rows).Render(r.Context(), w)
		return
	}

	http.Redirect(w, r, "/platform-admin?success=active_toggled", http.StatusSeeOther)
}

func (h *PlatformHandler) AdminSendReportSubmit(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireExclusiveSuperAdmin(w, r); !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Formular invalid.", http.StatusBadRequest)
		return
	}

	rawRecipients := strings.ReplaceAll(r.FormValue("recipients"), ";", ",")
	parts := strings.Split(rawRecipients, ",")
	validRecipients := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))

	for _, part := range parts {
		candidate := strings.TrimSpace(strings.ToLower(part))
		if candidate == "" {
			continue
		}
		if parsed, err := mail.ParseAddress(candidate); err == nil && parsed.Address != "" {
			addr := strings.ToLower(parsed.Address)
			if _, dup := seen[addr]; !dup {
				seen[addr] = struct{}{}
				validRecipients = append(validRecipients, addr)
			}
		}
	}

	if len(validRecipients) == 0 {
		http.Error(w, "Te rugăm să introduci cel puțin o adresă de email validă.", http.StatusBadRequest)
		return
	}

	userRows, locatariCount, asociatiiCount, prestatoriCount, activeSubCount := h.buildAdminUserRows(r.Context())
	allPayments, _ := h.queries.ListAllPaymentsForAdmin(r.Context())
	var totalRevenueBani int64
	for _, p := range allPayments {
		if p.Status == "succeeded" {
			totalRevenueBani += p.AmountBani
		}
	}

	allTickets, _ := h.queries.ListActiveReports(r.Context())
	var openCount, completedCount int
	for _, t := range allTickets {
		if t.Status == "completed" {
			completedCount++
		} else {
			openCount++
		}
	}

	nowLabel := formatTimestamp(time.Now().Unix())
	subject := fmt.Sprintf("Raport Executiv InstalBloc — %s", nowLabel)
	body := fmt.Sprintf(
		"RAPORT EXECUTIV PLATFORMA INSTALBLOC ROMÂNIA\r\n"+
			"Generat la: %s\r\n\r\n"+
			"1. UTILIZATORI & ABONAMENTE (CICLU 30 ZILE):\r\n"+
			"- Total Utilizatori: %d (%d Asociații, %d Prestatori, %d Locatari)\r\n"+
			"- Abonamente Active: %d\r\n\r\n"+
			"2. REGISTRU FINANCIAR IMUTABIL (LEDGER):\r\n"+
			"- Venituri Totale Procesate: %s RON (%d bani)\r\n"+
			"- Tranzacții Înregistrate: %d\r\n\r\n"+
			"3. PANOU LUCRĂRI & PROCESE-VERBALE:\r\n"+
			"- Lucrări Active în Derulare: %d\r\n"+
			"- Lucrări Finalizate cu PV: %d\r\n\r\n"+
			"Destinatari raport: %s\r\n",
		nowLabel,
		len(userRows), asociatiiCount, prestatoriCount, locatariCount,
		activeSubCount,
		formatBaniDecimal(totalRevenueBani), totalRevenueBani,
		len(allPayments),
		openCount,
		completedCount,
		strings.Join(validRecipients, ", "),
	)

	if h.email != nil {
		for _, recipient := range validRecipients {
			_ = h.email.SendTicketAlert(r.Context(), recipient, subject, body)
		}
	}

	http.Redirect(w, r, "/platform-admin?success=report_sent", http.StatusSeeOther)
}
