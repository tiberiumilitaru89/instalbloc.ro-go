package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"instalbloc/internal/auth"
	"instalbloc/internal/config"
	"instalbloc/internal/db"
	"instalbloc/internal/db/sqlc"
	"instalbloc/internal/domain"
	"instalbloc/internal/middleware"
)

func setupTicketTestServer(t *testing.T) (*sql.DB, *sqlc.Queries, *config.Config, http.Handler) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_tickets.sqlite")
	conn, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	cfg := &config.Config{
		Env:          config.EnvTest,
		Port:         3000,
		DatabasePath: dbPath,
		HMACSecret:   "test-hmac-secret-key-minimum-32-bytes",
	}
	queries := sqlc.New(conn)
	th := NewTicketHandler(conn, queries, cfg, nil)

	r := chi.NewRouter()
	r.Use(middleware.SecurityHeaders)
	r.Use(auth.SessionMiddleware(queries, cfg))

	r.Get("/dashboard", th.DashboardPage)
	r.Post("/dashboard/subscription/auto-renew", th.ToggleAutoRenew)
	r.Get("/dashboard/oferte", th.ProviderOffersPage)
	r.Get("/requests/new", th.NewRequestPage)
	r.Post("/requests/new", th.CreateRequestSubmit)
	r.Get("/requests/{id}", th.RequestDetailPage)
	r.Get("/requests/{id}/print", th.RequestPrintPage)
	r.Post("/requests/{id}/offers", th.SubmitOffer)
	r.Post("/requests/{id}/offers/{offerID}/withdraw", th.WithdrawOffer)
	r.Post("/requests/{id}/offers/{offerID}/accept", th.AcceptOffer)
	r.Post("/requests/{id}/offers/{offerID}/reject", th.RejectOffer)
	r.Post("/requests/{id}/funds", th.UpdateFundsApproval)
	r.Post("/requests/{id}/start-work", th.StartWork)
	r.Post("/requests/{id}/provider-report", th.SubmitProviderReport)
	r.Post("/requests/{id}/complete", th.CompleteTicket)
	r.Post("/requests/{id}/tacit-complete", th.TacitCompleteTicket)
	r.Post("/requests/{id}/extend", th.ExtendDeadline)
	r.Post("/requests/{id}/reopen", th.ReopenTicket)
	r.Post("/requests/{id}/confirm", th.ConfirmCommunityAffected)
	r.Post("/requests/{id}/delete", th.DeleteTicket)

	return conn, queries, cfg, r
}

func addAuthCookie(req *http.Request, userID int64, role domain.RoleType, secret string) {
	token := auth.SignSessionToken(userID, role, time.Now().Unix(), secret)
	req.AddCookie(&http.Cookie{
		Name:  auth.SessionCookieName,
		Value: token,
	})
}

