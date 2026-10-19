# Cloud Deployment Guide

> **Security — Placeholder Legend**
> Every sensitive value in this guide uses a named placeholder. **Never substitute real values into committed files.** The placeholders are:
>
> | Placeholder | What it represents |
> |---|---|
> | `YOUR_PROJECT_ID` | GCP project ID |
> | `YOUR_REGION` | GCP region (e.g. `us-east1`) |
> | `YOUR_DB_INSTANCE` | Cloud SQL instance name (e.g. `ricovr-db`) |
> | `YOUR_DB_NAME` | Database name (e.g. `ricovr_db`) |
> | `YOUR_DB_USER` | Cloud SQL app user (e.g. `gateway`) |
> | `YOUR_DB_ROOT_PASSWORD` | Cloud SQL root (`postgres`) password — **never expose** |
> | `YOUR_DB_USER_PASSWORD` | App user DB password — **never expose** |
> | `YOUR_SERVICE_ACCOUNT` | Full service account email |
> | `YOUR_GCS_BUCKET` | GCS bucket name |
> | `YOUR_FIREBASE_API_KEY` | Firebase Web API key — a project identifier, visible in browser bundles; do not commit to public repos to avoid quota abuse |
> | `YOUR_FIREBASE_UID` | Firebase UID of the bootstrap admin user |
> | `YOUR_ADMIN_EMAIL` | Bootstrap admin email address |
> | `YOUR_GATEWAY_URL` | Cloud Run service URL (printed after deploy) |
> | `YOUR_SA_KEY_FILENAME` | Filename of the service account JSON key in `Server/go_gateway/` — **contains a private key, never expose, never commit, never share** |

All commands are written as single lines because pasting multi-line commands into Google Cloud Shell can cause premature execution.

---

## Preface — One-Time Project Setup

Complete these two steps before any phase in this guide. They only need to be done once.

### 1. Create a Google Cloud Project

In **GCP Console → IAM & Admin → Create a Project** (or via the project selector dropdown at the top of the console):

- Give it a meaningful name (e.g. `ricovr-device-cloud`)
- Note the auto-generated **Project ID** — this is `YOUR_PROJECT_ID` used throughout this guide
- Set the billing account

### 2. Enable Firebase with Google Identity Platform

