# DOC_PLAN.md — README Documentation Plan

## Overview

This is a **monorepo** containing both the Python desktop client and the Go API gateway:

```
<repo-root>/
├── XALIVA_Log_Analyzer/   Python desktop client (PySide6 / QML)
└── Server/go_gateway/     Go API gateway (Gin, Cloud Run)
```

The root `README.md` lives at the repo root and covers both components. Directory-level READMEs go inside each sub-tree.

**Tier 1 — Root `README.md`** (monorepo's GitHub front page)
- Developer setup for both the Python client and the Go gateway
- Non-developer architecture guide with diagrams

**Tier 2 — Directory-level `README.md` files** — Low-level design (LLD) per directory, naming real files, structs, functions, and signals

| Component | Language | Root path |
|-----------|----------|-----------|
| Desktop client | Python 3.12 + PySide6/QML | `XALIVA_Log_Analyzer/` |
| API gateway | Go 1.22 + Gin | `Server/go_gateway/` |

---

## Tier 1: Root README.md

### Target audience

- **Developers** cloning the repo for the first time (both Python and Go paths)
- **Non-technical stakeholders** who want to understand what the system does and how it is built

### Sections (in order)

---

### 1. Project Header

> **File location:** `<repo-root>/README.md` — this is the monorepo's GitHub front page, one level above both `XALIVA_Log_Analyzer/` and `Server/`.

- Short paragraph: XALIVA Log Analyzer — a desktop tool that reads diagnostic logs from XALIVA medical devices over USB, securely uploads them to Google Cloud Storage through an authenticated Go API gateway, and visualises the data as interactive charts.
- Badges: Python 3.12, Go 1.22, PySide6 6.10.2, Cloud Run, Firebase Auth
- Screenshot placeholder: *"insert UI screenshot here"*

---

### 2. Table of Contents

Anchor links to every section.

---

### 3. Python Client — Environment Setup

Source material: `requirements.txt`, `environment.yml`

#### 3a. Prerequisites
- Python 3.12 via Miniconda/Anaconda (recommended)
- `conda` CLI
- Git

#### 3b. Conda environment (recommended)
```bash
conda env create -f environment.yml
conda activate xaliva-log
```
Explain pinned versions: PySide6 6.10.2, pyserial 3.5, google-cloud-storage 3.2.0, pyinstaller 6.14.1, pytest 8.4.0.

#### 3c. pip path (alternative)
```bash
python -m venv .venv
source .venv/bin/activate          # Windows: .venv\Scripts\activate
pip install -r requirements.txt
```
Warn: conda is preferred — pip installs on some systems produce Qt platform-plugin errors.

#### 3d. Python client environment variables
Create `.env` at the repo root:
```
GO_GATEWAY_URL=http://localhost:8080      # local dev
# GO_GATEWAY_URL=https://<cloud-run-url>  # production
```
Note: `FIREBASE_API_KEY` is currently hard-coded in `src/constants.py`; a TODO exists to move it here.

---

### 4. Go Gateway Server — Environment Setup

Source material: `Server/go_gateway/go.mod`, `Server/go_gateway/.env`, `Server/go_gateway/Dockerfile`, `Server/go_gateway/docker-compose.yml`

#### 4a. Prerequisites
- Go 1.22+
- Docker + Docker Compose (for local containerised run)
- A GCP project with Firebase Authentication and GCS enabled
- A service account JSON key with the roles listed in §4e

#### 4b. Running locally with Docker Compose (recommended for dev)

```bash
cd Server/go_gateway

# Copy environment template and fill in your values
cp .env .env.local
# Edit .env.local — set FIREBASE_PROJECT_ID, SERVICE_ACCOUNT_EMAIL, GCS_BUCKET, LOCAL_KEY_PATH

docker compose --env-file .env.local up --build
# Server listens on http://localhost:8080
```

Explain what `docker-compose.yml` does:
- Builds the two-stage Alpine image
- Mounts the local GCP service account key JSON read-only at `/run/secrets/sa_key.json`
- Sets `GOOGLE_APPLICATION_CREDENTIALS=/run/secrets/sa_key.json` so ADC picks it up

#### 4c. Running locally without Docker (Go toolchain)

```bash
cd Server/go_gateway
export FIREBASE_PROJECT_ID=ricovr-device-cloud
export SERVICE_ACCOUNT_EMAIL=firebase-adminsdk-fbsvc@<project>.iam.gserviceaccount.com
export GCS_BUCKET=ricovr_bucket
export GOOGLE_APPLICATION_CREDENTIALS=./precise-blend-348212-fbfeb8277407.json
export PORT=8080

go run .
```

#### 4d. Go server environment variables

| Variable | Required | Default | Purpose |
|----------|----------|---------|---------|
| `FIREBASE_PROJECT_ID` | Yes | — | Firebase Admin SDK initialisation; fatal if absent |
| `SERVICE_ACCOUNT_EMAIL` | Yes | — | IAM Credentials API `signBlob` subject; fatal if absent |
| `GCS_BUCKET` | Yes | — | GCS bucket name for all object operations; fatal if absent |
| `PORT` | No | `8080` | HTTP listen port (Cloud Run sets this automatically) |
| `GOOGLE_APPLICATION_CREDENTIALS` | No (prod) | — | Path to service account JSON key; only needed locally |

#### 4e. Required GCP IAM roles on the service account

| Role | Why needed |
|------|-----------|
| `roles/iam.serviceAccountTokenCreator` | Signs bytes for GCS signed URLs via IAM Credentials API |
| `roles/storage.objectAdmin` | Read / write / list objects in the GCS bucket |

Firebase Auth token verification uses the Admin SDK — no extra IAM role needed.

#### 4f. ACL data files

`Server/go_gateway/data/acl.csv` — one row per user (`firebase_uid, email, organization_id, role, read, write`)
`Server/go_gateway/data/devices.csv` — one row per device (`mac_id, organization_id`)

These files are baked into the Docker image at build time (`COPY data/ ./data/`). To add a user or device, edit the CSV and rebuild the image.

> **Note:** ACL enforcement is currently paused in `middleware/acl.go` — all authenticated users receive full permissions. Per-org device isolation will be re-enabled in a future phase.

#### 4g. Running Go tests

```bash
cd Server/go_gateway
go test ./...                   # all packages
go test ./handlers/ -v -run TestListFiles   # single test
```

---

### 5. How to Run the Full Stack Locally

Bring up the Gateway first, then start the Python client:

```bash
# Terminal 1 — Gateway
cd Server/go_gateway
docker compose up --build

# Terminal 2 — Python client
cd XALIVA_Log_Analyzer
echo "GO_GATEWAY_URL=http://localhost:8080" > .env
conda activate xaliva-log
python run.py
```

Python startup sequence (high level):
1. `run.py` loads `.env`, calls `src.main.main()`
2. Qt application and QML engine initialise
3. Login overlay appears — enter Firebase email/password
4. After auth, main window loads with USB / Cloud / PC tabs

#### Building a distributable Python executable

```bash
cd XALIVA_Log_Analyzer
pyinstaller log_analyzer.spec
# Output: dist/XALIVA_Log_Analyzer  (or .exe on Windows)
```

#### Running Python tests

```bash
pytest tests/ --maxfail=1 --disable-warnings -q      # full suite
pytest tests/test_controller.py::test_name -v         # single test
pytest tests/integration/ -v                          # live tests (needs network + .env)
```

---

### 6. Production Deployment

#### 6a. Deploying the Gateway to Cloud Run

```bash
cd Server/go_gateway

# Build and push container image
gcloud builds submit --tag gcr.io/<PROJECT>/gateway .

# Deploy to Cloud Run
gcloud run deploy gateway \
  --image gcr.io/<PROJECT>/gateway \
  --set-env-vars FIREBASE_PROJECT_ID=ricovr-device-cloud \
  --set-env-vars SERVICE_ACCOUNT_EMAIL=<sa>@<project>.iam.gserviceaccount.com \
  --set-env-vars GCS_BUCKET=ricovr_bucket \
  --region us-central1 \
  --allow-unauthenticated
```

In production, `GOOGLE_APPLICATION_CREDENTIALS` is **not** set. Cloud Run provides ADC automatically via the metadata server using the Cloud Run service account.

`PORT` is injected automatically by Cloud Run; the binary reads it with `os.Getenv("PORT")`.

#### 6b. Pointing the Python client at production

Update `.env`:
```
GO_GATEWAY_URL=https://<your-cloud-run-url>/
```

Or set `GO_GATEWAY_URL` in the PyInstaller bundle's environment before launch.

---

### 7. Monorepo Directory Structure

```
<repo-root>/
├── README.md                      Root README (covers both components)
│
├── XALIVA_Log_Analyzer/           Python desktop client
├── run.py                         Entry point
├── requirements.txt               Pip dependency lock (87 packages)
├── environment.yml                Conda environment spec (Python 3.12.11)
├── log_analyzer.spec              PyInstaller bundle config
├── .env                           Local env vars (not committed)
│
├── src/                           Python application source
│   ├── main.py                    Qt bootstrap + logging setup
│   ├── constants.py               Enums (ACTION, TAB, LOG_SRC), MSG strings, DEVICE_LOG_CODE_MAP
│   ├── dataclasses.py             Parameter dataclasses for workers
│   ├── utils.py                   Pure helper functions
│   ├── mvc/                       Model-View-Controller layer
│   │   ├── model.py               Data layer + worker orchestration
│   │   ├── bridge.py              QML ↔ Python signal bridge (single QObject)
│   │   └── controller.py          Signal wiring + OS dialog adapters
│   ├── login/                     Firebase authentication
│   │   ├── auth_manager.py        Firebase REST API client (V1 + V2)
│   │   └── login_controller.py    Auth state machine
│   ├── usb_conn/                  USB serial communication
│   │   └── usbserialconnection.py CP2102 serial protocol
│   ├── cloud_conn/                Go Gateway HTTP client
│   │   └── gateway_client.py      All gateway endpoint calls
│   ├── workflows/                 QRunnable background workers
│   │   └── workers.py             18 worker classes (USB, Cloud, Auth, Analysis)
│   └── disk_cache/                File-based log cache
│       └── disk_cache.py          Read-through disk cache
│
├── style/                         QML / JavaScript UI
│   ├── MainWindow.qml             Root ApplicationWindow; all runtime state lives here
│   ├── Sidebar.qml                Navigation panel (recent files, user info, sign-out)
│   ├── FilePanel.qml              USB / Cloud / PC file browser with tabs
│   ├── VisualizationPanel.qml     Device info table, log text, interactive chart
│   ├── LoginOverlay.qml           Auth UI (login / register / MFA / forgot-password)
│   ├── Theme.qml                  Design-token singleton (colours, fonts, spacing)
│   ├── Strings.qml                UI string constants (camelCase)
│   ├── components/                Reusable QML primitives (XButton, XInput, FileList, …)
│   ├── logic/                     JavaScript behaviour separated from declarative layout
│   └── SIGNALS_REFERENCE.md       Complete signal wiring table
│
├── tests/                         pytest test suite
│   ├── conftest.py                Global fixtures
│   ├── test_controller.py         Signal wiring tests
│   ├── test_gateway_client.py     GatewayClient HTTP tests (mocked)
│   ├── test_gateway_sync_worker.py Sync worker lifecycle
│   ├── test_login_form_clear.py   QML form-reset tests (pytest-qt)
│   └── integration/               Live round-trip tests (require network)
│
├── logo/                          SVG + ICO app assets
└── logs/                          Runtime log output (gitignored)

│
└── Server/go_gateway/             Go API gateway
├── main.go                        Entry point: init Firebase/GCS/IAM, register routes
├── go.mod                         Go 1.22 module (gin, firebase-go/v4, cloud.google.com/go/storage)
├── Dockerfile                     Two-stage Alpine build → static binary
├── docker-compose.yml             Local dev orchestration (mounts SA key)
├── .env                           Local environment variables template
├── config/
│   └── acl.go                     LoadACL() + LoadDevices() — parse CSV into ACLStore
├── data/
│   ├── acl.csv                    User permissions (uid, email, org, role, read, write)
│   └── devices.csv                Device registry (mac_id, organization_id)
├── handlers/
│   ├── env.go                     Env struct (GCSClient, IAMService, ACLStore, …)
│   ├── health.go                  GET /health
│   ├── profile.go                 GET /api/profile
│   ├── files.go                   GET /api/files, signed-URL endpoints, POST /api/commit
│   ├── parse.go                   POST /api/parse/device-log + /api/parse/test-log
│   └── files_test.go              10 table-driven GCS handler tests (httptest fake GCS)
├── middleware/
│   ├── auth.go                    Firebase Bearer token validation → inject UID
│   └── acl.go                     ACLStore lookup → inject User; RequireRead/Write/Admin gates
└── models/
    └── user.go                    User, DeviceEntry, ACLStore (RWMutex-protected maps)
```

---

### 8. High-Level Design (HLD)

Target: non-developers. All diagrams use Mermaid (renders natively on GitHub).

---

#### 8a. What the system does (narrative)

A field technician connects a XALIVA device to their laptop via USB. The desktop app reads diagnostic log files from the device's internal flash memory over a serial connection, parses them into human-readable events and time-series charts, and optionally uploads them to Google Cloud Storage for long-term archival. All cloud operations go through a Go API gateway running on Google Cloud Run; the gateway verifies the user's Firebase identity token, enforces per-device ACLs, and uses short-lived GCS signed URLs so the desktop client uploads/downloads directly to storage without streaming through the server.

---

#### 8b. System Context Diagram

Mermaid `flowchart TD` with two subgraphs ("Desktop" and "Google Cloud") and four external nodes:

```
flowchart TD
  subgraph Desktop["Desktop (XALIVA_Log_Analyzer)"]
    App["Python App\n(PySide6 / QML)"]
  end

  Device["XALIVA Device\n(USB / CP2102)"]

  subgraph Cloud["Google Cloud"]
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

Colour hints for the actual diagram:
- Desktop subgraph: blue background
- Cloud subgraph: light-green background
- XALIVA Device: grey
- Firebase: orange
- GCS: orange

---

#### 8c. Layered Architecture Diagram

Mermaid `flowchart TD` with subgraphs for UI, Python MVC, Go Gateway, and Google Cloud:

```
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
    USB[USBSerialConnection\nusbconn/]
    GC[GatewayClient\ncloud_conn/]
    AM[AuthManager\nlogin/]
  end

  subgraph GW["Go Gateway  (Server/go_gateway/)"]
    MID[middleware/auth.go\nmiddleware/acl.go]
    HDL[handlers/\nfiles, parse, profile]
    ACL[config/acl.go\nACLStore + DeviceStore]
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
  MID -- "Admin SDK verifyIdToken" --> FB
  HDL -- "IAM signBlob + GCS API" --> GCS
  UW -- "signed URL PUT/GET" --> GCS
```

---

#### 8d. Generic User-Interaction Sequence

Mermaid `sequenceDiagram` showing the full journey of any button click:

Participants: User, QML UI (MainWindow), Bridge (bridge.py), Controller (controller.py), Model (model.py), Worker (workers.py), GatewayClient (gateway_client.py), Go Gateway (Cloud Run), Firebase Auth, GCS

Flow:
```
User ->> QML: clicks button
QML ->> Bridge: emit signal (e.g. syncUsbToCloudClicked)
Bridge ->> Controller: (wired via connect() in controller.__init__)
Controller ->> Model: calls @Slot method
Model ->> Worker: QThreadPool.globalInstance().start(worker)
Worker ->> GatewayClient: e.g. get_upload_signed_urls(mac_id, files)
GatewayClient ->> Go Gateway: POST /api/signed-url/upload\nAuthorization: Bearer <idToken>
Go Gateway ->> Firebase Auth: Admin SDK verifyIdToken()
Firebase Auth -->> Go Gateway: UID + claims
Go Gateway ->> Go Gateway: ACLStore.GetUser(uid) → check read/write
Go Gateway -->> GatewayClient: {session_id, signed_urls: {file: url}}
Worker ->> GCS: PUT <signed_url> (per file, direct — no gateway proxy)
GCS -->> Worker: 200 OK
Worker ->> GatewayClient: commit_session(mac_id, session_id, files)
GatewayClient ->> Go Gateway: POST /api/commit
Go Gateway ->> GCS: write _COMMIT.json marker
Worker -->> Model: emit finished(worker_id, success, msg, payload)
Model -->> Bridge: emit usbFilesSyncedToCloud(success, msg)
Bridge -->> QML: bridge._call("setStatus", [msg, False])
QML -->> User: status bar updates
```

---

#### 8e. Sub-HLD: Authentication Flow

Mermaid `sequenceDiagram`

Participants: User, LoginOverlay QML, LoginOverlayLogic.qml, LoginController, AuthManager, Firebase V1 API, Firebase V2 API (TOTP MFA)

**Login path:**
```
User ->> LoginOverlay: enters email + password, clicks Sign In
LoginOverlay ->> Bridge: emit loginClicked(email, pw, remember)
Bridge ->> LoginController: handle_login(email, pw, remember)
LoginController ->> Model: sign_in_user(email, password)
Model ->> AuthWorker: submit to QThreadPool
AuthWorker ->> AuthManager: sign_in(email, password)
AuthManager ->> Firebase V1: POST /accounts:signInWithPassword
alt No MFA enrolled
  Firebase V1 -->> AuthManager: {idToken, ...}
  AuthManager -->> AuthWorker: (True, "Success", {type:"login", token:...})
  AuthWorker -->> Model: finished(id, True, msg, payload)
  Model -->> Bridge: authFinished(True, msg, payload)
  Bridge -->> LoginController: on_auth_finished()
  LoginController -->> Bridge: hide_login(), set_user(email, name)
  Bridge -->> QML: hideLoginOverlay(), setUser()
else MFA required
  Firebase V1 -->> AuthManager: {error: MFA_REQUIRED, mfaPendingCredential, ...}
  AuthManager -->> AuthWorker: (False, "MFA_REQUIRED", {pending_cred, ...})
  AuthWorker -->> Model: finished → authFinished
  LoginController ->> Bridge: login_show_mfa(qr_url_or_empty)
  Bridge -->> QML: showMFAPage()
  User ->> LoginOverlay: enters 6-digit TOTP code
  LoginOverlay ->> Bridge: emit mfaVerifyClicked(code)
  Bridge ->> LoginController: handle_mfa_verify(code)
  LoginController ->> Model: verify_mfa_login(pending_cred, code)
  Model ->> MFAWorker: FINALIZE_SIGNIN mode
  MFAWorker ->> AuthManager: finalize_mfa_signin(pending_cred, code)
  AuthManager ->> Firebase V2: POST /accounts/mfaSignIn:finalize
  Firebase V2 -->> AuthManager: {idToken, ...}
  (same success path as above)
end
```

Also cover in this section:
- **Signup flow**: `registerClicked` → `AuthWorker` (SIGNUP) → Firebase `accounts:signUp` → auto-start TOTP enrollment (`start_mfa_enrollment` → Firebase V2 `mfaEnrollment:start`) → `mfaQrReady` signal → LoginController generates QR PNG (base64, `qrcode` lib) → QML shows QR → user scans → `mfaVerifyClicked` → `MFAWorker` FINALIZE → `accounts/mfaEnrollment:finalize` → email verification sent
- **Forgot password**: `forgotPasswordClicked` → `showForgotPasswordForm()` (QML only) → user enters email → `forgotPasswordSubmitClicked(email)` → `ForgotPasswordWorker` → `auth_manager.send_password_reset(email)` → Firebase `sendOobCode` → `showForgotPasswordSuccess(email)`
- **Gateway 401 handling**: Gateway returns 401 → `GatewayClient` raises → `_auth_required_emitted` dedup flag in Model → `authRequired` signal → Controller calls `bridge.show_login_expired()` → login overlay reappears

Show Firebase V1 vs V2 API URL split in a table:

| Operation | API version | Endpoint |
|-----------|------------|----------|
| Sign in (no MFA) | V1 | `POST /v1/accounts:signInWithPassword` |
| Sign up | V1 | `POST /v1/accounts:signUp` |
| MFA sign-in finalize | V2 | `POST /v2/accounts/mfaSignIn:finalize` |
| MFA enroll start | V2 | `POST /v2/accounts/mfaEnrollment:start` |
| MFA enroll finalize | V2 | `POST /v2/accounts/mfaEnrollment:finalize` |
| Send password reset | V1 | `POST /v1/accounts:sendOobCode` |

---

#### 8f. Sub-HLD: USB → Cloud Sync Flow

Mermaid `sequenceDiagram`

Participants: User, FilePanel QML, Model, GatewaySyncToCloudWorker, USBSerialConnection, GatewayClient, Go Gateway, GCS

```
User ->> FilePanel: selects files, clicks Sync to Cloud
FilePanel ->> Bridge: emit syncUsbToCloudClicked(file_list)
Bridge ->> Controller ->> Model: sync_usb_files_to_cloud(file_list)
Model ->> GatewaySyncToCloudWorker: start()

Note over GatewaySyncToCloudWorker: Step 1 — ACL preflight
GatewaySyncToCloudWorker ->> GatewayClient: list_files(mac_id)
GatewayClient ->> Go Gateway: GET /api/files?mac_id=X
Go Gateway ->> Go Gateway: auth + ACL check
Go Gateway -->> GatewayClient: file list (or 401/403)

Note over GatewaySyncToCloudWorker: Step 2 — read files from device
loop for each file
  GatewaySyncToCloudWorker ->> USBSerialConnection: read_log_file(filename)
  USBSerialConnection -->> GatewaySyncToCloudWorker: bytes
end

Note over GatewaySyncToCloudWorker: Memory reflash detection
GatewaySyncToCloudWorker ->> GatewaySyncToCloudWorker: is_memory_reflash(existing, new) ?
alt reflash detected
  GatewaySyncToCloudWorker -->> Model: finished(False, MSG.USB.MEMORY_REFLASH)
end

Note over GatewaySyncToCloudWorker: Step 3 — get signed upload URLs
GatewaySyncToCloudWorker ->> GatewayClient: get_upload_signed_urls(mac_id, filenames)
GatewayClient ->> Go Gateway: POST /api/signed-url/upload {mac_id, files:[...]}
Go Gateway ->> Go Gateway: generate session_id (timestamp+nanos)
Go Gateway ->> GCS: (no write yet)
Go Gateway -->> GatewayClient: {session_id, signed_urls: {file: url}}

Note over GatewaySyncToCloudWorker: Step 4 — upload directly to GCS
loop for each (file, signed_url)
  GatewaySyncToCloudWorker ->> GCS: PUT signed_url (file bytes, 15-min TTL)
  GCS -->> GatewaySyncToCloudWorker: 200 OK
  GatewaySyncToCloudWorker -->> Model: emit progress()
end

Note over GatewaySyncToCloudWorker: Step 5 — commit session
GatewaySyncToCloudWorker ->> GatewayClient: commit_session(mac_id, session_id, files)
GatewayClient ->> Go Gateway: POST /api/commit?mac_id=X&session_id=Y
Go Gateway ->> GCS: write {mac_id}/{session_id}/_COMMIT.json
Go Gateway -->> GatewayClient: 200 OK

GatewaySyncToCloudWorker -->> Model: finished(True, MSG.USB.ALL_FILES_SYNCED)
Model -->> Bridge: usbFilesSyncedToCloud(True, msg)
Bridge -->> QML: setStatus(msg, False)
```

**GCS object layout — sessions:**
```
{mac_id}/                          ← legacy flat path (backward compat)
{mac_id}/{filename}

{mac_id}/{session_id}/             ← new session-scoped path
{mac_id}/{session_id}/{filename}
{mac_id}/{session_id}/_COMMIT.json ← commit marker (written by /api/commit)
```
`ListFiles` returns only filenames from committed sessions; uncommitted uploads are invisible to readers.

---

#### 8g. Sub-HLD: Log Analysis Flow

Mermaid `sequenceDiagram`

Participants: User, FilePanel/VisualizationPanel QML, Controller, Model, AnalyzeWorker (USB|Cloud|Disk), GatewayClient, Go Gateway (parse.go), Bridge, Chart (QML)

```
User ->> FilePanel: double-clicks log filename
FilePanel ->> Bridge: emit analyzeClicked(tab_id, filename)
Bridge ->> Controller: _translate_and_analyze(tab_id, filename)
Note over Controller: PC tab path → absolute path translation
Controller ->> Model: analyze_log(tab, filename)
Model ->> Model: pick worker by TAB enum
alt TAB.USB
  Model ->> AnalyzeUSBSerialWorker: start()
  AnalyzeUSBSerialWorker ->> USBSerialConnection: read_log_file(filename)
else TAB.CLOUD
  Model ->> AnalyzeCloudWorker: start()
  AnalyzeCloudWorker ->> GatewayClient: get_read_signed_url(mac_id, filename)
  GatewayClient ->> Go Gateway: GET /api/signed-url/read?mac_id=X&filename=Y
  Go Gateway ->> GCS: (IAM signBlob to generate URL)
  Go Gateway -->> GatewayClient: signed_url (15 min TTL)
  AnalyzeCloudWorker ->> GCS: GET signed_url
  GCS -->> AnalyzeCloudWorker: raw bytes
else TAB.PC (DISK)
  Model ->> AnalyzeDiskSerialWorker: start()
  AnalyzeDiskSerialWorker ->> disk: open(absolute_path)
end

Note over AnalyzeWorker: Server-side log parsing
AnalyzeWorker ->> GatewayClient: parse_test_log(mac_id, data)
GatewayClient ->> Go Gateway: POST /api/parse/test-log?mac_id=X (body: hex bytes)
Go Gateway ->> Go Gateway: ParseTestLog() — parse 0x5521 command frames,\nextract frequency (0x5130fff4 status),\nbuild PlotSpec JSON
Go Gateway -->> GatewayClient: PlotSpec {title, series[], x_values[], y_axes[], ...}

Note over AnalyzeWorker: Client-side point building
AnalyzeWorker ->> AnalyzeWorker: _build_points(plot_spec) → list[list[QPointF]]
AnalyzeWorker ->> AnalyzeWorker: enrich_plot_spec() → add axis ranges + metadata

AnalyzeWorker -->> Model: finished(payload={plot_spec, pts_per_series})
Model -->> Bridge: analysisCompleted(True, msg, payload)
Bridge ->> Bridge: stash _last_plot_spec, stash _pending_pts
Bridge -->> QML: root.setPlotConfig(plot_spec_json)

Note over QML: QML creates chart series, emits chartSeriesReady back
QML -->> Bridge: emit chartSeriesReady(series_refs)
Bridge ->> Bridge: on_chart_series_ready() — inject QPointF data via shiboken6
Bridge -->> QML: chart renders
QML -->> User: VisualizationPanel shows interactive chart
```

**PlotSpec JSON shape** (from `handlers/parse.go`):
```json
{
  "success": true,
  "title": "Test Log — <filename>",
  "legend": {"visible": true, "position": "bottom"},
  "x_axis": {"label": "Time", "unit": "s"},
  "y_axes": [{"label": "Voltage", "unit": "V", "min": 0, "max": 5}],
  "series": [
    {"label": "Vbatt", "data": [...], "color": "#...", "y_axis": 0,
     "line_style": "solid", "line_width": 1.5, "marker_style": "none", "marker_size": 0}
  ],
  "x_values": [0.0, 0.01, 0.02, ...],
  "frequency": 100.0
}
```

---

#### 8h. Sub-HLD: Device Log (Event Log) Flow

Mermaid `sequenceDiagram`

```
User ->> FilePanel: clicks *_devicelog.txt
FilePanel ->> Bridge: emit analyzeClicked(TAB.USB, filename)
Bridge ->> Controller ->> Model: analyze_log(USB, filename)
Model ->> Model: is_devicelog_fname(filename) → True
Model ->> ReadUSBDeviceLogFile: start()

Note over ReadUSBDeviceLogFile: Cache-aside read
ReadUSBDeviceLogFile ->> DiskCache: has(CacheKey("usb", filename)) ?
alt cache hit
  DiskCache -->> ReadUSBDeviceLogFile: bytes
else cache miss
  ReadUSBDeviceLogFile ->> USBSerialConnection: read_log_file(filename)
  USBSerialConnection -->> ReadUSBDeviceLogFile: raw bytes
  ReadUSBDeviceLogFile ->> DiskCache: put_bytes(key, bytes)
end

Note over ReadUSBDeviceLogFile: Server-side device log parsing
ReadUSBDeviceLogFile ->> GatewayClient: parse_device_log(mac_id, bytes)
GatewayClient ->> Go Gateway: POST /api/parse/device-log?mac_id=X
Go Gateway ->> Go Gateway: ParseDeviceLog() — parse 0x5A-delimited entries,\ndecode opcodes (Bootup 0x01, Battery 0x07, etc.),\nreturn [{key, val}] list
Go Gateway -->> GatewayClient: [{timestamp, description}, ...]

ReadUSBDeviceLogFile -->> Model: finished(payload={device_log_data: [...]})
Model -->> Bridge: usbDeviceLogParsed(True, entries)
Bridge -->> QML: root.setDeviceInfo(entries)
QML -->> User: VisualizationPanel shows event table
```

**Device log opcodes** decoded by `handlers/parse.go → parseDeviceLogEntry()`:

| Opcode | Event |
|--------|-------|
| `0x01` | Bootup — includes `decodeStatusWord()` + `decodePostfailByte()` |
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

#### 8i. Sub-HLD: Cloud List + Download Flow

Mermaid `sequenceDiagram`

```
User ->> FilePanel: enters MAC ID, clicks List Cloud Files
FilePanel ->> Bridge: emit listCloudFilesClicked(mac_id)
Bridge ->> Controller ->> Model: list_cloud_files(mac_id)
Model ->> GatewayWorker(LIST): start()
GatewayWorker ->> GatewayClient: list_files(mac_id)
GatewayClient ->> Go Gateway: GET /api/files?mac_id=X
Go Gateway ->> GCS: list objects with prefix {mac_id}/
Go Gateway ->> Go Gateway: deduplicate basenames across committed sessions\n(resolveObjectName: latest committed session wins)
Go Gateway -->> GatewayClient: ["file1.txt", "file2.txt", ...]
GatewayWorker -->> Model: finished
Model -->> Bridge: cloudFileNamesListed(True, msg, list)
Bridge -->> QML: setCloudLogs(list)
QML -->> User: FilePanel Cloud tab shows file list

User ->> FilePanel: selects files, clicks Download
FilePanel ->> Bridge: emit downloadCloudFilesToDiskClicked(files)
Bridge ->> Controller: _handle_download_cloud_files_to_disk()
Controller ->> Controller: QFileDialog.getExistingDirectory() → dest_path
Controller ->> Model: download_cloud_files_to_disk(files, dest_path)
Model ->> GatewaySaveFilesWorker: start()
loop for each file
  GatewaySaveFilesWorker ->> GatewayClient: get_read_signed_url(mac_id, filename)
  GatewayClient ->> Go Gateway: GET /api/signed-url/read?mac_id=X&filename=Y
  Go Gateway ->> Go Gateway: resolveObjectName() → full GCS object path
  Go Gateway ->> GCS: IAM signBlob for signed GET URL
  Go Gateway -->> GatewayClient: signed_url
  GatewaySaveFilesWorker ->> GCS: GET signed_url
  GCS -->> GatewaySaveFilesWorker: file bytes
  GatewaySaveFilesWorker ->> disk: save to dest_path/filename
end
GatewaySaveFilesWorker -->> Model: finished
Model -->> Bridge: cloudFilesDownloadedToDisk(success, msg, result_dict)
Bridge -->> QML: show_success / show_warning
```

**File resolution** (from `handlers/files.go → resolveObjectName()`):
1. Probe legacy flat path `{mac_id}/{filename}` — if exists, return it (backward compatibility)
2. List all objects with prefix `{mac_id}/` filtered to `*/{filename}`
3. Keep only objects in sessions that have a `_COMMIT.json` marker
4. Among committed sessions, return object from the lexicographically latest `session_id`

---

#### 8j. Sub-HLD: CSV Export and Screenshot Flow

```
User ->> VisualizationPanel: clicks Export CSV
VisualizationPanel ->> Bridge: emit exportCsvClicked()
Bridge ->> Controller: _handle_export_csv()
Controller ->> Controller: QFileDialog.getSaveFileName() → path.csv
Controller ->> Controller: reconstruct rows from bridge._last_plot_spec
Controller ->> disk: write CSV
Controller ->> Bridge: show_success(MSG.Export.EXPORTED.format(path))
Bridge -->> QML: showSuccess(msg)

User ->> VisualizationPanel: clicks Screenshot
VisualizationPanel ->> Bridge: emit screenshotClicked()
Bridge ->> Controller: _handle_screenshot_clicked()
Controller ->> Controller: QFileDialog.getSaveFileName() → path.png
Controller ->> Controller: grab VisualizationPanel chart → QImage
Controller ->> disk: save PNG
Controller ->> Bridge: show_success(MSG.Export.SCREENSHOT_SAVED.format(path))
```

---

### 9. Go Gateway — HTTP API Reference

Complete route table (for both developer and operator reference):

| Method | Path | Auth | ACL gate | Handler | Description |
|--------|------|------|----------|---------|-------------|
| `GET` | `/health` | None | None | `Health()` | Liveness probe; returns `{"status":"ok"}` |
| `GET` | `/api/profile` | Bearer | — | `Profile()` | Returns uid, email, org, role, read, write from context |
| `GET` | `/api/files` | Bearer | RequireRead | `ListFiles()` | List committed file names for `?mac_id=X` |
| `GET` | `/api/signed-url/read` | Bearer | RequireRead | `SignedReadURL()` | 15-min GCS GET signed URL for one file |
| `POST` | `/api/signed-url/upload` | Bearer | RequireWrite | `SignedUploadURL()` | Generate session + PUT signed URLs for file list |
| `POST` | `/api/commit` | Bearer | RequireWrite | `CommitSession()` | Write `_COMMIT.json` to atomically publish a session |
| `POST` | `/api/parse/device-log` | Bearer | RequireRead | `ParseDeviceLog()` | Decode binary device event log → `[{key,val}]` |
| `POST` | `/api/parse/test-log` | Bearer | RequireRead | `ParseTestLog()` | Decode binary test log → `PlotSpec` JSON |

Middleware stack applied to all `/api/*` routes:
1. `middleware.Auth(authClient)` — validates Firebase ID token; injects UID; 401 on failure
2. `middleware.ACL(store)` — looks up UID in `ACLStore`; injects `User`; unknown UIDs get full access (enforcement paused)

---

## Tier 2: Directory-Level README.md Files

Each directory gets its own `README.md`. These name every real file, struct, function, and signal. A developer should be able to contribute to any module by reading its README without opening the source files.

---

### Files to Create

| Path | Covers |
|------|--------|
| `src/README.md` | Startup sequence, constants, dataclasses, utils, logging |
| `src/mvc/README.md` | Bridge, Model, Controller — full signal/slot/method reference |
| `src/login/README.md` | AuthManager + LoginController — state machine, Firebase endpoints |
| `src/workflows/README.md` | All 18 worker classes — inputs, outputs, failure modes |
| `src/cloud_conn/README.md` | GatewayClient — all endpoints, request/response shapes |
| `src/usb_conn/README.md` | Serial protocol — commands, terminators, cancel/lock |
| `src/disk_cache/README.md` | Cache design — CacheKey, eviction, read_log_using_cache |
| `style/README.md` | QML architecture — panel/logic split, Theme, Strings, signal wiring |
| `style/components/README.md` | Every component's property API and emitted signals |
| `style/logic/README.md` | Every *Logic.qml object's functions and property dependencies |
| `tests/README.md` | Test strategy, fixtures, per-file catalogue |
| `Server/go_gateway/README.md` | Go gateway — full setup, route reference, struct catalogue |
| `Server/go_gateway/handlers/README.md` | Every handler function, request/response shapes, test coverage |
| `Server/go_gateway/middleware/README.md` | Auth + ACL middleware — behaviour, context keys, paused-enforcement note |
| `Server/go_gateway/models/README.md` | ACLStore, User, DeviceEntry — field tables, concurrency notes |
| `Server/go_gateway/config/README.md` | LoadACL + LoadDevices — CSV format, column spec, error handling |

---

### Content Specification Per Directory

---

#### `src/README.md`

1. **Module map** — one-line purpose per sub-package
2. **Startup sequence** with exact function names:
   `run.py → main() → get_base_path() → get_directories() → AuthManager() → Model() → Bridge() → LoginController() → ApplicationController() → engine.load(MainWindow.qml) → bridge.set_root(root) → app.exec()`
3. **Shared constants** (`src/constants.py`):
   - Full `ACTION` enum table (integer → human label)
   - Full `TAB` enum
   - `MSG.*` hierarchy with all string constants grouped by domain
   - `GATEWAY_STATUS` sentinel values
   - `DEVICE_LOG_CODE_MAP` full table (code → description)
4. **Parameter dataclasses** (`src/dataclasses.py`) — inheritance diagram + field table
5. **Utilities** (`src/utils.py`) — function signature + one-line description each
6. **Logging** — log file location (`logs/`), format, rotation (from `main.py`)
7. **Gotchas** — `sys._MEIPASS` PyInstaller path, `QT_QUICK_CONTROLS_STYLE = "Basic"` must be set before `QApplication`

---

#### `src/mvc/README.md`

1. **Layer overview diagram** (Mermaid: QML → Bridge → Controller → Model → Workers)
2. **`bridge.py` — Bridge class**
   - Every QML→Python signal: name, `(arg_types)`, → model slot it reaches
   - Every Python→QML method: name, args, JS function called on root
   - Every `@Slot` handler: model signal handled → QML state updated
   - `_safe_to_close` gate and shutdown protocol
   - `_last_plot_spec` / `_pending_pts` chart injection (shiboken6 pattern)
   - Per-tab busy routing (`_usb_busy`, `_cloud_busy`)
3. **`model.py` — Model class**
   - All Qt signals (name, param types)
   - All `@Slot` public methods (called by controller)
   - Worker submission: `_active_workers` dict lifecycle
   - `_handlers` dispatch table keyed by `ACTION`
   - `_auth_required_emitted` 401 dedup flag
   - Shutdown + cleanup protocol
4. **`controller.py` — ApplicationController**
   - `_wire_qml_to_model()` full table
   - `_wire_model_to_bridge()` full table
   - OS dialog adapter methods
   - `_translate_and_analyze` PC-tab path logic

---

#### `src/login/README.md`

1. **Auth state machine** (Mermaid `stateDiagram-v2`):
   States: `LoggedOut`, `PendingLogin`, `PendingMFAVerify (signin)`, `PendingMFAEnroll (signup)`, `LoggedIn`
   Transitions labelled with signal + outcome
2. **`AuthManager` API** — every method: signature, Firebase endpoint, return `(bool, str, dict)`
   - Token storage: `id_token` RAM-only
   - Firebase V1 vs V2 split table (same as HLD §8e)
3. **`LoginController` API** — every `@Slot` handler: triggering signal, validation, model call
   - `_is_login_challenge` flag
   - `on_mfa_qr_ready`: QR PNG generation (`qrcode` lib → base64 data URI)
4. **MFA enrollment vs MFA signin** — side-by-side comparison
5. **Firebase error code → `MSG.Auth.*` mapping table**
6. **Gotchas** — V2 API requires Bearer token from V1 login step; `mfaPendingCredential` is short-lived

---

#### `src/workflows/README.md`

1. **Threading model** — `QThreadPool`, `BaseThreadWorker(QRunnable)`, `SharedWorkerSignals`, `finished(worker_id, success, msg, payload)` contract
2. **Cancellation protocol** — `force_cancel()`, `_cancel` flag, loop-checking pattern
3. **Full worker catalogue table**:

| Worker class | ACTION | Input params | Success payload keys | Cancellable | Calls gateway? |
|---|---|---|---|---|---|
| `DetectUSBSerialWorker` | `DETECT_USB_SERIAL_PORTS` | `usb_conn` | `{ports: list}` | No | No |
| `ConnectUSBSerialWorker` | `CONNECT_USB_SERIAL_PORT` | `usb_conn, port` | — | No | No |
| `DisconnectUSBSerialWorker` | — | `usb_conn` | — | No | No |
| `ListUSBSerialWorker` | `LIST_USB_FILES` | `usb_conn` | `{files: list}` | No | No |
| `ReadUSBDeviceLogFile` | — | `usb_conn, gateway, cache, filename` | `{device_log_data: list}` | No | Yes (parse) |
| `AnalyzeUSBSerialWorker` | — | `usb_conn, gateway, cache, filename` | `{plot_spec, pts_per_series}` | No | Yes (parse) |
| `SaveAllUSBSerialWorker` | `SAVE_ALL_USB_FILES_TO_DISK` | `usb_conn, files, dest` | `{total, saved_count}` | Yes | No |
| `SaveOneUSBFileWorker` | `SAVE_ONE_USB_FILE_TO_DISK` | `usb_conn, filename, dest` | — | No | No |
| `GatewayWorker` | LIST/DOWNLOAD | `gateway_client, mac_id, action` | `{files}` or `{data}` | No | Yes |
| `GatewaySaveFilesWorker` | `DOWNLOAD_CLOUD_FILES_TO_DISK` | `gateway_client, mac_id, files, dest` | `{saved, failed}` | No | Yes |
| `GatewaySyncToCloudWorker` | `UPLOAD_USB_FILES_TO_CLOUD` | `usb_conn, gateway, mac_id, files` | — | Yes | Yes |
| `GatewayACLPreflightWorker` | — | `gateway_client, mac_id` | — | No | Yes |
| `AnalyzeCloudWorker` | — | `gateway_client, mac_id, filename` | `{plot_spec, pts_per_series}` | No | Yes |
| `AnalyzeDiskSerialWorker` | — | `gateway_client, mac_id, path` | `{plot_spec, pts_per_series}` | No | Yes (parse) |
| `AuthWorker` | `AUTHENTICATE` | `auth_manager, type, email, pw` | `{type, token, ...}` | No | No |
| `MFAWorker` | `MFA_ACTION` | `auth_manager, mode, ...` | `{secret}` or `{token}` | No | No |
| `ForgotPasswordWorker` | — | `auth_manager, email` | — | No | No |

4. **Detailed per-group sections** (USB, Cloud, Auth) — full description, failure modes, edge cases
5. **`_build_points(plot_spec)`** — QPointF array construction from gateway JSON
6. **`_http_error_msg(exc)`** — HTTP error → user string mapping
7. **Fatal exception handling** — `*_FATALS` tuples, how uncaught exceptions emit `finished(False, ...)`

---

#### `src/cloud_conn/README.md`

1. **Why a gateway?** — ACL enforcement, signed URL generation without raw key, server-side log parsing
2. **`GatewayClient` method table**:

| Method | HTTP | Path | Request body / params | Response |
|--------|------|------|-----------------------|----------|
| `list_files(mac_id)` | GET | `/api/files` | `?mac_id=X` | `["f1.txt", ...]` |
| `get_read_signed_url(mac_id, filename)` | GET | `/api/signed-url/read` | `?mac_id=X&filename=Y` | `"https://..."` |
| `read_file(mac_id, filename)` | GET | (signed URL) | — | `bytes` |
| `get_upload_signed_urls(mac_id, filenames)` | POST | `/api/signed-url/upload` | `{mac_id, files:[...]}` | `{session_id, signed_urls:{}}` |
| `upload_file(signed_url, data)` | PUT | (signed URL) | raw bytes | 200 OK |
| `commit_session(mac_id, session_id, files)` | POST | `/api/commit` | `?mac_id=X&session_id=Y` | 200 OK |
| `parse_device_log(mac_id, data)` | POST | `/api/parse/device-log` | `?mac_id=X`, body: hex bytes | `[{key,val}]` |
| `parse_test_log(mac_id, data)` | POST | `/api/parse/test-log` | `?mac_id=X`, body: hex bytes | `PlotSpec` JSON |

3. **Error handling** — `GATEWAY_STATUS` sentinel values, HTTP status → exception raised
4. **Token refresh** — `_headers()` calls `auth_manager.get_token()` on every request; no client-side caching
5. **Gotchas** — signed URL TTL is 15 min; large file uploads must complete within TTL; `upload_file` goes directly to GCS not the gateway

---

#### `src/usb_conn/README.md`

1. **Device identification** — CP2102 adapter; `detect_ports()` filters by description string
2. **Serial parameters** — `BAUDRATE = 115_200`, `TIMEOUT = 1.0` (from `src/constants.py`)
3. **Protocol reference**:

| Operation | Command | Response terminator | Notes |
|-----------|---------|---------------------|-------|
| List files | `file_list\r\n` | `\r\r\n` | Returns `.txt` filenames only |
| Read file | `file_read <name>\n` | `XALIVA>` prompt | Binary-safe; may be large |
| Connect | — | — | `\r\n` sent on connect to flush stale data |

4. **`USBSerialConnection` API** — every public method, `_io_lock` thread-safety, `is_open_bool()`
5. **Cancel pattern** — `cancel_io()` → `_cancel = True`; read loops check flag
6. **Gotchas** — `close_port()` must be called before process exit; pyserial raises `SerialException` on physical disconnect

---

#### `src/disk_cache/README.md`

1. **Purpose** — avoid re-reading large files from USB serial; read-through by default
2. **`CacheKey`** — frozen dataclass `(source: str, log_file_name: str)`; key sanitised to filesystem-safe filename
3. **`DiskCache` API** — `put_bytes`, `get_bytes`, `get_text`, `has`, `clear`, `is_enabled`, `root`
4. **`read_log_using_cache` helper** — cache-aside: `has` → miss → `usb_conn.read_log_file` → `put_bytes` → return
5. **Eviction** — none automatic; `clear()` deletes all; `Model.cleanup()` calls `clear()` on shutdown
6. **Disabled state** — `QStandardPaths.CacheLocation` unwritable → `_enabled = False`; all ops become no-ops

---

#### `style/README.md`

1. **Component tree** (Mermaid):
   `MainWindow` → `Sidebar | FilePanel | VisualizationPanel | LoginOverlay`
   Each panel → `*Logic.qml` via property injection
2. **Panel/Logic split pattern** — why, and the property-injection rule (no ID cross-references)
3. **State ownership** — all runtime props live in `MainWindow.qml`; binding/propagation pattern
4. **Theme singleton** — import pattern, token categories (colors, fonts, spacing, radii)
5. **Strings singleton** — camelCase naming rule; why UPPER_SNAKE breaks QML property binding
6. **QML→Python wiring summary table** (full detail in `SIGNALS_REFERENCE.md`)
7. **Python→QML call pattern** — `bridge._call(fn, args)` → `getattr(root, fn)(*args)`
8. **Qt 6 gotchas** — explicit handler params, `QT_QUICK_CONTROLS_STYLE = "Basic"`, bridge null guard

---

#### `style/components/README.md`

Component catalogue table (full property API + emitted signals per component), plus:
- Usage snippet for each component
- Theming pattern (Theme token references)
- `FileList.qml` — multi-select behaviour, `selectedFiles` property, `FileListLogic.qml` coupling

---

#### `style/logic/README.md`

For each `*Logic.qml`:
- Host panel it belongs to
- Properties it reads from the host panel (injected)
- All JS functions it exposes: name, args, what it does, which bridge signal it triggers
- Auto-reset timers and their TTLs
- `callBridge(fn)` null-guard pattern

---

#### `tests/README.md`

1. **Strategy** — unit (mocked) vs integration (live)
2. **Running** — commands
3. **Fixtures** (`conftest.py`) — list of provided fixtures
4. **Per-file catalogue**:

| File | What it tests | Mocking strategy |
|---|---|---|
| `test_controller.py` | Bridge signals → model/login slots | `unittest.mock.MagicMock` for Model and Bridge |
| `test_gateway_client.py` | GatewayClient HTTP methods | `unittest.mock.patch("requests.get/post/put")` |
| `test_gateway_sync_worker.py` | GatewaySyncToCloudWorker lifecycle | Mock GatewayClient + USBSerialConnection |
| `test_login_form_clear.py` | LoginOverlay field reset on mode switch | pytest-qt QML test harness |
| `integration/test_gateway_live.py` | Full login → upload → list → download | Live gateway + Firebase; skipped if env vars absent |

5. **Adding tests** — naming, location, reuse
6. **CI** — placeholder for GitHub Actions

---

#### `Server/go_gateway/README.md`

1. **Purpose** — why the gateway exists (ACL, signed URLs, log parsing)
2. **Dev setup** — Docker Compose path + raw `go run .` path (same detail as root README §4, not repeated verbatim — link back)
3. **Full route table** (same as root README §9 — cross-linked)
4. **Startup sequence** — `main()` step by step:
   1. `os.Getenv` — load required env vars, `log.Fatal` if absent
   2. `firebase.NewApp(ctx, &firebase.Config{ProjectID})` → `app.Auth(ctx)` → `authClient`
   3. `storage.NewClient(ctx)` → `gcsClient`
   4. `iamcredentials.NewService(ctx)` → `iamSvc`
   5. `config.LoadACL("data/acl.csv")` → `store`; `config.LoadDevices(store, "data/devices.csv")`
   6. Build `handlers.Env{GCSClient, GCSBucket, ServiceAccountEmail, IAMService, ACLStore}`
   7. `gin.Default()` → register middleware + routes
   8. `r.Run(":" + port)`
5. **ADC behaviour** — local: reads `GOOGLE_APPLICATION_CREDENTIALS` file; prod (Cloud Run): metadata server
6. **GCS object layout diagram** (legacy flat vs session-scoped)
7. **Session lifecycle** — `SignedUploadURL` → client PUTs → `CommitSession` → `_COMMIT.json` written
8. **ACL enforcement status** — what is implemented vs what is paused; re-enable checklist
9. **Gotchas** — `signBytesFunc` closure required for `SignedURL` opts; IAM `signBlob` latency (~100ms)

---

#### `Server/go_gateway/handlers/README.md`

For each handler function in `handlers/`:

**`health.go`**
- `Health(c *gin.Context)` — no params, returns `{"status":"ok"}` 200

**`profile.go`**
- `Profile(c *gin.Context)` — reads `User` from context (set by ACL middleware), returns uid/email/org/role/read/write

**`files.go`** — most complex; full section:
- `signBytesFunc(iamSvc, saEmail)` — returns closure calling `iamcredentials.Projects.ServiceAccounts.SignBlob`; used in `SignedURLOptions` to avoid raw private key
- `checkOrgAccess(c, store, macID)` — org-boundary enforcement (currently paused)
- `ListFiles(store, gcsClient, bucket)` — full algorithm: list all objects, parse session_id from path, check `_COMMIT.json`, deduplicate by basename, return only committed files
- `resolveObjectName(ctx, gcsClient, bucket, macID, filename)` — probe legacy path → search sessions → latest committed session wins
- `SignedReadURL(env)` — calls `resolveObjectName`, generates 15-min GET signed URL
- `SignedUploadURL(env)` — generates `session_id = fmt.Sprintf("%d_%d", now.Unix(), now.Nanosecond())`, returns PUT signed URLs per file
- `CommitSession(store, gcsClient, bucket)` — writes `_COMMIT.json` with `{committed_at, files:[]}` JSON

**`parse.go`** — full binary protocol doc:
- `ParseDeviceLog` — binary format: `0x5A` delimiter, each entry: `[logType uint8][timestamp uint32 LE][payloadLen uint16 LE][payload bytes]`
- `ParseTestLog` — hex line format, command `0x5521`, frequency extracted from status `0x5130fff4`, builds `PlotSpec`
- All structs: `PlotSpec`, `Series`, `Axis`, `Legend` — field table with JSON tags
- Decoder helper functions: `decodeStatusWord`, `decodePostfailByte`, `decodeBatteryByte`, `parseDeviceLogEntry`

**`files_test.go`** — 10 tests, `fakeGCS` httptest strategy:
- `fakeGCS(t, bucket, objects)` — starts httptest HTTP server mimicking GCS List + Attrs XML API; sets `STORAGE_EMULATOR_HOST`
- Full test case table: test name, setup, assertion

---

#### `Server/go_gateway/middleware/README.md`

1. **`auth.go` — `Auth(client *auth.Client) gin.HandlerFunc`**
   - Input: `Authorization: Bearer <Firebase ID token>` header
   - Calls `client.VerifyIDToken(ctx, token)` — Firebase Admin SDK
   - On success: `c.Set(ContextKeyUID, token.UID)`
   - On failure: `c.AbortWithStatusJSON(401, {"error": "unauthorized"})`
   - Context key constant: `ContextKeyUID = "uid"`

2. **`acl.go` — `ACL(store *models.ACLStore) gin.HandlerFunc`**
   - Reads UID from context → `store.GetUser(uid)`
   - If found: injects `User` struct
   - If **not** found (current paused behaviour): injects synthetic `User{Read:true, Write:true, Role:"admin"}`
   - Context key: `ContextKeyUser = "user"`

3. **Permission gate functions** — `RequireRead()`, `RequireWrite()`, `RequireAdmin()`
   - Each reads `User` from context, checks field
   - Currently: always call `c.Next()` (enforcement paused)
   - When re-enabled: abort 403 if check fails

4. **Paused ACL — re-enable checklist**:
   - Remove synthetic full-permission fallback in `ACL()`
   - Un-comment enforcement in `RequireRead/Write/Admin`
   - Un-comment org-boundary check in `checkOrgAccess`
   - Populate `data/acl.csv` with real production UIDs

---

#### `Server/go_gateway/models/README.md`

**`ACLStore`** — thread-safe in-memory store for users and devices
- `users map[string]User` — keyed by `firebase_uid`
- `devices map[string]DeviceEntry` — keyed by `mac_id`
- `mu sync.RWMutex` — `RLock` for reads, `Lock` for writes
- Methods: `NewACLStore()`, `AddUser(User)`, `AddDevice(DeviceEntry)`, `GetUser(uid)`, `GetDevice(macID)`, `UserCount()`, `DeviceCount()`

**`User`** struct field table:

| Field | Type | Source | Purpose |
|-------|------|--------|---------|
| `FirebaseUID` | string | `acl.csv` col 1 | Matches Firebase token UID |
| `Email` | string | `acl.csv` col 2 | Display only |
| `OrganizationID` | string | `acl.csv` col 3 | Org-boundary ACL |
| `Role` | string | `acl.csv` col 4 | `"admin"` or `"user"` |
| `Read` | bool | `acl.csv` col 5 | Can list + download |
| `Write` | bool | `acl.csv` col 6 | Can upload + commit |

**`DeviceEntry`** struct field table (similar format).

Concurrency note: `AddUser`/`AddDevice` are called only at startup from `config.LoadACL()` — single goroutine, no contention. `GetUser`/`GetDevice` are called from concurrent HTTP handlers — `RLock` protects them.

---

#### `Server/go_gateway/config/README.md`

1. **`LoadACL(path string) (*models.ACLStore, error)`**
   - Opens CSV at `path`
   - Skips header row
   - Columns: `firebase_uid, email, organization_id, role, read, write`
   - `read`/`write` parsed as `"true"` (case-insensitive) → `bool`
   - Returns populated `*ACLStore`; error if file unreadable or row malformed

2. **`LoadDevices(store *models.ACLStore, path string) error`**
   - Opens CSV at `path`
   - Skips header row
   - Columns: `mac_id, organization_id`
   - Calls `store.AddDevice()` per row

3. **CSV format specifications** (with header row shown):
```csv
# acl.csv
firebase_uid,email,organization_id,role,read,write
eK62j6LyNva62Ritu4NLa0MGrSW2,user@example.com,org_001,admin,true,true

# devices.csv
mac_id,organization_id
10352920,org_001
```

4. **Gotchas** — `strings.TrimSpace` required on each field (CSV parser may include trailing whitespace); Docker `COPY data/ ./data/` bakes the files into the image; to update prod, rebuild the image.

---

## Writing Guidelines

1. **Mermaid diagrams** for all non-trivial flows — renders natively on GitHub
2. **Tables** for catalogues (routes, signals, methods, workers, fields) — scannable
3. **Code blocks** for all commands, function signatures, JSON shapes, CSV examples
4. **"Why" before "What"** — explain motivation before mechanics (non-dev audience sections)
5. **Cross-links** — every README links to related READMEs and the root README
6. **No redundancy** — root README covers both repos at a high level; directory READMEs go deep; don't repeat
7. **Exact names** — use names verified from source (structs, functions, signals, env vars)
8. **Versioned externals** — Python 3.12.11, Go 1.22, PySide6 6.10.2, Firebase Admin SDK v4, GCS v3
9. **ACL-paused callouts** — every place ACL enforcement is disabled should have a visible note
10. **Gotchas section** — every README ends with known quirks

---

## Pending Updates to README.md (root)

These changes are needed in `README.md` (the monorepo front page at `XALIVA_Log_Analyzer/README.md`):

### 1. Fix participant order in all sequence diagrams

**Problem:** `Bridge` appears as the leftmost participant (next to QML) in all `sequenceDiagram` blocks. Architecturally Bridge is a Python object — it belongs on the Python side, grouped with Controller and Model, not next to QML.

**Fix:** In every `sequenceDiagram`, reorder participants so the layout reads left-to-right as:

```
User | QML UI | Bridge | Controller | Model | Worker | GatewayClient | Go Gateway | Firebase | GCS
```

Bridge should sit between QML and Controller — it receives QML signals and forwards them to Controller, then receives Model signals and forwards back to QML.

### 2. Standardize all arrow annotations

**Problem:** Arrow labels currently mix plain English, signal names, HTTP paths, and method calls inconsistently. Hard to follow which layer boundary each arrow crosses.

**Fix:** Apply these annotation conventions uniformly across all diagrams:

| Arrow type | Format | Example |
|---|---|---|
| QML emits signal to Bridge | `signal: signalName(args)` | `signal: syncUsbToCloudClicked(fileList)` |
| Bridge/Controller calls Python method | `method: functionName(args)` | `method: sync_usb_files_to_cloud(fileList)` |
| Python submits worker | `submit: WorkerClassName` | `submit: GatewaySyncToCloudWorker` |
| Worker calls client method | `method: methodName(args)` | `method: get_upload_signed_urls(macId, files)` |
| HTTP request | `HTTP_METHOD /path` (body/params on next `Note` line if needed) | `POST /api/signed-url/upload` |
| HTTP response / return value | `→ returnType: description` | `→ {session_id, signed_urls}` |
| Worker emits finished signal | `signal: finished(workerId, ok, msg, payload)` | |
| Model emits Qt signal | `signal: usbFilesSyncedToCloud(ok, msg)` | |
| Bridge calls QML JS function | `call: functionName(args)` | `call: setStatus(msg, false)` |

### 3. Add a "What is a Bearer token?" callout box

**Problem:** `Authorization: Bearer <token>` appears in sequence diagrams and the API table without explanation for non-developer readers.

**Fix:** Add a short callout block in §6 (High-Level Design) before the first diagram that uses Bearer tokens:

> **Bearer token** — After login, Firebase issues a signed JSON Web Token (JWT). The Python client attaches it to every gateway request as `Authorization: Bearer <token>`. The Go Gateway verifies the signature using the Firebase Admin SDK without a network round-trip to Firebase for each request. The token expires after 1 hour; the client re-authenticates to get a fresh one.

### 4. Diagrams to update

Apply the participant reorder (fix 1) and annotation standardization (fix 2) to all nine sequence diagrams:

- §6c Generic User Interaction Sequence
- §6d Authentication Flow
- §6e USB → Cloud Sync Flow
- §6f Log Analysis Flow
- §6g Device Log Flow
- §6h Cloud List + Download Flow

---

## File Creation Order

Bottom-up: leaf modules first, monorepo root last to avoid forward-reference gaps.
All paths below are relative to the repo root.

**Go gateway (Server/go_gateway/)**
1. `Server/go_gateway/models/README.md`
2. `Server/go_gateway/config/README.md`
3. `Server/go_gateway/middleware/README.md`
4. `Server/go_gateway/handlers/README.md`
5. `Server/go_gateway/README.md`

**Python client (XALIVA_Log_Analyzer/)**
6. `XALIVA_Log_Analyzer/src/disk_cache/README.md`
7. `XALIVA_Log_Analyzer/src/usb_conn/README.md`
8. `XALIVA_Log_Analyzer/src/cloud_conn/README.md`
9. `XALIVA_Log_Analyzer/src/login/README.md`
10. `XALIVA_Log_Analyzer/src/workflows/README.md`
11. `XALIVA_Log_Analyzer/src/mvc/README.md`
12. `XALIVA_Log_Analyzer/src/README.md`
13. `XALIVA_Log_Analyzer/style/components/README.md`
14. `XALIVA_Log_Analyzer/style/logic/README.md`
15. `XALIVA_Log_Analyzer/style/README.md`
16. `XALIVA_Log_Analyzer/tests/README.md`

**Monorepo root (last — links to everything above)**
17. `README.md` — root of the repo, covers both components
