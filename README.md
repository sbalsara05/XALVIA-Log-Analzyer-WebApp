# XALIVA Log Analyzer

![Python 3.12](https://img.shields.io/badge/Python-3.12-blue)
![Go 1.22](https://img.shields.io/badge/Go-1.22-00ADD8)
![PySide6 6.10.2](https://img.shields.io/badge/PySide6-6.10.2-41CD52)
![Cloud Run](https://img.shields.io/badge/Cloud_Run-deployed-4285F4)
![Firebase Auth](https://img.shields.io/badge/Firebase-Auth-FFCA28)

A desktop tool that reads logs from XALIVA devices over USB, securely uploads them to Google Cloud Storage through an authenticated Go API gateway, and visualises the data as interactive charts.

*[Insert UI screenshot here]*

---

## Monorepo Layout

```
XALIVA_Log_Analyzer/        ← repo root (this file lives here)
├── run.py                  Python client entry point
├── src/                    Python client source code
├── style/                  client-side QML / JavaScript UI
├── tests/                  pytest test suite for client code
├── environment.yml         Conda environment spec for client application
├── requirements.txt        Pip dependency lock for client application
├── log_analyzer.spec       PyInstaller bundle config for client application
├── admin_gui/              React Admin GUI (Control Plane web app)
└── Server/
    └── go_gateway/         Go 1.22 API gateway (Gin, Cloud Run, PostgreSQL)
```

---

## Table of Contents

1. [Getting Started](#getting-started)
2. [Directory Structure](#5-directory-structure)
3. [High-Level Design](#6-high-level-design)
   - [System Context](#6a-system-context-diagram)
   - [Layered Architecture](#6b-layered-architecture-diagram)
   - [User Interaction Sequence](#6c-generic-user-interaction-sequence)
   - [Authentication Flow](#6d-authentication-flow)
   - [USB → Cloud Sync Flow](#6e-usb--cloud-sync-flow)
   - [Log Analysis Flow](#6f-log-analysis-flow)
   - [Device Log Flow](#6g-device-log-event-log-flow)
   - [Cloud List + Download Flow](#6h-cloud-list--download-flow)
   - [CSV Export and Screenshot](#6i-csv-export-and-screenshot-flow)
4. [Go Gateway HTTP API Reference](#7-go-gateway-http-api-reference)
   - [Data Plane Routes](#7a-data-plane-routes-data_plane--unified)
   - [Control Plane Routes](#7b-control-plane-routes-control_plane--unified)
   - [Environment Variables](#7c-gateway-environment-variables)
5. [Directory-Level READMEs](#8-directory-level-readmes)

---

## Getting Started

For running the stack locally during development — conda environment, Python client, Go gateway with Docker Compose, Admin GUI, local PostgreSQL, and full stack orchestration — see [development.md](development.md).

For deploying to Google Cloud — Cloud Storage bucket, Cloud SQL, Secret Manager, IAM, Cloud Run, Firebase Hosting, Firebase Authentication, and desktop client distribution — see [cloud_deployment.md](cloud_deployment.md).

---

## 5. Directory Structure

```
XALIVA_Log_Analyzer/
├── README.md                              This file
├── run.py                                 Entry point
├── requirements.txt                       Pip dependency lock (87 packages)
├── environment.yml                        Conda environment spec (Python 3.12.11)
├── log_analyzer.spec                      PyInstaller bundle config
├── .env                                   Local env vars (not committed)
│
├── src/                                   Python application source
│   ├── main.py                            Qt bootstrap + logging setup
│   ├── constants.py                       Enums (ACTION, TAB), MSG strings, DEVICE_LOG_CODE_MAP
│   ├── dataclasses.py                     Parameter dataclasses for workers
│   ├── utils.py                           Pure helper functions
│   ├── mvc/
│   │   ├── model.py                       Data layer + worker orchestration
│   │   ├── bridge.py                      QML ↔ Python signal bridge (single QObject)
│   │   └── controller.py                  Signal wiring + OS dialog adapters
│   ├── login/
│   │   ├── auth_manager.py                Firebase REST API client (V1 + V2)
│   │   └── login_controller.py            Auth state machine
│   ├── usb_conn/
│   │   └── usbserialconnection.py         Serial protocol
│   ├── cloud_conn/
│   │   └── gateway_client.py              All gateway endpoint calls
│   ├── workflows/
│   │   └── workers.py                     17 worker classes (USB, Cloud, Auth)
│   └── disk_cache/
│       └── disk_cache.py                  Read-through disk cache
│
├── style/                                 QML / JavaScript UI
│   ├── MainWindow.qml                     Root ApplicationWindow; all runtime state
│   ├── Sidebar.qml / FilePanel.qml / LoginOverlay.qml / VisualizationPanel.qml
│   ├── Theme.qml                          Design-token singleton
│   ├── Strings.qml                        UI string constants (camelCase)
│   ├── components/                        Reusable QML primitives (XButton, XInput, FileList, …)
│   ├── logic/                             JavaScript behaviour objects
│   └── SIGNALS_REFERENCE.md              Complete signal wiring table
│
├── tests/                                 pytest test suite
│   ├── conftest.py
│   ├── test_controller.py
│   ├── test_gateway_client.py
│   ├── test_gateway_sync_worker.py
│   ├── test_login_form_clear.py
│   ├── test_auth_manager.py
│   ├── test_mfa_routing.py
│   └── integration/                       Live round-trip tests (require network)
│
├── logo/                                  SVG + ICO app assets
│
├── admin_gui/                             React Admin GUI (Control Plane web app)
│   ├── src/
│   │   ├── views/                         Page-level components (LoginView, OrgsView, UsersView, DevicesView, MfaSetupView)
│   │   ├── components/                    Shared UI primitives (AuthGuard, Sidebar, TableStates, …)
│   │   ├── contexts/AuthContext.tsx        Firebase auth state + MFA enrollment status
│   │   ├── hooks/                         useToast, useOrgSelector
│   │   ├── lib/                           Firebase client, API client, constants (MSG)
│   │   └── __tests__/                     Vitest + React Testing Library test suite
│   ├── Dockerfile                         Two-stage Node → nginx image
│   ├── docker-compose.yml                 Local dev orchestration
│   └── vite.config.ts
│
└── Server/
    └── go_gateway/                        Go API gateway
        ├── main.go                        Entry point; mode switch; CORS; Firebase + GCS + DB init
        ├── go.mod                         Go 1.22 module (gin, firebase-go/v4, cloud.google.com/go/storage)
        ├── api.yaml                       OpenAPI spec
        ├── Dockerfile                     Two-stage Alpine build → static binary
        ├── docker-compose.yml             Local dev orchestration (mounts SA key)
        ├── internal/
        │   ├── config/config.go           Typed env-var config (SERVER_MODE, DATABASE_URL, CORS_ORIGINS, …)
        │   └── db/db.go                   DB connection pool (DB_MAX_OPEN_CONNS, DB_MAX_IDLE_CONNS)
        ├── handlers/                      HTTP handler functions
        │   ├── admin.go                   CheckAdminAccess (GET /api/me/access)
        │   ├── devices.go                 ListOrgDevices, AddDevice, RemoveDevice
        │   ├── orgs.go                    Org CRUD handlers (CreateOrg, DeleteOrg, ListOrgs, UpsertOrgUser, …)
        │   ├── oob.go                     OOBSender interface + FirebaseOOBSender (invite emails on new-user creation)
        │   └── files.go / parse.go / profile.go / health.go / files_test.go
        └── middleware/                    Auth + device-access + org-admin + rate-limit
            ├── auth.go                    VerifyFirebaseJWT — Firebase JWT + TOTP MFA enforcement
            ├── device_access.go           RequireDeviceAccess — Postgres CTE ACL (read / upload)
            ├── org_admin.go               RequireOrgAdmin / IsOrgAdmin — recursive ancestor check on :org_id
            ├── super_admin.go             RequireRootOrgAdmin — root-org gate for user-deletion routes
            └── rate_limit.go              Token-bucket rate limiter
```

---

## 6. High-Level Design

### 6a. System Context Diagram

```mermaid
flowchart TD
  subgraph Desktop["Desktop (XALIVA_Log_Analyzer)"]
    style Desktop fill:#dbeafe
    App["Python App\n(PySide6 / QML)"]
  end

  Device["XALIVA Device\n(USB)"]

  subgraph Cloud["Google Cloud"]
    style Cloud fill:#dcfce7
    GW["Go Gateway\n(Cloud Run)"]
    GCS["Google Cloud Storage\n(ricovr_bucket)"]
    FB["Google Identity Platform\n(Firebase Auth)"]
  end

  App -- "USB serial\n115200 baud" --> Device
  App -- "HTTPS Bearer token\n/api/*" --> GW
  App -- "Firebase REST V1/V2\nsignIn / MFA" --> FB
  GW -- "Firebase Admin SDK\nverifyIdToken()" --> FB
  GW -- "IAM signBlob()\n+ GCS signed URLs" --> GCS
  App -- "PUT / GET\nsigned URL (15 min TTL)" --> GCS
```

---

### 6b. Layered Architecture Diagram

```mermaid
flowchart TD
  subgraph UI["QML / JavaScript UI  (style/)"]
    MW[MainWindow.qml]
    FP[FilePanel.qml]
    LO[LoginOverlay.qml]
    VP[VisualizationPanel.qml]
  end

  subgraph MVC["Python MVC  (src/mvc/)"]
    BR[Bridge\nbridge.py]
    CT[Controller\ncontroller.py]
    MD[Model\nmodel.py]
  end

  subgraph WK["Background Workers  (src/workflows/workers.py)"]
    UW[USB Workers]
    CW[Cloud Workers]
    AW[Auth Workers]
  end

  subgraph Clients["Transport Clients"]
    USB[USBSerialConnection]
    GC[GatewayClient]
    AM[AuthManager]
  end

  subgraph GW["Go Gateway  (Server/go_gateway/)"]
    MID_D[Data-plane middleware\nVerifyFirebaseJWT\nRequireDeviceAccess]
    MID_C[Control-plane middleware\nVerifyFirebaseJWT\nRequireOrgAdmin / RequireRootOrgAdmin]
    HDL[handlers/\nfiles, parse, profile\norgs, devices, admin]
  end

  subgraph Google["Google Cloud"]
    FB[Firebase Auth]
    GCS[Cloud Storage\nricovr_bucket]
  end

  UI -- "Qt signals/slots" --> BR
  BR --> CT --> MD
  MD -- "QThreadPool" --> WK
  UW --> USB
  CW --> GC
  AW --> AM
  AM -- "Firebase REST V1/V2" --> FB
  GC -- "HTTPS Bearer" --> GW
  MID_D -- "Admin SDK verifyIdToken" --> FB
  MID_C -- "Admin SDK verifyIdToken" --> FB
  HDL -- "IAM signBlob + GCS API" --> GCS
  CW -- "signed URL PUT/GET" --> GCS
```

---

### 6c. Generic User Interaction Sequence

> **Bearer token** — After login, Firebase issues a signed JSON Web Token (JWT). The Python client attaches it to every gateway request as `Authorization: Bearer <token>`. The Go Gateway verifies the signature using the Firebase Admin SDK — no network round-trip to Firebase per request. Tokens expire after 1 hour; the client re-authenticates to get a fresh one.

```mermaid
sequenceDiagram
  participant User
  participant QML as QML UI (MainWindow)
  participant Bridge as Bridge (bridge.py)
  participant Controller as Controller (controller.py)
  participant Model as Model (model.py)
  participant Worker as Worker (workers.py)
  participant GC as GatewayClient
  participant GW as Go Gateway (Cloud Run)
  participant FB as Firebase Auth
  participant GCS

  User->>QML: clicks button
  QML->>Bridge: signal: syncUsbToCloudClicked(fileList)
  Bridge->>Controller: method: sync_usb_files_to_cloud(fileList)
  Controller->>Model: method: sync_usb_files_to_cloud(fileList)
  Model->>Worker: submit: GatewaySyncToCloudWorker
  Worker->>GC: method: get_upload_signed_urls(mac_id, files)
  GC->>GW: POST /api/signed-url/upload
  GW->>FB: Admin SDK verifyIdToken()
  FB-->>GW: → UID + claims
  GW->>GW: RequireDeviceAccess CTE → check can_upload on device's org
  GW-->>GC: → {session_id, signed_urls: {file: url}}
  Worker->>GCS: PUT signed_url (per file, direct)
  GCS-->>Worker: → 200 OK
  Worker->>GC: method: commit_session(mac_id, session_id, files)
  GC->>GW: POST /api/commit
  GW->>GCS: write _COMMIT.json marker
  Worker-->>Model: signal: finished(worker_id, ok, msg, payload)
  Model-->>Bridge: signal: usbFilesSyncedToCloud(ok, msg)
  Bridge-->>QML: call: setStatus(msg, false)
  QML-->>User: status bar updates
```

---

### 6d. Authentication Flow

```mermaid
sequenceDiagram
  participant User
  participant QML as LoginOverlay QML
  participant Bridge as Bridge (bridge.py)
  participant LC as LoginController
  participant AM as AuthManager
  participant FB1 as Firebase V1 API
  participant FB2 as Firebase V2 API (TOTP MFA)

  User->>QML: enters email + password, clicks Sign In
  QML->>Bridge: signal: loginClicked(email, pw, remember)
  Bridge->>LC: method: handle_login(email, pw, remember)
  LC->>AM: submit: AuthWorker → sign_in(email, password)

  alt No MFA enrolled
    AM->>FB1: POST /v1/accounts:signInWithPassword
    FB1-->>AM: → {idToken, ...}
    AM-->>LC: → (True, "Success", {type:"login", token})
    LC-->>Bridge: method: hide_login(), set_user(email, name)
    Bridge-->>QML: call: hideLoginOverlay(), setUser()
  else MFA required
    AM->>FB1: POST /v1/accounts:signInWithPassword
    FB1-->>AM: → {error: MFA_REQUIRED, mfaPendingCredential}
    AM-->>LC: → (False, "MFA_REQUIRED", {pending_cred})
    LC-->>Bridge: method: login_show_mfa()
    Bridge-->>QML: call: showMFAPage()
    User->>QML: enters 6-digit TOTP code
    QML->>Bridge: signal: mfaVerifyClicked(code)
    Bridge->>LC: method: handle_mfa_verify(code)
    LC->>AM: submit: MFAWorker → finalize_mfa_signin(pending_cred, code)
    AM->>FB2: POST /v2/accounts/mfaSignIn:finalize
    FB2-->>AM: → {idToken, ...}
    LC-->>Bridge: method: hide_login(), set_user(email, name)
    Bridge-->>QML: call: hideLoginOverlay(), setUser()
  end
```

**Firebase V1 vs V2 endpoint reference:**

| Operation | API | Endpoint |
|-----------|-----|----------|
| Sign in (no MFA) | V1 | `POST /v1/accounts:signInWithPassword` |
| Sign up | V1 | `POST /v1/accounts:signUp` |
| MFA sign-in finalize | V2 | `POST /v2/accounts/mfaSignIn:finalize` |
| MFA enroll start | V2 | `POST /v2/accounts/mfaEnrollment:start` |
| MFA enroll finalize | V2 | `POST /v2/accounts/mfaEnrollment:finalize` |
| Send password reset | V1 | `POST /v1/accounts:sendOobCode` |

**Forgot password:** `forgotPasswordClicked` → `showForgotPasswordForm()` (QML only) → user enters email → `forgotPasswordSubmitClicked(email)` → `ForgotPasswordWorker` → `auth_manager.send_password_reset(email)` → `showForgotPasswordSuccess(email)`.

**Gateway 401 handling:** Gateway returns 401 → `_auth_required_emitted` dedup flag in Model → `authRequired` signal → `bridge.show_login_expired()` → login overlay reappears.

---

### 6e. USB → Cloud Sync Flow

```mermaid
sequenceDiagram
  participant User
  participant QML as FilePanel QML
  participant Bridge as Bridge (bridge.py)
  participant Controller as Controller (controller.py)
  participant Model as Model (model.py)
  participant Worker as GatewaySyncToCloudWorker
  participant USB as USBSerialConnection
  participant GC as GatewayClient
  participant GW as Go Gateway
  participant GCS

  User->>QML: selects files, clicks Sync to Cloud
  QML->>Bridge: signal: syncUsbToCloudClicked(fileList)
  Bridge->>Controller: method: sync_usb_files_to_cloud(fileList)
  Controller->>Model: method: sync_usb_files_to_cloud(fileList)
  Model->>Worker: submit: GatewaySyncToCloudWorker

  Note over Worker: Step 1 — ACL preflight
  Worker->>GC: method: list_files(mac_id)
  GC->>GW: GET /api/files?mac_id=X

  Note over Worker: Step 2 — read files from device
  loop for each file
    Worker->>USB: method: read_log_file(filename)
    USB-->>Worker: → bytes
  end

  Note over Worker: Memory reflash detection
  Worker->>Worker: is_memory_reflash(cloud_files, usb_files)?

  Note over Worker: Step 3 — get signed upload URLs
  Worker->>GC: method: get_upload_signed_urls(mac_id, filenames)
  GC->>GW: POST /api/signed-url/upload
  GW-->>GC: → {session_id, signed_urls}

  Note over Worker: Step 4 — upload directly to GCS
  loop for each (file, signed_url)
    Worker->>GCS: PUT signed_url (file bytes)
    GCS-->>Worker: → 200 OK
  end

  Note over Worker: Step 5 — commit session
  Worker->>GC: method: commit_session(mac_id, session_id, files)
  GC->>GW: POST /api/commit
  GW->>GCS: write _COMMIT.json
  Worker-->>Model: signal: finished(worker_id, ok, msg, payload)
  Model-->>Bridge: signal: usbFilesSyncedToCloud(ok, msg)
  Bridge-->>QML: call: setStatus(msg, false)
```

**GCS object layout:**

```
{mac_id}/{filename}                         ← legacy flat path (backward compat)
{mac_id}/{session_id}/{filename}            ← session-scoped path
{mac_id}/{session_id}/_COMMIT.json          ← commit marker; written by /api/commit
```

`ListFiles` returns only filenames from committed sessions — uncommitted uploads are invisible to readers.

---

### 6f. Log Analysis Flow

```mermaid
sequenceDiagram
  participant User
  participant QML as FilePanel / VisualizationPanel
  participant Bridge as Bridge (bridge.py)
  participant Controller as Controller (controller.py)
  participant Model as Model (model.py)
  participant Worker as AnalyzeWorker
  participant GC as GatewayClient
  participant GW as Go Gateway (parse.go)
  participant GCS

  User->>QML: double-clicks log filename
  QML->>Bridge: signal: analyzeClicked(tab_id, filename)
  Bridge->>Controller: method: _translate_and_analyze(tab_id, filename)
  Controller->>Model: method: analyze_log(tab, filename)

  alt TAB.USB
    Model->>Worker: submit: AnalyzeUSBSerialWorker
  else TAB.CLOUD
    Model->>Worker: submit: AnalyzeCloudWorker
    Worker->>GC: method: get_read_signed_url(mac_id, filename)
    GC->>GW: GET /api/signed-url/read?mac_id=X&filename=Y
    GW-->>GC: → signed_url
    Worker->>GCS: GET signed_url
    GCS-->>Worker: → bytes
  else TAB.PC
    Model->>Worker: submit: AnalyzeDiskSerialWorker
  end

  Worker->>GC: method: parse_test_log(mac_id, data)
  GC->>GW: POST /api/parse/test-log?mac_id=X
  GW-->>GC: → PlotSpec JSON
  Worker->>Worker: _build_points(plot_spec) → list[list[QPointF]]
  Worker-->>Model: signal: finished(worker_id, ok, msg, payload)
  Model-->>Bridge: signal: analysisCompleted(ok, msg, payload)
  Bridge-->>QML: call: setPlotConfig(plot_spec_json)
  QML-->>Bridge: signal: chartSeriesReady(series_refs)
  Bridge->>Bridge: inject QPointF data via shiboken6
  QML-->>User: interactive chart rendered
```

**PlotSpec JSON shape** (from `Server/go_gateway/handlers/parse.go`):

```json
{
  "success": true,
  "title": "Test Log — <filename>",
  "legend": {"visible": true, "position": "bottom"},
  "x_axis": {"label": "Time", "unit": "s"},
  "y_axes": [{"label": "Voltage", "unit": "V", "min": 0, "max": 5}],
  "series": [
    {"label": "Vbatt", "data": [...], "color": "#...", "y_axis": 0,
     "line_style": "solid", "line_width": 1.5, "marker_style": "none"}
  ],
  "x_values": [0.0, 0.01, 0.02, ...],
  "frequency": 100.0
}
```

---

### 6g. Device Log (Event Log) Flow

```mermaid
sequenceDiagram
  participant User
  participant QML as FilePanel
  participant Bridge as Bridge (bridge.py)
  participant Controller as Controller (controller.py)
  participant Model as Model (model.py)
  participant Worker as ReadUSBDeviceLogFile
  participant Cache as DiskCache
  participant USB as USBSerialConnection
  participant GC as GatewayClient
  participant GW as Go Gateway

  User->>QML: clicks *_devicelog.txt
  QML->>Bridge: signal: analyzeClicked(TAB.USB, filename)
  Bridge->>Controller: method: _translate_and_analyze(TAB.USB, filename)
  Controller->>Model: method: analyze_log(USB, filename)
  Model->>Worker: submit: ReadUSBDeviceLogFile

  Note over Worker: Cache-aside read
  Worker->>Cache: has(CacheKey("usb", filename))?
  alt cache hit
    Cache-->>Worker: → bytes
  else cache miss
    Worker->>USB: method: read_log_file(filename)
    USB-->>Worker: → raw bytes
    Worker->>Cache: put_bytes(key, bytes)
  end

  Worker->>GC: method: parse_device_log(mac_id, bytes)
  GC->>GW: POST /api/parse/device-log?mac_id=X
  GW-->>GC: → [{key, val}, ...]
  Worker-->>Model: signal: finished(worker_id, ok, msg, payload)
  Model-->>Bridge: signal: usbDeviceLogParsed(ok, entries)
  Bridge-->>QML: call: setDeviceInfo(entries)
  QML-->>User: VisualizationPanel shows event table
```

**Device log opcodes decoded by the gateway:**

| Opcode | Event |
|--------|-------|
| `0x01` | Bootup — `decodeStatusWord()` + `decodePostfailByte()` |
| `0x02` | Splash screen shown |
| `0x03` | Memory check |
| `0x04` | Run test started |
| `0x05` | Settings loaded |
| `0x06` | File vault entry |
| `0x07` | Battery status — `decodeBatteryByte()` → percentage |
| `0x08` | Test result |
| `0x09` | Config applied |
| `0x0F` | Shutdown |

---

### 6h. Cloud List + Download Flow

```mermaid
sequenceDiagram
  participant User
  participant QML as FilePanel
  participant Bridge as Bridge (bridge.py)
  participant Controller as Controller (controller.py)
  participant Model as Model (model.py)
  participant GC as GatewayClient
  participant GW as Go Gateway
  participant GCS

  User->>QML: enters MAC ID, clicks List Cloud Files
  QML->>Bridge: signal: listCloudFilesClicked(mac_id)
  Bridge->>Controller: method: list_cloud_files(mac_id)
  Controller->>Model: method: list_cloud_files(mac_id)
  Model->>GC: submit: GatewayWorker → list_files(mac_id)
  GC->>GW: GET /api/files?mac_id=X
  GW->>GCS: list objects with prefix {mac_id}/
  GW->>GW: deduplicate basenames, latest committed session wins
  GW-->>GC: → ["file1.txt", "file2.txt", ...]
  Model-->>Bridge: signal: cloudFileNamesListed(ok, msg, list)
  Bridge-->>QML: call: setCloudLogs(list)

  User->>QML: selects files, clicks Download
  QML->>Bridge: signal: downloadCloudFilesToDiskClicked(files)
  Bridge->>Controller: method: _handle_download_cloud_files_to_disk()
  Controller->>Controller: QFileDialog.getExistingDirectory() → dest_path
  Controller->>Model: method: download_cloud_files_to_disk(files, dest_path)
  Model->>GC: submit: GatewaySaveFilesWorker
  loop for each file
    GC->>GW: GET /api/signed-url/read?mac_id=X&filename=Y
    GW-->>GC: → signed_url
    GC->>GCS: GET signed_url
    GCS-->>GC: → bytes → save to dest_path
  end
  Model-->>Bridge: signal: cloudFilesDownloadedToDisk(ok, msg, result)
  Bridge-->>QML: call: showSuccess() or showWarning()
```

**`resolveObjectName` file resolution order:**
1. Probe legacy flat path `{mac_id}/{filename}` — if exists, return it.
2. List all objects matching `{mac_id}/*/{filename}`.
3. Keep only those whose session has a `_COMMIT.json` marker.
4. Return object from the lexicographically latest `session_id`.

---

### 6i. CSV Export and Screenshot Flow

```
User clicks Export CSV
  → bridge.exportCsvClicked → Controller._handle_export_csv()
  → QFileDialog.getSaveFileName() → path.csv
  → reconstruct rows from bridge._last_plot_spec → write CSV
  → bridge.show_success(MSG.Export.EXPORTED.format(path))

User clicks Screenshot
  → bridge.screenshotClicked → Controller._handle_screenshot_clicked()
  → QFileDialog.getSaveFileName() → path.png
  → grab VisualizationPanel chart → QImage → save PNG
  → bridge.show_success(MSG.Export.SCREENSHOT_SAVED.format(path))
```

---

## 7. Go Gateway HTTP API Reference

The gateway runs in one of three modes (`SERVER_MODE`): `DATA_PLANE`, `CONTROL_PLANE`, or `UNIFIED`. Routes are mounted per mode.

### 7a. Data Plane Routes (`DATA_PLANE` / `UNIFIED`)

| Method | Path | Auth | ACL gate | Handler | Description |
|--------|------|------|----------|---------|-------------|
| `GET` | `/health` | None | None | `Health()` | Liveness probe — `{"status":"ok"}` |
| `GET` | `/api/profile` | Bearer | — | `Profile()` | Returns uid and email from the `users` table |
| `GET` | `/api/files` | Bearer | RequireRead | `ListFiles()` | List committed filenames for `?mac_id=X` |
| `GET` | `/api/signed-url/read` | Bearer | RequireRead | `SignedReadURL()` | 15-min GCS GET signed URL for one file |
| `POST` | `/api/signed-url/upload` | Bearer | RequireWrite | `SignedUploadURL()` | Generate session + PUT signed URLs for file list |
| `POST` | `/api/commit` | Bearer | RequireWrite | `CommitSession()` | Write `_COMMIT.json` to atomically publish a session |
| `POST` | `/api/parse/device-log` | Bearer | RequireRead | `ParseDeviceLog()` | Decode binary event log → `[{key,val}]` |
| `POST` | `/api/parse/test-log` | Bearer | RequireRead | `ParseTestLog()` | Decode binary test log → `PlotSpec` JSON |

Middleware stack on device-scoped data-plane routes (applied in order):
1. `middleware.VerifyFirebaseJWT(authClient)` — Firebase ID token validation + TOTP MFA enforcement; 401 on invalid token, 403 with `{"code":"mfa_required"}` if TOTP second factor is absent
2. `middleware.RequireDeviceAccess(db, action)` — Postgres-backed device ACL. Admins (`role='admin'`): recursive org-tree walk, access granted if the role appears anywhere in the ancestor chain. Regular users: checked against the device's direct org only (`can_read` or `can_upload` flag required on that exact org). 403 if the check fails.

### 7b. Control Plane Routes (`CONTROL_PLANE` / `UNIFIED`)

All routes require a valid Bearer token (same JWT middleware as data plane, TOTP MFA enforced).

| Method | Path | Auth gate | Description |
|--------|------|-----------|-------------|
| `GET` | `/api/me/access` | Bearer | Returns 200 if caller has any admin role; 403 otherwise |
| `POST` | `/api/orgs` | Bearer + org admin of parent | Create a new org under `parent_org_id` |
| `GET` | `/api/orgs` | Bearer | List all orgs the caller can administer |
| `DELETE` | `/api/orgs/:org_id` | Bearer + org admin | Soft-delete an org |
| `GET` | `/api/orgs/:org_id/users` | Bearer + org admin | List active members of an org |
| `POST` | `/api/orgs/:org_id/users` | Bearer + org admin | Grant/update user permissions; creates Firebase account + sends invite email if user is new. Rate-limited: 10 req/min per IP |
| `DELETE` | `/api/orgs/:org_id/users/:user_id` | Bearer + org admin | Revoke org membership |
| `GET` | `/api/orgs/:org_id/devices` | Bearer + org admin | List active devices in an org |
| `POST` | `/api/orgs/:org_id/devices` | Bearer + org admin | Register a device to an org |
| `DELETE` | `/api/orgs/:org_id/devices/:mac_id` | Bearer + org admin | Soft-delete a device from an org |
| `DELETE` | `/api/users/:user_id` | Bearer + root-org admin | Permanently soft-delete a user account |

**Invite email flow:** When `POST /api/orgs/:org_id/users` is called for an email address that has no existing Firebase account, `UpsertOrgUser` creates the account via the Firebase Admin SDK and then calls `FirebaseOOBSender.SendPasswordResetEmail()` to deliver a set-password invite link. Existing users receive only the permission update; no email is sent.

---

### 7c. Gateway Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `SERVER_MODE` | Yes | — | `DATA_PLANE`, `CONTROL_PLANE`, or `UNIFIED` |
| `FIREBASE_PROJECT_ID` | Yes | — | Firebase / GCP project ID |
| `SERVICE_ACCOUNT_EMAIL` | Yes | — | SA email used for IAM `signBlob()` to generate GCS signed URLs |
| `GCS_BUCKET` | Yes | — | GCS bucket name |
| `DATABASE_URL` | Yes (CONTROL_PLANE / UNIFIED) | — | Postgres connection string |
| `PORT` | No | `8080` | Port the HTTP server listens on |
| `CORS_ORIGINS` | No | `http://localhost:5173` | Comma-separated list of allowed CORS origins |
| `DB_MAX_OPEN_CONNS` | No | `5` | Max open DB connections in the pool |
| `DB_MAX_IDLE_CONNS` | No | `2` | Max idle DB connections in the pool |

---

## 8. Directory-Level READMEs

| Path | What it covers |
|------|---------------|
| [`Server/go_gateway/README.md`](Server/go_gateway/README.md) | Gateway full setup, startup sequence, GCS object layout, session lifecycle |
| [`Server/go_gateway/handlers/README.md`](Server/go_gateway/handlers/README.md) | Every handler function, binary protocol docs, test catalogue |
| [`Server/go_gateway/middleware/README.md`](Server/go_gateway/middleware/README.md) | Auth, device-access, org-admin, root-org-admin, and rate-limit middleware |
| [`admin_gui/src/README.md`](admin_gui/src/README.md) | React Admin GUI — views, auth context, MFA setup, testing |
| [`src/README.md`](src/README.md) | Startup sequence, full constants reference, utils, logging |
| [`src/mvc/README.md`](src/mvc/README.md) | Bridge, Model, Controller — complete signal/slot/method tables |
| [`src/login/README.md`](src/login/README.md) | Auth state machine, Firebase endpoints, MFA enrollment vs signin |
| [`src/workflows/README.md`](src/workflows/README.md) | All 17 worker classes — inputs, outputs, failure modes |
| [`src/cloud_conn/README.md`](src/cloud_conn/README.md) | `GatewayClient` method reference, error handling, upload flow |
| [`src/usb_conn/README.md`](src/usb_conn/README.md) | Serial protocol commands, `USBSerialConnection` API, cancel pattern |
| [`src/disk_cache/README.md`](src/disk_cache/README.md) | Cache design, `CacheKey`, `read_log_using_cache`, eviction |
| [`style/README.md`](style/README.md) | QML architecture, panel/logic split, Theme, Strings, signal wiring |
| [`style/components/README.md`](style/components/README.md) | Every component's property API, signals, usage snippets |
| [`style/logic/README.md`](style/logic/README.md) | Every `*Logic.qml` object's functions, property dependencies, bridge calls |
| [`tests/README.md`](tests/README.md) | Test strategy, fixtures, per-file catalogue with mocking strategy |
