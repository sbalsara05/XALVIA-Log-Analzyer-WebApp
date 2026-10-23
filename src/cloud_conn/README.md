# cloud_conn — Go Gateway HTTP Client

## Overview

`gateway_client.py` is the sole HTTP client for the Go Gateway. It wraps `requests` to add Firebase Bearer token authentication on every call and provides typed methods for each gateway endpoint.

---

## Why a Gateway?

The Python client never calls GCS directly (except via signed URLs). All operations go through the Go Gateway because:

- **ACL enforcement** — only users with org membership and the correct permission flags can access devices.
- **Signed URL generation without raw keys** — the gateway signs GCS URLs via IAM; the private key stays in Google's infrastructure.
- **Server-side log parsing** — binary protocol parsing runs in Go on the server, keeping complex parsing logic centralised.

---

## `GatewayClient`

### Constructor

```python
GatewayClient(base_url: str, auth_manager: AuthManager)
```

`base_url` is read from the `GO_GATEWAY_URL` environment variable (set in `.env`). `auth_manager` is used to fetch a fresh Firebase ID token on every request.

### Token Handling

`_headers()` calls `auth_manager.get_token()` on every request. `get_token()` returns `auth_manager.id_token` — a cached value that is updated after each successful login or MFA step. The token is placed in `Authorization: Bearer <idToken>`. If the gateway returns `401`, the caller (a worker) emits `GATEWAY_STATUS.UNAUTHORIZED` which triggers the Model's 401 dedup re-login flow. A `403` with `code: "mfa_required"` raises `GatewayMfaRequiredError` rather than `GatewayForbiddenError` (see [Error Handling](#error-handling) below for the full exception hierarchy).

---

## Endpoint Reference

### `GET /api/files`

**Method:** `list_files(mac_id)`

| | Detail |
|---|---|
| Query params | `mac_id=<alphanumeric device ID>` |
| Auth header | `Authorization: Bearer <idToken>` |
| **200 OK** | JSON array of committed filenames: `["file1.txt", "file2.txt"]` |
| **400** | `{"error": "mac_id is missing or not a valid MAC address"}` — invalid mac_id format |
| **401** | Token invalid/expired → raises `GatewayUnauthorizedError` |
| **403** | User not in device's org tree, or org/device deleted → raises `GatewayForbiddenError` |
| **500** | GCS storage error |

---

### `GET /api/signed-url/read`

**Method:** `get_read_signed_url(mac_id, filename)` → called internally by `read_file(mac_id, filename)`

| | Detail |
|---|---|
| Query params | `mac_id=<id>`, `filename=<bare filename>` |
| Auth header | `Authorization: Bearer <idToken>` |
| **200 OK** | `{"url": "<GCS signed GET URL>"}` — expires in 15 minutes |
| **400** | `{"error": "mac_id and filename query params required"}` |
| **401** | → raises `GatewayUnauthorizedError` |
| **403** | → raises `GatewayForbiddenError` |
| **404** | File not found in any committed session |
| **500** | `{"error": "could not generate download URL"}` — IAM signing failed → raises `requests.HTTPError` |

`read_file` then GETs the returned URL directly from GCS (no gateway in the data path). The GCS GET uses `requests.raise_for_status()` — a 403/404 there raises `requests.HTTPError`.

---

### `POST /api/signed-url/upload`

**Method:** `get_upload_signed_urls(mac_id, filenames)` — Step 1 of the upload flow

| | Detail |
|---|---|
| Auth header | `Authorization: Bearer <idToken>` |
| Request body | `{"mac_id": "<id>", "files": ["f1.txt", "f2.txt"]}` |
| **200 OK** | `{"session_id": "YYYYMMDD_HHMMSS_microseconds", "urls": {"f1.txt": "<signed PUT URL>", ...}, "required_headers": {"X-Goog-Content-Length-Range": "0,52428800"}}` |
| **400** | `{"error": "mac_id must be a non-empty alphanumeric string"}` or `{"error": "files array must not be empty"}` |
| **401** | → raises `GatewayUnauthorizedError` |
| **403** | → raises `GatewayForbiddenError` |
| **500** | Failed to generate signed URL via IAM |

**`required_headers`** must be forwarded verbatim in every subsequent `upload_file` PUT — GCS verifies them as part of the signed URL signature. If omitted, GCS returns `SignatureDoesNotMatch` (403).

---

### `PUT <signed GCS URL>`

**Method:** `upload_file(signed_url, data, required_headers)` — Step 2 of the upload flow

This call goes **directly to GCS**, not through the gateway. The signed URL is self-authenticating.

| | Detail |
|---|---|
| Headers | `Content-Type: application/octet-stream` + all keys from `required_headers` |
| Request body | Raw file bytes |
| **200 OK** | Empty body — upload succeeded |
| **403** | `SignatureDoesNotMatch` — `required_headers` missing or wrong, or URL expired |
| **400** | Content-Length-Range constraint violated (file too large) |

Uses `resp.raise_for_status()` (not `_raise_for_status`) — raises `requests.HTTPError` on failure.

---

### `POST /api/commit`

**Method:** `commit_session(mac_id, session_id, files) -> str` — Step 3 of the upload flow; returns the committed GCS path on success

| | Detail |
|---|---|
| Query params | `mac_id=<id>`, `session_id=YYYYMMDD_HHMMSS_microseconds` |
| Auth header | `Authorization: Bearer <idToken>` |
| Request body | JSON array of uploaded filenames: `["f1.txt", "f2.txt"]` |
| **200 OK** | `{"committed": "<mac_id>/<session_id>/_COMMIT.json"}` |
| **400** | `{"error": "mac_id and session_id required"}` or `{"error": "session_id format invalid"}` |
| **401** | → raises `GatewayUnauthorizedError` |
| **403** | → raises `GatewayForbiddenError` |
| **500** | GCS write/finalize failed |

