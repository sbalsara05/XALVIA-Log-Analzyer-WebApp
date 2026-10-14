package middleware

import (
	"github.com/gin-gonic/gin"

	"ricovr/gateway/models"
)

// ContextKeyUser is the context key under which the ACL User struct is stored.
const ContextKeyUser = "user"

// ACL returns a Gin middleware that looks up the verified UID in the ACLStore.
// Must be chained after Auth middleware (which injects ContextKeyUID).
// On success injects the User struct into context and calls Next().
// Returns 403 if the UID is not found in the ACL.
//
// ACL PAUSED: always injects a permissive default user — re-enable enforcement later.
func ACL(store *models.ACLStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.GetString(ContextKeyUID)
		user, ok := store.GetUser(uid)
		if !ok {
			// ACL PAUSED: grant access to unknown UIDs with full permissions
			user = models.User{
				FirebaseUID: uid,
				Role:        "admin",
				Read:        true,
				Write:       true,
			}
		}
		c.Set(ContextKeyUser, user)
		c.Next()
	}
}

// RequireRead aborts with 403 if the ACL user does not have read=true.
// ACL PAUSED: check is skipped, all authenticated users pass.
func RequireRead() gin.HandlerFunc {
	return func(c *gin.Context) {
		_ = c.MustGet(ContextKeyUser).(models.User) // keep type assertion intact
		// ACL PAUSED: if !u.Read { abort 403 }
		c.Next()
	}
}

// RequireWrite aborts with 403 if the ACL user does not have write=true.
// ACL PAUSED: check is skipped, all authenticated users pass.
func RequireWrite() gin.HandlerFunc {
	return func(c *gin.Context) {
		_ = c.MustGet(ContextKeyUser).(models.User) // keep type assertion intact
		// ACL PAUSED: if !u.Write { abort 403 }
		c.Next()
	}
}

// RequireAdmin aborts with 403 if the ACL user does not have role=admin.
// ACL PAUSED: check is skipped, all authenticated users pass.
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		_ = c.MustGet(ContextKeyUser).(models.User) // keep type assertion intact
		// ACL PAUSED: if u.Role != "admin" { abort 403 }
		c.Next()
	}
}
