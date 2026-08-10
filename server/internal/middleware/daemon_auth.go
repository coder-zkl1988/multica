package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Daemon context keys.
type daemonContextKey int

const (
	ctxKeyDaemonWorkspaceID daemonContextKey = iota
	ctxKeyDaemonID
	ctxKeyDaemonAuthPath
)

// Daemon auth path labels exposed via context for slow-log attribution.
const (
	DaemonAuthPathDaemonToken = "daemon_token"
	DaemonAuthPathPAT         = "pat"
	DaemonAuthPathCloudPAT    = "cloud_pat"
	DaemonAuthPathJWT         = "jwt"
	DaemonAuthPathService     = "service_account"
)

// DaemonWorkspaceIDFromContext returns the workspace ID set by DaemonAuth middleware.
func DaemonWorkspaceIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyDaemonWorkspaceID).(string)
	return id
}

// DaemonIDFromContext returns the daemon ID set by DaemonAuth middleware.
func DaemonIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyDaemonID).(string)
	return id
}

// DaemonAuthPathFromContext returns which token kind authenticated this
// request — "daemon_token", "cloud_pat", "jwt", or "service_account" — for telemetry.
// Empty when the request did not pass through DaemonAuth.
func DaemonAuthPathFromContext(ctx context.Context) string {
	p, _ := ctx.Value(ctxKeyDaemonAuthPath).(string)
	return p
}

// WithDaemonContext returns a new context with the daemon workspace ID and daemon ID set.
// This is used by tests to simulate daemon token authentication.
func WithDaemonContext(ctx context.Context, workspaceID, daemonID string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyDaemonWorkspaceID, workspaceID)
	ctx = context.WithValue(ctx, ctxKeyDaemonID, daemonID)
	ctx = context.WithValue(ctx, ctxKeyDaemonAuthPath, DaemonAuthPathDaemonToken)
	return ctx
}

