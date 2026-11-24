package sanitize

import (
	"regexp"
	"strings"
)

var (
	// Bearer tokens in error messages
	bearerRe = regexp.MustCompile(`Bearer\s+[\w\-\.]+`)

	// URL query params with sensitive names
	queryParamRe = regexp.MustCompile(`([?&])(token|key|secret|password|api_key|apikey|access_token|client_secret)=([^&\s]+)`)

	// Authorization header values
	authHeaderRe = regexp.MustCompile(`(?i)Authorization:\s*\S+`)

	// Recall API key patterns
	recallKeyRe = regexp.MustCompile(`cai_rcl_sdk_\w+`)
	proKeyRe    = regexp.MustCompile(`rp_(prod|plus)_[\w\-]+`)

	// X-API-Key and similar custom auth headers
	xAPIKeyRe = regexp.MustCompile(`(?i)(X-API-Key|X-Auth-Token):\s*\S+`)

	// Database connection strings with credentials
	dbConnRe = regexp.MustCompile(`(postgres|mysql|mongodb(\+srv)?|redis)://[^@\s"']+@`)
)

// Error redacts credentials from error messages for safe logging.
// Redacts: Bearer tokens, URL query params (token/key/secret/password),
// Authorization headers, and Recall API key patterns.
func Error(s string) string {
	s = bearerRe.ReplaceAllString(s, "Bearer ****")
	s = queryParamRe.ReplaceAllString(s, "${1}${2}=****")
	s = authHeaderRe.ReplaceAllString(s, "Authorization: ****")
	s = recallKeyRe.ReplaceAllString(s, "cai_rcl_sdk_****")
	s = proKeyRe.ReplaceAllString(s, "rp_****")
	s = xAPIKeyRe.ReplaceAllString(s, "${1}: ****")
	s = dbConnRe.ReplaceAllStringFunc(s, func(m string) string {
		idx := strings.Index(m, "://")
		if idx < 0 {
			return m
		}
		return m[:idx+3] + "****:****@"
	})
	return s
}

// URL redacts sensitive query parameters from URLs for safe logging.
func URL(u string) string {
	idx := strings.IndexByte(u, '?')
	if idx == -1 {
		return u
	}
	return u[:idx] + queryParamRe.ReplaceAllString(u[idx:], "${1}${2}=****")
}
