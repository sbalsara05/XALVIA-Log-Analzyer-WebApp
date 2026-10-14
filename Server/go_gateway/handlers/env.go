package handlers

import (
	"database/sql"

	"cloud.google.com/go/storage"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/iamcredentials/v1"
)

// Env holds shared dependencies injected into handler functions.
type Env struct {
	GCSClient           *storage.Client
	GCSBucket           string
	ServiceAccountEmail string
	IAMService          *iamcredentials.Service
	AuthClient          *auth.Client
	DB                  *sql.DB
}
