package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"instalbloc/internal/auth"
	"instalbloc/internal/config"
	"instalbloc/internal/db"
	"instalbloc/internal/db/sqlc"
	"instalbloc/internal/domain"
	"instalbloc/internal/handlers"
	appmw "instalbloc/internal/middleware"
	"instalbloc/internal/services"
	"instalbloc/public"
)

type HealthResponse struct {
	Status      string `json:"status"`
	JournalMode string `json:"journal_mode"`
	BusyTimeout int    `json:"busy_timeout"`
	ForeignKeys int    `json:"foreign_keys"`
	Timestamp   string `json:"timestamp"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	conn, pragmaState, err := db.Open(ctx, cfg.DatabasePath)
	cancel()
	if err != nil {
		logger.Error("failed to initialize database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		_ = conn.Close()
	}()

	queries := sqlc.New(conn)
	emailSvc := services.NewEmailService(cfg, logger)
	slaCron := services.NewSLACronWorker(queries, emailSvc, logger)

	logger.Info("database initialized",
		slog.String("journal_mode", pragmaState.JournalMode),
		slog.Int("busy_timeout", pragmaState.BusyTimeout),
		slog.Int("foreign_keys", pragmaState.ForeignKeys),
	)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(15 * time.Second))
	r.Use(appmw.SecurityHeaders)
	r.Use(auth.SessionMiddleware(queries, cfg))

	staticServer := http.FileServer(http.FS(public.FS))
	r.Handle("/css/*", staticServer)
	r.Handle("/js/*", staticServer)
	r.Handle("/manifest.json", staticServer)
	r.Handle("/sw.js", staticServer)
	r.Handle("/icon.svg", staticServer)
	r.Handle("/icon-192.png", staticServer)
	r.Handle("/icon-512.png", staticServer)
	r.Handle("/icon-maskable-512.png", staticServer)
	r.Handle("/apple-touch-icon.png", staticServer)
	r.Handle("/qr-tiberiu.webp", staticServer)

	seoHandler := handlers.NewSEOHandler(func(req *http.Request) domain.SessionView {
		return auth.SessionFromContext(req.Context())
	})
	r.Get("/", seoHandler.Home)
	r.Get("/robots.txt", seoHandler.RobotsTXT)
	r.Get("/sitemap.xml", seoHandler.SitemapXML)

	authHandler := handlers.NewAuthHandler(conn, queries, cfg, emailSvc)
	r.Get("/login", authHandler.LoginPage)
	r.Post("/login", authHandler.LoginSubmit)
	r.Get("/verify-otp", authHandler.VerifyOTPPage)
	r.Post("/verify-otp", authHandler.VerifyOTPSubmit)
	r.Get("/logout", authHandler.Logout)
	r.Post("/logout", authHandler.Logout)
	r.Get("/forgot-password", authHandler.ForgotPasswordPage)
	r.Post("/forgot-password", authHandler.ForgotPasswordSubmit)
	r.Get("/reset-password", authHandler.ResetPasswordPage)
	r.Post("/reset-password", authHandler.ResetPasswordSubmit)
	r.Get("/onboarding", authHandler.OnboardingPage)
	r.Post("/onboarding", authHandler.OnboardingSubmit)
	r.Get("/termeni", authHandler.Termeni)
	r.Get("/gdpr", authHandler.GDPR)
	r.Get("/cookies", authHandler.Cookies)
	r.Get("/garantie", authHandler.Garantie)

	ticketHandler := handlers.NewTicketHandler(conn, queries, cfg, emailSvc)
	r.Get("/dashboard", ticketHandler.DashboardPage)
	r.Post("/dashboard/subscription/auto-renew", ticketHandler.ToggleAutoRenew)
	r.Get("/dashboard/oferte", ticketHandler.ProviderOffersPage)
	r.Get("/requests/new", ticketHandler.NewRequestPage)
	r.Post("/requests/new", ticketHandler.CreateRequestSubmit)
	r.Get("/requests/{id}", ticketHandler.RequestDetailPage)
	r.Get("/requests/{id}/print", ticketHandler.RequestPrintPage)
	r.Post("/requests/{id}/offers", ticketHandler.SubmitOffer)
	r.Post("/requests/{id}/offers/{offerID}/withdraw", ticketHandler.WithdrawOffer)
	r.Post("/requests/{id}/offers/{offerID}/accept", ticketHandler.AcceptOffer)
	r.Post("/requests/{id}/offers/{offerID}/reject", ticketHandler.RejectOffer)
	r.Post("/requests/{id}/funds", ticketHandler.UpdateFundsApproval)
	r.Post("/requests/{id}/start-work", ticketHandler.StartWork)
	r.Post("/requests/{id}/provider-report", ticketHandler.SubmitProviderReport)
	r.Post("/requests/{id}/complete", ticketHandler.CompleteTicket)
	r.Post("/requests/{id}/tacit-complete", ticketHandler.TacitCompleteTicket)
	r.Post("/requests/{id}/extend", ticketHandler.ExtendDeadline)
	r.Post("/requests/{id}/reopen", ticketHandler.ReopenTicket)
	r.Post("/requests/{id}/confirm", ticketHandler.ConfirmCommunityAffected)
	r.Post("/requests/{id}/delete", ticketHandler.DeleteTicket)

	platformHandler := handlers.NewPlatformHandler(conn, queries, cfg, emailSvc, slaCron)
	r.Get("/associations", platformHandler.AssociationsPage)
	r.Post("/associations", platformHandler.CreateAssociationSubmit)
	r.Post("/associations/{id}/apartments", platformHandler.UpdateAssociationApartmentsSubmit)
	r.Post("/associations/{id}/buildings", platformHandler.CreateBuildingSubmit)
	r.Get("/qr-avizier", platformHandler.QRAvizierPage)
	r.Post("/qr-avizier", platformHandler.GenerateQRInviteSubmit)
	r.Post("/qr-avizier/{id}/deactivate", platformHandler.DeactivateQRInviteSubmit)
	r.Get("/avizier/{code}", platformHandler.PublicQRReportPage)
	r.Post("/avizier/{code}", platformHandler.PublicQRReportSubmit)
	r.Get("/billing", platformHandler.BillingPage)
	r.Post("/billing/invoices/generate", platformHandler.GenerateInvoiceSubmit)
	r.Post("/billing/invoices/{id}/pay", platformHandler.PayInvoiceSubmit)
	r.Post("/webhooks/netopia", platformHandler.NetopiaWebhook)
	r.Post("/api/media/upload", platformHandler.UploadMediaSubmit)
	r.Get("/platform-admin", platformHandler.PlatformAdminPage)
	r.Post("/platform-admin/users/{id}/toggle-subscription", platformHandler.AdminToggleUserSubscription)
	r.Post("/platform-admin/users/{id}/toggle-autorenew", platformHandler.AdminToggleUserAutoRenew)
	r.Post("/platform-admin/users/{id}/toggle-active", platformHandler.AdminToggleUserActive)
	r.Post("/platform-admin/reports/send", platformHandler.AdminSendReportSubmit)

	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		state, verifyErr := db.VerifyPragmas(req.Context(), conn)
		if verifyErr != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "error",
				"error":  verifyErr.Error(),
			})
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(HealthResponse{
			Status:      "ok",
			JournalMode: state.JournalMode,
			BusyTimeout: state.BusyTimeout,
			ForeignKeys: state.ForeignKeys,
			Timestamp:   time.Now().Format("02.01.2006, 15:04:05"),
		})
	})

	addr := ":" + strconv.Itoa(cfg.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slaCron.Start(stopCtx, 15*time.Minute)

	go func() {
		logger.Info("starting InstalBloc Go server", slog.String("addr", fmt.Sprintf("http://localhost%s", addr)))
		if serveErr := srv.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("http server error", slog.String("error", serveErr.Error()))
			os.Exit(1)
		}
	}()

	<-stopCtx.Done()
	logger.Info("shutting down server gracefully")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}
