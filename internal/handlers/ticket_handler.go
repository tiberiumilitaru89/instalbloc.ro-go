package handlers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"net/url"
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

type TicketHandler struct {
	db      *sql.DB
	queries *sqlc.Queries
	cfg     *config.Config
	email   *services.EmailService
}

func NewTicketHandler(conn *sql.DB, queries *sqlc.Queries, cfg *config.Config, emailSvc *services.EmailService) *TicketHandler {
	return &TicketHandler{
		db:      conn,
		queries: queries,
		cfg:     cfg,
		email:   emailSvc,
	}
}

func (h *TicketHandler) pageMeta(r *http.Request, title string, noIndex bool) layouts.PageMeta {
	return layouts.PageMeta{
		Title:        title,
		CanonicalURL: canonicalBaseURL + r.URL.Path,
		NoIndex:      noIndex,
		CSPNonce:     middleware.NonceFromContext(r.Context()),
		CurrentYear:  time.Now().Year(),
		Session:      auth.SessionFromContext(r.Context()),
	}
}

func isHTMX(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("HX-Request"), "true")
}

func formatTimestamp(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	return time.Unix(ts, 0).Format("02.01.2006, 15:04:05")
}

func formatBaniRON(bani int64) string {
	if bani < 0 {
		bani = 0
	}
	whole := bani / 100
	rem := bani % 100
	if rem == 0 {
		return fmt.Sprintf("%d RON", whole)
	}
	return fmt.Sprintf("%d.%02d RON", whole, rem)
}

func floatRONToBani(val float64) int64 {
	if math.IsNaN(val) || math.IsInf(val, 0) || val < 0 {
		return 0
	}
	return int64(math.Round(val * 100))
}