// DaemonAuth validates daemon, internal, service-account, and cloud tokens.
//
// Both caches are optional. When non-nil:
//   - daemonCache short-circuits the daemon_token DB lookup on the mdt_ path
//
// cloudPAT is optional; when non-nil, tokens with the mcn_ prefix are
// validated by calling the Multica Cloud Fleet service (X-User-ID gets the
// returned owner_id). When nil, mcn_ tokens are rejected at the prefix
// branch — same fail-closed contract as the regular Auth middleware.
//
// Cache misses fall back to the original DB-backed behavior.
func DaemonAuth(queries *db.Queries, patCache *auth.PATCache, daemonCache *auth.DaemonTokenCache, cloudPAT *auth.CloudPATVerifier, useSySSO bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// X-Actor-Source is server-set only — strip any
			// client-supplied value before any branch can re-stamp
			// it. This mirrors what Auth middleware does (see auth.go
			// "X-Actor-Source is server-set only..." comment) and
			// keeps the contract uniform across both middlewares: a
			// downstream guard like handler.RequireHumanActor can
			// trust this header regardless of which auth path the
			// request arrived on.
			r.Header.Del("X-Actor-Source")
			r.Header.Del("X-Service-Workspace-ID")
			r.Header.Del("X-Auth-Expires-At")

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				slog.Debug("daemon_auth: missing authorization header", "path", r.URL.Path)
				writeError(w, http.StatusUnauthorized, "missing authorization header")
				return
			}

			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			if tokenString == authHeader {
				slog.Debug("daemon_auth: invalid format", "path", r.URL.Path)
				writeError(w, http.StatusUnauthorized, "invalid authorization format")
				return
			}

			if useSySSO && strings.HasPrefix(tokenString, auth.ServiceAccountTokenPrefix) {
				if queries == nil {
					writeError(w, http.StatusUnauthorized, "invalid token")
					return
				}
				serviceToken, err := queries.GetServiceAccountTokenByHash(r.Context(), auth.HashToken(tokenString))
				if err != nil {
					writeError(w, http.StatusUnauthorized, "invalid token")
					return
				}
				workspaceID := uuidToString(serviceToken.WorkspaceID)
				r.Header.Set("X-User-ID", uuidToString(serviceToken.UserID))
				r.Header.Set("X-Actor-Source", "service_account")
				r.Header.Set("X-Service-Workspace-ID", workspaceID)
				r.Header.Set("X-Workspace-ID", workspaceID)
				r.Header.Set("X-Auth-Expires-At", serviceToken.ExpiresAt.Time.UTC().Format(time.RFC3339))
				w.Header().Set("X-Auth-Expires-At", serviceToken.ExpiresAt.Time.UTC().Format(time.RFC3339))
				go queries.UpdateServiceAccountTokenLastUsed(context.Background(), serviceToken.ID)
				ctx := context.WithValue(r.Context(), ctxKeyDaemonWorkspaceID, workspaceID)
				ctx = context.WithValue(ctx, ctxKeyDaemonAuthPath, DaemonAuthPathService)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Daemon token: "mdt_" prefix.
			if strings.HasPrefix(tokenString, "mdt_") {
				hash := auth.HashToken(tokenString)

				if id, ok := daemonCache.Get(r.Context(), hash); ok {
					ctx := context.WithValue(r.Context(), ctxKeyDaemonWorkspaceID, id.WorkspaceID)
					ctx = context.WithValue(ctx, ctxKeyDaemonID, id.DaemonID)
					ctx = context.WithValue(ctx, ctxKeyDaemonAuthPath, DaemonAuthPathDaemonToken)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}

				if queries == nil {
					writeError(w, http.StatusUnauthorized, "invalid daemon token")
					return
				}
				dt, err := queries.GetDaemonTokenByHash(r.Context(), hash)
				if err != nil {
					slog.Warn("daemon_auth: invalid daemon token", "path", r.URL.Path, "error", err)
					writeError(w, http.StatusUnauthorized, "invalid daemon token")
					return
				}

				identity := auth.DaemonTokenIdentity{
					WorkspaceID: uuidToString(dt.WorkspaceID),
					DaemonID:    dt.DaemonID,
				}
				// daemon_token.expires_at is NOT NULL; pgtype Valid is true
				// in normal operation, but defend against zero just in case.
				var expiresAt time.Time
				if dt.ExpiresAt.Valid {
					expiresAt = dt.ExpiresAt.Time
				}
				daemonCache.Set(r.Context(), hash, identity, auth.TTLForExpiry(time.Now(), expiresAt))

				ctx := context.WithValue(r.Context(), ctxKeyDaemonWorkspaceID, identity.WorkspaceID)
				ctx = context.WithValue(ctx, ctxKeyDaemonID, identity.DaemonID)
				ctx = context.WithValue(ctx, ctxKeyDaemonAuthPath, DaemonAuthPathDaemonToken)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Cloud Node PAT: "mcn_" prefix. Mirrors the mcn_ branch
			// in Auth — Multica Cloud Fleet is authoritative, we only
			// surface the resolved owner_id as X-User-ID for the
			// downstream daemon handlers (which then check workspace
			// membership the usual way). Same fail-closed semantics:
			// no Fleet URL configured → 401, Fleet unreachable → 503.
			// We additionally require the owner_id to map to a real
			// local user — see the Auth comment for the rationale.
			if strings.HasPrefix(tokenString, auth.CloudPATPrefix) {
				if cloudPAT == nil {
					slog.Warn("daemon_auth: mcn_ token presented but cloud verifier not configured", "path", r.URL.Path)
					writeError(w, http.StatusUnauthorized, "invalid token")
					return
				}
				identity, err := cloudPAT.Verify(r.Context(), tokenString, ownerLookupFor(queries))
				if err != nil {
					if errors.Is(err, auth.ErrCloudPATInvalid) {
						slog.Warn("daemon_auth: cloud rejected mcn_ token", "path", r.URL.Path, "error", err)
						writeError(w, http.StatusUnauthorized, "invalid token")
						return
					}
					slog.Warn("daemon_auth: cloud pat verify unavailable", "path", r.URL.Path, "error", err)
					writeError(w, http.StatusServiceUnavailable, "cloud pat verifier unavailable")
					return
				}
				r.Header.Set("X-User-ID", identity.OwnerID)
				// Mirror the regular Auth middleware: tag the auth
				// path so any downstream guard (handler.
				// RequireHumanActor and friends) can recognize this
				// request as a machine credential rather than a
				// human PAT. Daemon routes don't currently use these
				// guards, but keeping the stamp uniform with Auth
				// avoids a future surprise where an endpoint moved
				// or shared between the two middlewares would behave
				// differently depending on which one routed it.
				r.Header.Set("X-Actor-Source", "cloud_pat")
				ctx := context.WithValue(r.Context(), ctxKeyDaemonAuthPath, DaemonAuthPathCloudPAT)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if !useSySSO && strings.HasPrefix(tokenString, "mul_") {
				hash := auth.HashToken(tokenString)
				if userID, ok := patCache.Get(r.Context(), hash); ok {
					r.Header.Set("X-User-ID", userID)
					ctx := context.WithValue(r.Context(), ctxKeyDaemonAuthPath, DaemonAuthPathPAT)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				if queries == nil {
					writeError(w, http.StatusUnauthorized, "invalid token")
					return
				}
				pat, err := queries.GetPersonalAccessTokenByHash(r.Context(), hash)
				if err != nil {
					slog.Warn("daemon_auth: invalid PAT", "path", r.URL.Path, "error", err)
					writeError(w, http.StatusUnauthorized, "invalid token")
					return
				}
				userID := uuidToString(pat.UserID)
				r.Header.Set("X-User-ID", userID)
				var expiresAt time.Time
				if pat.ExpiresAt.Valid {
					expiresAt = pat.ExpiresAt.Time
				}
				patCache.Set(r.Context(), hash, userID, auth.TTLForExpiry(time.Now(), expiresAt))
				go queries.UpdatePersonalAccessTokenLastUsed(context.Background(), pat.ID)
				ctx := context.WithValue(r.Context(), ctxKeyDaemonAuthPath, DaemonAuthPathPAT)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			var identity auth.InternalTokenIdentity
			var err error
			if useSySSO {
				identity, err = auth.ParseInternalToken(tokenString)
			} else {
				identity, err = auth.ParseLegacyJWT(tokenString)
			}
			if err != nil {
				slog.Warn("daemon_auth: invalid token", "path", r.URL.Path, "error", err)
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}
			r.Header.Set("X-User-ID", identity.UserID)
			if identity.Source != "" {
				r.Header.Set("X-Actor-Source", identity.Source)
			}
			r.Header.Set("X-Auth-Expires-At", identity.ExpiresAt.UTC().Format(time.RFC3339))
			w.Header().Set("X-Auth-Expires-At", identity.ExpiresAt.UTC().Format(time.RFC3339))
			ctx := context.WithValue(r.Context(), ctxKeyDaemonAuthPath, DaemonAuthPathJWT)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