---

### `POST /api/parse/device-log`

**Method:** `parse_device_log(mac_id, data)`

| | Detail |
|---|---|
| Query params | `mac_id=<id>` |
| Auth header | `Authorization: Bearer <idToken>` |
| Request body | Raw hex log bytes (`Content-Type: application/octet-stream`) |
| **200 OK** | `[{"key": "<timestamp>", "val": "<description>"}, ...]` |
| **400** | `{"error": "mac_id query param required"}` |
| **401** | → raises `GatewayUnauthorizedError` |
| **403** | → raises `GatewayForbiddenError` |

---

### `POST /api/parse/test-log`

**Method:** `parse_test_log(mac_id, data)` → returns `(success: bool, plot_spec: dict)`

| | Detail |
|---|---|
| Query params | `mac_id=<id>` |
| Auth header | `Authorization: Bearer <idToken>` |
| Request body | Raw test log bytes (`Content-Type: application/octet-stream`) |
| **200 OK** | `{"success": true/false, "title": "...", "x_values": [...], "series": [...], ...}` — full `PlotSpec` |
| **400** | `{"error": "mac_id query param required"}` |
| **401** | → raises `GatewayUnauthorizedError` |
| **403** | → raises `GatewayForbiddenError` |

`success: false` in the 200 body means parsing succeeded but no optical values were found (not an error — emits `MSG.USB.NO_OPTICAL_VALUES`).

---

## Upload Flow Detail

```
Client                  Gateway                 GCS
  |                        |                     |
  |--POST /api/signed-url/upload (JSON body)-->  |
  |<--200 {session_id, urls, required_headers}-- |
  |                        |                     |
  |--PUT <signed_url> (raw bytes + req headers)--------->
  |<--200 OK--------------------------------------------- |
  |                        |                     |
  |--POST /api/commit?mac_id=X&session_id=Y-->   |
  |                        |--write _COMMIT.json-------->
  |<--200 {committed: ".../_COMMIT.json"}---     |
```

The gateway is **not** in the upload data path — bytes go directly from the Python client to GCS. Only URL generation (step 1) and commit marker writing (step 3) touch the gateway.

---

## Error Handling

### Exception hierarchy

```
GatewayError
├── GatewayUnauthorizedError   (HTTP 401) — token invalid/expired
└── GatewayForbiddenError      (HTTP 403) — no permission
    └── GatewayMfaRequiredError (HTTP 403 + code="mfa_required") — TOTP not enrolled
```

`_raise_for_status(resp)` raises these typed exceptions for 401/403. All other 4xx/5xx fall through to `requests.raise_for_status()` which raises `requests.HTTPError`.

### Worker sentinel mapping

Workers catch both typed gateway exceptions and `requests.HTTPError` separately and emit `finished(False, sentinel, payload)`:

| Exception | Sentinel emitted | Handling in Model |
|-----------|-----------------|-------------------|
| `GatewayUnauthorizedError` | `GATEWAY_STATUS.UNAUTHORIZED` | Triggers re-login overlay |
| `GatewayForbiddenError` | `GATEWAY_STATUS.FORBIDDEN` | Shows access denied message |
| `GatewayMfaRequiredError` | `GATEWAY_STATUS.MFA_REQUIRED` | Routes to MFA enrollment flow |
| `requests.HTTPError` (404) | `GATEWAY_STATUS.FILE_NOT_FOUND` (file ops) or `GATEWAY_STATUS.NOT_FOUND` | Shows not-found message |
| `requests.HTTPError` (other) | `GATEWAY_ERROR_<status>` | Shows unexpected error message |
| `requests.ConnectionError` / `Timeout` | `MSG.USB.SERVER_UNREACHABLE` | Shows server unreachable message |

Workers that make gateway calls: `ReadUSBDeviceLogFile`, `AnalyzeUSBSerialWorker`, `AnalyzeCloudWorker`, `AnalyzeDiskSerialWorker`, `GatewaySyncToCloudWorker`, `GatewayACLPreflightWorker`, `GatewayWorker`, `GatewaySaveFilesWorker`.

---

## Gotchas

- **Signed URL TTL** — PUT and GET signed URLs expire after 15 minutes. Large uploads must complete within this window.
- **`required_headers` are mandatory** — they are part of the GCS signed URL string-to-sign. Omitting them causes `SignatureDoesNotMatch` (403) directly from GCS, not the gateway.
- **`upload_file` goes to GCS, not the gateway** — the URL is self-authenticating via query parameters; no `Authorization` header is sent.
- **`read_file` is two calls** — it calls `get_read_signed_url` first, then GETs the returned URL. Workers that only need the URL call `get_read_signed_url` directly.
- **`mac_id` normalisation** — the gateway uppercases and validates alphanumeric mac_ids. `10352920` and `DEVICE01` are valid; `AA:BB:CC` (colons) or empty strings are rejected with 400.
- **Device org membership is required** — even with a valid token, 403 is returned if the user has no `org_users` row in the device's org tree with `can_read=true` or `role='admin'`.

---

## Related Files

- [`src/workflows/README.md`](../workflows/README.md) — workers that call `GatewayClient`
- [`src/login/README.md`](../login/README.md) — `AuthManager` that provides tokens
- [`Server/go_gateway/README.md`](../../../Server/go_gateway/README.md) — gateway implementation
- [`src/README.md`](../README.md) — startup sequence and module map
