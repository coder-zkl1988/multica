package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func uuidToString(u pgtype.UUID) string { return util.UUIDToString(u) }

func rejectTemporarilyDisabledUser(w http.ResponseWriter, r *http.Request, userID, email, authPath string) bool {
	if !auth.IsTemporarilyDisabledUser(userID, email) {
		return false
	}
	slog.Warn(
		"auth: temporarily disabled user rejected",
		"path", r.URL.Path,
		"user_id", userID,
		"auth_path", authPath,
	)
	writeError(w, http.StatusForbidden, auth.TemporarilyDisabledUserError)
	return true
}

// Auth middleware validates internal, service-account, task, and cloud tokens.
// Token sources (in priority order):
//  1. Authorization: Bearer <token> header
//  2. multica_auth HttpOnly cookie — requires valid CSRF token for state-changing requests
//
// Sets X-User-ID and X-User-Email headers on the request for downstream handlers.
//
// patCache is optional; when non-nil, PAT lookups are cached with a short
// TTL (auth.AuthCacheTTL). On cache hit the middleware skips both the DB
// SELECT and the last_used_at UPDATE — last_used_at is therefore refreshed
// at most once per TTL window per token, not per request.
//
// cfSigner is optional; when non-nil, a session renewed here also gets fresh
// CloudFront cookies. It is wired into THIS middleware rather than left to
// RefreshCloudFrontCookies because renewal is what makes the two clocks
// diverge: every route group that can renew must re-sign, and only the
// renewer knows it renewed. A group that mounts Auth without the CDN
// middleware (the plugin bridge) would otherwise slide the session forward
// while leaving the CDN policy pinned to the original login.
//
// cloudPAT is optional; when non-nil, tokens with the mcn_ prefix are
// validated by calling the Multica Cloud Fleet service rather than the
// local DB. When nil (Fleet URL unset) mcn_ tokens are rejected at the
// prefix branch — we don't fall through to the mul_ / JWT paths, since
// an mcn_ string is by construction not a valid mul_ PAT or JWT.
//
// useSySSO reports whether this deployment signs in through the corporate SSO
// provider; the fork's SSO branches read it.
func Auth(queries *db.Queries, patCache *auth.PATCache, cloudPAT *auth.CloudPATVerifier, cfSigner *auth.CloudFrontSigner, useSySSO bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// X-Actor-Source is server-set only — any value supplied by
			// the client is untrusted and discarded before the auth
			// branches run. Only trusted branches below re-set it. This
			// prevents a client from forging an actor source
			// to convince a downstream handler that its request came
			// from a non-task-token path.
			r.Header.Del("X-Actor-Source")
			r.Header.Del("X-Service-Workspace-ID")
			r.Header.Del("X-Auth-Expires-At")

			// Agent identity is server-set for exactly the same reason,
			// and the rest of the codebase already assumes it (see
			// resolveActor, actor_guards.go, CreateIssue). Only the mat_
			// branch below re-stamps these from the token row.
			//
			// Without the strip, resolveActor's pair requirement was not a
			// boundary: BOTH ids are readable by any workspace member
			// (GET /api/issues/{id}/task-runs returns agent_id + task_id),
			// so a member could replay a live pair on their own JWT/PAT and
			// be resolved as that agent — and, since MUL-6951, act with the
			// authority of that run's originator. MUL-3428, reported and
			// fixed in #4313.
			r.Header.Del("X-Agent-ID")
			r.Header.Del("X-Task-ID")

			tokenString, fromCookie := extractToken(r)
			if tokenString == "" {
				slog.Debug("auth: no token found", "path", r.URL.Path)
				http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
				return
			}

			// Cookie-based auth requires CSRF validation for state-changing methods.
			if fromCookie && !auth.ValidateCSRF(r) {
				slog.Debug("auth: CSRF validation failed", "path", r.URL.Path)
				http.Error(w, `{"error":"CSRF validation failed"}`, http.StatusForbidden)
				return
			}

			if useSySSO && strings.HasPrefix(tokenString, auth.ServiceAccountTokenPrefix) {
				if queries == nil {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				serviceToken, err := queries.GetServiceAccountTokenByHash(r.Context(), auth.HashToken(tokenString))
				if err != nil {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
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
				next.ServeHTTP(w, r)
				return
			}

			// Agent task token: "mat_" prefix. Minted by the server at
			// task-claim time and injected by the daemon into the agent
			// process. Authoritative for actor identity — the bound
			// (user_id, agent_id, task_id, workspace_id) triple is
			// written into request headers here, OVERRIDING whatever the
			// client sent, so a downstream actor-resolver cannot be
			// tricked by a client that strips or forges X-Agent-ID /
			// X-Task-ID. Human-only endpoints (e.g. agent env
			// management) reject requests authenticated this way; see
			// `actorSourceFromRequest`. MUL-2600.
			if strings.HasPrefix(tokenString, "mat_") {
				if queries == nil {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				hash := auth.HashToken(tokenString)
				tt, err := queries.GetTaskTokenByHash(r.Context(), hash)
				if err != nil {
					slog.Warn("auth: invalid task token", "path", r.URL.Path, "error", err)
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				userID := uuidToString(tt.UserID)
				if rejectTemporarilyDisabledUser(w, r, userID, "", "task_token") {
					return
				}
				r.Header.Set("X-User-ID", userID)
				r.Header.Set("X-Agent-ID", uuidToString(tt.AgentID))
				r.Header.Set("X-Task-ID", uuidToString(tt.TaskID))
				r.Header.Set("X-Workspace-ID", uuidToString(tt.WorkspaceID))
				// X-Actor-Source flags the auth path so resolveActor and
				// any owner-only handler can deny without re-querying the
				// token table. The value "task_token" is the only signal
				// this header is allowed to carry — strip anything else a
				// client tried to send.
				r.Header.Set("X-Actor-Source", "task_token")
				next.ServeHTTP(w, r)
				return
			}

			// Cloud Node PAT: "mcn_" prefix. Verified by calling the
			// Multica Cloud Fleet service — Cloud (not us) is the
			// authoritative owner of the token's status and owner_id
			// binding. We never look at the local
			// personal_access_tokens table for this prefix; an mcn_
			// string is not a valid mul_ value, so falling through
			// would just be a redundant DB miss. When the verifier
			// is unconfigured (no MULTICA_CLOUD_URL) we reject
			// at this branch rather than treating the token as a
			// JWT/PAT — failing closed avoids a misconfigured prod
			// silently downgrading auth.
			//
			// After Cloud confirms the token, we also confirm that
			// the returned owner_id maps to a real local user. The
			// Cloud `owner_id` and our `users.id` share the same UUID
			// space by contract, so this is a defense in depth: a
			// missing user means the local row was deleted out from
			// under a still-active node, or something is forging
			// owner_ids — either way we must not let the request
			// pass with a phantom X-User-ID.
			if strings.HasPrefix(tokenString, auth.CloudPATPrefix) {
				if cloudPAT == nil {
					slog.Warn("auth: mcn_ token presented but cloud verifier not configured", "path", r.URL.Path)
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				identity, err := cloudPAT.Verify(r.Context(), tokenString, ownerLookupFor(queries))
				if err != nil {
					if errors.Is(err, auth.ErrCloudPATInvalid) {
						slog.Warn("auth: cloud rejected mcn_ token", "path", r.URL.Path, "error", err)
						http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
						return
					}
					// Cloud unreachable / 5xx / decode error. We surface
					// 503 so callers (CLI / daemon) can retry — a 401
					// here would tell them to throw out a valid token.
					slog.Warn("auth: cloud pat verify unavailable", "path", r.URL.Path, "error", err)
					http.Error(w, `{"error":"cloud pat verifier unavailable"}`, http.StatusServiceUnavailable)
					return
				}
				if rejectTemporarilyDisabledUser(w, r, identity.OwnerID, "", "cloud_pat") {
					return
				}
				r.Header.Set("X-User-ID", identity.OwnerID)
				// Tag the auth path so account-level guards (e.g.
				// handler.RequireHumanActor on /api/cloud-billing/*)
				// can distinguish a cloud-node machine credential
				// from a human PAT/JWT. Mirrors the mat_ branch's
				// stamp of "task_token" — both are server-set,
				// authoritative, and stripped from any client-
				// supplied value at the top of this middleware. Same
				// rationale as MUL-2600: a machine credential
				// (running agent or running cloud node) must not be
				// treated as the owner having approved an account-
				// level action.
				r.Header.Set("X-Actor-Source", "cloud_pat")
				next.ServeHTTP(w, r)
				return
			}

			if !useSySSO && strings.HasPrefix(tokenString, "mul_") {
				hash := auth.HashToken(tokenString)
				if userID, ok := patCache.Get(r.Context(), hash); ok {
					if rejectTemporarilyDisabledUser(w, r, userID, "", "pat_cache") {
						return
					}
					r.Header.Set("X-User-ID", userID)
					next.ServeHTTP(w, r)
					return
				}
				if queries == nil {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				pat, err := queries.GetPersonalAccessTokenByHash(r.Context(), hash)
				if err != nil {
					slog.Warn("auth: invalid PAT", "path", r.URL.Path, "error", err)
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				userID := uuidToString(pat.UserID)
				if rejectTemporarilyDisabledUser(w, r, userID, "", "pat") {
					return
				}
				r.Header.Set("X-User-ID", userID)
				var expiresAt time.Time
				if pat.ExpiresAt.Valid {
					expiresAt = pat.ExpiresAt.Time
				}
				patCache.Set(r.Context(), hash, userID, auth.TTLForExpiry(time.Now(), expiresAt))
				go queries.UpdatePersonalAccessTokenLastUsed(context.Background(), pat.ID)
				next.ServeHTTP(w, r)
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
				slog.Warn("auth: invalid token", "path", r.URL.Path, "error", err)
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}
			if rejectTemporarilyDisabledUser(w, r, identity.UserID, identity.Email, "jwt") {
				return
			}
			r.Header.Set("X-User-ID", identity.UserID)
			if identity.Source != "" {
				r.Header.Set("X-Actor-Source", identity.Source)
			}
			r.Header.Set("X-Auth-Expires-At", identity.ExpiresAt.UTC().Format(time.RFC3339))
			w.Header().Set("X-Auth-Expires-At", identity.ExpiresAt.UTC().Format(time.RFC3339))
			if identity.Email != "" {
				r.Header.Set("X-User-Email", identity.Email)
			}

			// Sliding session: a browser that keeps using the app keeps its
			// cookie, instead of being logged out on the anniversary of its
			// login. No-op until the session drops below half its TTL
			// (MUL-7436).
			r = renewCookieSession(w, r, identity.Claims, fromCookie, cfSigner)

			next.ServeHTTP(w, r)
		})
	}
}

// extractToken returns the bearer token and whether it came from a cookie.
// Priority: Authorization header > multica_auth cookie.
func extractToken(r *http.Request) (token string, fromCookie bool) {
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString != authHeader {
			return tokenString, false
		}
	}

	if cookie, err := r.Cookie(auth.AuthCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, true
	}

	return "", false
}
