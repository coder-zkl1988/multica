package middleware

import (
	"net/http"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

// RefreshCloudFrontCookies is middleware that refreshes CloudFront signed cookies
// on authenticated requests when the cookie is missing (expired or first request
// after login). This prevents 403s from the CDN when cookies expire before the
// user's session does.
//
// Renewal-time re-signing is NOT done here — it belongs to the auth
// middleware, which is the only place that knows a session was renewed and is
// mounted on every group that can renew one. This middleware only skips its
// own work when that already happened, so a single response does not carry
// two sets of CDN cookies (MUL-7436).
func RefreshCloudFrontCookies(signer *auth.CloudFrontSigner) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if signer == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The policy is signed for the session that authenticated THIS
			// request, which the auth middleware states in X-Auth-Expires-At —
			// not for a fresh TTL, which would outlive the session and 403 every
			// asset once it lapsed. No authoritative expiry means no cookies.
			if _, err := r.Cookie("CloudFront-Policy"); err != nil && !SessionRenewed(r) {
				expiresAt, parseErr := time.Parse(time.RFC3339, r.Header.Get("X-Auth-Expires-At"))
				if parseErr == nil && time.Until(expiresAt) > 0 {
					for _, cookie := range signer.SignedCookies(expiresAt) {
						http.SetCookie(w, cookie)
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
