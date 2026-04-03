package client

import (
	"fmt"
)

// APIError represents an error response from the WireLog API.
type APIError struct {
	StatusCode int
	Message    string
	RetryAfter string // from Retry-After header, if present
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("API error %d", e.StatusCode)
}

// IsAuthError returns true if the error is a 401 or 403.
func (e *APIError) IsAuthError() bool {
	return e.StatusCode == 401 || e.StatusCode == 403
}

// IsRateLimit returns true if the error is a 429.
func (e *APIError) IsRateLimit() bool {
	return e.StatusCode == 429
}

// AuthHint returns a human-readable suggestion for fixing auth errors
// based on the key type needed for the operation.
func AuthHint(operation string) string {
	switch operation {
	case "query":
		return "This operation requires a secret key (sk_) or access token (aat_) with query scope."
	case "track", "identify":
		return "This operation requires a public key (pk_), secret key (sk_), or access token (aat_) with track scope."
	case "admin":
		return "This operation requires an org admin key (ak_). Find it in your org settings."
	case "gdpr":
		return "This operation requires a secret key (sk_) or access token (aat_) with admin scope."
	default:
		return "Check your API key and ensure it has the required scope."
	}
}
