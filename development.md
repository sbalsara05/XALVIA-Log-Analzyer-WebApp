# Development Guide

This guide covers running each component of the XALIVA stack locally for development. For deploying to Google Cloud, see [cloud_deployment.md](cloud_deployment.md).

---

## Prerequisites

### Python Client
- [Miniconda](https://docs.conda.io/en/latest/miniconda.html) or Anaconda — conda is strongly preferred over pip; some systems produce Qt platform-plugin errors (`qt.qpa.plugin: Could not load the Qt platform plugin`) with pip installs

### Go Gateway
- Go 1.22+
- Docker and Docker Compose (for the recommended Docker setup)
- A GCP service account JSON key (`YOUR_SA_KEY_FILENAME.json`) with the IAM roles listed in [cloud_deployment.md Phase 3](cloud_deployment.md#phase-3--service-account). Place this file in `Server/go_gateway/` — it is excluded from git and cloud uploads by the `.gitignore` and `.gcloudignore` already in that directory

### Admin GUI
- Node.js 18+
- npm

---

## Local PostgreSQL

The Go Gateway connects to PostgreSQL. For local development, run Postgres in Docker. The gateway's `docker-compose.yml` expects an **external** Docker network called `postgres_default` and resolves the database host as `postgres` (the Docker service name inside that network).

Create the network and start the container (run once; skip on subsequent sessions if the container is already running):

```bash
docker network create postgres_default
docker run -d --name postgres --network postgres_default -e POSTGRES_PASSWORD=localpassword -e POSTGRES_DB=ricovr_db -p 5432:5432 postgres:16
```

Apply the schema and seed from the repo root. The seed file contains placeholder values — edit it locally before running, and do not commit the edited version:

```bash
psql -h localhost -U postgres -d ricovr_db -f Postgres/init/01_schema.sql
psql -h localhost -U postgres -d ricovr_db -f Postgres/init/02_seed.sql
```

See [cloud_deployment.md Phase 5](cloud_deployment.md#phase-5--initialize-postgres-schema-and-seed) for the explanation of how to update the seed file with the real Firebase UID.

---

## Python Client

### Conda Environment

```bash
conda env create -f environment.yml
conda activate xaliva-log
```

Pinned versions: Python 3.12.11, PySide6 6.10.2, pyserial 3.5, google-cloud-storage 3.2.0, PyInstaller 6.14.1, pytest 8.4.0.

To update an existing environment after `environment.yml` changes:

```bash
conda env update -f environment.yml --prune
```

A pip path also exists via `requirements.txt` but is not recommended — it does not guarantee the same Qt binaries and can produce platform-plugin errors on some systems.

### Local Environment Variables

Create `.env` at the repo root:

```env
XALIVA_GATEWAY_URL=http://localhost:8080
```

To test against the production Cloud Run gateway instead of a local one, set `XALIVA_GATEWAY_URL` to the Cloud Run URL from [cloud_deployment.md Phase 4](cloud_deployment.md#4e-deploy-to-cloud-run).

### Running in Development

With the conda environment active and `.env` in place, run from the repo root:

```bash
python run.py
```

`run.py` is the sole entry point. It loads `.env`, then calls `src.main.main()`, which bootstraps the Qt application and QML engine. The login overlay appears first — all users must authenticate with email/password and TOTP MFA before the main window loads.

Python startup sequence:
1. `run.py` loads `.env`, calls `src.main.main()`
2. Qt application and QML engine initialise
3. Login overlay appears — enter Firebase email/password and TOTP code
4. After auth, the main window loads with USB / Cloud / PC tabs

### Running Tests

```bash
# Full suite
pytest tests/ --maxfail=1 --disable-warnings -q

# Single test
pytest tests/test_controller.py::test_name -v

# Live integration tests (require network and a populated .env)
pytest tests/integration/ -v
```

---

## Go Gateway

### Local Environment File

Copy the template and fill in your values:

```bash
cd Server/go_gateway
cp .env .env.local
```

`.env.local` is excluded from git. Edit it with the following values:

| Variable | Local dev value |
|---|---|
| `FIREBASE_PROJECT_ID` | Your GCP project ID |
| `SERVICE_ACCOUNT_EMAIL` | `ricovr-go-gateway-runner@YOUR_PROJECT_ID.iam.gserviceaccount.com` |
| `GCS_BUCKET` | Your GCS bucket name |
| `LOCAL_KEY_PATH` | `./YOUR_SA_KEY_FILENAME.json` — path to the SA key file for the Docker volume mount |
| `SERVER_MODE` | `UNIFIED` |
| `DATABASE_URL` | `postgres://postgres:localpassword@postgres:5432/ricovr_db?sslmode=disable` |
| `DB_MAX_OPEN_CONNS` | `5` |
| `DB_MAX_IDLE_CONNS` | `2` |
| `CORS_ORIGINS` | Leave empty — defaults to `http://localhost:5173` |

> `LOCAL_KEY_PATH` is the local filesystem path to the SA JSON key. The Docker Compose file mounts it read-only inside the container at `/run/secrets/sa_key.json` and sets `GOOGLE_APPLICATION_CREDENTIALS` to that path automatically. Do not set `GOOGLE_APPLICATION_CREDENTIALS` directly in `.env.local` when using Docker Compose.

### Running with Docker Compose (recommended)

The `docker-compose.yml` builds the two-stage Alpine image, mounts the SA key read-only, and connects to the `postgres_default` external network. The Local PostgreSQL section above must be completed first.

```bash
cd Server/go_gateway
docker compose --env-file .env.local up --build
```

The gateway listens on `http://localhost:8080`.

### Running without Docker

When running without Docker, the database host changes from `postgres` (Docker service name) to `localhost`:

```bash
cd Server/go_gateway
export FIREBASE_PROJECT_ID="YOUR_PROJECT_ID"
export SERVICE_ACCOUNT_EMAIL="YOUR_SERVICE_ACCOUNT"
export GCS_BUCKET="YOUR_GCS_BUCKET"
export GOOGLE_APPLICATION_CREDENTIALS="./YOUR_SA_KEY_FILENAME.json"
export SERVER_MODE="UNIFIED"
export DATABASE_URL="postgres://postgres:localpassword@localhost:5432/ricovr_db?sslmode=disable"
export PORT=8080
go run .
```

### All Environment Variables

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `FIREBASE_PROJECT_ID` | Yes | — | Firebase Admin SDK initialisation; fatal if absent |
| `SERVICE_ACCOUNT_EMAIL` | Yes | — | IAM Credentials API `signBlob` subject for GCS signed URLs; fatal if absent |
| `GCS_BUCKET` | Yes | — | GCS bucket name for all object operations; fatal if absent |
| `SERVER_MODE` | Yes | — | `data_plane`, `control_plane`, or `unified`; fatal if absent or invalid |
| `DATABASE_URL` | Yes (control/unified) | — | Postgres DSN used by org/user/device management handlers |
| `PORT` | No | `8080` | HTTP listen port (Cloud Run injects this automatically in production) |
| `CORS_ORIGINS` | No | `http://localhost:5173` | Comma-separated allowed CORS origins for the Admin GUI |
| `GOOGLE_APPLICATION_CREDENTIALS` | No (production) | — | Path to SA JSON key; only needed locally when running without Docker |
| `LOCAL_KEY_PATH` | No | — | Docker Compose only — local path to SA key, mounted into the container |

### Access Control

The gateway enforces access control via Postgres, not static configuration files. All device-scoped routes (`/files`, `/signed-url/read`, `/signed-url/upload`, `/parse/*`, `/commit`) are guarded by the `RequireDeviceAccess` middleware in `middleware/device_access.go`. It runs a recursive CTE against the `org_tree`, `org_users`, `users`, and `devices` tables to verify that the calling user has the required permission (`can_read` or `can_upload`) on the device's org — or holds `role='admin'` anywhere in the org's ancestry chain.

To grant or restrict a user's access to a device locally, update the `org_users` table in your local Postgres instance rather than editing any config file.

### Running Tests

```bash
cd Server/go_gateway
go test ./...                              # all packages
go test ./handlers/ -v -run TestListFiles  # single test
```

To run the gateway's integration test profile (connects to the Postgres container via the `postgres_default` network):

```bash
cd Server/go_gateway
docker compose --env-file .env.local --profile test run test
```

---

## Admin GUI

### Local Environment File

Create `admin_gui/.env.local` (excluded from git by `admin_gui/.gitignore`):

```env
VITE_FIREBASE_API_KEY=YOUR_FIREBASE_API_KEY
VITE_FIREBASE_AUTH_DOMAIN=YOUR_PROJECT_ID.firebaseapp.com
VITE_FIREBASE_PROJECT_ID=YOUR_PROJECT_ID
VITE_API_BASE_URL=http://localhost:8080
```

`VITE_FIREBASE_API_KEY` is a project identifier that ends up in the compiled JS bundle — see [cloud_deployment.md Phase 8](cloud_deployment.md#8a-create-envproduction) for context. To test against the production gateway, change `VITE_API_BASE_URL` to `YOUR_GATEWAY_URL`.

### Running Locally with npm

```bash
cd admin_gui
npm install
npm run dev
```

The Vite dev server starts at `http://localhost:5173` with hot module reload.

### Running Locally with Docker Compose

The `docker-compose.yml` in `admin_gui/` builds the container and mounts the source directory so changes are reflected without rebuilding:

```bash
cd admin_gui
docker compose up
```

The app is available at `http://localhost:5173`.

### Running Tests

```bash
cd admin_gui
npm test
```

In watch mode:

```bash
npm run test:watch
```

Via Docker:

```bash
cd admin_gui
docker compose --profile test run test
```

---

## Full Stack Local Run

Start each component in a separate terminal in this order:

**Terminal 1 — PostgreSQL** (skip if the container is already running)

```bash
docker network create postgres_default
docker run -d --name postgres --network postgres_default -e POSTGRES_PASSWORD=localpassword -e POSTGRES_DB=ricovr_db -p 5432:5432 postgres:16
```

**Terminal 2 — Go Gateway**

```bash
cd Server/go_gateway
docker compose --env-file .env.local up --build
```

**Terminal 3 — Admin GUI**

```bash
cd admin_gui
npm run dev
```

**Terminal 4 — Python Client**

```bash
conda activate xaliva-log
python run.py
```

The Python client reads `XALIVA_GATEWAY_URL=http://localhost:8080` from `.env` at the repo root and points at the local gateway. The Admin GUI reads `VITE_API_BASE_URL=http://localhost:8080` from `admin_gui/.env.local` and points at the same gateway. Both share the same Firebase project for authentication.
