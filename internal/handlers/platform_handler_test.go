package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
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
	"instalbloc/internal/services"
)

func setupPlatformTestServer(t *testing.T) (*sql.DB, *sqlc.Queries, *config.Config, http.Handler) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_platform.sqlite")
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
	slaCron := services.NewSLACronWorker(queries, nil, nil)
	ph := NewPlatformHandler(conn, queries, cfg, nil, slaCron)

	r := chi.NewRouter()
	r.Use(middleware.SecurityHeaders)
	r.Use(auth.SessionMiddleware(queries, cfg))

	r.Get("/associations", ph.AssociationsPage)
	r.Post("/associations", ph.CreateAssociationSubmit)
	r.Post("/associations/{id}/apartments", ph.UpdateAssociationApartmentsSubmit)
	r.Post("/associations/{id}/buildings", ph.CreateBuildingSubmit)
	r.Get("/qr-avizier", ph.QRAvizierPage)
	r.Post("/qr-avizier", ph.GenerateQRInviteSubmit)
	r.Post("/qr-avizier/{id}/deactivate", ph.DeactivateQRInviteSubmit)
	r.Get("/avizier/{code}", ph.PublicQRReportPage)
	r.Post("/avizier/{code}", ph.PublicQRReportSubmit)
	r.Get("/billing", ph.BillingPage)
	r.Post("/billing/invoices/generate", ph.GenerateInvoiceSubmit)
	r.Post("/billing/invoices/{id}/pay", ph.PayInvoiceSubmit)
	r.Post("/webhooks/netopia", ph.NetopiaWebhook)
	r.Post("/api/media/upload", ph.UploadMediaSubmit)
	r.Get("/platform-admin", ph.PlatformAdminPage)
	r.Post("/platform-admin/users/{id}/toggle-subscription", ph.AdminToggleUserSubscription)
	r.Post("/platform-admin/users/{id}/toggle-autorenew", ph.AdminToggleUserAutoRenew)
	r.Post("/platform-admin/users/{id}/toggle-active", ph.AdminToggleUserActive)
	r.Post("/platform-admin/reports/send", ph.AdminSendReportSubmit)

	return conn, queries, cfg, r
}

