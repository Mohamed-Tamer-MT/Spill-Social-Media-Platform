package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
)

var (
	ErrInvalidEmail = errors.New("invalid email address format")
	ErrInvalidCode  = errors.New("invalid code format")
)

// GenerateOTP produces a secure random 6-digit numeric code.
func GenerateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", fmt.Errorf("failed to generate random OTP: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// HashOTP computes the HMAC-SHA256 hex digest of the OTP code using the server key.
// Storing an HMAC rather than plain SHA-256 protects against precomputed rainbow table attacks
// over the 1,000,000 (6-digit) search space.
func HashOTP(code string, hmacKey []byte) string {
	h := hmac.New(sha256.New, hmacKey)
	h.Write([]byte(code))
	return hex.EncodeToString(h.Sum(nil))
}

// VerifyOTP checks the presented OTP code against the expected hex hash in constant time.
func VerifyOTP(code string, hmacKey []byte, expectedHashHex string) bool {
	actualHash := HashOTP(code, hmacKey)
	return subtle.ConstantTimeCompare([]byte(actualHash), []byte(expectedHashHex)) == 1
}

// NormalizeEmail parses, trims and normalizes an email address.
func NormalizeEmail(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", ErrInvalidEmail
	}
	// Also ensure no internal whitespace and basic domain structure
	normalized := strings.ToLower(addr.Address)
	if !strings.Contains(normalized, "@") || !strings.Contains(normalized, ".") {
		return "", ErrInvalidEmail
	}
	return normalized, nil
}
