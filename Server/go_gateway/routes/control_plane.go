package routes

import (
	"database/sql"

	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"

	"ricovr/gateway/handlers"
	"ricovr/gateway/middleware"
)

// inviteRPS is the token bucket refill rate for the UpsertOrgUser (invite) endpoint.
// 10 requests per minute per IP — prevents billing attacks via shadow account spam.
const inviteRPS = 10.0 / 60.0 // 10 req/min expressed as req/sec

// MountControlPlaneRoutes registers org/user admin routes onto r.
//
// Route summary:
//
//	POST   /api/orgs                              — create org under a caller-specified parent (caller must be admin of that parent)
//	GET    /api/orgs                              — list orgs the caller can admin (full downward subtree)
//	DELETE /api/orgs/:org_id                      — org admin only, soft-delete org within caller's subtree
//	GET    /api/orgs/:org_id/users                — org admin only, list active members
//	POST   /api/orgs/:org_id/users                — org admin only, grant/update permissions (email input); creates Firebase account + sends invite if new
//	DELETE /api/orgs/:org_id/users/:user_id       — org admin only, revoke membership
//	GET    /api/orgs/:org_id/devices              — org admin only, list active devices in the org
//	POST   /api/orgs/:org_id/devices              — org admin only, register a device to the org
//	DELETE /api/orgs/:org_id/devices/:mac_id      — org admin only, soft-delete a device from the org
//	DELETE /api/users/:user_id                    — root-org admin only, soft-delete user account
func MountControlPlaneRoutes(r *gin.Engine, db *sql.DB, authClient *auth.Client, oob handlers.OOBSender) {
	api := r.Group("/api")
	api.Use(middleware.VerifyFirebaseJWT(authClient))
	{
		// Access check — any authenticated user; handler decides 200 vs 403.
		api.GET("/me/access", handlers.CheckAdminAccess(db))

		// User account management — root-org admin only (Ricovr employees).
		api.DELETE("/users/:user_id", middleware.RequireRootOrgAdmin(db), handlers.DeleteUser(db))

		orgs := api.Group("/orgs")
		{
			// Org creation — caller supplies parent_org_id; handler verifies admin of that parent.
			orgs.POST("", handlers.CreateOrg(db))
			// Org deletion — caller must be admin of the target org or any ancestor.
			orgs.DELETE("/:org_id", middleware.RequireOrgAdmin(db), handlers.DeleteOrg(db))

			// Caller's admin-subtree org listing — any authenticated user.
			orgs.GET("", handlers.ListOrgs(db))

			// Org-scoped user routes — all require org admin in the target org or any ancestor.
			orgs.GET("/:org_id/users",
				middleware.RequireOrgAdmin(db),
				handlers.ListOrgUsers(db),
			)
			orgs.POST("/:org_id/users",
				middleware.RateLimit(inviteRPS, 10),
				middleware.RequireOrgAdmin(db),
				handlers.UpsertOrgUser(db, authClient, oob),
			)
			orgs.DELETE("/:org_id/users/:user_id",
				middleware.RequireOrgAdmin(db),
				handlers.RevokeOrgUser(db),
			)

			// Org-scoped device routes — all require org admin in the target org or any ancestor.
			orgs.GET("/:org_id/devices",
				middleware.RequireOrgAdmin(db),
				handlers.ListOrgDevices(db),
			)
			orgs.POST("/:org_id/devices",
				middleware.RequireOrgAdmin(db),
				handlers.AddDevice(db),
			)
			orgs.DELETE("/:org_id/devices/:mac_id",
				middleware.RequireOrgAdmin(db),
				handlers.RemoveDevice(db),
			)
		}
	}
}
