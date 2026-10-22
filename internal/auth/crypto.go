package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"instalbloc/internal/domain"
)

const (
	pbkdf2Iterations = 100000
	pbkdf2KeyLen     = 32
	pbkdf2SaltBytes  = 16
	SessionMaxAgeSec = 30 * 24 * 60 * 60 // 30 days
)

var (
	ErrInvalidTokenFormat = errors.New("auth: invalid session token format")
	ErrInvalidSignature   = errors.New("auth: invalid session token signature")
	ErrExpiredSession     = errors.New("auth: session token expired")
)

// pbkdf2SHA256 derives a key from password and salt using RFC 2898 PBKDF2-HMAC-SHA256.
func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen

	var buf [4]byte
	dk := make([]byte, 0, numBlocks*hashLen)
	u := make([]byte, hashLen)

	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		binary.BigEndian.PutUint32(buf[:], uint32(block))
		prf.Write(buf[:])
		dk = prf.Sum(dk)
		t := dk[len(dk)-hashLen:]
		copy(u, t)

		for n := 2; n <= iter; n++ {
			prf.Reset()
			prf.Write(u)
			u = u[:0]
			u = prf.Sum(u)
			for x := range u {
				t[x] ^= u[x]
			}
		}
	}
	return dk[:keyLen]
}

// HashPassword produces a canonical pbkdf2$100000$<saltHex>$<derivedHex> string.
func HashPassword(password string) (string, error) {
	if len(password) < 6 {
		return "", errors.New("auth: password must be at least 6 characters")
	}
	salt := make([]byte, pbkdf2SaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: failed to read CSPRNG salt: %w", err)
	}
	derived := pbkdf2SHA256([]byte(password), salt, pbkdf2Iterations, pbkdf2KeyLen)
	return fmt.Sprintf("pbkdf2$%d$%s$%s", pbkdf2Iterations, hex.EncodeToString(salt), hex.EncodeToString(derived)), nil
}

// VerifyPassword validates a raw password against a stored pbkdf2$... hash in constant time.
func VerifyPassword(password, storedHash string) bool {
	if password == "" || storedHash == "" {
		return false
	}
	parts := strings.Split(storedHash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1000 || iterations > 1000000 {
		return false
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return false
	}
	expectedDerived, err := hex.DecodeString(parts[3])
	if err != nil || len(expectedDerived) == 0 {
		return false
	}

	actualDerived := pbkdf2SHA256([]byte(password), salt, iterations, len(expectedDerived))
	return subtle.ConstantTimeCompare(actualDerived, expectedDerived) == 1
}

// GenerateOTPCode produces a 6-digit CSPRNG code in the range [100000, 999999].
func GenerateOTPCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "", fmt.Errorf("auth: failed to generate OTP: %w", err)
	}
	code := 100000 + n.Int64()
	return strconv.FormatInt(code, 10), nil
}

// HashOTPCode computes the canonical hmac$sha256$<digest> for a 6-digit OTP code.
func HashOTPCode(code, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(strings.TrimSpace(code)))
	return "hmac$sha256$" + hex.EncodeToString(mac.Sum(nil))
}

// VerifyOTPCode compares an input OTP code against a stored hmac$sha256$<digest> in constant time.
func VerifyOTPCode(code, storedHash, secret string) bool {
	computed := HashOTPCode(code, secret)
	return hmac.Equal([]byte(computed), []byte(storedHash))
}

// SignSessionToken creates a signed uid.role.timestamp.signature session token.
func SignSessionToken(userID int64, role domain.RoleType, issuedAt int64, secret string) string {
	payload := fmt.Sprintf("%d.%s.%d", userID, string(role), issuedAt)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

// VerifySessionToken verifies the HMAC-SHA256 signature and expiration of a session token.
func VerifySessionToken(token, secret string, maxAgeSec int64) (int64, domain.RoleType, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 4 {
		return 0, "", ErrInvalidTokenFormat
	}
	uid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || uid <= 0 {
		return 0, "", ErrInvalidTokenFormat
	}
	role := domain.RoleType(parts[1])
	issuedAt, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || issuedAt <= 0 {
		return 0, "", ErrInvalidTokenFormat
	}

	payload := parts[0] + "." + parts[1] + "." + parts[2]
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(parts[3]), []byte(expectedSig)) {
		return 0, "", ErrInvalidSignature
	}

	now := time.Now().Unix()
	if maxAgeSec > 0 && (now-issuedAt > maxAgeSec || issuedAt > now+300) {
		return 0, "", ErrExpiredSession
	}

	return uid, role, nil
}
