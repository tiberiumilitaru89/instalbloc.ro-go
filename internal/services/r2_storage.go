package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"instalbloc/internal/config"
)

const (
	MaxMediaUploadBytes int64 = 10 * 1024 * 1024 // 10 MB
	r2DefaultRegion           = "auto"
	r2ServiceName             = "s3"
)

var allowedMediaMIMETypes = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"application/pdf": ".pdf",
}

// R2StorageService handles Cloudflare R2 (S3-compatible) object uploads and AWS4-HMAC-SHA256 signing
// with bounded timeouts, exponential backoff retries, and zero CGO dependencies.
type R2StorageService struct {
	cfg          *config.Config
	logger       *slog.Logger
	httpClient   *http.Client
	endpointBase string // optional override for testing
}

func NewR2StorageService(cfg *config.Config, logger *slog.Logger) *R2StorageService {
	if logger == nil {
		logger = slog.Default()
	}
	return &R2StorageService{
		cfg:    cfg,
		logger: logger,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// WithEndpointOverride configures a custom S3 endpoint URL (used for deterministic httptest verification).
func (s *R2StorageService) WithEndpointOverride(endpoint string) *R2StorageService {
	s.endpointBase = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	return s
}

// ValidateMediaFile enforces strict MIME allowlist and maximum payload size (10 MB).
func ValidateMediaFile(contentType string, sizeBytes int64) (string, error) {
	if sizeBytes <= 0 {
		return "", errors.New("fișierul încărcat este gol")
	}
	if sizeBytes > MaxMediaUploadBytes {
		return "", fmt.Errorf("fișierul depășește limita maximă admisă de %d MB", MaxMediaUploadBytes/(1024*1024))
	}
	normalizedMIME := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	ext, ok := allowedMediaMIMETypes[normalizedMIME]
	if !ok {
		return "", fmt.Errorf("tipul de fișier %q nu este permis (acceptat: JPEG, PNG, WEBP, PDF)", normalizedMIME)
	}
	return ext, nil
}

// BuildSafeObjectKey generates a collision-free, path-traversal-safe object key for R2 storage.
func BuildSafeObjectKey(prefix string, userID int64, contentType string) (string, error) {
	ext, ok := allowedMediaMIMETypes[strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))]
	if !ok {
		return "", errors.New("tip media neacceptat")
	}
	cleanPrefix := strings.Trim(path.Clean(strings.TrimSpace(prefix)), "/.")
	if cleanPrefix == "" || strings.Contains(cleanPrefix, "..") {
		cleanPrefix = "tickets"
	}
	var randBytes [8]byte
	_, _ = rand.Read(randBytes[:])
	stamp := time.Now().UTC().Format("20060102T150405Z")
	return fmt.Sprintf("%s/u%d/%s-%s%s", cleanPrefix, userID, stamp, hex.EncodeToString(randBytes[:]), ext), nil
}

