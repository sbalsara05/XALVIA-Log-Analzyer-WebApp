# Cloud Run Migration Plan: ADC-Based Authentication

## Goal

Remove all hard-coded service account JSON key file dependencies from the Go gateway. Run on Google Cloud Run using Application Default Credentials (ADC), while keeping local Docker Compose development fully functional via a mounted key file.

---

## Problem Summary

**Current approach (breaks on Cloud Run):**
- `main.go` reads `GOOGLE_APPLICATION_CREDENTIALS` file at startup, parses `client_email` and `private_key` from JSON manually
- Firebase Admin SDK initialized with `option.WithCredentialsFile(credFile)`
- GCS client initialized with `option.WithCredentialsFile(credFile)`
- `storage.SignedURLOptions` uses `GoogleAccessID` + `PrivateKey` fields directly — requires the raw RSA key bytes in memory

**Why this fails on Cloud Run:**
- Cloud Run injects ADC via its metadata server — there is no `.json` key file on disk
- Mounting a secret JSON key file to Cloud Run is possible but is a security anti-pattern
- The `PrivateKey` field in `SignedURLOptions` cannot be sourced from ADC — it must be an explicit private key string

---

## Target Architecture

| Concern | Local (Docker Compose) | Cloud Run |
|---|---|---|
| Firebase Admin SDK auth | ADC via mounted key file | ADC via metadata server |
| GCS client auth | ADC via mounted key file | ADC via metadata server |
| Signed URL signing | IAM `signBlob` API call | IAM `signBlob` API call |
| Service account email | `SERVICE_ACCOUNT_EMAIL` env var | `SERVICE_ACCOUNT_EMAIL` env var |
| Key file | Mounted read-only volume | Not present |

---

## Changes Required

### 1. `main.go`

#### Remove
- All `os.ReadFile(credFile)` / `json.Unmarshal` logic for parsing `client_email` / `private_key`
- `option.WithCredentialsFile(credFile)` from Firebase Admin SDK initialization
- `option.WithCredentialsFile(credFile)` from GCS client initialization
- The `credFile` variable and its env var check (`GOOGLE_APPLICATION_CREDENTIALS`)
- The `sa` struct used for parsing the JSON key
- The `gcsBucket` constant moved from `main.go` into `handlers` package (or kept, but not tied to signing)

#### Add
- Read `SERVICE_ACCOUNT_EMAIL` from env var — fatal if missing
- Initialize Firebase Admin SDK: `firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID})` — no options, ADC is picked up automatically
- Initialize GCS client: `storage.NewClient(ctx)` — no options
- Initialize IAM Credentials API client: `iamcredentials.NewService(ctx)` from `google.golang.org/api/iamcredentials/v1`
- Pass `serviceAccountEmail` and `iamService` into the handlers (via a new `handlers.Env` struct or updated `NewHandler` constructor)

#### Updated env var contract
```
Required:
  FIREBASE_PROJECT_ID        — Firebase project ID (unchanged)
  SERVICE_ACCOUNT_EMAIL      — e.g. my-sa@my-project.iam.gserviceaccount.com

Optional (local dev only, consumed by ADC automatically):
  GOOGLE_APPLICATION_CREDENTIALS — path to key file; ADC reads this, app code does not
```

#### Rough new `main()` skeleton
```go
projectID := os.Getenv("FIREBASE_PROJECT_ID")
if projectID == "" { log.Fatal("FIREBASE_PROJECT_ID required") }

saEmail := os.Getenv("SERVICE_ACCOUNT_EMAIL")
if saEmail == "" { log.Fatal("SERVICE_ACCOUNT_EMAIL required") }

ctx := context.Background()

// Firebase Admin — ADC, no key file option
app, _ := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID})
authClient, _ := app.Auth(ctx)

// GCS — ADC, no key file option
gcsClient, _ := storage.NewClient(ctx)

// IAM Credentials API — for signBlob
iamSvc, _ := iamcredentials.NewService(ctx)

// Pass saEmail + iamSvc into handlers
```

---

### 2. `handlers/files.go` — Signed URL Generation

#### Remove
- All `GoogleAccessID` + `PrivateKey` fields from `storage.SignedURLOptions`
- Any direct RSA signing logic

#### Add
A `SignBytes` function that calls the IAM Credentials API `signBlob` endpoint:

```go
// signBytesFunc returns a SignBytes-compatible function backed by the IAM API.
func signBytesFunc(iamSvc *iamcredentials.Service, saEmail string) func([]byte) ([]byte, error) {
    return func(payload []byte) ([]byte, error) {
        resource := fmt.Sprintf("projects/-/serviceAccounts/%s", saEmail)
        req := &iamcredentials.SignBlobRequest{
            Payload: base64.StdEncoding.EncodeToString(payload),
        }
        resp, err := iamSvc.Projects.ServiceAccounts.SignBlob(resource, req).Do()
        if err != nil {
            return nil, err
        }
        return base64.StdEncoding.DecodeString(resp.SignedBlob)
    }
}
```

