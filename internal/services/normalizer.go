package services

import (
	"fmt"
	"strings"
	"unicode"
)

var stopWords = map[string]struct{}{
	"sc":           {},
	"srl":          {},
	"sa":           {},
	"pfa":          {},
	"ii":           {},
	"if":           {},
	"snc":          {},
	"scs":          {},
	"asociatia":    {},
	"de":           {},
	"proprietari":  {},
	"as":           {},
	"pr":           {},
	"prop":         {},
}

var romanianDiacritics = strings.NewReplacer(
	"ă", "a", "Ă", "a",
	"â", "a", "Â", "a",
	"î", "i", "Î", "i",
	"ș", "s", "Ș", "s",
	"ş", "s", "Ş", "s",
	"ț", "t", "Ț", "t",
	"ţ", "t", "Ţ", "t",
)

// NormalizeEntityName strips Romanian diacritics, lowercases, and collapses non-alphanumeric separators.
func NormalizeEntityName(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	ascii := romanianDiacritics.Replace(trimmed)
	lower := strings.ToLower(ascii)

	var b strings.Builder
	lastWasSpace := true
	for _, r := range lower {
		if r == '.' {
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastWasSpace = false
		} else if !lastWasSpace && !unicode.IsMark(r) {
			b.WriteByte(' ')
			lastWasSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// NormalizeCoreCompanyName removes legal entity suffixes and association stop-words for anti-duplicate comparison.
func NormalizeCoreCompanyName(value string) string {
	base := NormalizeEntityName(value)
	if base == "" {
		return ""
	}
	tokens := strings.Fields(base)
	filtered := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if _, isStop := stopWords[tok]; !isStop {
			filtered = append(filtered, tok)
		}
	}
	if len(filtered) == 0 {
		return base
	}
	return strings.Join(filtered, " ")
}

// NormalizeFiscalCode strips the 'RO' prefix and non-digit characters from a CUI/CIF.
func NormalizeFiscalCode(value string) string {
	trimmed := strings.TrimSpace(strings.ToUpper(value))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "RO")
	var b strings.Builder
	for _, r := range trimmed {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// NormalizePhone extracts canonical Romanian phone digits (normalizing +40 / 0040 prefixes to 07xx...).
func NormalizePhone(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if strings.HasPrefix(digits, "40") && len(digits) == 11 {
		return "0" + digits[2:]
	}
	if strings.HasPrefix(digits, "0040") && len(digits) == 13 {
		return "0" + digits[4:]
	}
	return digits
}

// FormatDuplicateEntityError returns the canonical Romanian error message when an entity already exists under another email.
func FormatDuplicateEntityError(existingEmail string) error {
	return fmt.Errorf("Există deja acest utilizator/firmă înregistrat(ă) cu adresa de email: %s", strings.TrimSpace(existingEmail))
}