func parseRONToBani(raw string) (int64, error) {
	cleaned := strings.TrimSpace(strings.ReplaceAll(raw, ",", "."))
	if cleaned == "" {
		return 0, nil
	}
	parts := strings.Split(cleaned, ".")
	if len(parts) > 2 {
		return 0, fmt.Errorf("format monetar invalid")
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole < 0 {
		return 0, fmt.Errorf("sumă invalidă")
	}
	var frac int64
	if len(parts) == 2 {
		fracStr := parts[1]
		if len(fracStr) > 2 {
			fracStr = fracStr[:2]
		} else if len(fracStr) == 1 {
			fracStr += "0"
		}
		if fracStr != "" {
			frac, err = strconv.ParseInt(fracStr, 10, 64)
			if err != nil || frac < 0 {
				return 0, fmt.Errorf("subdiviziuni invalide")
			}
		}
	}
	return whole*100 + frac, nil
}

func maskPhone(phone string) string {
	trimmed := strings.TrimSpace(phone)
	if trimmed == "" {
		return "07X XXX XXX"
	}
	return "07X XXX XXX"
}

func statusPhase(status string) (int, string) {
	switch status {
	case "open", "pending_offers":
		return 1, "1. Deschis"
	case "claimed", "accepted":
		return 2, "2. Ofertă Acceptată"
	case "in_progress":
		return 3, "3. În Lucru"
	case "completed":
		return 4, "4. Finalizat"
	default:
		return 1, "1. Deschis"
	}
}

func urgencyLabel(urgency string) string {
	switch strings.ToLower(strings.TrimSpace(urgency)) {
	case "high", "urgenta_imediata":
		return "Urgență Majoră"
	case "low", "programare":
		return "Cu Programare"
	default:
		return "Normală"
	}
}

func generateTrackingToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

func normalizeMonthlyMods(profile sqlc.ProviderProfile, now time.Time) int64 {
	if !profile.LastModificationReset.Valid || profile.LastModificationReset.Int64 <= 0 {
		return profile.MonthlyModifications
	}
	lastReset := time.Unix(profile.LastModificationReset.Int64, 0)
	if lastReset.Year() != now.Year() || lastReset.Month() != now.Month() {
		return 0
	}
	if profile.MonthlyModifications < 0 {
		return 0
	}
	return profile.MonthlyModifications
}

func (h *TicketHandler) requirePlatformUser(w http.ResponseWriter, r *http.Request) (domain.SessionView, bool) {
	session := auth.SessionFromContext(r.Context())
	if !session.IsLoggedIn() {
		if isHTMX(r) {
			w.Header().Set("HX-Redirect", "/login")
			w.WriteHeader(http.StatusUnauthorized)
			return session, false
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return session, false
	}
	if session.IsSuperAdmin || session.Role == domain.RolePlatformAdmin {
		if isHTMX(r) {
			w.Header().Set("HX-Redirect", "/platform-admin")
			w.WriteHeader(http.StatusForbidden)
			return session, false
		}
		http.Redirect(w, r, "/platform-admin", http.StatusSeeOther)
		return session, false
	}
	return session, true
}

func (h *TicketHandler) isManagingAdminForReport(ctx context.Context, userID int64, report sqlc.Report) bool {
	if userID <= 0 {
		return false
	}
	if report.AssociationID.Valid && report.AssociationID.Int64 > 0 {
		_, err := h.queries.GetAssociationAdmin(ctx, sqlc.GetAssociationAdminParams{
			UserID:        userID,
			AssociationID: report.AssociationID.Int64,
		})
		if err == nil {
			return true
		}
	}
	if report.Asociatie.Valid && strings.TrimSpace(report.Asociatie.String) != "" {
		assocs, err := h.queries.ListUserAssociations(ctx, userID)
		if err == nil {
			target := services.NormalizeEntityName(report.Asociatie.String)
			for _, a := range assocs {
				if services.NormalizeEntityName(a.Name) == target {
					return true
				}
			}
		}
	}
	return false
}

func (h *TicketHandler) buildSubscriptionSummary(ctx context.Context, user sqlc.User) pages.SubscriptionSummaryView {
	planLabel := "Abonament Client Independent (10 RON / lună)"
	switch domain.RoleType(user.PrimaryRole) {
	case domain.RoleAsociatie:
		planLabel = "Abonament Asociație (3 RON / ap. / lună)"
	case domain.RolePrestator:
		planLabel = "Abonament Membru Profesionist (15 RON / lună)"
	}

	sub, err := h.queries.GetActiveSubscriptionByUserID(ctx, user.ID)
	if err != nil {
		started := user.CreatedAt
		expires := started + 30*86400
		return pages.SubscriptionSummaryView{
			ID:                 0,
			PlanLabel:          planLabel,
			Status:             "inactive",
			IsSubscribed:       user.IsSubscribed == 1,
			StartedAtFormatted: formatTimestamp(started),
			ExpiresAtFormatted: formatTimestamp(expires),
			AutoRenew:          false,
		}
	}

	isSub := user.IsSubscribed == 1 || (sub.Status == "active" && sub.ExpiresAt > time.Now().Unix())
	return pages.SubscriptionSummaryView{
		ID:                 sub.ID,
		PlanLabel:          planLabel,
		Status:             sub.Status,
		IsSubscribed:       isSub,
		StartedAtFormatted: formatTimestamp(sub.StartedAt),
		ExpiresAtFormatted: formatTimestamp(sub.ExpiresAt),
		AutoRenew:          sub.AutoRenew == 1,
	}
}

func (h *TicketHandler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	user, err := h.queries.GetUserByID(ctx, session.UserID)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	filterStatus := strings.TrimSpace(r.URL.Query().Get("status"))
	filterCategory := strings.TrimSpace(r.URL.Query().Get("category"))
	filterCounty := strings.TrimSpace(r.URL.Query().Get("county"))
	if strings.EqualFold(filterCounty, "toate") {
		filterCounty = ""
	}
	asocParam := strings.TrimSpace(r.URL.Query().Get("asoc_id"))
	if asocParam == "" {
		asocParam = strings.TrimSpace(r.URL.Query().Get("asoc"))
	}
	filterAsocID, _ := strconv.ParseInt(asocParam, 10, 64)

	var providerSlugs []string
	var providerDomains []string
	var providerCounty string
	if session.Role == domain.RolePrestator {
		slugRows, _ := h.queries.ListProviderCategorySlugs(ctx, session.UserID)
		for _, row := range slugRows {
			providerSlugs = append(providerSlugs, row.Slug)
			if row.Name != "" {
				providerDomains = append(providerDomains, row.Name)
			} else if row.Slug != "" {
				providerDomains = append(providerDomains, row.Slug)
			}
		}
		if prof, pErr := h.queries.GetProviderProfileByUserID(ctx, session.UserID); pErr == nil && prof.County.Valid {
			providerCounty = prof.County.String
		}
	}

	var managedAssocs []pages.AssociationOptionView
	managedAssocIDs := make(map[int64]bool)
	managedAssocNames := make(map[string]bool)
	if session.Role == domain.RoleAsociatie {
		assocs, _ := h.queries.ListUserAssociations(ctx, session.UserID)
		for _, a := range assocs {
			managedAssocIDs[a.ID] = true
			managedAssocNames[services.NormalizeEntityName(a.Name)] = true
			managedAssocs = append(managedAssocs, pages.AssociationOptionView{
				ID:      a.ID,
				Name:    a.Name,
				Address: a.Address,
				County:  a.County.String,
				City:    a.City.String,
			})
		}
	}

	var locatarAssocID int64
	var locatarAssocName string
	if session.Role == domain.RoleLocatar {
		if lp, lErr := h.queries.GetLocatarProfileByUserID(ctx, session.UserID); lErr == nil {
			if lp.AssociationID.Valid {
				locatarAssocID = lp.AssociationID.Int64
				if assocRow, aErr := h.queries.GetAssociationByID(ctx, locatarAssocID); aErr == nil {
					locatarAssocName = services.NormalizeEntityName(assocRow.Name)
				}
			}
		}
	}

	reports, _ := h.queries.ListActiveReports(ctx)
	now := time.Now().Unix()

	var visibleCards []pages.TicketCardView
	totalActive := 0
	totalUrgent := 0
	totalCompleted := 0

	for _, rep := range reports {
		isOwner := rep.UserID == session.UserID
		isAssignedProvider := rep.ClaimedByProviderID.Valid && rep.ClaimedByProviderID.Int64 == session.UserID
		isManagingAdmin := false
		if session.Role == domain.RoleAsociatie {
			if rep.AssociationID.Valid && managedAssocIDs[rep.AssociationID.Int64] {
				isManagingAdmin = true
			} else if rep.Asociatie.Valid && managedAssocNames[services.NormalizeEntityName(rep.Asociatie.String)] {
				isManagingAdmin = true
			}
		}

		// Visibility Guard: Phase 3 (in_progress) & Phase 4 (completed) tickets are strictly private
		// to the creator, managing association admin, and assigned provider.
		if rep.Status == "in_progress" || rep.Status == "completed" {
			if !isOwner && !isManagingAdmin && !isAssignedProvider {
				continue
			}
		}

		// Role-level visibility
		switch session.Role {
		case domain.RolePrestator:
			if !isAssignedProvider {
				if len(providerSlugs) > 0 {
					domainMatch := false
					for _, slug := range providerSlugs {
						if domain.MatchesCategoryFilter(rep.Category, slug) {
							domainMatch = true
							break
						}
					}
					if !domainMatch {
						continue
					}
				}
			}
		case domain.RoleAsociatie:
			if !isOwner && !isManagingAdmin {
				continue
			}
			if filterAsocID > 0 && (!rep.AssociationID.Valid || rep.AssociationID.Int64 != filterAsocID) {
				continue
			}
		case domain.RoleLocatar:
			sameAssoc := false
			if locatarAssocID > 0 && rep.AssociationID.Valid && rep.AssociationID.Int64 == locatarAssocID {
				sameAssoc = true
			} else if locatarAssocName != "" && rep.Asociatie.Valid && services.NormalizeEntityName(rep.Asociatie.String) == locatarAssocName {
				sameAssoc = true
			}
			if !isOwner && !sameAssoc {
				continue
			}
		}

		if rep.Status == "completed" {
			totalCompleted++
		} else {
			totalActive++
			if rep.Urgency == "high" || rep.Urgency == "urgenta_imediata" || rep.TipInterventie == "avarie" {
				totalUrgent++
			}
		}

		if filterStatus != "" {
			switch filterStatus {
			case "urgente":
				if rep.Urgency != "high" && rep.Urgency != "urgenta_imediata" && rep.TipInterventie != "avarie" {
					continue
				}
			case "programate":
				if rep.TipInterventie != "oferta" && rep.Urgency != "programare" {
					continue
				}
			case "finalizate":
				if rep.Status != "completed" {
					continue
				}
			default:
				if rep.Status != filterStatus {
					continue
				}
			}
		}
		if session.Role != domain.RolePrestator && filterCategory != "" {
			if !domain.MatchesCategoryFilter(rep.Category, filterCategory) {
				continue
			}
		}
		if filterCounty != "" && !strings.EqualFold(strings.TrimSpace(rep.County), filterCounty) {
			continue
		}

		offers, _ := h.queries.ListOffersByRequestWithProvider(ctx, rep.ID)
		activeOffersCount := 0
		for _, off := range offers {
			if off.Status != "withdrawn" {
				activeOffersCount++
			}
		}

		phase, phaseLabel := statusPhase(rep.Status)
		isEscalated := (rep.Status == "open" || rep.Status == "pending_offers") && (now-rep.CreatedAt > 7200)

		visibleCards = append(visibleCards, pages.TicketCardView{
			ID:                 rep.ID,
			Title:              rep.Title,
			Description:        rep.Description,
			Category:           rep.Category,
			Status:             rep.Status,
			Phase:              phase,
			PhaseLabel:         phaseLabel,
			Urgency:            rep.Urgency,
			UrgencyLabel:       urgencyLabel(rep.Urgency),
			County:             rep.County,
			Zone:               rep.Zone,
			Asociatie:          rep.Asociatie.String,
			Apartament:         rep.Apartament.String,
			TipSpatiu:          rep.TipSpatiu.String,
			TipInterventie:     rep.TipInterventie,
			ConfirmationsCount: rep.ConfirmationsCount,
			OffersCount:        activeOffersCount,
			IsRead:             rep.IsRead == 1,
			IsReadByAdmin:      rep.IsReadByAdmin == 1,
			IsEscalated2h:      isEscalated,
			CreatedAtFormatted: formatTimestamp(rep.CreatedAt),
		})
	}

	viewData := pages.DashboardViewData{
		UserName:            user.Name,
		UserEmail:           user.Email,
		UserPhone:           user.Phone.String,
		UserRole:            string(session.Role),
		RoleBadge:           session.RoleLabel(),
		Subscription:        h.buildSubscriptionSummary(ctx, user),
		FilterStatus:        filterStatus,
		FilterCategory:      filterCategory,
		FilterCounty:        filterCounty,
		FilterAsocID:        filterAsocID,
		ProviderDomains:     providerDomains,
		ProviderCounty:      providerCounty,
		ManagedAssociations: managedAssocs,
		Tickets:             visibleCards,
		TotalActive:         totalActive,
		TotalUrgent:         totalUrgent,
		TotalCompleted:      totalCompleted,
		FlashMsg:            r.URL.Query().Get("flash"),
		ErrorMsg:            r.URL.Query().Get("error"),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if isHTMX(r) {
		_ = pages.DashboardTicketsPartial(viewData).Render(ctx, w)
		return
	}

	meta := h.pageMeta(r, "Panou Lucrări", true)
	_ = pages.DashboardPage(meta, viewData).Render(ctx, w)
}

func (h *TicketHandler) ToggleAutoRenew(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Cerere invalidă", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	user, err := h.queries.GetUserByID(ctx, session.UserID)
	if err != nil {
		http.Error(w, "Utilizator inexistent", http.StatusNotFound)
		return
	}

	desired := int64(0)
	if r.FormValue("auto_renew") == "1" {
		desired = 1
	}
	now := time.Now().Unix()

	sub, err := h.queries.GetActiveSubscriptionByUserID(ctx, user.ID)
	if err != nil {
		expires := now + 30*86400
		status := "inactive"
		if user.IsSubscribed == 1 {
			status = "active"
		}
		_, _ = h.queries.CreateSubscription(ctx, sqlc.CreateSubscriptionParams{
			UserID:    user.ID,
			PlanID:    user.PrimaryRole,
			Status:    status,
			StartedAt: now,
			ExpiresAt: expires,
			AutoRenew: desired,
			CreatedAt: now,
			UpdatedAt: now,
		})
	} else {
		_, _ = h.queries.ToggleSubscriptionAutoRenew(ctx, sqlc.ToggleSubscriptionAutoRenewParams{
			AutoRenew: desired,
			UpdatedAt: now,
			ID:        sub.ID,
			UserID:    user.ID,
		})
	}

	if isHTMX(r) {
		summary := h.buildSubscriptionSummary(ctx, user)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = pages.SubscriptionAutoRenewPartial(summary).Render(ctx, w)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *TicketHandler) ProviderOffersPage(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	if session.Role != domain.RolePrestator {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	activeTab := strings.TrimSpace(r.URL.Query().Get("tab"))
	if activeTab == "" {
		activeTab = "toate"
	}

	var modsUsed int64
	if prof, err := h.queries.GetProviderProfileByUserID(ctx, session.UserID); err == nil {
		modsUsed = normalizeMonthlyMods(prof, time.Now())
	}
	modsRemaining := int64(3) - modsUsed
	if modsRemaining < 0 {
		modsRemaining = 0
	}

	rows, _ := h.queries.ListOffersByProviderWithReport(ctx, session.UserID)
	var items []pages.ProviderOfferDashboardItem
	totalCount := 0
	pendingCount := 0
	acceptedCount := 0
	rejectedCount := 0

	for _, row := range rows {
		totalCount++
		switch row.Status {
		case "pending":
			pendingCount++
		case "accepted":
			acceptedCount++
		case "rejected", "withdrawn":
			rejectedCount++
		}

		if activeTab == "asteptare" && row.Status != "pending" {
			continue
		}
		if activeTab == "acceptate" && row.Status != "accepted" {
			continue
		}
		if activeTab == "respinse" && row.Status != "rejected" && row.Status != "withdrawn" {
			continue
		}

		priceBani := row.PriceBani
		if priceBani == 0 && row.Price > 0 {
			priceBani = floatRONToBani(row.Price)
		}
		matBani := row.MaterialsPriceBani
		if matBani == 0 && row.MaterialsPrice.Valid && row.MaterialsPrice.Float64 > 0 {
			matBani = floatRONToBani(row.MaterialsPrice.Float64)
		}
		_, phaseLabel := statusPhase(row.ReportStatus)

		statusLabel := "În așteptare"
		switch row.Status {
		case "accepted":
			statusLabel = "Acceptată"
		case "rejected":
			statusLabel = "Respinsă"
		case "withdrawn":
			statusLabel = "Retrasă"
		}

		items = append(items, pages.ProviderOfferDashboardItem{
			OfferID:            row.ID,
			RequestID:          row.RequestID,
			ReportTitle:        row.ReportTitle,
			ReportCategory:     row.ReportCategory,
			ReportCounty:       row.ReportCounty,
			ReportZone:         row.ReportZone,
			ReportAsociatie:    row.ReportAsociatie,
			ReportStatus:       row.ReportStatus,
			ReportPhaseLabel:   phaseLabel,
			PriceFormatted:     formatBaniRON(priceBani),
			MaterialsFormatted: formatBaniRON(matBani),
			TotalFormatted:     formatBaniRON(priceBani + matBani),
			EstimatedTime:      row.EstimatedTime,
			OfferStatus:        row.Status,
			OfferStatusLabel:   statusLabel,
			CreatedAtFormatted: formatTimestamp(row.CreatedAt),
		})
	}

	data := pages.ProviderOffersPageData{
		ActiveTab:            activeTab,
		TotalCount:           totalCount,
		PendingCount:         pendingCount,
		AcceptedCount:        acceptedCount,
		RejectedCount:        rejectedCount,
		MonthlyModsUsed:      modsUsed,
		MonthlyModsRemaining: modsRemaining,
		Items:                items,
	}

	meta := h.pageMeta(r, "Ofertele Mele — Panou Lucrări", true)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.ProviderOffersPage(meta, data).Render(ctx, w)
}

func (h *TicketHandler) buildNewRequestViewData(ctx context.Context, session domain.SessionView, reqType, errMsg string) pages.NewRequestViewData {
	if reqType != "oferta" {
		reqType = "avarie"
	}
	user, _ := h.queries.GetUserByID(ctx, session.UserID)

	data := pages.NewRequestViewData{
		RequestType:   reqType,
		DefaultName:   user.Name,
		DefaultPhone:  user.Phone.String,
		RequesterType: string(session.Role),
		ErrorMsg:      errMsg,
	}

	allAssocs, _ := h.queries.ListAssociations(ctx)
	for _, a := range allAssocs {
		data.AllAssociations = append(data.AllAssociations, pages.AssociationOptionView{
			ID:      a.ID,
			Name:    a.Name,
			Address: a.Address,
			County:  a.County.String,
			City:    a.City.String,
		})
	}

	if session.Role == domain.RoleAsociatie {
		managed, _ := h.queries.ListUserAssociations(ctx, session.UserID)
		for _, a := range managed {
			opt := pages.AssociationOptionView{
				ID:      a.ID,
				Name:    a.Name,
				Address: a.Address,
				County:  a.County.String,
				City:    a.City.String,
			}
			data.ManagedAssociations = append(data.ManagedAssociations, opt)
		}
		if len(data.ManagedAssociations) > 0 {
			first := data.ManagedAssociations[0]
			data.DefaultAssociationID = first.ID
			data.DefaultAssociation = first.Name
			data.DefaultCounty = first.County
			data.DefaultCity = first.City
			data.DefaultAddress = first.Address
		}
	} else if session.Role == domain.RoleLocatar {
		if lp, err := h.queries.GetLocatarProfileByUserID(ctx, session.UserID); err == nil {
			data.DefaultCounty = lp.County.String
			data.DefaultCity = lp.City.String
			data.DefaultAddress = lp.Address.String
			if lp.AssociationID.Valid {
				data.DefaultAssociationID = lp.AssociationID.Int64
				if assocRow, aErr := h.queries.GetAssociationByID(ctx, lp.AssociationID.Int64); aErr == nil {
					data.DefaultAssociation = assocRow.Name
				}
			}
		}
	}
	return data
}

func (h *TicketHandler) NewRequestPage(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	if session.Role == domain.RolePrestator {
		http.Redirect(w, r, "/dashboard?error="+url.QueryEscape("Conturile de prestator depun oferte pe lucrările existente."), http.StatusSeeOther)
		return
	}

	reqType := strings.TrimSpace(r.URL.Query().Get("type"))
	data := h.buildNewRequestViewData(r.Context(), session, reqType, "")
	meta := h.pageMeta(r, "Publică Solicitare în Panou Lucrări", true)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.NewRequestPage(meta, data).Render(r.Context(), w)
}

func (h *TicketHandler) CreateRequestSubmit(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	if session.Role == domain.RolePrestator {
		http.Error(w, "Prestatorii nu pot publica solicitări noi.", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Formular invalid", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	now := time.Now().Unix()
	tipInterventie := strings.TrimSpace(r.FormValue("tip_interventie"))
	if tipInterventie == "" {
		tipInterventie = strings.TrimSpace(r.FormValue("request_type"))
	}
	if tipInterventie != "oferta" {
		tipInterventie = "avarie"
	}

	assocIDRaw, _ := strconv.ParseInt(strings.TrimSpace(r.FormValue("association_id")), 10, 64)

	// Enforce 96h SLA Gate on uncompleted accepted tickets
	uncompleted, _ := h.queries.ListUncompletedAcceptedReportsByUser(ctx, sqlc.ListUncompletedAcceptedReportsByUserParams{
		UserID:        session.UserID,
		AssociationID: sql.NullInt64{Int64: assocIDRaw, Valid: assocIDRaw > 0},
	})
	for _, oldRep := range uncompleted {
		if oldRep.AcceptedAt.Valid && (now-oldRep.AcceptedAt.Int64 > 96*3600) {
			if !oldRep.ExtendedUntil.Valid || oldRep.ExtendedUntil.Int64 < now {
				errMsg := fmt.Sprintf("Ai o lucrare acceptată de peste 96h (#%d — %s) nefinalizată. Te rugăm să o finalizezi, să îi prelungești termenul SLA sau să o repui pe liber înainte de a deschide o lucrare nouă.", oldRep.ID, oldRep.Title)
				data := h.buildNewRequestViewData(ctx, session, tipInterventie, errMsg)
				meta := h.pageMeta(r, "Publică Solicitare în Panou Lucrări", true)
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = pages.NewRequestPage(meta, data).Render(ctx, w)
				return
			}
		}
	}

	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	category := strings.TrimSpace(r.FormValue("category"))
	county := strings.TrimSpace(r.FormValue("county"))
	zone := strings.TrimSpace(r.FormValue("zone"))
	address := strings.TrimSpace(r.FormValue("address"))
	apartament := strings.TrimSpace(r.FormValue("apartament"))
	tipSpatiu := strings.TrimSpace(r.FormValue("tip_spatiu"))
	urgency := strings.TrimSpace(r.FormValue("urgency"))
	sursaFonduri := strings.TrimSpace(r.FormValue("sursa_fonduri"))
	clientName := strings.TrimSpace(r.FormValue("client_name"))
	clientPhone := strings.TrimSpace(r.FormValue("client_phone"))
	asociatieName := strings.TrimSpace(r.FormValue("asociatie"))

	if title == "" || description == "" || category == "" || county == "" || zone == "" {
		data := h.buildNewRequestViewData(ctx, session, tipInterventie, "Te rugăm să completezi toate câmpurile obligatorii (titlu, categorie, județ, zonă, descriere).")
		meta := h.pageMeta(r, "Publică Solicitare în Panou Lucrări", true)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = pages.NewRequestPage(meta, data).Render(ctx, w)
		return
	}

	if urgency == "" {
		urgency = "urgenta_imediata"
	}
	if tipSpatiu == "" {
		tipSpatiu = "spatiu_comun"
	}
	if sursaFonduri == "" {
		if session.Role == domain.RoleAsociatie {
			sursaFonduri = "fond_reparatii_bloc"
		} else {
			sursaFonduri = "locatar"
		}
	}

	var assocID sql.NullInt64
	if assocIDRaw > 0 {
		assocID = sql.NullInt64{Int64: assocIDRaw, Valid: true}
		if asocRow, err := h.queries.GetAssociationByID(ctx, assocIDRaw); err == nil && asociatieName == "" {
			asociatieName = asocRow.Name
		}
	}

	aprobatAdmin := int64(0)
	if session.Role == domain.RoleAsociatie {
		aprobatAdmin = 1
	}

	report, err := h.queries.CreateReport(ctx, sqlc.CreateReportParams{
		Title:                title,
		Description:          description,
		Category:             category,
		Status:               "open",
		TipInterventie:       tipInterventie,
		Urgency:              urgency,
		UserID:               session.UserID,
		AssociationID:        assocID,
		County:               county,
		Zone:                 zone,
		Address:              sql.NullString{String: address, Valid: address != ""},
		City:                 sql.NullString{String: zone, Valid: zone != ""},
		ClientName:           sql.NullString{String: clientName, Valid: clientName != ""},
		ClientPhone:          sql.NullString{String: clientPhone, Valid: clientPhone != ""},
		RequesterType:        string(session.Role),
		Asociatie:            sql.NullString{String: asociatieName, Valid: asociatieName != ""},
		Apartament:           sql.NullString{String: apartament, Valid: apartament != ""},
		TipSpatiu:            sql.NullString{String: tipSpatiu, Valid: tipSpatiu != ""},
		SursaFonduri:         sursaFonduri,
		AprobatAdministrator: aprobatAdmin,
		TrackingToken:        sql.NullString{String: generateTrackingToken(), Valid: true},
		CreatedAt:            now,
		UpdatedAt:            now,
	})
	if err != nil {
		data := h.buildNewRequestViewData(ctx, session, tipInterventie, "Eroare la salvarea solicitării în baza de date.")
		meta := h.pageMeta(r, "Publică Solicitare în Panou Lucrări", true)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_ = pages.NewRequestPage(meta, data).Render(ctx, w)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/requests/%d", report.ID), http.StatusSeeOther)
}

func (h *TicketHandler) canAccessReport(ctx context.Context, report sqlc.Report, session domain.SessionView, token string) (isOwner, isManagingAdmin, isBeneficiary, isAssignedProvider bool, allowed bool) {
	isOwner = session.IsLoggedIn() && report.UserID == session.UserID
	isManagingAdmin = session.IsLoggedIn() && h.isManagingAdminForReport(ctx, session.UserID, report)
	validToken := token != "" && report.TrackingToken.Valid && report.TrackingToken.String != "" && token == report.TrackingToken.String
	isBeneficiary = isOwner || isManagingAdmin || validToken
	isAssignedProvider = session.IsLoggedIn() && report.ClaimedByProviderID.Valid && report.ClaimedByProviderID.Int64 == session.UserID

	if report.Status == "in_progress" || report.Status == "completed" {
		allowed = isBeneficiary || isAssignedProvider
		return
	}
	allowed = session.IsLoggedIn() || validToken
	return
}

func (h *TicketHandler) buildRequestDetailViewData(ctx context.Context, report sqlc.Report, session domain.SessionView, token, flashMsg, errMsg string) pages.RequestDetailViewData {
	isOwner, isManagingAdmin, isBeneficiary, isAssignedProvider, _ := h.canAccessReport(ctx, report, session, token)
	isProvider := session.Role == domain.RolePrestator

	var providerSubscribed bool
	var providerMonthlyMods int64
	var providerRemainingMods int64
	if session.IsLoggedIn() {
		if u, err := h.queries.GetUserByID(ctx, session.UserID); err == nil {
			providerSubscribed = u.IsSubscribed == 1
		}
		if isProvider {
			if prof, err := h.queries.GetProviderProfileByUserID(ctx, session.UserID); err == nil {
				providerMonthlyMods = normalizeMonthlyMods(prof, time.Now())
			}
			providerRemainingMods = 3 - providerMonthlyMods
			if providerRemainingMods < 0 {
				providerRemainingMods = 0
			}
		}
	}

	// Anti-IDOR Phone Masking: unmasked ONLY for beneficiary (owner/managing admin) or assigned provider with active subscription
	phoneUnmasked := isBeneficiary || (isAssignedProvider && providerSubscribed)
	displayPhone := maskPhone(report.ClientPhone.String)
	if phoneUnmasked && strings.TrimSpace(report.ClientPhone.String) != "" {
		displayPhone = report.ClientPhone.String
	}

	offerRows, _ := h.queries.ListOffersByRequestWithProvider(ctx, report.ID)
	var offerCards []pages.OfferCardView
	var existingPendingOffer *pages.OfferCardView

	for _, off := range offerRows {
		if off.Status == "withdrawn" {
			continue
		}
		priceBani := off.PriceBani
		if priceBani == 0 && off.Price > 0 {
			priceBani = floatRONToBani(off.Price)
		}
		matBani := off.MaterialsPriceBani
		if matBani == 0 && off.MaterialsPrice.Valid && off.MaterialsPrice.Float64 > 0 {
			matBani = floatRONToBani(off.MaterialsPrice.Float64)
		}
		totalBani := priceBani + matBani

		provPhone := maskPhone(off.ProviderPhone)
		if isBeneficiary || off.PlumberID == session.UserID {
			if strings.TrimSpace(off.ProviderPhone) != "" {
				provPhone = off.ProviderPhone
			}
		}

		card := pages.OfferCardView{
			ID:                 off.ID,
			RequestID:          off.RequestID,
			PlumberID:          off.PlumberID,
			ProviderName:       off.ProviderName,
			CompanyName:        off.CompanyName,
			CompanyCUI:         off.CompanyCui,
			ProviderPhone:      provPhone,
			ProviderRating:     fmt.Sprintf("%.1f", off.ProviderRating),
			ProviderReviews:    off.ProviderReviews,
			ProviderVerified:   off.ProviderVerified == 1,
			PriceBani:          priceBani,
			MaterialsPriceBani: matBani,
			TotalBani:          totalBani,
			PriceFormatted:     formatBaniRON(priceBani),
			MaterialsFormatted: formatBaniRON(matBani),
			TotalFormatted:     formatBaniRON(totalBani),
			EstimatedTime:      off.EstimatedTime,
			Message:            off.Message.String,
			RequiresInspection: off.RequiresInspection == 1,
			Status:             off.Status,
			CreatedAtFormatted: formatTimestamp(off.CreatedAt),
			IsOwnOffer:         session.IsLoggedIn() && off.PlumberID == session.UserID,
		}
		if card.IsOwnOffer && card.Status == "pending" {
			copyCard := card
			existingPendingOffer = &copyCard
		}
		offerCards = append(offerCards, card)
	}

	followerRows, _ := h.queries.ListTicketFollowers(ctx, report.ID)
	var followers []pages.TicketFollowerView
	for _, f := range followerRows {
		followers = append(followers, pages.TicketFollowerView{
			Apartament:         f.Apartament,
			MaskedPhone:        maskPhone(f.ClientPhone),
			CreatedAtFormatted: formatTimestamp(f.CreatedAt),
		})
	}

	viewRows, _ := h.queries.ListTicketViewsWithUsers(ctx, report.ID)
	var auditViews []pages.TicketAuditView
	for _, v := range viewRows {
		name := v.UserName
		if name == "" {
			name = "Utilizator #" + strconv.FormatInt(v.UserID, 10)
		}
		auditViews = append(auditViews, pages.TicketAuditView{
			ViewerName:        name,
			ViewerRole:        v.UserRole,
			ViewedAtFormatted: formatTimestamp(v.ViewedAt),
		})
	}

	var hasReview bool
	var reviewRating int
	var reviewComment string
	if rev, err := h.queries.GetReviewByRequestID(ctx, report.ID); err == nil {
		hasReview = true
		reviewRating = int(math.Round(rev.Rating))
		reviewComment = rev.Comment.String
	}

	phase, phaseLabel := statusPhase(report.Status)
	now := time.Now().Unix()
	canTacit := report.Status == "in_progress" && report.ReportedCompletedAt.Valid && (now-report.ReportedCompletedAt.Int64 >= 72*3600)

	manoperaFinalBani := report.CostFinalManopera.Int64
	materialeFinalBani := report.CostFinalMateriale.Int64

	return pages.RequestDetailViewData{
		ID:                           report.ID,
		Title:                        report.Title,
		Description:                  report.Description,
		Category:                     report.Category,
		Status:                       report.Status,
		Phase:                        phase,
		PhaseLabel:                   phaseLabel,
		TipInterventie:               report.TipInterventie,
		Urgency:                      report.Urgency,
		UrgencyLabel:                 urgencyLabel(report.Urgency),
		County:                       report.County,
		Zone:                         report.Zone,
		Address:                      report.Address.String,
		City:                         report.City.String,
		Asociatie:                    report.Asociatie.String,
		Apartament:                   report.Apartament.String,
		TipSpatiu:                    report.TipSpatiu.String,
		ClientName:                   report.ClientName.String,
		DisplayPhone:                 displayPhone,
		PhoneUnmasked:                phoneUnmasked,
		RequesterType:                report.RequesterType,
		SursaFonduri:                 report.SursaFonduri,
		AprobatAdministrator:         report.AprobatAdministrator == 1,
		InvoiceNumber:                report.InvoiceNumber.String,
		ConfirmationsCount:           report.ConfirmationsCount,
		IsMajorEmergency:             report.IsMajorEmergency == 1,
		CreatedAtFormatted:           formatTimestamp(report.CreatedAt),
		AcceptedAtFormatted:          formatTimestamp(report.AcceptedAt.Int64),
		CompletedAtFormatted:         formatTimestamp(report.CompletedAt.Int64),
		ExtendedUntilFormatted:       formatTimestamp(report.ExtendedUntil.Int64),
		ExtensionReason:              report.ExtensionReason.String,
		RaportLucrariEfectuate:       report.RaportLucrariEfectuate.String,
		RaportMateriale:              report.RaportMateriale.String,
		CostFinalManoperaFormatted:   formatBaniRON(manoperaFinalBani),
		CostFinalMaterialeFormatted:  formatBaniRON(materialeFinalBani),
		CostFinalTotalFormatted:      formatBaniRON(manoperaFinalBani + materialeFinalBani),
		RaportObservatii:             report.RaportObservatii.String,
		ReportedCompletedAtFormatted: formatTimestamp(report.ReportedCompletedAt.Int64),
		CanTacitFinalize:             canTacit,
		TrackingToken:                report.TrackingToken.String,
		IsOwner:                      isOwner,
		IsManagingAssocAdmin:         isManagingAdmin,
		IsBeneficiary:                isBeneficiary,
		IsAssignedProvider:           isAssignedProvider,
		IsProvider:                   isProvider,
		ProviderSubscribed:           providerSubscribed,
		ProviderMonthlyMods:          providerMonthlyMods,
		ProviderRemainingMods:        providerRemainingMods,
		ExistingPendingOffer:         existingPendingOffer,
		Offers:                       offerCards,
		Followers:                    followers,
		AuditViews:                   auditViews,
		HasReview:                    hasReview,
		ReviewRating:                 reviewRating,
		ReviewComment:                reviewComment,
		FlashMsg:                     flashMsg,
		ErrorMsg:                     errMsg,
	}
}

func (h *TicketHandler) respondTicketDetail(w http.ResponseWriter, r *http.Request, reportID int64, flashMsg, errMsg string, errStatus int) {
	ctx := r.Context()
	session := auth.SessionFromContext(ctx)
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		token = strings.TrimSpace(r.FormValue("token"))
	}

	report, err := h.queries.GetReportByID(ctx, reportID)
	if err != nil {
		http.Error(w, "Lucrarea nu a fost găsită.", http.StatusNotFound)
		return
	}

	detailData := h.buildRequestDetailViewData(ctx, report, session, token, flashMsg, errMsg)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if isHTMX(r) {
		if errStatus == http.StatusForbidden || errStatus == http.StatusUnauthorized {
			w.WriteHeader(errStatus)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_ = pages.RequestDetailPartial(detailData).Render(ctx, w)
		return
	}

	if errMsg != "" && errStatus >= 400 {
		w.WriteHeader(errStatus)
		meta := h.pageMeta(r, fmt.Sprintf("Lucrare #%d — %s", report.ID, report.Title), true)
		_ = pages.RequestDetailPage(meta, detailData).Render(ctx, w)
		return
	}

	targetURL := fmt.Sprintf("/requests/%d", report.ID)
	var qParams []string
	if token != "" {
		qParams = append(qParams, "token="+url.QueryEscape(token))
	}
	if flashMsg != "" {
		qParams = append(qParams, "flash="+url.QueryEscape(flashMsg))
	}
	if errMsg != "" {
		qParams = append(qParams, "error="+url.QueryEscape(errMsg))
	}
	if len(qParams) > 0 {
		targetURL += "?" + strings.Join(qParams, "&")
	}
	http.Redirect(w, r, targetURL, http.StatusSeeOther)
}

func (h *TicketHandler) RequestDetailPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session := auth.SessionFromContext(ctx)
	token := strings.TrimSpace(r.URL.Query().Get("token"))

	if session.IsSuperAdmin || session.Role == domain.RolePlatformAdmin {
		http.Redirect(w, r, "/platform-admin", http.StatusSeeOther)
		return
	}
	if !session.IsLoggedIn() && token == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || reportID <= 0 {
		http.Error(w, "ID tichet invalid", http.StatusBadRequest)
		return
	}

	report, err := h.queries.GetReportByID(ctx, reportID)
	if err != nil {
		http.Error(w, "Lucrarea nu a fost găsită", http.StatusNotFound)
		return
	}

	isOwner, isManagingAdmin, _, _, allowed := h.canAccessReport(ctx, report, session, token)
	if !allowed {
		http.Error(w, "Acces restricționat: această lucrare este în execuție sau finalizată cu un alt prestator.", http.StatusForbidden)
		return
	}

	now := time.Now().Unix()
	if session.IsLoggedIn() {
		_ = h.queries.UpsertTicketView(ctx, sqlc.UpsertTicketViewParams{
			ReportID: report.ID,
			UserID:   session.UserID,
			ViewedAt: now,
		})
		if isOwner && report.IsRead == 0 {
			_ = h.queries.MarkReportReadByOwner(ctx, sqlc.MarkReportReadByOwnerParams{
				ReadAt:    sql.NullInt64{Int64: now, Valid: true},
				UpdatedAt: now,
				ID:        report.ID,
			})
			report.IsRead = 1
		}
		if isManagingAdmin && report.IsReadByAdmin == 0 {
			_ = h.queries.MarkReportReadByAdmin(ctx, sqlc.MarkReportReadByAdminParams{
				UpdatedAt: now,
				ID:        report.ID,
			})
			report.IsReadByAdmin = 1
		}
	}

	detailData := h.buildRequestDetailViewData(
		ctx,
		report,
		session,
		token,
		r.URL.Query().Get("flash"),
		r.URL.Query().Get("error"),
	)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if isHTMX(r) {
		_ = pages.RequestDetailPartial(detailData).Render(ctx, w)
		return
	}

	meta := h.pageMeta(r, fmt.Sprintf("Lucrare #%d — %s", report.ID, report.Title), true)
	_ = pages.RequestDetailPage(meta, detailData).Render(ctx, w)
}

func (h *TicketHandler) SubmitOffer(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || reportID <= 0 {
		http.Error(w, "ID tichet invalid", http.StatusBadRequest)
		return
	}
	if session.Role != domain.RolePrestator {
		h.respondTicketDetail(w, r, reportID, "", "Doar conturile de Membru Profesionist (Prestator) pot trimite oferte.", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.respondTicketDetail(w, r, reportID, "", "Date formular invalide.", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	user, err := h.queries.GetUserByID(ctx, session.UserID)
	if err != nil || user.IsSubscribed == 0 {
		h.respondTicketDetail(w, r, reportID, "", "Este necesar un abonament activ de Membru Profesionist (15 RON / lună) pentru a depune oferte.", http.StatusForbidden)
		return
	}

	report, err := h.queries.GetReportByID(ctx, reportID)
	if err != nil {
		http.Error(w, "Lucrare inexistentă", http.StatusNotFound)
		return
	}
	if report.Status != "open" && report.Status != "pending_offers" {
		h.respondTicketDetail(w, r, reportID, "", "Această lucrare nu mai primește oferte noi.", http.StatusConflict)
		return
	}

	requiresInspection := int64(0)
	if r.FormValue("requires_inspection") == "1" {
		requiresInspection = 1
	}

	rawPrice := r.FormValue("price_ron")
	if rawPrice == "" {
		rawPrice = r.FormValue("price")
	}
	rawMat := r.FormValue("materials_ron")
	if rawMat == "" {
		rawMat = r.FormValue("materials_price")
	}
	priceBani, pErr := parseRONToBani(rawPrice)
	matBani, mErr := parseRONToBani(rawMat)
	if pErr != nil || mErr != nil {
		h.respondTicketDetail(w, r, reportID, "", "Sumele introduse pentru manoperă sau materiale sunt invalide.", http.StatusBadRequest)
		return
	}
	if requiresInspection == 0 && priceBani <= 0 {
		h.respondTicketDetail(w, r, reportID, "", "Introdu costul manoperei sau bifează opțiunea de constatare la fața locului.", http.StatusBadRequest)
		return
	}

	estimatedTime := strings.TrimSpace(r.FormValue("estimated_time"))
	message := strings.TrimSpace(r.FormValue("message"))
	if estimatedTime == "" || message == "" {
		h.respondTicketDetail(w, r, reportID, "", "Timpul estimat de intervenție și mesajul tehnic sunt obligatorii.", http.StatusBadRequest)
		return
	}

	now := time.Now()
	nowUnix := now.Unix()
	priceFloat := float64(priceBani) / 100.0
	matFloat := float64(matBani) / 100.0

	existingOffer, existingErr := h.queries.GetPendingOfferByRequestAndProvider(ctx, sqlc.GetPendingOfferByRequestAndProviderParams{
		RequestID: reportID,
		PlumberID: session.UserID,
	})

	if existingErr == nil {
		// Updating an existing pending offer -> enforce max 3 modifications per month
		prof, profErr := h.queries.GetProviderProfileByUserID(ctx, session.UserID)
		if profErr != nil {
			h.respondTicketDetail(w, r, reportID, "", "Profilul de prestator nu a putut fi verificat.", http.StatusInternalServerError)
			return
		}
		usedMods := normalizeMonthlyMods(prof, now)
		if usedMods >= 3 {
			h.respondTicketDetail(w, r, reportID, "", "Ai atins plafonul maxim de 3 modificări lunare ale ofertelor. Poți retrage gratuit oferta dacă nu mai poți onora lucrarea.", http.StatusUnprocessableEntity)
			return
		}

		_, err = h.queries.UpdateOfferDetails(ctx, sqlc.UpdateOfferDetailsParams{
			Price:              priceFloat,
			MaterialsPrice:     sql.NullFloat64{Float64: matFloat, Valid: true},
			PriceBani:          priceBani,
			MaterialsPriceBani: matBani,
			Message:            sql.NullString{String: message, Valid: true},
			EstimatedTime:      estimatedTime,
			RequiresInspection: requiresInspection,
			UpdatedAt:          nowUnix,
			ID:                 existingOffer.ID,
			PlumberID:          session.UserID,
		})
		if err != nil {
			h.respondTicketDetail(w, r, reportID, "", "Eroare la actualizarea ofertei.", http.StatusInternalServerError)
			return
		}

		_ = h.queries.UpdateProviderModifications(ctx, sqlc.UpdateProviderModificationsParams{
			MonthlyModifications:  usedMods + 1,
			LastModificationReset: sql.NullInt64{Int64: nowUnix, Valid: true},
			UpdatedAt:             nowUnix,
			UserID:                session.UserID,
		})

		h.respondTicketDetail(w, r, reportID, fmt.Sprintf("Oferta a fost actualizată cu succes (%d / 3 modificări lunare utilizate).", usedMods+1), "", http.StatusOK)
		return
	}

	// Initial offer submission -> 100% free, does NOT consume monthly modification quota
	_, err = h.queries.CreateOffer(ctx, sqlc.CreateOfferParams{
		RequestID:          reportID,
		PlumberID:          session.UserID,
		Price:              priceFloat,
		MaterialsPrice:     sql.NullFloat64{Float64: matFloat, Valid: true},
		PriceBani:          priceBani,
		MaterialsPriceBani: matBani,
		Message:            sql.NullString{String: message, Valid: true},
		EstimatedTime:      estimatedTime,
		RequiresInspection: requiresInspection,
		CreatedAt:          nowUnix,
		UpdatedAt:          nowUnix,
	})
	if err != nil {
		h.respondTicketDetail(w, r, reportID, "", "Nu s-a putut înregistra oferta.", http.StatusInternalServerError)
		return
	}

	if report.Status == "open" {
		_, _ = h.queries.UpdateReportStatus(ctx, sqlc.UpdateReportStatusParams{
			Status:      "pending_offers",
			CompletedAt: sql.NullInt64{},
			UpdatedAt:   nowUnix,
			ID:          reportID,
		})
	}

	if h.email != nil {
		if ownerUser, uErr := h.queries.GetUserByID(ctx, report.UserID); uErr == nil {
			_ = h.email.SendTicketAlert(ctx, ownerUser.Email, fmt.Sprintf("Ofertă nouă pe lucrarea #%d", report.ID), fmt.Sprintf("Ai primit o ofertă nouă pentru solicitarea „%s”.", report.Title))
		}
	}

	h.respondTicketDetail(w, r, reportID, "Oferta inițială a fost transmisă gratuit către beneficiar!", "", http.StatusOK)
}

func (h *TicketHandler) WithdrawOffer(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	offerID, oErr := strconv.ParseInt(chi.URLParam(r, "offerID"), 10, 64)
	if err != nil || oErr != nil || reportID <= 0 || offerID <= 0 {
		http.Error(w, "Parametri invalizi", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	offer, err := h.queries.GetOfferByID(ctx, offerID)
	if err != nil || offer.RequestID != reportID {
		h.respondTicketDetail(w, r, reportID, "", "Oferta nu a fost găsită.", http.StatusNotFound)
		return
	}
	if offer.PlumberID != session.UserID {
		h.respondTicketDetail(w, r, reportID, "", "Poți retrage doar propria ofertă.", http.StatusForbidden)
		return
	}
	if offer.Status != "pending" {
		h.respondTicketDetail(w, r, reportID, "", "Doar ofertele aflate în așteptare pot fi retrase direct.", http.StatusConflict)
		return
	}

	now := time.Now().Unix()
	_, _ = h.queries.UpdateOfferStatus(ctx, sqlc.UpdateOfferStatusParams{
		Status:    "withdrawn",
		UpdatedAt: now,
		ID:        offer.ID,
	})

	remaining, _ := h.queries.CountPendingOffersForRequest(ctx, reportID)
	if remaining == 0 {
		_, _ = h.queries.UpdateReportStatus(ctx, sqlc.UpdateReportStatusParams{
			Status:      "open",
			CompletedAt: sql.NullInt64{},
			UpdatedAt:   now,
			ID:          reportID,
		})
	}

	h.respondTicketDetail(w, r, reportID, "Oferta a fost retrasă gratuit (fără consum din plafonul lunar).", "", http.StatusOK)
}

func (h *TicketHandler) AcceptOffer(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	offerID, oErr := strconv.ParseInt(chi.URLParam(r, "offerID"), 10, 64)
	if err != nil || oErr != nil || reportID <= 0 || offerID <= 0 {
		http.Error(w, "Parametri invalizi", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	report, err := h.queries.GetReportByID(ctx, reportID)
	if err != nil {
		http.Error(w, "Lucrare inexistentă", http.StatusNotFound)
		return
	}

	isOwner := report.UserID == session.UserID
	isManagingAdmin := h.isManagingAdminForReport(ctx, session.UserID, report)
	if !isOwner && !isManagingAdmin {
		h.respondTicketDetail(w, r, reportID, "", "Doar proprietarul solicitării sau administratorul asociației poate accepta o ofertă.", http.StatusForbidden)
		return
	}
	if report.Status != "open" && report.Status != "pending_offers" {
		h.respondTicketDetail(w, r, reportID, "", "Solicitarea are deja o ofertă acceptată.", http.StatusConflict)
		return
	}

	offer, err := h.queries.GetOfferByID(ctx, offerID)
	if err != nil || offer.RequestID != reportID || offer.Status != "pending" {
		h.respondTicketDetail(w, r, reportID, "", "Oferta selectată nu mai este disponibilă.", http.StatusBadRequest)
		return
	}

	now := time.Now().Unix()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		h.respondTicketDetail(w, r, reportID, "", "Eroare la inițierea tranzacției.", http.StatusInternalServerError)
		return
	}
	defer func() {
		_ = tx.Rollback()
	}()

	qtx := h.queries.WithTx(tx)
	if _, err := qtx.UpdateOfferStatus(ctx, sqlc.UpdateOfferStatusParams{
		Status:    "accepted",
		UpdatedAt: now,
		ID:        offer.ID,
	}); err != nil {
		h.respondTicketDetail(w, r, reportID, "", "Eroare la actualizarea ofertei.", http.StatusInternalServerError)
		return
	}

	if err := qtx.RejectOtherOffersForRequest(ctx, sqlc.RejectOtherOffersForRequestParams{
		UpdatedAt: now,
		RequestID: reportID,
		ID:        offer.ID,
	}); err != nil {
		h.respondTicketDetail(w, r, reportID, "", "Eroare la actualizarea celorlalte oferte.", http.StatusInternalServerError)
		return
	}

	if _, err := qtx.AcceptReportOffer(ctx, sqlc.AcceptReportOfferParams{
		AcceptedOfferID:     sql.NullInt64{Int64: offer.ID, Valid: true},
		ClaimedByProviderID: sql.NullInt64{Int64: offer.PlumberID, Valid: true},
		AcceptedAt:          sql.NullInt64{Int64: now, Valid: true},
		UpdatedAt:           now,
		ID:                  reportID,
	}); err != nil {
		h.respondTicketDetail(w, r, reportID, "", "Eroare la actualizarea stării lucrării.", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		h.respondTicketDetail(w, r, reportID, "", "Tranzacția nu a putut fi finalizată.", http.StatusInternalServerError)
		return
	}

	if h.email != nil {
		if provUser, uErr := h.queries.GetUserByID(ctx, offer.PlumberID); uErr == nil {
			_ = h.email.SendTicketAlert(ctx, provUser.Email, fmt.Sprintf("Oferta ta a fost acceptată pe lucrarea #%d", report.ID), fmt.Sprintf("Beneficiarul a acceptat oferta ta pentru lucrarea „%s”.", report.Title))
		}
	}

	h.respondTicketDetail(w, r, reportID, "Oferta a fost acceptată! Lucrarea a trecut în Faza 2 (Ofertă Acceptată).", "", http.StatusOK)
}

func (h *TicketHandler) RejectOffer(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	offerID, oErr := strconv.ParseInt(chi.URLParam(r, "offerID"), 10, 64)
	if err != nil || oErr != nil || reportID <= 0 || offerID <= 0 {
		http.Error(w, "Parametri invalizi", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	report, err := h.queries.GetReportByID(ctx, reportID)
	if err != nil {
		http.Error(w, "Lucrare inexistentă", http.StatusNotFound)
		return
	}
	if report.UserID != session.UserID && !h.isManagingAdminForReport(ctx, session.UserID, report) {
		h.respondTicketDetail(w, r, reportID, "", "Nu ai permisiunea de a respinge oferte pe această lucrare.", http.StatusForbidden)
		return
	}

	offer, err := h.queries.GetOfferByID(ctx, offerID)
	if err != nil || offer.RequestID != reportID {
		h.respondTicketDetail(w, r, reportID, "", "Oferta nu a fost găsită.", http.StatusNotFound)
		return
	}

	now := time.Now().Unix()
	_, _ = h.queries.UpdateOfferStatus(ctx, sqlc.UpdateOfferStatusParams{
		Status:    "rejected",
		UpdatedAt: now,
		ID:        offer.ID,
	})

	remaining, _ := h.queries.CountPendingOffersForRequest(ctx, reportID)
	if remaining == 0 && report.Status == "pending_offers" {
		_, _ = h.queries.UpdateReportStatus(ctx, sqlc.UpdateReportStatusParams{
			Status:      "open",
			CompletedAt: sql.NullInt64{},
			UpdatedAt:   now,
			ID:          reportID,
		})
	}

	h.respondTicketDetail(w, r, reportID, "Oferta a fost respinsă.", "", http.StatusOK)
}

func (h *TicketHandler) UpdateFundsApproval(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || reportID <= 0 {
		http.Error(w, "ID tichet invalid", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.respondTicketDetail(w, r, reportID, "", "Formular invalid.", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	report, err := h.queries.GetReportByID(ctx, reportID)
	if err != nil {
		http.Error(w, "Lucrare inexistentă", http.StatusNotFound)
		return
	}

	if report.UserID != session.UserID && !h.isManagingAdminForReport(ctx, session.UserID, report) {
		h.respondTicketDetail(w, r, reportID, "", "Doar administratorul asociației sau proprietarul poate actualiza regimul fondurilor.", http.StatusForbidden)
		return
	}

	sursaFonduri := strings.TrimSpace(r.FormValue("sursa_fonduri"))
	if sursaFonduri == "" {
		sursaFonduri = "locatar"
	}

	aprobat := int64(0)
	if r.FormValue("aprobat_administrator") == "1" {
		aprobat = 1
	}
	invoiceNum := strings.TrimSpace(r.FormValue("invoice_number"))

	_, _ = h.queries.UpdateReportFundsAndApproval(ctx, sqlc.UpdateReportFundsAndApprovalParams{
		SursaFonduri:         sursaFonduri,
		AprobatAdministrator: aprobat,
		InvoiceNumber:        sql.NullString{String: invoiceNum, Valid: invoiceNum != ""},
		UpdatedAt:            time.Now().Unix(),
		ID:                   reportID,
	})

	h.respondTicketDetail(w, r, reportID, "Regimul financiar și aprobarea administrativă au fost salvate.", "", http.StatusOK)
}

func (h *TicketHandler) StartWork(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ctx := r.Context()
	session := auth.SessionFromContext(ctx)
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		token = strings.TrimSpace(r.FormValue("token"))
	}

	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || reportID <= 0 {
		http.Error(w, "ID tichet invalid", http.StatusBadRequest)
		return
	}

	report, err := h.queries.GetReportByID(ctx, reportID)
	if err != nil {
		http.Error(w, "Lucrare inexistentă", http.StatusNotFound)
		return
	}

	// GOLDEN RULE: STRICTLY the beneficiary (owner, managing association admin, or holder of valid QR tracking_token)
	// can confirm work start and transition the ticket to Phase 3 (in_progress).
	// Providers are strictly prohibited from calling StartWork.
	_, _, isBeneficiary, isAssignedProvider, _ := h.canAccessReport(ctx, report, session, token)
	if isAssignedProvider && !isBeneficiary {
		h.respondTicketDetail(w, r, reportID, "", "Regulă de Securitate: Prestatorul NU poate porni singur execuția lucrării. Doar beneficiarul (proprietar / asociație / cod QR) poate confirma începerea lucrării (Faza 3 — În Lucru).", http.StatusForbidden)
		return
	}
	if !isBeneficiary {
		h.respondTicketDetail(w, r, reportID, "", "Doar beneficiarul lucrării poate confirma începerea execuției (Faza 3 — În Lucru).", http.StatusForbidden)
		return
	}

	if report.Status != "accepted" && report.Status != "claimed" {
		h.respondTicketDetail(w, r, reportID, "", "Lucrarea trebuie să aibă o ofertă acceptată (Faza 2) înainte de începerea execuției.", http.StatusConflict)
		return
	}

	_, _ = h.queries.UpdateReportStatus(ctx, sqlc.UpdateReportStatusParams{
		Status:      "in_progress",
		CompletedAt: sql.NullInt64{},
		UpdatedAt:   time.Now().Unix(),
		ID:          reportID,
	})

	h.respondTicketDetail(w, r, reportID, "Începerea lucrării a fost confirmată! Tichetul este acum în Faza 3 (În Lucru) și este vizibil exclusiv părților implicate.", "", http.StatusOK)
}

func (h *TicketHandler) SubmitProviderReport(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || reportID <= 0 {
		http.Error(w, "ID tichet invalid", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.respondTicketDetail(w, r, reportID, "", "Formular invalid.", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	report, err := h.queries.GetReportByID(ctx, reportID)
	if err != nil {
		http.Error(w, "Lucrare inexistentă", http.StatusNotFound)
		return
	}

	if !report.ClaimedByProviderID.Valid || report.ClaimedByProviderID.Int64 != session.UserID {
		h.respondTicketDetail(w, r, reportID, "", "Doar prestatorul desemnat pe această lucrare poate completa devizul și raportul tehnic.", http.StatusForbidden)
		return
	}
	if report.Status != "in_progress" && report.Status != "accepted" && report.Status != "claimed" {
		h.respondTicketDetail(w, r, reportID, "", "Devizul tehnic poate fi transmis doar pentru o lucrare activă.", http.StatusConflict)
		return
	}

	lucrari := strings.TrimSpace(r.FormValue("raport_lucrari_efectuate"))
	if lucrari == "" {
		lucrari = strings.TrimSpace(r.FormValue("raport_lucrari"))
	}
	materiale := strings.TrimSpace(r.FormValue("raport_materiale"))
	observatii := strings.TrimSpace(r.FormValue("raport_observatii"))
	rawManopera := r.FormValue("cost_final_manopera")
	if rawManopera == "" {
		rawManopera = r.FormValue("cost_manopera")
	}
	rawMateriale := r.FormValue("cost_final_materiale")
	if rawMateriale == "" {
		rawMateriale = r.FormValue("cost_materiale")
	}
	manoperaBani, mErr := parseRONToBani(rawManopera)
	materialeBani, matErr := parseRONToBani(rawMateriale)

	if lucrari == "" || mErr != nil || matErr != nil {
		h.respondTicketDetail(w, r, reportID, "", "Descrierea lucrărilor efectuate și costurile finale sunt obligatorii.", http.StatusBadRequest)
		return
	}

	now := time.Now().Unix()
	_, err = h.queries.SubmitProviderReport(ctx, sqlc.SubmitProviderReportParams{
		RaportLucrariEfectuate: sql.NullString{String: lucrari, Valid: true},
		RaportMateriale:        sql.NullString{String: materiale, Valid: materiale != ""},
		CostFinalManopera:      sql.NullInt64{Int64: manoperaBani, Valid: true},
		CostFinalMateriale:     sql.NullInt64{Int64: materialeBani, Valid: true},
		RaportObservatii:       sql.NullString{String: observatii, Valid: observatii != ""},
		ReportedCompletedAt:    sql.NullInt64{Int64: now, Valid: true},
		UpdatedAt:              now,
		ID:                     reportID,
		ClaimedByProviderID:    sql.NullInt64{Int64: session.UserID, Valid: true},
	})
	if err != nil {
		h.respondTicketDetail(w, r, reportID, "", "Eroare la salvarea raportului tehnic.", http.StatusInternalServerError)
		return
	}

	h.respondTicketDetail(w, r, reportID, "Raportul tehnic și devizul final au fost înregistrate. Termenul de 72h pentru recepție a început.", "", http.StatusOK)
}

func (h *TicketHandler) CompleteTicket(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ctx := r.Context()
	session := auth.SessionFromContext(ctx)
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		token = strings.TrimSpace(r.FormValue("token"))
	}

	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || reportID <= 0 {
		http.Error(w, "ID tichet invalid", http.StatusBadRequest)
		return
	}

	report, err := h.queries.GetReportByID(ctx, reportID)
	if err != nil {
		http.Error(w, "Lucrare inexistentă", http.StatusNotFound)
		return
	}

	_, _, isBeneficiary, isAssignedProvider, _ := h.canAccessReport(ctx, report, session, token)
	if !isBeneficiary || (isAssignedProvider && !isBeneficiary) {
		h.respondTicketDetail(w, r, reportID, "", "Doar beneficiarul lucrării poate recepționa și finaliza tichetul.", http.StatusForbidden)
		return
	}
	if report.Status == "completed" {
		h.respondTicketDetail(w, r, reportID, "Lucrarea este deja finalizată.", "", http.StatusOK)
		return
	}
	if report.Status != "in_progress" && report.Status != "accepted" && report.Status != "claimed" {
		h.respondTicketDetail(w, r, reportID, "", "Lucrarea trebuie să aibă un prestator desemnat pentru a fi recepționată.", http.StatusConflict)
		return
	}

	rating, rErr := strconv.ParseInt(strings.TrimSpace(r.FormValue("rating")), 10, 64)
	if rErr != nil || rating < 1 || rating > 5 {
		h.respondTicketDetail(w, r, reportID, "", "Acordarea unui calificativ (1–5 stele) este obligatorie la recepția lucrării.", http.StatusBadRequest)
		return
	}
	comment := strings.TrimSpace(r.FormValue("comment"))
	now := time.Now().Unix()

	plumberID := report.ClaimedByProviderID.Int64
	if plumberID > 0 {
		if _, revErr := h.queries.GetReviewByRequestID(ctx, reportID); revErr != nil {
			_, _ = h.queries.CreateReview(ctx, sqlc.CreateReviewParams{
				PlumberID: plumberID,
				ClientID:  report.UserID,
				RequestID: reportID,
				Rating:    float64(rating),
				Comment:   sql.NullString{String: comment, Valid: comment != ""},
				CreatedAt: now,
			})
			h.recalculateProviderStats(ctx, plumberID, now)
		}
	}

	_, _ = h.queries.UpdateReportStatus(ctx, sqlc.UpdateReportStatusParams{
		Status:      "completed",
		CompletedAt: sql.NullInt64{Int64: now, Valid: true},
		UpdatedAt:   now,
		ID:          reportID,
	})

	h.respondTicketDetail(w, r, reportID, "Lucrarea a fost recepționată și finalizată (Faza 4)! Procesul-Verbal și Certificatul de Garanție sunt disponibile pentru tipărire.", "", http.StatusOK)
}

func (h *TicketHandler) TacitCompleteTicket(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || reportID <= 0 {
		http.Error(w, "ID tichet invalid", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	report, err := h.queries.GetReportByID(ctx, reportID)
	if err != nil {
		http.Error(w, "Lucrare inexistentă", http.StatusNotFound)
		return
	}

	if !report.ClaimedByProviderID.Valid || report.ClaimedByProviderID.Int64 != session.UserID {
		h.respondTicketDetail(w, r, reportID, "", "Doar prestatorul desemnat poate solicita închiderea tacită.", http.StatusForbidden)
		return
	}

	now := time.Now().Unix()
	if !report.ReportedCompletedAt.Valid || (now-report.ReportedCompletedAt.Int64 < 72*3600) {
		h.respondTicketDetail(w, r, reportID, "", "Închiderea prin recepție tacită este disponibilă doar după expirarea celor 72 de ore de la depunerea raportului tehnic.", http.StatusConflict)
		return
	}

	if _, revErr := h.queries.GetReviewByRequestID(ctx, reportID); revErr != nil {
		_, _ = h.queries.CreateReview(ctx, sqlc.CreateReviewParams{
			PlumberID: session.UserID,
			ClientID:  report.UserID,
			RequestID: reportID,
			Rating:    5.0,
			Comment:   sql.NullString{String: "Lucrare recepționată tacit după 72h de la depunerea raportului tehnic final.", Valid: true},
			CreatedAt: now,
		})
		h.recalculateProviderStats(ctx, session.UserID, now)
	}

	_, _ = h.queries.UpdateReportStatus(ctx, sqlc.UpdateReportStatusParams{
		Status:      "completed",
		CompletedAt: sql.NullInt64{Int64: now, Valid: true},
		UpdatedAt:   now,
		ID:          reportID,
	})

	h.respondTicketDetail(w, r, reportID, "Lucrarea a fost închisă automat prin Recepție Tacită (72h)!", "", http.StatusOK)
}

func (h *TicketHandler) recalculateProviderStats(ctx context.Context, plumberID int64, now int64) {
	revs, err := h.queries.ListReviewsByPlumberID(ctx, plumberID)
	if err != nil || len(revs) == 0 {
		return
	}
	var sum float64
	for _, r := range revs {
		sum += r.Rating
	}
	totalRevs := int64(len(revs))
	avg := sum / float64(totalRevs)

	_ = h.queries.UpdateProviderStatsAfterReview(ctx, sqlc.UpdateProviderStatsAfterReviewParams{
		Rating:       avg,
		TotalReviews: totalRevs,
		UpdatedAt:    now,
		UserID:       plumberID,
	})
}

func (h *TicketHandler) ExtendDeadline(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || reportID <= 0 {
		http.Error(w, "ID tichet invalid", http.StatusBadRequest)
		return
	}
	_ = r.ParseForm()

	ctx := r.Context()
	report, err := h.queries.GetReportByID(ctx, reportID)
	if err != nil {
		http.Error(w, "Lucrare inexistentă", http.StatusNotFound)
		return
	}
	if report.UserID != session.UserID && !h.isManagingAdminForReport(ctx, session.UserID, report) {
		h.respondTicketDetail(w, r, reportID, "", "Doar beneficiarul poate prelungi termenul SLA.", http.StatusForbidden)
		return
	}

	days, _ := strconv.ParseInt(strings.TrimSpace(r.FormValue("days")), 10, 64)
	if days != 7 {
		days = 3
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	if reason == "" {
		reason = fmt.Sprintf("Prelungire SLA cu %d zile", days)
	}

	now := time.Now().Unix()
	base := now
	if report.ExtendedUntil.Valid && report.ExtendedUntil.Int64 > now {
		base = report.ExtendedUntil.Int64
	}
	newDeadline := base + days*86400

	_, _ = h.queries.ExtendReportDeadline(ctx, sqlc.ExtendReportDeadlineParams{
		ExtendedUntil:   sql.NullInt64{Int64: newDeadline, Valid: true},
		ExtensionReason: sql.NullString{String: reason, Valid: true},
		UpdatedAt:       now,
		ID:              reportID,
	})

	h.respondTicketDetail(w, r, reportID, fmt.Sprintf("Termenul SLA a fost prelungit până la %s.", formatTimestamp(newDeadline)), "", http.StatusOK)
}

func (h *TicketHandler) ReopenTicket(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || reportID <= 0 {
		http.Error(w, "ID tichet invalid", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	report, err := h.queries.GetReportByID(ctx, reportID)
	if err != nil {
		http.Error(w, "Lucrare inexistentă", http.StatusNotFound)
		return
	}
	if report.UserID != session.UserID && !h.isManagingAdminForReport(ctx, session.UserID, report) {
		h.respondTicketDetail(w, r, reportID, "", "Doar beneficiarul poate decupla prestatorul și repune tichetul pe liber.", http.StatusForbidden)
		return
	}
	if report.Status == "completed" {
		h.respondTicketDetail(w, r, reportID, "", "Lucrările finalizate nu mai pot fi redeschise.", http.StatusConflict)
		return
	}

	now := time.Now().Unix()
	if report.AcceptedOfferID.Valid {
		_, _ = h.queries.UpdateOfferStatus(ctx, sqlc.UpdateOfferStatusParams{
			Status:    "rejected",
			UpdatedAt: now,
			ID:        report.AcceptedOfferID.Int64,
		})
	}

	newStatus := "open"
	if rem, _ := h.queries.CountPendingOffersForRequest(ctx, reportID); rem > 0 {
		newStatus = "pending_offers"
	}

	_, _ = h.queries.ReopenReportForBidding(ctx, sqlc.ReopenReportForBiddingParams{
		Status:    newStatus,
		UpdatedAt: now,
		ID:        reportID,
	})

	h.respondTicketDetail(w, r, reportID, "Prestatorul a fost decuplat, iar lucrarea a fost repusă pe liber în Faza 1 (Deschis).", "", http.StatusOK)
}

func (h *TicketHandler) ConfirmCommunityAffected(w http.ResponseWriter, r *http.Request) {
	_, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || reportID <= 0 {
		http.Error(w, "ID tichet invalid", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.respondTicketDetail(w, r, reportID, "", "Formular invalid.", http.StatusBadRequest)
		return
	}

	apartament := strings.TrimSpace(r.FormValue("apartament"))
	telefon := strings.TrimSpace(r.FormValue("telefon"))
	if telefon == "" {
		telefon = strings.TrimSpace(r.FormValue("client_phone"))
	}
	if apartament == "" || telefon == "" {
		h.respondTicketDetail(w, r, reportID, "", "Completează apartamentul și telefonul pentru confirmarea comunitară.", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	now := time.Now().Unix()
	_, _ = h.queries.CreateTicketFollower(ctx, sqlc.CreateTicketFollowerParams{
		ReportID:    reportID,
		Apartament:  apartament,
		ClientPhone: telefon,
		CreatedAt:   now,
	})
	_ = h.queries.IncrementReportConfirmations(ctx, sqlc.IncrementReportConfirmationsParams{
		UpdatedAt: now,
		ID:        reportID,
	})

	h.respondTicketDetail(w, r, reportID, "Confirmarea ta a fost înregistrată pe acest tichet!", "", http.StatusOK)
}

func (h *TicketHandler) DeleteTicket(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requirePlatformUser(w, r)
	if !ok {
		return
	}
	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || reportID <= 0 {
		http.Error(w, "ID tichet invalid", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	report, err := h.queries.GetReportByID(ctx, reportID)
	if err != nil {
		http.Error(w, "Lucrare inexistentă", http.StatusNotFound)
		return
	}
	if report.UserID != session.UserID && !h.isManagingAdminForReport(ctx, session.UserID, report) {
		h.respondTicketDetail(w, r, reportID, "", "Nu ai permisiunea de a arhiva această solicitare.", http.StatusForbidden)
		return
	}
	if report.Status != "open" && report.Status != "pending_offers" {
		h.respondTicketDetail(w, r, reportID, "", "Lucrările cu ofertă acceptată sau în execuție nu pot fi șterse direct.", http.StatusConflict)
		return
	}

	now := time.Now().Unix()
	_ = h.queries.SoftDeleteReport(ctx, sqlc.SoftDeleteReportParams{
		DeletedAt: sql.NullInt64{Int64: now, Valid: true},
		UpdatedAt: now,
		ID:        reportID,
	})

	if isHTMX(r) {
		w.Header().Set("HX-Redirect", "/dashboard?flash="+url.QueryEscape("Solicitarea a fost arhivată."))
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/dashboard?flash="+url.QueryEscape("Solicitarea a fost arhivată."), http.StatusSeeOther)
}

func (h *TicketHandler) RequestPrintPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session := auth.SessionFromContext(ctx)
	token := strings.TrimSpace(r.URL.Query().Get("token"))

	if !session.IsLoggedIn() && token == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	reportID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || reportID <= 0 {
		http.Error(w, "ID tichet invalid", http.StatusBadRequest)
		return
	}

	report, err := h.queries.GetReportByID(ctx, reportID)
	if err != nil {
		http.Error(w, "Lucrarea nu a fost găsită", http.StatusNotFound)
		return
	}

	_, _, _, _, allowed := h.canAccessReport(ctx, report, session, token)
	if !allowed {
		http.Error(w, "Acces restricționat la documentele acestei lucrări.", http.StatusForbidden)
		return
	}

	docType := strings.TrimSpace(r.URL.Query().Get("type"))
	docTitle := "Proces-Verbal de Recepție a Lucrării"
	switch docType {
	case "draft":
		docTitle = "Fișă de Intervenție și Constatare (Draft)"
	case "garantie":
		docTitle = "Certificat de Garanție (12 Luni)"
	default:
		docType = "pv"
	}

	var provName, provCompany, provCUI, provPhone, estTime string
	var manoperaBani, materialeBani int64

	offers, _ := h.queries.ListOffersByRequestWithProvider(ctx, report.ID)
	for _, off := range offers {
		if (report.AcceptedOfferID.Valid && off.ID == report.AcceptedOfferID.Int64) || off.Status == "accepted" {
			provName = off.ProviderName
			provCompany = off.CompanyName
			provCUI = off.CompanyCui
			provPhone = off.ProviderPhone
			estTime = off.EstimatedTime
			manoperaBani = off.PriceBani
			if manoperaBani == 0 && off.Price > 0 {
				manoperaBani = floatRONToBani(off.Price)
			}
			materialeBani = off.MaterialsPriceBani
			if materialeBani == 0 && off.MaterialsPrice.Valid && off.MaterialsPrice.Float64 > 0 {
				materialeBani = floatRONToBani(off.MaterialsPrice.Float64)
			}
			break
		}
	}

	if report.CostFinalManopera.Valid && report.CostFinalManopera.Int64 > 0 {
		manoperaBani = report.CostFinalManopera.Int64
	}
	if report.CostFinalMateriale.Valid && report.CostFinalMateriale.Int64 > 0 {
		materialeBani = report.CostFinalMateriale.Int64
	}

	sursaLabel := "Buget Propriu Beneficiar"
	switch report.SursaFonduri {
	case "fond_rulment", "fond_reparatii_bloc", "fond_reparatii", "fond_urgenta":
		sursaLabel = "Fond Reparații Asociație"
	}

	issuedTs := report.CompletedAt.Int64
	if issuedTs <= 0 {
		issuedTs = time.Now().Unix()
	}
	warrantyTs := issuedTs + 365*86400

	printData := pages.RequestPrintViewData{
		DocType:                  docType,
		DocTitle:                 docTitle,
		TicketID:                 report.ID,
		TicketTitle:              report.Title,
		TicketCategory:           report.Category,
		TicketDescription:        report.Description,
		TicketAddress:            report.Address.String,
		TicketCounty:             report.County,
		TicketAsociatie:          report.Asociatie.String,
		TicketApartament:         report.Apartament.String,
		ClientName:               report.ClientName.String,
		ClientPhone:              report.ClientPhone.String,
		ProviderName:             provName,
		ProviderCompany:          provCompany,
		ProviderCUI:              provCUI,
		ProviderPhone:            provPhone,
		SursaFonduriLabel:        sursaLabel,
		AprobatAdmin:             report.AprobatAdministrator == 1,
		InvoiceNumber:            report.InvoiceNumber.String,
		ManoperaFormatted:        formatBaniRON(manoperaBani),
		MaterialeFormatted:       formatBaniRON(materialeBani),
		TotalFormatted:           formatBaniRON(manoperaBani + materialeBani),
		EstimatedTime:            estTime,
		RaportLucrari:            report.RaportLucrariEfectuate.String,
		RaportMateriale:          report.RaportMateriale.String,
		RaportObservatii:         report.RaportObservatii.String,
		IssuedAtFormatted:        formatTimestamp(issuedTs),
		WarrantyExpiresFormatted: formatTimestamp(warrantyTs),
	}

	meta := h.pageMeta(r, fmt.Sprintf("%s — Lucrare #%d", docTitle, report.ID), true)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = pages.RequestPrintPage(meta, printData).Render(ctx, w)
}
