package main

import (
	"context"
	"log"
	"os"
	"strings"

	"cloud.google.com/go/storage"
	firebase "firebase.google.com/go/v4"
	firebaseauth "firebase.google.com/go/v4/auth"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"google.golang.org/api/iamcredentials/v1"

	"ricovr/gateway/handlers"
	appconfig "ricovr/gateway/internal/config"
	appdb "ricovr/gateway/internal/db"
	"ricovr/gateway/routes"
)

func main() {
	cfg, err := appconfig.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()

	authClient, err := initFirebase(ctx, cfg)
	if err != nil {
		log.Fatalf("firebase: %v", err)
	}

	env, err := initEnv(ctx, cfg, authClient)
	if err != nil {
		log.Fatalf("env: %v", err)
	}
	defer env.GCSClient.Close()

	oob, err := handlers.NewFirebaseOOBSender(ctx, cfg.FirebaseProjectID)
	if err != nil {
		log.Fatalf("oob sender: %v", err)
	}

	db, err := appdb.Connect(cfg)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer db.Close()

	r := gin.Default()
	r.Use(corsMiddleware())

	switch cfg.ServerMode {
	case appconfig.ModeDataPlane:
		routes.MountDataPlaneRoutes(r, env, db)

	case appconfig.ModeControlPlane:
		routes.MountControlPlaneRoutes(r, db, env.AuthClient, oob)

	case appconfig.ModeUnified:
		routes.MountDataPlaneRoutes(r, env, db)
		routes.MountControlPlaneRoutes(r, db, env.AuthClient, oob)

	default:
		log.Fatalf("invalid SERVER_MODE %q — must be %s, %s, or %s", cfg.ServerMode, appconfig.ModeDataPlane, appconfig.ModeControlPlane, appconfig.ModeUnified)
	}

	log.Printf("Gateway listening on :%s (mode=%s)", cfg.Port, cfg.ServerMode)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// corsMiddleware locks allowed origins to CORS_ORIGINS env var (comma-separated).
// Falls back to http://localhost:5173 when the var is unset (local dev only).
func corsMiddleware() gin.HandlerFunc {
	raw := os.Getenv("CORS_ORIGINS")
	var origins []string
	if raw == "" {
		origins = []string{"http://localhost:5173"}
	} else {
		for _, o := range strings.Split(raw, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				origins = append(origins, trimmed)
			}
		}
	}
	log.Printf("CORS allowed origins: %v", origins)
	return cors.New(cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type"},
		AllowCredentials: false,
	})
}

func initFirebase(ctx context.Context, cfg *appconfig.Config) (*firebaseauth.Client, error) {
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: cfg.FirebaseProjectID})
	if err != nil {
		return nil, err
	}
	return app.Auth(ctx)
}

func initEnv(ctx context.Context, cfg *appconfig.Config, authClient *firebaseauth.Client) (*handlers.Env, error) {
	gcsClient, err := storage.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	iamSvc, err := iamcredentials.NewService(ctx)
	if err != nil {
		gcsClient.Close()
		return nil, err
	}

	return &handlers.Env{
		GCSClient:           gcsClient,
		GCSBucket:           cfg.GCSBucket,
		ServiceAccountEmail: cfg.ServiceAccountEmail,
		IAMService:          iamSvc,
		AuthClient:          authClient,
	}, nil
}
