package middleware

import (
	"context"
	"log"
	"net/http"
	"strings"

	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
)

// ContextKeyUID is the context key under which the verified Firebase UID is stored.
const ContextKeyUID = "uid"

// 401/403 error codes returned in the JSON body so clients can distinguish causes.
const (
	// ErrCodeTokenMissing is returned when the Authorization header is absent or malformed.
	ErrCodeTokenMissing = "token_missing"
	// ErrCodeTokenExpired is returned when Firebase confirms the token is past its exp claim.
	ErrCodeTokenExpired = "token_expired"
	// ErrCodeTokenInvalid is returned for any other verification failure (bad signature, wrong project, etc.).
	ErrCodeTokenInvalid = "token_invalid"
	// ErrCodeMFARequired is returned when the user authenticated without completing TOTP MFA.
	ErrCodeMFARequired = "mfa_required"
)

// TokenVerifier is satisfied by *auth.Client and allows test doubles to be injected.
type TokenVerifier interface {
	VerifyIDToken(ctx context.Context, idToken string) (*auth.Token, error)
}

// VerifyFirebaseJWT returns a Gin middleware that validates a Firebase Bearer token
// and enforces TOTP MFA completion for all sign-in providers.
// On success it injects the verified UID into the context under "uid" and calls Next().
// On failure it aborts with 401/403 and a machine-readable "code" field so clients can
// distinguish an expired token (show "session expired") from any other auth failure.
func VerifyFirebaseJWT(client TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authorization header missing or malformed",
				"code":  ErrCodeTokenMissing,
			})
			return
		}

		idToken := strings.TrimPrefix(header, "Bearer ")
		token, err := client.VerifyIDToken(c.Request.Context(), idToken)
		if err != nil {
			code := ErrCodeTokenInvalid
			if auth.IsIDTokenExpired(err) {
				code = ErrCodeTokenExpired
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthorized",
				"code":  code,
			})
			return
		}

		// Enforce TOTP MFA for all providers. Real Firebase JWTs always carry a
		// "firebase" claim containing "sign_in_second_factor" when TOTP was completed.
		// Absence of the claim or an empty second factor is treated as fail-closed.
		fbClaims, hasClaim := token.Claims["firebase"].(map[string]interface{})
		var secondFactor string
		if hasClaim {
			secondFactor, _ = fbClaims["sign_in_second_factor"].(string)
		}
		if secondFactor == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "MFA required",
				"code":  ErrCodeMFARequired,
			})
			return
		}

		c.Set(ContextKeyUID, token.UID)
		log.Printf("[DEBUG] auth: uid=%s email=%s path=%s", token.UID, token.Claims["email"], c.FullPath())
		c.Next()
	}
}
