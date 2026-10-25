package routes

import (
	"database/sql"

	"github.com/gin-gonic/gin"

	"ricovr/gateway/handlers"
	"ricovr/gateway/middleware"
)

// MountDataPlaneRoutes registers all device/log/file routes onto r.
//
// Device-scoped routes use RequireDeviceAccess with the correct action so that
// permission flags are enforced at the middleware layer:
//   - "read"   → checks can_read  (or role='admin')
//   - "upload" → checks can_upload (or role='admin')
//
// This prevents IDOR and ensures permissions are strictly bound to the
// specific org_users row for the requesting user.
func MountDataPlaneRoutes(r *gin.Engine, env *handlers.Env, db *sql.DB) {
	r.GET("/health", handlers.Health)

	api := r.Group("/api")
	api.Use(middleware.VerifyFirebaseJWT(env.AuthClient))
	{
		// No device context — no device ACL needed.
		api.GET("/profile", handlers.Profile(db))
		// Caller's own memberships and effective device access.
		api.GET("/me/permissions", handlers.MyPermissions(db))

		// ── Read-only device routes ──────────────────────────────────────────
		readRoutes := api.Group("/")
		readRoutes.Use(middleware.RequireDeviceAccess(db, "read"))
		{
			readRoutes.GET("/files", handlers.ListFiles(env.GCSClient, env.GCSBucket))
			readRoutes.GET("/signed-url/read", handlers.SignedReadURL(env))
			readRoutes.POST("/parse/device-log", handlers.ParseDeviceLog())
			readRoutes.POST("/parse/test-log", handlers.ParseTestLog())
		}

		// ── Upload / commit routes ───────────────────────────────────────────
		uploadRoutes := api.Group("/")
		uploadRoutes.Use(middleware.RequireDeviceAccess(db, "upload"))
		{
			uploadRoutes.POST("/signed-url/upload", handlers.SignedUploadURL(env))
			uploadRoutes.POST("/commit", handlers.CommitSession(env.GCSClient, env.GCSBucket))
		}
	}
}