func TestTicketLifecycleGoldenRuleAndPhoneMasking(t *testing.T) {
	ctx := context.Background()
	conn, queries, cfg, router := setupTicketTestServer(t)
	now := time.Now().Unix()

	// 1. Create Beneficiary (Asociatie Admin)
	adminUser, err := queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        "admin@bloc10.ro",
		Name:         "Ion Administrator",
		Phone:        sql.NullString{String: "0722111222", Valid: true},
		PrimaryRole:  string(domain.RoleAsociatie),
		IsVerified:   1,
		IsSubscribed: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("create admin user: %v", err)
	}

	assoc, err := queries.CreateAssociation(ctx, sqlc.CreateAssociationParams{
		Name:           "Asociația Bloc A1",
		Address:        "Str. Victoriei 10",
		City:           sql.NullString{String: "Sector 1", Valid: true},
		County:         sql.NullString{String: "București", Valid: true},
		ApartmentCount: 40,
		IsSubscribed:   1,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		t.Fatalf("create association: %v", err)
	}
	_, _ = queries.CreateAssociationAdmin(ctx, sqlc.CreateAssociationAdminParams{
		UserID:        adminUser.ID,
		AssociationID: assoc.ID,
		Role:          "administrator",
		CreatedAt:     now,
	})

	// 2. Create Subscribed Provider 1 & Provider 2
	prov1, err := queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        "instalator1@firma.ro",
		Name:         "Vasile Instalatorul",
		Phone:        sql.NullString{String: "0744999888", Valid: true},
		PrimaryRole:  string(domain.RolePrestator),
		IsVerified:   1,
		IsSubscribed: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("create provider 1: %v", err)
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO provider_profiles (user_id, company_name, cui, city, county, rating, total_reviews, total_jobs, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 0, 0, 0, ?, ?)`,
		prov1.ID, "Instal Termo SRL", "12345678", "Sector 1", "București", now, now)
	if err != nil {
		t.Fatalf("insert provider 1 profile: %v", err)
	}

	prov2, err := queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        "instalator2@firma.ro",
		Name:         "Gheorghe Instal",
		Phone:        sql.NullString{String: "0755111333", Valid: true},
		PrimaryRole:  string(domain.RolePrestator),
		IsVerified:   1,
		IsSubscribed: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("create provider 2: %v", err)
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO provider_profiles (user_id, company_name, cui, city, county, rating, total_reviews, total_jobs, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 0, 0, 0, ?, ?)`,
		prov2.ID, "Gheorghe Rapid SRL", "87654321", "Sector 1", "București", now, now)
	if err != nil {
		t.Fatalf("insert provider 2 profile: %v", err)
	}

	// 3. Beneficiary creates a new ticket via POST /requests/new
	form := url.Values{
		"tip_interventie": {"avarie"},
		"title":           {"Țeavă spartă la subsol scara B"},
		"category":        {"Instalații sanitare"},
		"urgency":         {"high"},
		"county":          {"București"},
		"zone":            {"Sector 1"},
		"address":         {"Str. Victoriei 10"},
		"association_id":  {strconv.FormatInt(assoc.ID, 10)},
		"asociatie":       {"Asociația Bloc A1"},
		"tip_spatiu":      {"comun"},
		"sursa_fonduri":   {"fond_urgenta"},
		"client_name":     {"Ion Administrator"},
		"client_phone":    {"0722111222"},
		"description":     {"Pierdere majoră de apă rece pe coloana principală."},
	}
	reqCreate := httptest.NewRequest(http.MethodPost, "/requests/new", strings.NewReader(form.Encode()))
	reqCreate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addAuthCookie(reqCreate, adminUser.ID, domain.RoleAsociatie, cfg.HMACSecret)
	recCreate := httptest.NewRecorder()
	router.ServeHTTP(recCreate, reqCreate)

	if recCreate.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect after creating ticket, got %d: %s", recCreate.Code, recCreate.Body.String())
	}

	reports, _ := queries.ListActiveReports(ctx)
	if len(reports) != 1 {
		t.Fatalf("expected 1 active report, got %d", len(reports))
	}
	ticketID := reports[0].ID
	ticketURL := "/requests/" + strconv.FormatInt(ticketID, 10)

	// 4. Verify Anti-IDOR Phone Masking before offer acceptance for Provider 1
	reqView1 := httptest.NewRequest(http.MethodGet, ticketURL, nil)
	addAuthCookie(reqView1, prov1.ID, domain.RolePrestator, cfg.HMACSecret)
	recView1 := httptest.NewRecorder()
	router.ServeHTTP(recView1, reqView1)
	if recView1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK viewing ticket, got %d", recView1.Code)
	}
	if strings.Contains(recView1.Body.String(), "0722111222") {
		t.Fatalf("SECURITY FAILURE: client phone must be masked for unassigned provider")
	}
	if !strings.Contains(recView1.Body.String(), "07X XXX XXX") {
		t.Fatalf("expected masked phone placeholder 07X XXX XXX in response")
	}

	// 5. Provider 1 submits initial offer (free) and modifies it 3 times, then 4th modification is blocked
	offerForm := url.Values{
		"price_ron":           {"250"},
		"materials_ron":       {"100"},
		"estimated_time":      {"Sosire în 45 minute"},
		"message":             {"Intervenim cu echipă completă și piese."},
		"requires_inspection": {"0"},
	}
	reqOffer := httptest.NewRequest(http.MethodPost, ticketURL+"/offers", strings.NewReader(offerForm.Encode()))
	reqOffer.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqOffer.Header.Set("HX-Request", "true")
	addAuthCookie(reqOffer, prov1.ID, domain.RolePrestator, cfg.HMACSecret)
	recOffer := httptest.NewRecorder()
	router.ServeHTTP(recOffer, reqOffer)
	if recOffer.Code != http.StatusOK || !strings.Contains(recOffer.Body.String(), "Oferta inițială a fost transmisă gratuit") {
		t.Fatalf("expected initial free offer success, got %d", recOffer.Code)
	}

	// Modify 3 times (allowed)
	for i := 1; i <= 3; i++ {
		offerForm.Set("price_ron", strconv.Itoa(250+i*10))
		reqMod := httptest.NewRequest(http.MethodPost, ticketURL+"/offers", strings.NewReader(offerForm.Encode()))
		reqMod.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		reqMod.Header.Set("HX-Request", "true")
		addAuthCookie(reqMod, prov1.ID, domain.RolePrestator, cfg.HMACSecret)
		recMod := httptest.NewRecorder()
		router.ServeHTTP(recMod, reqMod)
		if !strings.Contains(recMod.Body.String(), "modificări lunare utilizate") {
			t.Fatalf("modification %d should succeed, got body: %s", i, recMod.Body.String())
		}
	}

	// 4th modification must be blocked
	offerForm.Set("price_ron", "400")
	reqMod4 := httptest.NewRequest(http.MethodPost, ticketURL+"/offers", strings.NewReader(offerForm.Encode()))
	reqMod4.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqMod4.Header.Set("HX-Request", "true")
	addAuthCookie(reqMod4, prov1.ID, domain.RolePrestator, cfg.HMACSecret)
	recMod4 := httptest.NewRecorder()
	router.ServeHTTP(recMod4, reqMod4)
	if !strings.Contains(recMod4.Body.String(), "plafonul maxim de 3 modificări lunare") {
		t.Fatalf("expected 4th modification to be rejected by 3/month limit")
	}

	// 6. Beneficiary accepts Provider 1's offer -> Phase 2 (accepted)
	offersList, _ := queries.ListOffersByRequestWithProvider(ctx, ticketID)
	if len(offersList) != 1 {
		t.Fatalf("expected 1 offer, got %d", len(offersList))
	}
	offerID := offersList[0].ID

	reqAccept := httptest.NewRequest(http.MethodPost, ticketURL+"/offers/"+strconv.FormatInt(offerID, 10)+"/accept", nil)
	reqAccept.Header.Set("HX-Request", "true")
	addAuthCookie(reqAccept, adminUser.ID, domain.RoleAsociatie, cfg.HMACSecret)
	recAccept := httptest.NewRecorder()
	router.ServeHTTP(recAccept, reqAccept)
	if recAccept.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on accept offer, got %d", recAccept.Code)
	}

	// Phone must now be UNMASKED for assigned subscribed Provider 1
	reqViewAssigned := httptest.NewRequest(http.MethodGet, ticketURL, nil)
	addAuthCookie(reqViewAssigned, prov1.ID, domain.RolePrestator, cfg.HMACSecret)
	recViewAssigned := httptest.NewRecorder()
	router.ServeHTTP(recViewAssigned, reqViewAssigned)
	if !strings.Contains(recViewAssigned.Body.String(), "0722111222") {
		t.Fatalf("expected unmasked phone 0722111222 for assigned subscribed provider")
	}

	// 7. GOLDEN RULE TEST: Assigned Provider 1 tries to start work -> MUST BE 403 FORBIDDEN
	reqProvStart := httptest.NewRequest(http.MethodPost, ticketURL+"/start-work", nil)
	reqProvStart.Header.Set("HX-Request", "true")
	addAuthCookie(reqProvStart, prov1.ID, domain.RolePrestator, cfg.HMACSecret)
	recProvStart := httptest.NewRecorder()
	router.ServeHTTP(recProvStart, reqProvStart)
	if recProvStart.Code != http.StatusForbidden {
		t.Fatalf("GOLDEN RULE VIOLATION: provider must be forbidden (403) from starting work, got %d", recProvStart.Code)
	}

	// 8. Beneficiary starts work -> transitions to Phase 3 (in_progress)
	reqBenStart := httptest.NewRequest(http.MethodPost, ticketURL+"/start-work", nil)
	reqBenStart.Header.Set("HX-Request", "true")
	addAuthCookie(reqBenStart, adminUser.ID, domain.RoleAsociatie, cfg.HMACSecret)
	recBenStart := httptest.NewRecorder()
	router.ServeHTTP(recBenStart, reqBenStart)
	if recBenStart.Code != http.StatusOK {
		t.Fatalf("expected beneficiary to start work (200), got %d", recBenStart.Code)
	}
	repAfterStart, _ := queries.GetReportByID(ctx, ticketID)
	if repAfterStart.Status != "in_progress" {
		t.Fatalf("expected report status in_progress, got %q", repAfterStart.Status)
	}

	// 9. Phase 3 Visibility Isolation: Unassigned Provider 2 MUST receive 403 Forbidden on /requests/{id}
	reqProv2View := httptest.NewRequest(http.MethodGet, ticketURL, nil)
	addAuthCookie(reqProv2View, prov2.ID, domain.RolePrestator, cfg.HMACSecret)
	recProv2View := httptest.NewRecorder()
	router.ServeHTTP(recProv2View, reqProv2View)
	if recProv2View.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for unassigned provider on in_progress ticket, got %d", recProv2View.Code)
	}

	// 10. Assigned Provider 1 submits final technical report & integer RON/bani costs
	repForm := url.Values{
		"raport_lucrari_efectuate": {"Înlocuit tronson țeavă PPR DN32 și robineți izolare."},
		"raport_materiale":         {"Țeavă PPR 4m, 2 robineți bilă"},
		"cost_final_manopera":      {"280"},
		"cost_final_materiale":     {"120.50"},
		"raport_observatii":        {"Presiune testată la 6 bar fără pierderi."},
	}
	reqProvRep := httptest.NewRequest(http.MethodPost, ticketURL+"/provider-report", strings.NewReader(repForm.Encode()))
	reqProvRep.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqProvRep.Header.Set("HX-Request", "true")
	addAuthCookie(reqProvRep, prov1.ID, domain.RolePrestator, cfg.HMACSecret)
	recProvRep := httptest.NewRecorder()
	router.ServeHTTP(recProvRep, reqProvRep)
	if recProvRep.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on provider report, got %d", recProvRep.Code)
	}

	// 11. Beneficiary completes ticket with mandatory 5-star review -> Phase 4 (completed)
	compForm := url.Values{
		"rating":  {"5"},
		"comment": {"Intervenție impecabilă și rapidă!"},
	}
	reqComp := httptest.NewRequest(http.MethodPost, ticketURL+"/complete", strings.NewReader(compForm.Encode()))
	reqComp.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqComp.Header.Set("HX-Request", "true")
	addAuthCookie(reqComp, adminUser.ID, domain.RoleAsociatie, cfg.HMACSecret)
	recComp := httptest.NewRecorder()
	router.ServeHTTP(recComp, reqComp)
	if recComp.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on complete ticket, got %d", recComp.Code)
	}

	repFinal, _ := queries.GetReportByID(ctx, ticketID)
	if repFinal.Status != "completed" {
		t.Fatalf("expected status completed, got %q", repFinal.Status)
	}
	profAfter, _ := queries.GetProviderProfileByUserID(ctx, prov1.ID)
	if profAfter.TotalReviews != 1 || profAfter.Rating != 5.0 {
		t.Fatalf("expected provider rating 5.0 with 1 review, got %.1f (%d reviews)", profAfter.Rating, profAfter.TotalReviews)
	}
}
