package services

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"instalbloc/internal/config"
)

// EmailService dispatches transactional HTML/text emails (OTP 2FA, Password Reset, Alerts)
// with bounded timeouts, exponential backoff, and PII-masked structured logging.
type EmailService struct {
	cfg    *config.Config
	logger *slog.Logger
}

func NewEmailService(cfg *config.Config, logger *slog.Logger) *EmailService {
	if logger == nil {
		logger = slog.Default()
	}
	return &EmailService{
		cfg:    cfg,
		logger: logger,
	}
}

// SendOTP dispatches a 6-digit verification code for login, onboarding, or password reset.
func (s *EmailService) SendOTP(ctx context.Context, recipientEmail, code, purpose string) error {
	masked := config.MaskEmail(recipientEmail)
	subject := "Codul tău de verificare InstalBloc: " + code
	if purpose == "reset_password" {
		subject = "Resetare parolă InstalBloc — Cod: " + code
	}

	body := fmt.Sprintf(
		"Salut,\r\n\r\nCodul tău de verificare pe platforma InstalBloc este: %s\r\n\r\nAcest cod este valabil timp de 10 minute. Nu divulga acest cod nimănui.\r\n\r\nCu respect,\r\nEchipa InstalBloc (https://www.instalbloc.ro)\r\n",
		code,
	)

	if strings.TrimSpace(s.cfg.SMTPHost) == "" || strings.TrimSpace(s.cfg.SMTPUser) == "" {
		s.logger.Info("smtp not configured; otp generated in local/dev mode",
			slog.String("masked_email", string(masked)),
			slog.String("purpose", purpose),
		)
		return nil
	}

	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		sendCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		err := s.sendRawSMTP(sendCtx, recipientEmail, subject, body)
		cancel()
		if err == nil {
			s.logger.Info("otp email sent",
				slog.String("masked_email", string(masked)),
				slog.String("purpose", purpose),
				slog.Int("attempt", attempt),
			)
			return nil
		}
		lastErr = err
		time.Sleep(time.Duration(attempt*250) * time.Millisecond)
	}

	s.logger.Error("failed to send otp email after retries",
		slog.String("masked_email", string(masked)),
		slog.String("purpose", purpose),
		slog.String("error", lastErr.Error()),
	)
	return lastErr
}

// SendTicketAlert dispatches a transactional notification email for ticket/offer lifecycle transitions.
func (s *EmailService) SendTicketAlert(ctx context.Context, recipientEmail, subject, body string) error {
	trimmed := strings.TrimSpace(recipientEmail)
	if trimmed == "" {
		return nil
	}
	masked := config.MaskEmail(trimmed)

	if strings.TrimSpace(s.cfg.SMTPHost) == "" || strings.TrimSpace(s.cfg.SMTPUser) == "" {
		s.logger.Info("smtp not configured; ticket alert logged in local/dev mode",
			slog.String("masked_email", string(masked)),
			slog.String("subject", subject),
		)
		return nil
	}

	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		sendCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		err := s.sendRawSMTP(sendCtx, trimmed, subject, body)
		cancel()
		if err == nil {
			s.logger.Info("ticket alert email sent",
				slog.String("masked_email", string(masked)),
				slog.String("subject", subject),
				slog.Int("attempt", attempt),
			)
			return nil
		}
		lastErr = err
		time.Sleep(time.Duration(attempt*250) * time.Millisecond)
	}

	s.logger.Error("failed to send ticket alert email",
		slog.String("masked_email", string(masked)),
		slog.String("subject", subject),
		slog.String("error", lastErr.Error()),
	)
	return lastErr
}

func (s *EmailService) sendRawSMTP(ctx context.Context, to, subject, textBody string) error {
	addr := net.JoinHostPort(s.cfg.SMTPHost, strconv.Itoa(s.cfg.SMTPPort))
	dialer := &net.Dialer{Timeout: 8 * time.Second}

	var conn net.Conn
	var err error
	if s.cfg.SMTPSecure {
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config: &tls.Config{
				ServerName: s.cfg.SMTPHost,
				MinVersion: tls.VersionTLS12,
			},
		}
		conn, err = tlsDialer.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	client, err := smtp.NewClient(conn, s.cfg.SMTPHost)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if s.cfg.SMTPPass != "" {
		auth := smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPass, s.cfg.SMTPHost)
		if err := client.Auth(auth); err != nil {
			return err
		}
	}

	from := s.cfg.EmailFrom
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}

	wc, err := client.Data()
	if err != nil {
		return err
	}
	msg := fmt.Sprintf(
		"From: InstalBloc <%s>\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		from, to, subject, textBody,
	)
	if _, err := wc.Write([]byte(msg)); err != nil {
		_ = wc.Close()
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return client.Quit()
}
