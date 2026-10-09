package codex

import (
	"fmt"
	"strings"
	"unicode"
)

// CredentialFileName returns the filename used to persist Codex OAuth credentials.
// The account hash is included when available to keep accounts with the same email
// and plan distinct. The legacy email-based format remains the fallback.
//
// The email and the account hash are untrusted: the email comes from the "email"
// claim of a caller-supplied Codex ID token. Both are sanitized here so the result
// is always a bare file name that cannot escape the credentials directory when it
// is joined onto it.
func CredentialFileName(email, planType, hashAccountID string, includeProviderPrefix bool) string {
	return formatCredentialFileName(sanitizeFileNameComponent(email), planType, sanitizeFileNameComponent(hashAccountID), includeProviderPrefix)
}

// unsanitizedCredentialFileName returns the name CredentialFileName produced before
// it sanitized its components, so that credentials saved then can still be found.
func unsanitizedCredentialFileName(email, planType, hashAccountID string) string {
	return formatCredentialFileName(strings.TrimSpace(email), planType, strings.TrimSpace(hashAccountID), true)
}

func formatCredentialFileName(email, planType, hashAccountID string, includeProviderPrefix bool) string {
	plan := normalizePlanTypeForFilename(planType)

	prefix := ""
	if includeProviderPrefix {
		prefix = "codex"
	}

	if hashAccountID != "" {
		if plan == "" {
			return fmt.Sprintf("%s-%s-%s.json", prefix, hashAccountID, email)
		}
		return fmt.Sprintf("%s-%s-%s-%s.json", prefix, hashAccountID, email, plan)
	}
	if plan == "" {
		return fmt.Sprintf("%s-%s.json", prefix, email)
	}
	return fmt.Sprintf("%s-%s-%s.json", prefix, email, plan)
}

// forbiddenFileNameChars are the path separators and the other characters that
// Windows does not allow in a file name.
const forbiddenFileNameChars = `/\<>:"|?*`

// sanitizeFileNameComponent makes an untrusted string safe to embed in a file
// name: the characters in forbiddenFileNameChars and control characters become
// "_". Every other character is kept, so a credential saved before this check
// keeps its file name unless it contained one of them.
func sanitizeFileNameComponent(value string) string {
	value = strings.TrimSpace(value)
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(forbiddenFileNameChars, r) || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, value)
}

func normalizePlanTypeForFilename(planType string) string {
	planType = strings.TrimSpace(planType)
	if planType == "" {
		return ""
	}

	parts := strings.FieldsFunc(planType, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(parts) == 0 {
		return ""
	}

	for i, part := range parts {
		parts[i] = strings.ToLower(strings.TrimSpace(part))
	}
	return strings.Join(parts, "-")
}