func TestAssociationsQRAvizierAndBillingLedger(t *testing.T) {
	ctx := context.Background()
	_, queries, cfg, router := setupPlatformTestServer(t)
	now := time.Now().Unix()

	assocAdmin, err := queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        "presedinte@bloc-central.ro",
		Name:         "Mihai Popescu",
		Phone:        sql.NullString{String: "0744111222", Valid: true},
		PrimaryRole:  string(domain.RoleAsociatie),
		IsVerified:   1,
		IsSubscribed: 0,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("create assoc admin: %v", err)
	}

	// 1. Create Association via POST /associations (50 apartments)
	createForm := url.Values{
		"name":                {"Asociația de Proprietari Bloc Central"},
		"cui":                 {"RO12345678"},
		"apartment_count":     {"50"},
		"county":              {"București"},
		"city":                {"Sector 1"},
		"address":             {"Bd. Magheru 20"},
		"representative_name": {"Mihai Popescu"},
	}
	reqCreate := httptest.NewRequest(http.MethodPost, "/associations", strings.NewReader(createForm.Encode()))
	reqCreate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addAuthCookie(reqCreate, assocAdmin.ID, domain.RoleAsociatie, cfg.HMACSecret)
	recCreate := httptest.NewRecorder()
	router.ServeHTTP(recCreate, reqCreate)

	if recCreate.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 on create association, got %d: %s", recCreate.Code, recCreate.Body.String())
	}

	assocs, err := queries.ListUserAssociations(ctx, assocAdmin.ID)
	if err != nil || len(assocs) != 1 {
		t.Fatalf("expected 1 association, got %d (err=%v)", len(assocs), err)
	}
	assocID := assocs[0].ID

	// 2. Anti-duplicate CUI check must return 409 Conflict
	reqDup := httptest.NewRequest(http.MethodPost, "/associations", strings.NewReader(createForm.Encode()))
	reqDup.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addAuthCookie(reqDup, assocAdmin.ID, domain.RoleAsociatie, cfg.HMACSecret)
	recDup := httptest.NewRecorder()
	router.ServeHTTP(recDup, reqDup)
	if recDup.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on duplicate association CUI, got %d", recDup.Code)
	}

	// 3. Update apartment count to 80 (80 * 300 bani = 24000 bani = 240.00 RON)
	aptForm := url.Values{"apartment_count": {"80"}}
	reqApt := httptest.NewRequest(http.MethodPost, "/associations/"+strconv.FormatInt(assocID, 10)+"/apartments", strings.NewReader(aptForm.Encode()))
	reqApt.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addAuthCookie(reqApt, assocAdmin.ID, domain.RoleAsociatie, cfg.HMACSecret)
	recApt := httptest.NewRecorder()
	router.ServeHTTP(recApt, reqApt)
	if recApt.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 SeeOther on update apartments, got %d: %s", recApt.Code, recApt.Body.String())
	}

	reqAssocPage := httptest.NewRequest(http.MethodGet, "/associations", nil)
	addAuthCookie(reqAssocPage, assocAdmin.ID, domain.RoleAsociatie, cfg.HMACSecret)
	recAssocPage := httptest.NewRecorder()
	router.ServeHTTP(recAssocPage, reqAssocPage)
	if recAssocPage.Code != http.StatusOK || !strings.Contains(recAssocPage.Body.String(), "240.00 RON") {
		t.Fatalf("expected 200 OK with 240.00 RON fee on /associations, got %d", recAssocPage.Code)
	}

	// 4. Add a Building ("Scara B") and Generate QR Avizier code
	bldgForm := url.Values{"building_name": {"Scara B"}}
	reqBldg := httptest.NewRequest(http.MethodPost, "/associations/"+strconv.FormatInt(assocID, 10)+"/buildings", strings.NewReader(bldgForm.Encode()))
	reqBldg.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addAuthCookie(reqBldg, assocAdmin.ID, domain.RoleAsociatie, cfg.HMACSecret)
	recBldg := httptest.NewRecorder()
	router.ServeHTTP(recBldg, reqBldg)
	if recBldg.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 on create building, got %d", recBldg.Code)
	}

	qrForm := url.Values{
		"association_id": {strconv.FormatInt(assocID, 10)},
		"building_id":    {"0"},
	}
	reqQR := httptest.NewRequest(http.MethodPost, "/qr-avizier", strings.NewReader(qrForm.Encode()))
	reqQR.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addAuthCookie(reqQR, assocAdmin.ID, domain.RoleAsociatie, cfg.HMACSecret)
	recQR := httptest.NewRecorder()
	router.ServeHTTP(recQR, reqQR)
	if recQR.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 on generate QR invite, got %d", recQR.Code)
	}

	qrList, err := queries.ListQrInvitesByAssociation(ctx, assocID)
	if err != nil || len(qrList) == 0 {
		t.Fatalf("expected QR invite created, got %d (err=%v)", len(qrList), err)
	}
	qrCode := qrList[0].Code

	// 5. Public QR report submission + 5/hour rate limit check
	for i := 1; i <= 5; i++ {
		pubForm := url.Values{
			"client_name":  {"Locatar Scara A"},
			"client_phone": {"0722333444"},
			"apartament":   {"Sc. A, Ap. 12"},
			"category":     {"Instalații Sanitare"},
			"urgency":      {"high"},
			"title":        {"Țeavă fisurată la subsol #" + strconv.Itoa(i)},
			"description":  {"Scurgere continuă lângă apometrul general din subsol."},
		}
		reqPub := httptest.NewRequest(http.MethodPost, "/avizier/"+qrCode, strings.NewReader(pubForm.Encode()))
		reqPub.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		reqPub.RemoteAddr = "203.0.113.55:54321"
		recPub := httptest.NewRecorder()
		router.ServeHTTP(recPub, reqPub)
		if recPub.Code != http.StatusSeeOther {
			t.Fatalf("iteration %d: expected 303 on public QR report, got %d: %s", i, recPub.Code, recPub.Body.String())
		}
	}

	// 6th request from same IP within the hour must be blocked with 429 Too Many Requests
	pubForm6 := url.Values{
		"client_name":  {"Locatar Spam"},
		"client_phone": {"0722333444"},
		"apartament":   {"Sc. A, Ap. 12"},
		"category":     {"Instalații Sanitare"},
		"urgency":      {"low"},
		"title":        {"Sesizare peste limită"},
		"description":  {"Această sesizare depășește plafonul de 5 pe oră."},
	}
	reqPub6 := httptest.NewRequest(http.MethodPost, "/avizier/"+qrCode, strings.NewReader(pubForm6.Encode()))
	reqPub6.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqPub6.RemoteAddr = "203.0.113.55:54321"
	recPub6 := httptest.NewRecorder()
	router.ServeHTTP(recPub6, reqPub6)
	if recPub6.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests on 6th QR submission, got %d", recPub6.Code)
	}

	// 6. Generate Invoice & Execute Atomic Payment in int64 bani (80 apts * 300 bani = 24000 bani)
	reqGenInv := httptest.NewRequest(http.MethodPost, "/billing/invoices/generate", nil)
	addAuthCookie(reqGenInv, assocAdmin.ID, domain.RoleAsociatie, cfg.HMACSecret)
	recGenInv := httptest.NewRecorder()
	router.ServeHTTP(recGenInv, reqGenInv)
	if recGenInv.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 on invoice generation, got %d", recGenInv.Code)
	}

	invoices, err := queries.ListInvoicesByUserID(ctx, assocAdmin.ID)
	if err != nil || len(invoices) != 1 {
		t.Fatalf("expected 1 invoice, got %d (err=%v)", len(invoices), err)
	}
	if invoices[0].AmountBani != 24000 {
		t.Fatalf("expected invoice amount 24000 bani (80 * 300), got %d", invoices[0].AmountBani)
	}

	// Execute webhook payment with idempotency_key
	webhookPayload, _ := json.Marshal(map[string]any{
		"invoice_id":      invoices[0].ID,
		"idempotency_key": "NETOPIA-TX-998877",
		"payment_method":  "netopia_card",
		"status":          "confirmed",
	})
	reqWebhook := httptest.NewRequest(http.MethodPost, "/webhooks/netopia", bytes.NewReader(webhookPayload))
	reqWebhook.Header.Set("Content-Type", "application/json")
	recWebhook := httptest.NewRecorder()
	router.ServeHTTP(recWebhook, reqWebhook)
	if recWebhook.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from NetopiaWebhook, got %d: %s", recWebhook.Code, recWebhook.Body.String())
	}

	// Verify immutable ledger payment & 30-day subscription activation
	updatedUser, _ := queries.GetUserByID(ctx, assocAdmin.ID)
	if updatedUser.IsSubscribed != 1 {
		t.Fatalf("expected user IsSubscribed=1 after invoice payment, got %d", updatedUser.IsSubscribed)
	}
	payments, _ := queries.ListPaymentsByUserID(ctx, assocAdmin.ID)
	if len(payments) != 1 || payments[0].AmountBani != 24000 || payments[0].IdempotencyKey != "NETOPIA-TX-998877" {
		t.Fatalf("unexpected ledger payment state: %+v", payments)
	}
}

