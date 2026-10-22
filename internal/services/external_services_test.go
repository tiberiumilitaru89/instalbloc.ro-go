package services

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"instalbloc/internal/config"
	"instalbloc/internal/db"
	"instalbloc/internal/db/sqlc"
	"instalbloc/internal/domain"
)

func TestR2StorageSigV4AndUploadResilience(t *testing.T) {
	// 1. Validate MIME allowlist & 10 MB size limit
	if _, err := ValidateMediaFile("image/webp", 2048); err != nil {
		t.Fatalf("expected image/webp to be valid: %v", err)
	}
	if _, err := ValidateMediaFile("application/pdf", 4096); err != nil {
		t.Fatalf("expected application/pdf to be valid: %v", err)
	}
	if _, err := ValidateMediaFile("text/html", 512); err == nil {
		t.Fatal("expected text/html to be rejected by MIME allowlist")
	}
	if _, err := ValidateMediaFile("image/jpeg", MaxMediaUploadBytes+1); err == nil {
		t.Fatal("expected file > 10MB to be rejected")
	}

	// 2. Verify AWS4-HMAC-SHA256 presigned URL & exponential backoff upload
	var attempts int32
	mockR2Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&attempts, 1)
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256 Credential=test-access-key/") {
			t.Errorf("invalid AWS4 Authorization header: %s", authHeader)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Header.Get("X-Amz-Content-Sha256") == "" {
			t.Error("missing X-Amz-Content-Sha256 header")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// Simulate transient R2 error on 1st attempt to test exponential backoff retry
		if cur == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer mockR2Server.Close()

	cfg := &config.Config{
		Env:           config.EnvTest,
		R2AccountID:   "acc123456",
		R2AccessKeyID: "test-access-key",
		R2SecretKey:   "test-secret-key-min-32-characters",
		R2BucketName:  "instalbloc-media",
		R2PublicURL:   "https://media.instalbloc.ro",
	}
	r2 := NewR2StorageService(cfg, nil).WithEndpointOverride(mockR2Server.URL)

	presignedURL, err := r2.GeneratePresignedPutURL("tickets/u10/photo.webp", 15*time.Minute, time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GeneratePresignedPutURL failed: %v", err)
	}
	if !strings.Contains(presignedURL, "X-Amz-Algorithm=AWS4-HMAC-SHA256") || !strings.Contains(presignedURL, "X-Amz-Signature=") {
		t.Fatalf("unexpected presigned URL format: %s", presignedURL)
	}

	payload := []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")
	pubURL, err := r2.UploadObject(context.Background(), "tickets/u10/photo.webp", "image/webp", payload)
	if err != nil {
		t.Fatalf("UploadObject failed after retry: %v", err)
	}
	if pubURL != "https://media.instalbloc.ro/tickets/u10/photo.webp" {
		t.Fatalf("unexpected public URL: %s", pubURL)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("expected exactly 2 attempts (1 transient failure + 1 retry success), got %d", attempts)
	}
}

func TestNetopiaCheckoutAndWebhookSignature(t *testing.T) {
	cfg := &config.Config{
		Env:        config.EnvTest,
		HMACSecret: "netopia-hmac-secret-key-32-bytes-long",
		AppURL:     "https://www.instalbloc.ro",
	}
	ntp := NewNetopiaService(cfg)

	sess, err := ntp.BuildCheckoutSession(42, 7, 24000, "INV-2026-0042")
	if err != nil {
		t.Fatalf("BuildCheckoutSession failed: %v", err)
	}
	if sess.AmountBani != 24000 || sess.AmountRON != "240.00" {
		t.Fatalf("unexpected session amount: %+v", sess)
	}

	rawWebhook := []byte(`{"invoice_id":42,"idempotency_key":"NTP-IDEMP-42","external_id":"NTP-EXT-1"}`)
	sig := ntp.SignPayload(rawWebhook)

	if !ntp.VerifyWebhookSignature(rawWebhook, sig) {
		t.Fatal("expected valid HMAC-SHA256 signature to pass verification")
	}
	if !ntp.VerifyWebhookSignature(rawWebhook, "sha256="+sig) {
		t.Fatal("expected sha256= prefixed signature to pass verification")
	}
	if ntp.VerifyWebhookSignature([]byte(`{"invoice_id":42,"idempotency_key":"TAMPERED"}`), sig) {
		t.Fatal("expected tampered payload to fail HMAC verification")
	}
}

func TestSLACronWorkerSubscriptionsAnd96hOverdue(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_sla_cron.sqlite")
	conn, _, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = conn.Close() }()

	queries := sqlc.New(conn)
	now := time.Now().Unix()

	usr, err := queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email:        "client.expirat@bloc.ro",
		Name:         "Client Test SLA",
		PrimaryRole:  string(domain.RoleLocatar),
		IsVerified:   1,
		IsSubscribed: 1,
		CreatedAt:    now - 40*86400,
		UpdatedAt:    now - 40*86400,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create an overdue 30-day subscription (expired 2 hours ago)
	_, err = queries.CreateSubscription(ctx, sqlc.CreateSubscriptionParams{
		UserID:    usr.ID,
		PlanID:    "locatar_monthly",
		Status:    "active",
		StartedAt: now - 31*86400,
		ExpiresAt: now - 7200,
		AutoRenew: 0,
		CreatedAt: now - 31*86400,
		UpdatedAt: now - 31*86400,
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	// Create a ticket accepted 98 hours ago (> 96h SLA limit)
	rep, err := queries.CreateReport(ctx, sqlc.CreateReportParams{
		Title:          "Infiltrație terasă bloc",
		Description:    "Infiltrații puternice la ultimul etaj.",
		Category:       "Hidroizolații & Acoperișuri",
		Status:         "accepted",
		TipInterventie: "urgenta_imediata",
		Urgency:        "high",
		UserID:         usr.ID,
		County:         "București",
		Zone:           "Sector 3",
		RequesterType:  "locatar",
		SursaFonduri:   "fond_rulment",
		CreatedAt:      now - 100*3600,
		UpdatedAt:      now - 98*3600,
	})
	if err != nil {
		t.Fatalf("create report: %v", err)
	}

	_, err = conn.ExecContext(ctx, `UPDATE reports SET accepted_at = ? WHERE id = ?`, now-98*3600, rep.ID)
	if err != nil {
		t.Fatalf("set accepted_at: %v", err)
	}

	emailSvc := NewEmailService(&config.Config{Env: config.EnvTest}, nil)
	worker := NewSLACronWorker(queries, emailSvc, nil)

	expiredSubs, overdueTickets, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("worker.RunOnce failed: %v", err)
	}
	if expiredSubs != 1 {
		t.Fatalf("expected 1 expired subscription, got %d", expiredSubs)
	}
	if overdueTickets != 1 {
		t.Fatalf("expected 1 overdue 96h ticket, got %d", overdueTickets)
	}

	updatedUser, _ := queries.GetUserByID(ctx, usr.ID)
	if updatedUser.IsSubscribed != 0 {
		t.Fatalf("expected user IsSubscribed=0 after SLA cron expiry pass, got %d", updatedUser.IsSubscribed)
	}
	_ = sql.ErrNoRows
}