1. Go to the **[Firebase Console](https://console.firebase.google.com/)** and click **Add project**
2. When the project selector asks which Google Cloud project to link to, **select the same project you just created** (`YOUR_PROJECT_ID`). Do not create a new project here — linking to the existing GCP project means Firebase Authentication, Cloud Run, and Cloud SQL all share the same project namespace and IAM.
3. Follow the setup wizard; you do not need to enable Google Analytics unless required
4. Once inside the Firebase project, go to **Authentication → Get started** to activate the Authentication service

---

## Pre-flight: Security Cleanup

**Do this before anything else.** The service account key JSON file sitting in `Server/go_gateway/` (`YOUR_SA_KEY_FILENAME.json`) contains a private RSA key that grants full impersonation of the service account. **Never commit it, never push it to any remote, never include it in any upload, and never share it publicly.** Anyone who obtains this file has the same GCP permissions as the service account.

Check whether git is already tracking it:

```bash
git ls-files Server/go_gateway/*.json
```

If that prints any filenames, remove them from tracking immediately:

```bash
git rm --cached Server/go_gateway/YOUR_SA_KEY_FILENAME.json
```

Add a gitignore rule so it can never be accidentally staged:

```bash
echo "YOUR_SA_KEY_FILENAME.json" >> Server/go_gateway/.gitignore
```

Also add `admin_gui/.env.production` to gitignore — it will hold `YOUR_FIREBASE_API_KEY` which should not be committed to public repos:

```bash
echo ".env.production" >> admin_gui/.gitignore
```

---

## Accessing Google Cloud Shell

Every `gcloud`, `firebase`, and `psql` command in this guide must be run inside a Google Cloud Shell environment. You have two options.

### Option A — Browser Cloud Shell (recommended for first-time setup)

1. Open **[GCP Console](https://console.cloud.google.com/)** in your browser
2. Click the **Activate Cloud Shell** button (terminal icon `>_`) in the top-right toolbar
3. A shell pane opens at the bottom of the browser — all `gcloud` commands run here authenticated as your GCP account automatically
4. To upload files, use the **⋮ (three-dot menu) → Upload** button in the Cloud Shell toolbar and upload ZIP files as described in each phase

### Option B — Desktop `gcloud` CLI

1. Download and install the [Google Cloud CLI](https://cloud.google.com/sdk/docs/install) for your OS
2. Authenticate: `gcloud auth login`
3. Set your project: `gcloud config set project YOUR_PROJECT_ID`
4. Run all commands in your local terminal from the relevant directory

**Important — `.gcloudignore` files are required for this option.**

When you run `gcloud builds submit` from your desktop, the CLI uploads the entire source directory to Cloud Build. A `.gcloudignore` file in the source directory works like `.gitignore` and tells the CLI which files to exclude from that upload. **Without it, sensitive files like the service account key and env files would be sent to Cloud Build.**

The `.gcloudignore` files are already created in the relevant directories:

- `Server/go_gateway/.gcloudignore` — includes `.gitignore` (which excludes `*.json`), plus additional exclusions for `.env*` files, `*.md`, and `.git*`
- `admin_gui/.gcloudignore` — includes `.gitignore` (which excludes `node_modules/`, `dist/`, `.env.local`), plus `.env*`

Both files use `#!include:.gitignore` as their first line. This is good practice: anything already protected from git commits is automatically protected from cloud uploads too, and you only need to add cloud-specific exclusions below it. If you ever update `.gitignore`, the protection carries over to `gcloud builds submit` without any extra step.

If you add any other directories that use `gcloud builds submit`, create a `.gcloudignore` with `#!include:.gitignore` as the first line, then add any additional cloud-specific exclusions below. Check what it excludes before every build — particularly confirm that no `*.json` key files and no `.env*` files are left unignored.

---

## Phase 1 — Cloud SQL (PostgreSQL)

### 1a. Create the instance

In **GCP Console → SQL → Create Instance → PostgreSQL**:

| Setting | Value |
|---|---|
| Edition | Enterprise |
| Preset | Sandbox (~$10/mo, upgradeable) |
| Instance ID | `YOUR_DB_INSTANCE` |
| Region | `YOUR_REGION` (match Cloud Run region to avoid cross-region latency) |
| Root password | `YOUR_DB_ROOT_PASSWORD` — **save securely in a password manager, never expose** |

After the instance is created:

1. **Databases** tab → **Create database** → name it `YOUR_DB_NAME`
2. **Users** tab → **Create user** → username `YOUR_DB_USER`, password `YOUR_DB_USER_PASSWORD` — **save this separately from the root password, never expose**
3. **Overview** tab → copy the **Connection name** — it looks like `YOUR_PROJECT_ID:YOUR_REGION:YOUR_DB_INSTANCE`. You will need this in Phase 2.

### 1b. Public IP and Authorized Networks

Cloud SQL creates a public IP by default. To connect from Cloud Shell for seeding (Phase 5), you need to temporarily whitelist Cloud Shell's outbound IP.

1. Go to **GCP Console → SQL → your instance → Connections → Networking**
2. Under **Public IP** → **Add a network**
3. In Cloud Shell, run `curl -s ifconfig.me` to get your current IP; enter it in CIDR form (e.g. `34.x.x.x/32`)
4. Name it `cloud-shell-temp` → **Done → Save**

> **Note**: Cloud Shell IP addresses rotate between sessions. If you reconnect later you may need to re-add your new IP. After seeding is complete (Phase 5e) **remove this entry** — Cloud Run connects via the internal Unix socket, not the public IP, so the app continues to work without it.

---

## Phase 2 — Secret Manager

> **Security**: The database connection string contains `YOUR_DB_USER_PASSWORD`. Always store it in Secret Manager, never in environment variables set in plaintext or in committed files.

In **GCP Console → Secret Manager → Create Secret**:

- **Name**: `GATEWAY_DATABASE_URL`
- **Value** — the full Cloud SQL Unix-socket DSN (replace all placeholders with real values when typing into the console; do not save the filled-in string anywhere else):

```
host=/cloudsql/YOUR_PROJECT_ID:YOUR_REGION:YOUR_DB_INSTANCE user=YOUR_DB_USER password=YOUR_DB_USER_PASSWORD dbname=YOUR_DB_NAME sslmode=disable
```

`sslmode=disable` is intentional here — Cloud Run routes this traffic through Google's internal encrypted Unix socket, not the public network.

---

## Phase 3 — Service Account

In **GCP Console → IAM & Admin → Service Accounts → Create**:

- **Name**: `ricovr-go-gateway-runner`
- The generated email will be: `ricovr-go-gateway-runner@YOUR_PROJECT_ID.iam.gserviceaccount.com` — use this as `YOUR_SERVICE_ACCOUNT`

Grant the service account these roles:

| Role | Why |
|---|---|
| Cloud SQL Client | Connect to Cloud SQL via Unix socket |
| Firebase Authentication Admin | Verify Firebase JWTs via Admin SDK |
| Storage Object Admin | Generate signed URLs for GCS |
| Service Account Token Creator | Sign GCS URLs via IAM Credentials API |
| Secret Manager Secret Accessor | Read `GATEWAY_DATABASE_URL` at runtime |

> Firebase JWT verification is handled by the Firebase Admin SDK using Google's public key endpoint — it does not require a separate IAM role beyond the ones listed above.

> Cloud Run uses ADC (Application Default Credentials) via this attached service account. **No JSON key file needs to be present on Cloud Run** — the service account JSON key in `Server/go_gateway/` was only needed for local development. This key JSON file can be deleted once the Cloud Run deployment is working.

---

## Phase 4 — Deploy Go Gateway to Cloud Run

### Uploading code to Cloud Shell

In **Google Cloud Shell**, click the **⋮ (three-dot menu) → Upload files** button in the toolbar (or the paperclip icon depending on your Cloud Shell version). Upload `Server/go_gateway/` as a ZIP file. Then in Cloud Shell:

```bash
export UPLOAD_ZIP="go_gateway.zip"
unzip ${UPLOAD_ZIP} -d go_gateway && cd go_gateway
```

> **Do not include `YOUR_SA_KEY_FILENAME.json`** in the uploaded ZIP — it is not needed on Cloud Run.

### 4a. Enable required APIs (one-time)

```bash
export PROJECT_ID="YOUR_PROJECT_ID"
gcloud services enable run.googleapis.com sqladmin.googleapis.com secretmanager.googleapis.com artifactregistry.googleapis.com --project=${PROJECT_ID}
```

### 4b. Grant Cloud Build permissions (if not already set)

```bash
export PROJECT_ID="YOUR_PROJECT_ID" ; export YOUR_GCP_USER_EMAIL="YOUR_ADMIN_EMAIL"
gcloud projects add-iam-policy-binding ${PROJECT_ID} --member="user:${YOUR_GCP_USER_EMAIL}" --role="roles/cloudbuild.builds.editor"
```

### 4c. Create Artifact Registry repository

```bash
export PROJECT_ID="YOUR_PROJECT_ID" ; export REGION="YOUR_REGION"
gcloud artifacts repositories create ricovr-images --repository-format=docker --location=${REGION} --project=${PROJECT_ID}
```

### 4d. Build and push image via Cloud Build

Run this from inside the `go_gateway/` directory. Cloud Build does the build and push — no local Docker required.

```bash
export PROJECT_ID="YOUR_PROJECT_ID" ; export REGION="YOUR_REGION"
gcloud builds submit --tag ${REGION}-docker.pkg.dev/${PROJECT_ID}/ricovr-images/gateway:latest --project=${PROJECT_ID}
```

### 4e. Deploy to Cloud Run

> **Security — Public Availability**: The `--allow-unauthenticated` flag makes this service publicly reachable on the internet without any Cloud Run-level authentication. This is intentional because the gateway validates Firebase JWTs on every request itself. However, there is **no rate limiting** at this layer. **For a production deployment, front this service with [Google Cloud Armor](https://cloud.google.com/armor) as a WAF and rate limiter before going live.** Without it, the endpoint is fully exposed and vulnerable to brute-force and DDoS attacks.

Export all variables, then deploy on one line:

```bash
export PROJECT_ID="YOUR_PROJECT_ID" ; export REGION="YOUR_REGION" ; export SA="YOUR_SERVICE_ACCOUNT" ; export GCS_BUCKET="YOUR_GCS_BUCKET" ; export CORS_ORIGINS="https://YOUR_PROJECT_ID.web.app"
gcloud run deploy xaliva-gateway --image ${REGION}-docker.pkg.dev/${PROJECT_ID}/ricovr-images/gateway:latest --region ${REGION} --platform managed --service-account ${SA} --add-cloudsql-instances ${PROJECT_ID}:${REGION}:YOUR_DB_INSTANCE --set-secrets DATABASE_URL=GATEWAY_DATABASE_URL:latest --set-env-vars "SERVER_MODE=UNIFIED,FIREBASE_PROJECT_ID=${PROJECT_ID},SERVICE_ACCOUNT_EMAIL=${SA},GCS_BUCKET=${GCS_BUCKET},CORS_ORIGINS=${CORS_ORIGINS},DB_MAX_OPEN_CONNS=5,DB_MAX_IDLE_CONNS=2" --allow-unauthenticated --project=${PROJECT_ID}
```

After the deploy completes, Cloud Run prints:

```
Service URL: https://xaliva-gateway-XXXXXXXXX.YOUR_REGION.run.app
```

Save this URL — it is `YOUR_GATEWAY_URL`. You will need it for the admin_gui `.env.production` (Phase 8).

---

## Phase 5 — Initialize Postgres: Schema and Seed

### Uploading SQL files to Cloud Shell

Upload `Postgres/init/` as a ZIP using the Cloud Shell upload button:

```bash
export UPLOAD_ZIP="init.zip"
unzip ${UPLOAD_ZIP} -d pg_init && cd pg_init
```

### 5a. Apply schema

```bash
export DB_HOST="YOUR_CLOUD_SQL_PUBLIC_IP" ; export DB_USER="YOUR_DB_USER" ; export DB_NAME="YOUR_DB_NAME"
psql -h ${DB_HOST} -U ${DB_USER} -d ${DB_NAME} -f 01_schema.sql
```

You will be prompted for `YOUR_DB_USER_PASSWORD`.

### 5b. Update the seed file with the real Firebase UID

Before running the seed, you must edit `02_seed.sql` to replace the placeholder UID and email with real values. **Do not commit this edited file.**

To find your bootstrap admin's Firebase UID:
1. Sign in to the app at least once using the admin account
2. Go to **Firebase Console → Authentication → Users**
3. Copy the **UID** column for that user

Edit `02_seed.sql` in Cloud Shell:

```bash
nano 02_seed.sql
```

Replace every occurrence of `YOUR_FIREBASE_UID` with the actual Firebase UID, and `YOUR_ADMIN_EMAIL` with the admin email. The UID appears in **three places** in the seed file: as the `users.id`, `users.added_by` (self-referential bootstrap), and `org_users.user_id` / `org_users.added_by`.

> **Why the seed is self-referential**: There is no higher authority at initialisation time, so the bootstrap admin's `added_by` references their own `id`. This is by design and satisfies the foreign key constraint.

### 5c. Apply seed

```bash
export DB_HOST="YOUR_CLOUD_SQL_PUBLIC_IP" ; export DB_USER="YOUR_DB_USER" ; export DB_NAME="YOUR_DB_NAME"
psql -h ${DB_HOST} -U ${DB_USER} -d ${DB_NAME} -f 02_seed.sql
```

### 5d. Remove the Cloud Shell authorized network

After seeding, go to **GCP Console → SQL → your instance → Connections → Networking → Authorized Networks**, remove the `cloud-shell-temp` entry, and save. The app connects via the internal Unix socket and is unaffected.

---

## Phase 6 — Grant Table Permissions to the Cloud Run Database User

The `YOUR_DB_USER` database user was created but has no privileges on the tables yet. Connect as the root `postgres` user and grant them:

```bash
export DB_HOST="YOUR_CLOUD_SQL_PUBLIC_IP" ; export DB_NAME="YOUR_DB_NAME" ; export APP_USER="YOUR_DB_USER"
psql -h ${DB_HOST} -U postgres -d ${DB_NAME} -c "GRANT SELECT, INSERT, UPDATE ON TABLE users, orgs, org_users, devices TO ${APP_USER}; GRANT USAGE, SELECT ON SEQUENCE orgs_id_seq TO ${APP_USER};"
```

You will be prompted for `YOUR_DB_ROOT_PASSWORD`.

> This grants DML access on all four application tables (`users`, `orgs`, `org_users`, `devices`) and sequence access for the `orgs` auto-increment primary key. The root user retains DDL privileges — `YOUR_DB_USER` cannot drop or alter tables.

---

## Phase 7 — Firebase Authentication

### 7a. Enable sign-in methods

In **Firebase Console → Authentication → Sign-in method**:
- Enable **Email/Password**

### 7b. Enable Multi-Factor Authentication

In **Firebase Console → Authentication → Sign-in method → Multi-factor authentication**:
- Enable **SMS** or **TOTP** as a second factor

### 7c. Configure authorized domains

After Firebase Hosting is deployed (Phase 8), add the hosting domain to the allowed list:

**Firebase Console → Authentication → Settings → Authorized domains** → Add:
- `YOUR_PROJECT_ID.web.app`
- `YOUR_PROJECT_ID.firebaseapp.com`
- Any custom domain if applicable

### 7d. Note on invite-only signup

The app uses invite-only registration enforced at two layers. First, disable self sign-up in **Firebase Console → Authentication → Settings → User actions** — uncheck **Enable create (sign-up)**. This prevents Firebase from creating new accounts at all unless an admin does so via the Admin SDK. Second, the gateway additionally checks that the user's email exists in the `users` table before allowing access. Ensure the bootstrap admin row from Phase 5 is correctly seeded before inviting any users.

---

## Phase 8 — Deploy admin_gui to Firebase Hosting

### Uploading code to Cloud Shell

Upload `admin_gui/` as a ZIP (exclude `node_modules/` to keep the upload small):

```bash
export UPLOAD_ZIP="admin_gui.zip"
unzip ${UPLOAD_ZIP} -d admin_gui && cd admin_gui
```

### 8a. Create `.env.production`

> **Security**: This file will contain `YOUR_FIREBASE_API_KEY`. **Never commit it.** Confirm `admin_gui/.gitignore` contains `.env.production` (set up in Pre-flight) before proceeding.

Create the file using `nano`:

```bash
nano .env.production
```

Paste the following content, replacing each placeholder with the real value:

```env
VITE_FIREBASE_API_KEY=YOUR_FIREBASE_API_KEY
VITE_FIREBASE_AUTH_DOMAIN=YOUR_PROJECT_ID.firebaseapp.com
VITE_FIREBASE_PROJECT_ID=YOUR_PROJECT_ID
VITE_API_BASE_URL=YOUR_GATEWAY_URL
```

> `YOUR_FIREBASE_API_KEY` is the Web API key from **Firebase Console → Project Settings → General → Your apps → Web app → SDK setup and configuration**. It is a project identifier that ends up bundled into the compiled frontend JavaScript and is visible to anyone who inspects the browser's network traffic or JS bundle — it is not a secret. However, avoid committing it to public repos to prevent quota abuse by third parties.

### 8b. Install dependencies and build

```bash
npm install
npm run build
```

### 8c. Install Firebase CLI and log in (one-time)

```bash
npm install -g firebase-tools
firebase login --no-localhost
```

`--no-localhost` is required in Cloud Shell — it prints a URL for you to open in your browser to complete the OAuth flow.

### 8d. Initialize Firebase Hosting (one-time)

```bash
export PROJECT_ID="YOUR_PROJECT_ID"
firebase init hosting --project ${PROJECT_ID}
```

When prompted:
- **Public directory**: `dist`
- **Single-page app (rewrite all URLs to /index.html)**: `Yes`
- **Overwrite `dist/index.html`**: `No`

### 8e. Deploy

```bash
export PROJECT_ID="YOUR_PROJECT_ID"
firebase deploy --only hosting --project ${PROJECT_ID}
```

Firebase prints the live URL:

```
Hosting URL: https://YOUR_PROJECT_ID.web.app
```

Go back to **Phase 7c** and add this domain to Firebase Authentication's authorized domains list if you have not done so already.

---

## Phase 9 — Desktop Client Distribution

For local development setup (conda environment, running with `python run.py`, running tests), see [development.md](development.md).

This phase covers building the distributable executable and configuring it to point at the production gateway.

### 9a. Build the Distributable

The `log_analyzer.spec` file configures a PyInstaller **onedir** build — the output is a folder containing the executable and all Qt shared libraries. Qt libraries are kept as separate shared libraries to satisfy PySide6's LGPL dynamic-linking requirement. All commands run locally on your development machine, not in Cloud Shell.

With the conda environment active, run from the repo root:

```bash
pyinstaller log_analyzer.spec
```

PyInstaller produces two directories:

| Directory | Contents |
|---|---|
| `build/log_analyzer/` | Intermediate build artefacts — safe to delete after a successful build |
| `dist/log_analyzer/` | The distributable folder — ship this to end users on Windows and Linux |
| `dist/log_analyzer.app` | macOS only: the same build wrapped as an app bundle - ship this to macOS users |

### 9b. Configure the Production Gateway URL

The `.env` file is **not** bundled into the executable. The app reads it from the working directory at launch. Create a `.env` file to distribute alongside the executable:

```env
XALIVA_GATEWAY_URL=YOUR_GATEWAY_URL
```

`YOUR_GATEWAY_URL` is the Cloud Run service URL printed at the end of Phase 4e.

### 9c. Run the Distributable

No Python installation or conda environment is required on the target machine:

```bash
# Linux
dist/log_analyzer/log_analyzer

# macOS
open dist/log_analyzer.app

# Windows
dist\log_analyzer\log_analyzer.exe
```

On Windows and Linux, place the `.env` from 9b in the same directory as the executable before launching.

On macOS, an app opened from Finder or with `open` starts in `/`, so it never finds a `.env` and uses the default gateway URL in `src/constants.py`. To use a `.env`, launch the executable inside the bundle from a terminal, in the directory that holds the `.env`:

```bash
cd path/to/folder-with-env
/path/to/log_analyzer.app/Contents/MacOS/log_analyzer
```

---

## Post-Deployment Checklist

- [ ] Service account JSON key (`YOUR_SA_KEY_FILENAME.json`) is not tracked by git and not in any remote branch
- [ ] `admin_gui/.env.production` is in `.gitignore` and not pushed anywhere
- [ ] `GATEWAY_DATABASE_URL` secret is only readable by the `ricovr-go-gateway-runner` service account
- [ ] Cloud Shell authorized network entry (`cloud-shell-temp`) has been removed from Cloud SQL
- [ ] Bootstrap admin can sign in at `YOUR_PROJECT_ID.web.app` and appears in the users table
- [ ] Firebase Authentication authorized domains includes the Hosting URL
- [ ] Desktop client `.env` distributed alongside the executable has `XALIVA_GATEWAY_URL` pointing at the Cloud Run URL
- [ ] **Production TODO**: Add Google Cloud Armor rate limiting in front of the Cloud Run service before going live