func TestSuperAdminExclusiveIsolationAndControls(t *testing.T) {
	ctx := context.Background()
	_, queries, cfg, router := setupPlatformTestServer(t)
	now := time.Now().Unix()

	superOana, err := queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        "oana.becheanu@yahoo.com",
		Name:         "Oana Becheanu",
		PrimaryRole:  string(domain.RolePlatformAdmin),
		IsVerified:   1,
		IsSubscribed: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("create superOana: %v", err)
	}

	superGeorge, err := queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        "george.becheanu11@yahoo.com",
		Name:         "George Becheanu",
		PrimaryRole:  string(domain.RolePlatformAdmin),
		IsVerified:   1,
		IsSubscribed: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("create superGeorge: %v", err)
	}

	regularProvider, err := queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        "instalator@firma.ro",
		Name:         "Vasile Instalator",
		PrimaryRole:  string(domain.RolePrestator),
		IsVerified:   1,
		IsSubscribed: 0,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("create regularProvider: %v", err)
	}

	// 1. Regular user attempting to access /platform-admin is blocked by SessionMiddleware and redirected to /dashboard
	reqForbidden := httptest.NewRequest(http.MethodGet, "/platform-admin", nil)
	addAuthCookie(reqForbidden, regularProvider.ID, domain.RolePrestator, cfg.HMACSecret)
	recForbidden := httptest.NewRecorder()
	router.ServeHTTP(recForbidden, reqForbidden)
	if recForbidden.Code != http.StatusSeeOther || recForbidden.Header().Get("Location") != "/dashboard" {
		t.Fatalf("expected 303 redirect to /dashboard for non-super-admin on /platform-admin, got %d (%s)", recForbidden.Code, recForbidden.Header().Get("Location"))
	}

	// 2. Super Admin attempting to access /associations or /billing must be redirected to /platform-admin
	for _, path := range []string{"/associations", "/qr-avizier", "/billing"} {
		reqRedir := httptest.NewRequest(http.MethodGet, path, nil)
		addAuthCookie(reqRedir, superOana.ID, domain.RolePlatformAdmin, cfg.HMACSecret)
		recRedir := httptest.NewRecorder()
		router.ServeHTTP(recRedir, reqRedir)
		if recRedir.Code != http.StatusSeeOther || recRedir.Header().Get("Location") != "/platform-admin" {
			t.Fatalf("expected Super Admin on %s to redirect to /platform-admin, got %d (%s)", path, recRedir.Code, recRedir.Header().Get("Location"))
		}
	}

	// 3. Super Admin accesses /platform-admin and sees exclusive Super Admin console
	reqAdmin := httptest.NewRequest(http.MethodGet, "/platform-admin", nil)
	addAuthCookie(reqAdmin, superOana.ID, domain.RolePlatformAdmin, cfg.HMACSecret)
	recAdmin := httptest.NewRecorder()
	router.ServeHTTP(recAdmin, reqAdmin)
	if recAdmin.Code != http.StatusOK || !strings.Contains(recAdmin.Body.String(), "Panou Super Admin") {
		t.Fatalf("expected 200 OK on /platform-admin for Oana, got %d", recAdmin.Code)
	}

	// 4. Super Admin activates 30-day subscription for regularProvider via HTMX
	reqToggleSub := httptest.NewRequest(http.MethodPost, "/platform-admin/users/"+strconv.FormatInt(regularProvider.ID, 10)+"/toggle-subscription", nil)
	reqToggleSub.Header.Set("HX-Request", "true")
	addAuthCookie(reqToggleSub, superOana.ID, domain.RolePlatformAdmin, cfg.HMACSecret)
	recToggleSub := httptest.NewRecorder()
	router.ServeHTTP(recToggleSub, reqToggleSub)
	if recToggleSub.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on toggle subscription, got %d", recToggleSub.Code)
	}
	updatedProvider, _ := queries.GetUserByID(ctx, regularProvider.ID)
	if updatedProvider.IsSubscribed != 1 {
		t.Fatalf("expected regularProvider IsSubscribed=1 after admin toggle, got %d", updatedProvider.IsSubscribed)
	}

	// 5. Canonical Super Admin account cannot be suspended (403 Forbidden)
	reqSuspendSuper := httptest.NewRequest(http.MethodPost, "/platform-admin/users/"+strconv.FormatInt(superGeorge.ID, 10)+"/toggle-active", nil)
	addAuthCookie(reqSuspendSuper, superOana.ID, domain.RolePlatformAdmin, cfg.HMACSecret)
	recSuspendSuper := httptest.NewRecorder()
	router.ServeHTTP(recSuspendSuper, reqSuspendSuper)
	if recSuspendSuper.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden when attempting to suspend canonical Super Admin, got %d", recSuspendSuper.Code)
	}

	// 6. Media upload to R2 via POST /api/media/upload and Netopia invalid signature rejection
	var bodyBuf bytes.Buffer
	mw := multipart.NewWriter(&bodyBuf)
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", `form-data; name="file"; filename="avarie.webp"`)
	partHeader.Set("Content-Type", "image/webp")
	part, _ := mw.CreatePart(partHeader)
	_, _ = part.Write([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "))
	_ = mw.Close()

	reqUpload := httptest.NewRequest(http.MethodPost, "/api/media/upload", &bodyBuf)
	reqUpload.Header.Set("Content-Type", mw.FormDataContentType())
	addAuthCookie(reqUpload, regularProvider.ID, domain.RolePrestator, cfg.HMACSecret)
	recUpload := httptest.NewRecorder()
	router.ServeHTTP(recUpload, reqUpload)
	if recUpload.Code != http.StatusOK || !strings.Contains(recUpload.Body.String(), "https://media.instalbloc.ro/tickets/") {
		t.Fatalf("expected 200 OK with R2 URL on /api/media/upload, got %d: %s", recUpload.Code, recUpload.Body.String())
	}

	// Invalid X-Netopia-Signature must return 401 Unauthorized
	reqBadSig := httptest.NewRequest(http.MethodPost, "/webhooks/netopia", strings.NewReader(`{"invoice_id":1,"idempotency_key":"k1"}`))
	reqBadSig.Header.Set("Content-Type", "application/json")
	reqBadSig.Header.Set("X-Netopia-Signature", "deadbeef00112233")
	recBadSig := httptest.NewRecorder()
	router.ServeHTTP(recBadSig, reqBadSig)
	if recBadSig.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized on invalid X-Netopia-Signature, got %d", recBadSig.Code)
	}
}
