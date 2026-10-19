# admin_gui — React Admin Dashboard

## Overview

React/TypeScript admin dashboard for managing organisations, users, and devices via the Go Gateway control-plane API. Firebase Authentication provides identity; the Go Gateway enforces TOTP MFA and org-admin ACLs.

All dashboard views (`OrgsView`, `UsersView`, `DevicesView`) are lazily loaded and sit behind `AuthGuard`. This means their code bundles — which import the Go Gateway service files — are not downloaded by the browser until after the user has authenticated. No Go Gateway API calls are made on the initial page load; the login page is the only thing served to an unauthenticated visitor. This avoids unnecessary traffic to the Cloud Run gateway URL before a session is established.

---

## Architecture

```
Browser
  └── Firebase Auth SDK         ← identity (sign-in, token, MFA enrollment)
  └── apiClient (Axios)         ← every Go Gateway call, token injected per request
        ├── orgsService          → GET/POST/DELETE /api/orgs
        ├── usersService         → GET/POST/DELETE /api/orgs/:id/users
        └── devicesService       → GET/POST/DELETE /api/orgs/:id/devices
```

---

## Auth Flow

Login (credential entry) calls Firebase Auth REST API (`identitytoolkit.googleapis.com`) — handled entirely by the Firebase Client SDK. On success, Firebase fires `onAuthStateChanged` locally. From there:

```
onAuthStateChanged (Firebase SDK — fires locally, no network call)
  │
  ├─ no user              → show LoginView
  │
  └─ user present
       │
       ├─ emailVerified?  (read from Firebase SDK user object — no network call)
       │     false → show VerifyEmailView
       │               "I've verified" → auth.currentUser.reload() → re-evaluate
       │
       └─ emailVerified = true
             │
             ├─ read multiFactor(user).enrolledFactors synchronously (no network call)
             │     false → show MfaSetupView (enroll TOTP before proceeding)
             │
             └─ enrolled
                   │
                   └─ GET /api/me/access  ← Go Gateway endpoint
                         200  → hasAdminAccess = true  → show dashboard
                         403  → hasAdminAccess = false → show AccessDeniedView
                         5xx  → accessCheckError = true → show AccessCheckErrorView
```

`AuthGuard` enforces this in four phases: loading spinner → email verification → MFA check → access check. Only after all four pass does it render the dashboard `<Outlet>`.

The `/api/me/access` call is guarded so it only fires when **both** `emailVerified = true` and `mfaEnrolled = true`. If either gate fails, the API call is skipped entirely — no leaking requests past an earlier failing gate.

### emailVerified behaviour — invite-only path

This app is invite-only. All users are added via the admin dashboard, which calls the Go Gateway (`POST /api/orgs/:id/users`). The gateway:

1. Creates the Firebase account via the **Admin SDK** with `EmailVerified(false)` (Firebase's documented default).
2. Sends a **password-reset OOB code** (`generatePasswordResetLink`) so the user can set their initial password.

When the invited user clicks the password-reset link and sets their password, Firebase marks `emailVerified = true` as a side effect — receiving and acting on the link proves ownership of the address. As a result, **invited users always arrive at the app with `emailVerified = true`** and the `VerifyEmailView` gate is not expected to trigger under normal operation.

The gate exists as a safeguard for accounts created outside the invite flow — for example, a user added directly in the Firebase console. Console-created users receive no OOB email automatically, so they arrive with `emailVerified = false` and are held at the gate until they click a verification link.

---

## `lib/api.ts` — Axios Client & Interceptors

`apiClient` is used exclusively for calls to the Go Gateway. All Firebase Auth operations (sign-in, token refresh, MFA enrollment) go through the Firebase Client SDK's own transport and are **not** intercepted by `apiClient`.

### Request interceptor

Calls `user.getIdToken(false)` on every request to the Go Gateway (returns the cached Firebase ID token; asks the Firebase token service at `securetoken.googleapis.com` to refresh it transparently if expired). Places the resulting token in `Authorization: Bearer <idToken>`.

### Response interceptor — Go Gateway error codes handled globally

Errors from Go Gateway responses are handled centrally in the response interceptor rather than in each individual service call. This prevents duplicating 401/403 logic across every endpoint and ensures a consistent sign-out and redirect behaviour app-wide.

The interceptor rejects with `ApiError` (exported from `lib/api.ts`), which extends `Error` with `status: number` and `code?: string`. Callers that need to distinguish denial from server error (e.g. `AuthContext`) check `instanceof ApiError && err.status === 403`.

| Status | Condition | Action |
|--------|-----------|--------|
| **401** | `code: "token_expired"` | Signs out via Firebase, redirects to `/login?reason=expired` |
| **401** | `code: "token_missing"` / `"token_invalid"` | Rejects with `ApiError(msg, 401, code)` |
| **403** | `code: "mfa_required"` | Signs out via Firebase, redirects to `/login?reason=mfa_required` |
| **403** | other | Rejects with `ApiError(serverMsg, 403, code)` |
| **any other 4xx / 5xx** | — | Rejects with `ApiError(msg, status)` |

Views never inspect status codes directly — they receive an `ApiError` with a user-facing message string from the interceptor.

The Firebase Client SDK independently handles errors for auth operations (e.g., `auth/wrong-password`, `auth/too-many-requests`, `auth/multi-factor-auth-required`, `securetoken` token-refresh failures). `LoginView` translates those SDK error codes into user-facing strings via `friendlyFirebaseError()`.

---

## Firebase API Calls (`src/lib/firebase.ts`, `src/contexts/AuthContext.tsx`, `src/views/`)

These call the Firebase Client SDK, which talks to Firebase Auth endpoints (`identitytoolkit.googleapis.com`, `securetoken.googleapis.com`). None of these go through `apiClient` or the Go Gateway.

| Call | Where | Purpose |
|------|-------|---------|
| `signInWithEmailAndPassword` | `LoginView` | Password auth — calls Firebase Auth REST API (`identitytoolkit.googleapis.com`) |
| `signInWithPopup` | `LoginView` | Google OAuth sign-in — calls Firebase Auth REST API |
| `onAuthStateChanged` | `AuthContext` | Reactive auth state listener — fires locally in the Firebase SDK when auth state changes; no network call for the listener itself |
| `multiFactor(user).enrolledFactors` | `AuthContext` | **No network call.** Reads enrolled MFA factors from the Firebase SDK's in-memory user profile, which was populated when `onAuthStateChanged` last fired |
| `user.getIdToken(false)` | `api.ts` interceptor | Returns the cached Firebase JWT if still valid; if expired, transparently calls Firebase's token service (`securetoken.googleapis.com`) to refresh it. The Go Gateway never issues tokens — it only validates the JWT it receives |
| `signOut(auth)` | `api.ts` interceptor | **Token expiry**: called automatically on 401 `token_expired` → redirects to `/login?reason=expired` |
| `signOut(auth)` | `api.ts` interceptor | **MFA required**: called automatically on 403 `mfa_required` → redirects to `/login?reason=mfa_required` |
| `sendEmailVerification(user)` | `VerifyEmailView` (rendered by `AuthGuard`) | Sends verification email on mount; also called on "Resend" (30 s cooldown). Only reached for non-invite-path accounts — see note above |
| `auth.currentUser.reload()` | `reloadUser()` in `AuthContext` | Refreshes the in-memory Firebase user object after the user clicks "I've verified". `reload()` mutates `auth.currentUser` without firing `onAuthStateChanged`, so `setEmailVerified` is called manually after the reload |
| `signOut(auth)` | `VerifyEmailView` (rendered by `AuthGuard`) | **Unverified email**: user clicks "Sign out" escape hatch |
| `signOut(auth)` | `AccessDeniedView` (rendered by `AuthGuard`) | **No admin access**: user clicks "Sign out" on the access-denied screen after `GET /api/me/access` returns a non-200 |
| `signOut(auth)` | `Header` | **Manual sign-out**: user clicks the sign-out button from the dashboard nav |
| TOTP enroll (`TotpMultiFactorGenerator`) | `MfaSetupView` | First-time authenticator app setup — calls Firebase Auth (`identitytoolkit.googleapis.com`) |
| TOTP verify (`TotpMultiFactorGenerator`) | `MfaSetupView` | Confirm enrollment with 6-digit code — calls Firebase Auth |

---

## Outgoing Go Gateway API Calls

All calls go to `VITE_API_BASE_URL` (default `http://localhost:8080`). Every request carries `Authorization: Bearer <idToken>`.

---

### `GET /api/me/access`

**Service:** `AuthContext` — called once per `onAuthStateChanged` event (i.e. on login, on page reload, after MFA enrollment completes) to gate dashboard entry.

| Response | Handled as |
|----------|------------|
| **200** `{"admin": true}` | `hasAdminAccess = true` — renders dashboard |
| **403** `{"error": "no admin access", "code": "no_admin_access"}` | `ApiError` (status 403) → `hasAdminAccess = false` → `AccessDeniedView` |
| **5xx / network error** | `ApiError` (status ≥ 500 or 0) → `accessCheckError = true` → `AccessCheckErrorView` with retry |
| **401** | Interceptor handles (see above) |

---

### `GET /api/orgs`

**Service:** `orgsService.list()` — fetched via React Query with key `["orgs"]`. Consumed by `OrgsView` (directly), `UsersView` and `DevicesView` (via the shared `useOrgSelector` hook), and `Header` (to display the primary org name in the top bar). React Query deduplicates concurrent fetches and returns the cached result if it was fetched within the last 30 seconds. The cache is invalidated (and the list refetched immediately) after org create or delete mutations in `OrgsView`.

| Response | Handled as |
|----------|------------|
| **200** `[{"id": int, "name": "...", "parent_org_id": int\|null}, ...]` | Populates org tree / selector |
| **401 / 403** | Interceptor rejects → toast error |
| **500** | Interceptor rejects → toast error |

---

### `POST /api/orgs`

**Service:** `orgsService.create(payload)` — triggered by "Create Org" form submission.

**Request body:** `{"name": "<org name>", "parent_org_id": <int>}`

The Go Gateway returns **201 Created** on success; there is no 200 response for this endpoint.

On 201, `OrgsView` calls `qc.invalidateQueries({ queryKey: ["orgs"] })`, which marks the React Query cache for the org list as stale and triggers an immediate background refetch of `GET /api/orgs` so the table reflects the new org.

| Response | Handled as |
|----------|------------|
| **201** `{"id": int, "name": "...", "parent_org_id": int}` | Invalidates `["orgs"]` React Query cache → refetch → success toast |
| **400** | Interceptor rejects with server message → destructive toast |
| **403** `"forbidden: not an admin of the specified parent org"` | Interceptor rejects → destructive toast |
| **409** `"an org with that name already exists under this parent"` | Interceptor rejects → destructive toast |
| **401** | Interceptor handles globally |

---

### `DELETE /api/orgs/:org_id`

**Service:** `orgsService.delete(orgId)` — triggered by "Delete Org" confirmation dialog.

| Response | Handled as |
|----------|------------|
| **200** `{"ok": true}` | Invalidates `["orgs"]` React Query cache → success toast |
| **200** `{"ok": true, "already_deleted": true}` | Same — idempotent |
| **403** `"forbidden: cannot delete the root org"` | Interceptor rejects → destructive toast |
| **401 / 403** | Interceptor handles globally |
| **500** | Interceptor rejects → destructive toast |

---

### `GET /api/orgs/:org_id/users`

**Service:** `usersService.listByOrg(orgId)` — fetched via React Query with key `["users", orgId]`. Called when `UsersView` is mounted with an org selected, and again whenever the selected org changes. Cache is invalidated after user upsert or revoke mutations.

`:org_id` is a URL path parameter. No request body.

| Response | Handled as |
|----------|------------|
| **200** `[{"user_id": "...", "email": "...", "role": "...", "can_read": bool, "can_upload": bool, "granted_at": "...", "added_by": "..."}, ...]` | Populates users table |
| **401 / 403** | Interceptor handles globally |
| **500** | Interceptor rejects → toast error |

---

### `POST /api/orgs/:org_id/users`

**Service:** `usersService.upsert(orgId, payload)` — triggered by "Add User" / "Edit Permissions" form.

**Request body:** `{"email": "...", "role": "admin"|"user", "can_read": bool, "can_upload": bool}`

Note: the view sends `can_read` / `can_upload` as supplied; the gateway forces both `true` when `role="admin"`.

| Response | Handled as |
|----------|------------|
| **200** `{"ok": true, "user_id": "...", "changed": true, "invited": bool}` | Invalidates `["users"]` React Query cache → `MSG.Users.SAVED` toast; if `email_warning` is also present, shows a second destructive toast (`MSG.Users.INVITE_EMAIL_WARNING`) |
| **200** `{"ok": true, "user_id": "...", "changed": false}` | Same — no-op on server, no `email_warning` |
| **400** | Interceptor rejects with server message → `MSG.Users.SAVE_FAILED` toast |
| **403** `"cannot change your own permissions"` | Interceptor rejects → destructive toast (UI also disables the action for self) |
| **403** `"forbidden: not an admin of this org"` | Interceptor rejects → destructive toast (handled globally) |
| **403** `code: "mfa_required"` | Interceptor signs out, redirects to `/login?reason=mfa_required` (handled globally) |
| **409** | Interceptor rejects → destructive toast |
| **401** | Interceptor handles globally |

---

### `DELETE /api/orgs/:org_id/users/:user_id`

**Service:** `usersService.revoke(orgId, userId)` — triggered by "Remove User" confirmation.

| Response | Handled as |
|----------|------------|
| **200** `{"ok": true}` | Invalidates `["users"]` React Query cache → `MSG.Users.REMOVED` toast |
| **200** `{"ok": true, "already_revoked": true}` | Same — idempotent |
| **403** `"cannot revoke your own membership"` | Interceptor rejects → `MSG.Users.REMOVE_FAILED` toast (UI also disables action for self) |
| **401 / 403** | Interceptor handles globally |
| **500** | Interceptor rejects → destructive toast |

---

### `GET /api/orgs/:org_id/devices`

**Service:** `devicesService.listByOrg(orgId)` — fetched via React Query with key `["devices", orgId]`. Called when `DevicesView` is mounted with an org selected, and again whenever the selected org changes. Cache is invalidated after device add or remove mutations.

`:org_id` is a URL path parameter. No request body.

| Response | Handled as |
|----------|------------|
| **200** `[{"id": "...", "org_id": int, "added_at": "...", "added_by": "..."}, ...]` | Populates devices table |
| **401 / 403** | Interceptor handles globally |
| **500** | Interceptor rejects → toast error |

---

### `POST /api/orgs/:org_id/devices`

**Service:** `devicesService.add(orgId, macId)` — triggered by "Register Device" form.

**Request body:** `{"mac_id": "<alphanumeric device ID>"}` — validated client-side against `MAC_RE = /^[0-9a-zA-Z]+$/` before sending.

| Response | Handled as |
|----------|------------|
| **201** `{"ok": true, "id": "<normalised mac_id>", "org_id": int}` | Invalidates `["devices"]` React Query cache → `MSG.Devices.REGISTERED` toast |
| **400** `"mac_id must be a non-empty alphanumeric string"` | Interceptor rejects → `MSG.Devices.REGISTER_FAILED` toast |
| **409** `"device already registered"` | Interceptor rejects → destructive toast |
| **401 / 403** | Interceptor handles globally |
| **500** | Interceptor rejects → destructive toast |

---

### `DELETE /api/orgs/:org_id/devices/:mac_id`

**Service:** `devicesService.remove(orgId, macId)` — triggered by "Remove Device" confirmation.

| Response | Handled as |
|----------|------------|
| **200** `{"ok": true}` | Invalidates `["devices"]` React Query cache → `MSG.Devices.REMOVED` toast |
| **200** `{"ok": true, "already_deleted": true}` | Same — idempotent |
| **404** `"device not found"` | Interceptor rejects → `MSG.Devices.REMOVE_FAILED` toast |
| **401 / 403** | Interceptor handles globally |
| **500** | Interceptor rejects → destructive toast |

---

## Error Surfacing Pattern

All mutations follow the same pattern:

```
onError: (err: Error) => toast({ title: MSG.<Resource>.<ACTION_FAILED>, description: err.message, variant: "destructive" })
```

`err.message` is the string produced by the interceptor — either the Go Gateway's `error` field or a hard-coded fallback. Views never inspect HTTP status codes or response bodies.

---

## Related Files

- [`src/lib/api.ts`](src/lib/api.ts) — Axios client and interceptors
- [`src/lib/constants.ts`](src/lib/constants.ts) — all user-facing strings (`MSG`)
- [`src/services/`](src/services/) — typed Go Gateway call wrappers
- [`src/contexts/AuthContext.tsx`](src/contexts/AuthContext.tsx) — Firebase auth state + Go Gateway access check
- [`src/components/AuthGuard.tsx`](src/components/AuthGuard.tsx) — route guard (MFA + admin check)
- [`Server/go_gateway/README.md`](../Server/go_gateway/README.md) — Go Gateway implementation reference
