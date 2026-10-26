package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"ricovr/gateway/handlers"
)

// TestDataPlane_NewRoutesMountedBehindAuth checks the permissions and metadata
// routes exist and reject unauthenticated calls (401, not 404).
func TestDataPlane_NewRoutesMountedBehindAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	MountDataPlaneRoutes(r, &handlers.Env{}, nil)

	for _, path := range []string{"/api/me/permissions", "/api/files/metadata?mac_id=AABBCCDDEEFF"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without token: want 401, got %d", path, w.Code)
		}
	}
}