// GeneratePresignedPutURL constructs an AWS Signature Version 4 (AWS4-HMAC-SHA256) presigned PUT URL for Cloudflare R2.
func (s *R2StorageService) GeneratePresignedPutURL(objectKey string, expires time.Duration, now time.Time) (string, error) {
	cleanKey := strings.TrimLeft(path.Clean(objectKey), "/")
	if cleanKey == "" || strings.Contains(cleanKey, "..") {
		return "", errors.New("cheie obiect R2 invalidă")
	}
	if expires <= 0 || expires > 24*time.Hour {
		expires = 15 * time.Minute
	}

	if strings.TrimSpace(s.cfg.R2AccountID) == "" || strings.TrimSpace(s.cfg.R2AccessKeyID) == "" || strings.TrimSpace(s.cfg.R2SecretKey) == "" {
		basePublic := strings.TrimRight(s.cfg.R2PublicURL, "/")
		if basePublic == "" {
			basePublic = "https://media.instalbloc.ro"
		}
		return fmt.Sprintf("%s/%s?local_dev_presigned=1", basePublic, cleanKey), nil
	}

	utcNow := now.UTC()
	amzDate := utcNow.Format("20060102T150405Z")
	dateStamp := utcNow.Format("20060102")

	host := fmt.Sprintf("%s.r2.cloudflarestorage.com", s.cfg.R2AccountID)
	bucket := strings.TrimSpace(s.cfg.R2BucketName)
	if bucket == "" {
		bucket = "instalbloc-media"
	}
	canonicalURI := fmt.Sprintf("/%s/%s", bucket, cleanKey)
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, r2DefaultRegion, r2ServiceName)

	query := url.Values{}
	query.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	query.Set("X-Amz-Credential", fmt.Sprintf("%s/%s", s.cfg.R2AccessKeyID, credentialScope))
	query.Set("X-Amz-Date", amzDate)
	query.Set("X-Amz-Expires", fmt.Sprintf("%d", int(expires.Seconds())))
	query.Set("X-Amz-SignedHeaders", "host")

	canonicalQueryString := query.Encode()
	canonicalHeaders := fmt.Sprintf("host:%s\n", host)
	canonicalRequest := strings.Join([]string{
		http.MethodPut,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")

	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := deriveAWS4SigningKey(s.cfg.R2SecretKey, dateStamp, r2DefaultRegion, r2ServiceName)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	return fmt.Sprintf("https://%s%s?%s&X-Amz-Signature=%s", host, canonicalURI, canonicalQueryString, signature), nil
}

// UploadObject uploads media bytes to Cloudflare R2 using AWS4-HMAC-SHA256 signing and exponential backoff retry.
func (s *R2StorageService) UploadObject(ctx context.Context, objectKey string, contentType string, payload []byte) (string, error) {
	if _, err := ValidateMediaFile(contentType, int64(len(payload))); err != nil {
		return "", err
	}
	cleanKey := strings.TrimLeft(path.Clean(objectKey), "/")
	if cleanKey == "" || strings.Contains(cleanKey, "..") {
		return "", errors.New("cheie obiect R2 invalidă")
	}

	basePublic := strings.TrimRight(strings.TrimSpace(s.cfg.R2PublicURL), "/")
	if basePublic == "" {
		basePublic = "https://media.instalbloc.ro"
	}
	publicURL := fmt.Sprintf("%s/%s", basePublic, cleanKey)

	// Graceful local/dev degradation when R2 credentials are not configured
	if s.endpointBase == "" && (strings.TrimSpace(s.cfg.R2AccountID) == "" || strings.TrimSpace(s.cfg.R2AccessKeyID) == "" || strings.TrimSpace(s.cfg.R2SecretKey) == "") {
		s.logger.Info("r2 storage in local/dev fallback mode",
			slog.String("object_key", cleanKey),
			slog.Int("size_bytes", len(payload)),
		)
		return publicURL, nil
	}

	bucket := strings.TrimSpace(s.cfg.R2BucketName)
	if bucket == "" {
		bucket = "instalbloc-media"
	}

	var targetURL string
	var hostHeader string
	canonicalURI := fmt.Sprintf("/%s/%s", bucket, cleanKey)
	if s.endpointBase != "" {
		parsed, err := url.Parse(s.endpointBase)
		if err != nil {
			return "", err
		}
		hostHeader = parsed.Host
		targetURL = s.endpointBase + canonicalURI
	} else {
		hostHeader = fmt.Sprintf("%s.r2.cloudflarestorage.com", s.cfg.R2AccountID)
		targetURL = fmt.Sprintf("https://%s%s", hostHeader, canonicalURI)
	}

	payloadHash := sha256Hex(payload)
	var lastErr error

	for attempt := 1; attempt <= 2; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		utcNow := time.Now().UTC()
		amzDate := utcNow.Format("20060102T150405Z")
		dateStamp := utcNow.Format("20060102")
		credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, r2DefaultRegion, r2ServiceName)

		canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n",
			contentType, hostHeader, payloadHash, amzDate)
		signedHeaders := "content-type;host;x-amz-content-sha256;x-amz-date"

		canonicalRequest := strings.Join([]string{
			http.MethodPut,
			canonicalURI,
			"",
			canonicalHeaders,
			signedHeaders,
			payloadHash,
		}, "\n")

		stringToSign := strings.Join([]string{
			"AWS4-HMAC-SHA256",
			amzDate,
			credentialScope,
			sha256Hex([]byte(canonicalRequest)),
		}, "\n")

		signingKey := deriveAWS4SigningKey(s.cfg.R2SecretKey, dateStamp, r2DefaultRegion, r2ServiceName)
		signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))
		authHeader := fmt.Sprintf(
			"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
			s.cfg.R2AccessKeyID, credentialScope, signedHeaders, signature,
		)

		req, err := http.NewRequestWithContext(reqCtx, http.MethodPut, targetURL, bytes.NewReader(payload))
		if err != nil {
			cancel()
			return "", err
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("X-Amz-Content-Sha256", payloadHash)
		req.Header.Set("X-Amz-Date", amzDate)
		req.Header.Set("Authorization", authHeader)

		resp, doErr := s.httpClient.Do(req)
		if doErr == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			cancel()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return publicURL, nil
			}
			lastErr = fmt.Errorf("r2 upload unexpected status %d", resp.StatusCode)
		} else {
			cancel()
			lastErr = doErr
		}
		time.Sleep(time.Duration(attempt*200) * time.Millisecond)
	}

	s.logger.Error("r2 object upload failed after retries",
		slog.String("object_key", cleanKey),
		slog.String("error", lastErr.Error()),
	)
	return "", lastErr
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func deriveAWS4SigningKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}