Updated `storage.SignedURLOptions`:
```go
opts := &storage.SignedURLOptions{
    GoogleAccessID: saEmail,
    SignBytes:      signBytesFunc(iamSvc, saEmail),
    Method:         "GET",  // or "PUT"
    Expires:        time.Now().Add(15 * time.Minute),
}
```

#### Handler dependency injection
The handlers currently receive `*storage.Client` and `gcsBucket` string. They also need `saEmail string` and `iamSvc *iamcredentials.Service`. Update the handler constructor or env struct:

```go
// handlers/env.go (new file)
type Env struct {
    GCSClient          *storage.Client
    GCSBucket          string
    ServiceAccountEmail string
    IAMService         *iamcredentials.Service
    ACLStore           *models.ACLStore
}
```

All signed URL handler functions (`SignedReadURL`, `SignedUploadURL`) become methods on `*Env` or accept `*Env` as a parameter.

---

### 3. `go.mod` — New Dependency

Add:
```
google.golang.org/api/iamcredentials/v1
```

This package is already transitively present via `google.golang.org/api` (already in `go.mod` at v0.187.0) — importing the sub-package requires no version bump, only an explicit import path in the source files.

Run after changes:
```bash
go mod tidy
```

---

### 4. `Dockerfile`

#### Remove
- `COPY precise-blend-348212-fbfeb8277407.json /app/key.json` line — the key file must not be baked into the image

#### Keep / Update
```dockerfile
FROM golang:alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o gateway .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/gateway .
COPY data/ ./data/
EXPOSE 8080
CMD ["./gateway"]
```

No key file, no `GOOGLE_APPLICATION_CREDENTIALS` baked in. On Cloud Run, ADC is provided by the runtime environment. Locally, Docker Compose mounts the key file.

---

### 5. `docker-compose.yml`

Update to inject all three env vars and mount the key file as a read-only volume:

```yaml
version: "3.9"
services:
  gateway:
    build: .
    ports:
      - "8080:8080"
    environment:
      FIREBASE_PROJECT_ID: ricovr-device-cloud
      SERVICE_ACCOUNT_EMAIL: <your-sa>@ricovr-device-cloud.iam.gserviceaccount.com
      GOOGLE_APPLICATION_CREDENTIALS: /run/secrets/sa_key.json
    volumes:
      - ./precise-blend-348212-fbfeb8277407.json:/run/secrets/sa_key.json:ro
```

The key file lives only on the developer's machine. ADC reads `GOOGLE_APPLICATION_CREDENTIALS` automatically — the application code never touches the file path.

---

### 6. Cloud Run Deployment

On Cloud Run no key file is involved. The service account attached to the Cloud Run revision provides ADC automatically via the metadata server.

Required Cloud Run environment variables (set via Console, `gcloud`, or Terraform):
```
FIREBASE_PROJECT_ID=ricovr-device-cloud
SERVICE_ACCOUNT_EMAIL=<cloud-run-sa>@ricovr-device-cloud.iam.gserviceaccount.com
```

Required IAM roles on the Cloud Run service account:
- `roles/iam.serviceAccountTokenCreator` — needed to call `signBlob` on itself
- `roles/storage.objectAdmin` (or scoped) — for GCS read/write
- Firebase Auth is handled server-side by the Admin SDK, no extra role needed beyond the project membership

---

## File Change Summary

| File | Action | Key Change |
|---|---|---|
| `main.go` | Modify | Remove JSON key parsing; remove `WithCredentialsFile`; add `SERVICE_ACCOUNT_EMAIL`; add IAM client |
| `handlers/files.go` | Modify | Replace `GoogleAccessID`+`PrivateKey` with `SignBytes` via IAM API |
| `handlers/env.go` | Create | New `Env` struct to carry shared dependencies into handlers |
| `Dockerfile` | Modify | Remove key file `COPY` line |
| `docker-compose.yml` | Modify | Add `SERVICE_ACCOUNT_EMAIL` env var; add volume mount for key file |
| `go.mod` / `go.sum` | Update | `go mod tidy` after adding `iamcredentials` import |

---

## Implementation Order

1. Create `handlers/env.go` with `Env` struct
2. Update `handlers/files.go` — add `signBytesFunc`, update `SignedURLOptions`, convert handlers to `Env` methods
3. Update `main.go` — remove key file logic, add IAM client, wire `Env`
4. Update `Dockerfile` — remove key file copy
5. Update `docker-compose.yml` — add env vars and volume mount
6. `go mod tidy`
7. Local smoke test via Docker Compose
8. Deploy to Cloud Run and verify signed URLs work end-to-end
