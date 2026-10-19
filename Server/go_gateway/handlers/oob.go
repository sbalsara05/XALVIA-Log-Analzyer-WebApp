package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// OOBSender sends an out-of-band (OOB) email action via Firebase.
// Extracted as an interface so tests can inject a fake without a real Firebase project.
type OOBSender interface {
	SendPasswordReset(ctx context.Context, email string) error
}

// FirebaseOOBSender sends Firebase password-reset emails via the Identity Toolkit REST API
// authenticated with the service account's OAuth2 credentials (ADC).
// The service account must hold the Firebase Authentication Admin role
// (includes firebaseauth.users.sendEmail) so Firebase delivers the email directly.
type FirebaseOOBSender struct {
	projectID  string
	httpClient *http.Client
}

// NewFirebaseOOBSender constructs a sender backed by Application Default Credentials.
// Call once at startup and share the instance across handlers.
func NewFirebaseOOBSender(ctx context.Context, projectID string) (*FirebaseOOBSender, error) {
	ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/identitytoolkit")
	if err != nil {
		return nil, fmt.Errorf("oob sender: default token source: %w", err)
	}
	return &FirebaseOOBSender{
		projectID:  projectID,
		httpClient: oauth2.NewClient(context.Background(), ts),
	}, nil
}

const oobEndpoint = "https://identitytoolkit.googleapis.com/v1/accounts:sendOobCode"

// SendPasswordReset triggers Firebase to email a password-reset link to email.
// Firebase generates and delivers the message; no continueUrl is configured.
func (s *FirebaseOOBSender) SendPasswordReset(ctx context.Context, email string) error {
	payload, err := json.Marshal(map[string]interface{}{
		"requestType":     "PASSWORD_RESET",
		"email":           email,
		"returnOobLink":   false,
		"targetProjectId": s.projectID,
	})
	if err != nil {
		return fmt.Errorf("oob sender: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oobEndpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("oob sender: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("oob sender: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("oob sender: status %d: %s", resp.StatusCode, body)
	}
	return nil
}
