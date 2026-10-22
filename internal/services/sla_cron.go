package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"instalbloc/internal/db/sqlc"
)

// SLACronWorker manages periodic SLA 96h checks and 30-day subscription expirations.
type SLACronWorker struct {
	queries *sqlc.Queries
	email   *EmailService
	logger  *slog.Logger
}

// NewSLACronWorker creates a new background SLA & Subscription monitor.
func NewSLACronWorker(queries *sqlc.Queries, email *EmailService, logger *slog.Logger) *SLACronWorker {
	return &SLACronWorker{
		queries: queries,
		email:   email,
		logger:  logger,
	}
}

// Start launches the periodic ticker loop until ctx is canceled.
func (w *SLACronWorker) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _, _ = w.RunOnce(ctx)
			}
		}
	}()
}

// RunOnce executes a single deterministic pass of subscription expiry synchronization and 96h SLA inspection.
func (w *SLACronWorker) RunOnce(ctx context.Context) (expiredSubs int, overdueTickets int, err error) {
	now := time.Now().Unix()
	traceID := fmt.Sprintf("sla-cron-%d", now)

	// 1. Mark overdue active subscriptions as expired and synchronize user is_subscribed flag
	allSubs, subErr := w.queries.ListAllSubscriptionsForAdmin(ctx)
	if subErr != nil {
		if w.logger != nil {
			w.logger.Error("SLA_CRON_SUBS_QUERY_FAILED",
				slog.String("correlation_id", traceID),
				slog.String("error", subErr.Error()),
			)
		}
		return 0, 0, subErr
	}

	_ = w.queries.ExpireOverdueSubscriptions(ctx, sqlc.ExpireOverdueSubscriptionsParams{
		UpdatedAt: now,
		ExpiresAt: now,
	})

	for _, sub := range allSubs {
		if sub.Status == "active" && sub.ExpiresAt < now {
			expiredSubs++
			_ = w.queries.UpdateUserSubscriptionStatus(ctx, sqlc.UpdateUserSubscriptionStatusParams{
				IsSubscribed: 0,
				UpdatedAt:    now,
				ID:           sub.UserID,
			})
			if w.email != nil {
				if usr, uErr := w.queries.GetUserByID(ctx, sub.UserID); uErr == nil && usr.Email != "" {
					_ = w.email.SendTicketAlert(
						ctx,
						usr.Email,
						"Expirare abonament 30 zile — InstalBloc",
						fmt.Sprintf("Salut %s,\r\n\r\nAbonamentul tău pe 30 de zile pe platforma InstalBloc a expirat. Îl poți reînnoi oricând din secțiunea Facturare & Abonament (/billing).\r\n", usr.Name),
					)
				}
			}
		}
	}

	// 2. Inspect active tickets in 'accepted' or 'in_progress' exceeding 96 hours without active extension
	reports, repErr := w.queries.ListActiveReports(ctx)
	if repErr != nil {
		if w.logger != nil {
			w.logger.Error("SLA_CRON_REPORTS_QUERY_FAILED",
				slog.String("correlation_id", traceID),
				slog.String("error", repErr.Error()),
			)
		}
		return expiredSubs, 0, repErr
	}

	for _, rep := range reports {
		if (rep.Status == "accepted" || rep.Status == "in_progress") && rep.AcceptedAt.Valid {
			if now-rep.AcceptedAt.Int64 > 96*3600 {
				if !rep.ExtendedUntil.Valid || rep.ExtendedUntil.Int64 < now {
					overdueTickets++
					if w.email != nil {
						if usr, uErr := w.queries.GetUserByID(ctx, rep.UserID); uErr == nil && usr.Email != "" {
							_ = w.email.SendTicketAlert(
								ctx,
								usr.Email,
								fmt.Sprintf("Avertizare SLA 96h — Lucrarea #%d (%s)", rep.ID, rep.Title),
								fmt.Sprintf("Salut,\r\n\r\nLucrarea #%d (%s) a depășit termenul standard SLA de 96 de ore de la acceptarea ofertei. Te rugăm să finalizezi recepția cu Proces-Verbal, să prelungești termenul cu 48h sau să repui lucrarea pe liber în Panoul de Lucrări.\r\n", rep.ID, rep.Title),
							)
						}
					}
				}
			}
		}
	}

	if w.logger != nil && (expiredSubs > 0 || overdueTickets > 0) {
		w.logger.Info("SLA_CRON_PASS_COMPLETED",
			slog.String("correlation_id", traceID),
			slog.Int("expired_subscriptions", expiredSubs),
			slog.Int("overdue_96h_tickets", overdueTickets),
		)
	}

	return expiredSubs, overdueTickets, nil
}
