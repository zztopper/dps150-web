package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"dps150-web/backend/internal/storage"
)

// authMethodKey is the gin.Context key authGate sets to record how a
// request was authenticated, so a later middleware on the same route
// (currently only requireAuthelia) can see it without re-deriving it from
// headers.
const authMethodKey = "api.auth.method"

// Authentication methods recorded under authMethodKey.
const (
	authMethodRemoteUser = "remote-user"
	authMethodBearer     = "bearer"
)

// authGate implements the API contract v3 / ADR-006 authentication model on
// every /api/v1/* route: a request is let through when EITHER
//   - it carries a valid `Authorization: Bearer <secret>` for a
//     non-revoked token whose scope covers the request (scope "control" for
//     mutating methods, "read" or "control" for reads), OR
//   - it carries a non-empty `Remote-User` header.
//
// Otherwise it answers 401 unauthorized; a validly-authenticated bearer
// token with insufficient scope answers 403 forbidden instead.
//
// SECURITY REQUIREMENT (ADR-006): Remote-User is trusted UNCONDITIONALLY by
// this middleware -- it assumes the header can only reach the backend after
// Authelia's forward-auth has set it. That guarantee is enforced at the
// Ingress, NOT here: the browser-facing host (dps150.r2bnj.ru) runs Authelia
// forward-auth in front of this service, while the script-facing host
// (dps150-api.r2bnj.ru) MUST run a headers-middleware that unconditionally
// strips any client-supplied Remote-User/Remote-Groups before the request
// reaches the backend. If that stripping is ever missing on a host
// reachable by untrusted clients, any caller can set Remote-User itself and
// bypass authentication entirely. This is deliberately out of the backend's
// control (see ADR-006 Consequences); it must be verified in the Helm
// chart/Ingress configuration, not in this middleware.
//
// Fail-soft note: when the token store is unavailable (storage never
// configured, or the database down) a bearer secret cannot be checked
// against anything -- there is no hash to compare it to. That is treated as
// an authentication failure (401 unauthorized), NOT service unavailability
// (503): a bearer caller gets exactly the response they would get for an
// invalid secret, so a down database can never be leveraged to bypass auth,
// and the response correctly describes a rejected credential rather than a
// server fault. A Remote-User caller is unaffected, since that path never
// touches storage.
func authGate(deps routerDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Auth off (local single-user, dev, e2e, docker-compose): the API is
		// open. Treat the caller as a trusted local user so downstream
		// requireAuthelia (token management) also lets them through.
		if !deps.authRequired {
			c.Set(authMethodKey, authMethodRemoteUser)
			c.Next()
			return
		}
		if remoteUser := strings.TrimSpace(c.GetHeader("Remote-User")); remoteUser != "" {
			c.Set(authMethodKey, authMethodRemoteUser)
			c.Next()
			return
		}

		secret, hasBearer := bearerSecret(c.GetHeader("Authorization"))
		if !hasBearer {
			writeError(c, http.StatusUnauthorized, "unauthorized",
				"missing Authorization: Bearer <token> or Remote-User")
			c.Abort()
			return
		}

		store := deps.tokens()
		var tok storage.ApiToken
		var err error
		if store == nil {
			err = storage.ErrUnavailable
		} else {
			tok, err = store.LookupToken(c.Request.Context(), secret)
		}
		if err != nil {
			// Both "storage unavailable" (nothing to validate against) and
			// "unknown/revoked secret" are, from the caller's perspective,
			// "this bearer token does not authenticate you": 401, never 503
			// (see the fail-soft note in the doc comment above).
			writeError(c, http.StatusUnauthorized, "unauthorized",
				"invalid, revoked or unverifiable bearer token")
			c.Abort()
			return
		}

		if !scopeCovers(tok.Scope, c.Request.Method) {
			writeError(c, http.StatusForbidden, "forbidden",
				"token scope does not permit this request")
			c.Abort()
			return
		}
		c.Set(authMethodKey, authMethodBearer)
		c.Next()
	}
}

// requireAuthelia additionally restricts a route to requests authGate
// authenticated via Remote-User, rejecting an otherwise-valid bearer token.
// ADR-006 requires this for token management (GET/POST/DELETE
// /api/v1/tokens): it must only be reachable through the browser UI behind
// Authelia, so a leaked or compromised token -- even scope "control" --
// can never mint or revoke further tokens. It must run after authGate on
// the same route (see router.go).
func requireAuthelia() gin.HandlerFunc {
	return func(c *gin.Context) {
		if method, _ := c.Get(authMethodKey); method != authMethodRemoteUser {
			writeError(c, http.StatusForbidden, "forbidden",
				"token management is only available through the browser UI (Authelia), not a bearer token")
			c.Abort()
			return
		}
		c.Next()
	}
}

// bearerSecret extracts the secret from an "Authorization: Bearer <secret>"
// header value. It reports ok=false for a missing/malformed header, which
// authGate treats as "no bearer attempt" (falling back to 401 unless
// Remote-User is present) rather than a distinct error, since a missing
// header is the common case, not a failure to report.
func bearerSecret(header string) (secret string, ok bool) {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	secret = strings.TrimSpace(header[len(prefix):])
	return secret, secret != ""
}

// scopeCovers reports whether scope authorizes method: mutating methods
// require storage.ScopeControl; safe methods accept storage.ScopeRead or
// storage.ScopeControl.
func scopeCovers(scope, method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return scope == storage.ScopeRead || scope == storage.ScopeControl
	default:
		return scope == storage.ScopeControl
	}
}
