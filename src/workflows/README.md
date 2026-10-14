# workflows — Background Worker Threads

## Overview

`workers.py` contains all background thread classes. Each worker inherits `BaseThreadWorker(QRunnable)`, runs in `QThreadPool`, and reports back via a shared signal `finished(worker_id, success, msg, payload)`.

---

## Threading Model

```
Model.submit_worker(worker)
  └─ QThreadPool.globalInstance().start(worker)
        └─ worker.run()  [background thread]
              └─ emit SharedWorkerSignals.finished(id, ok, msg, payload)
                    └─ Model._on_worker_finished()  [main thread via queued connection]
                          └─ _handlers[action](payload)
                                └─ emit Model signal → Bridge slot → QML update
```

### `BaseThreadWorker`

```python
class BaseThreadWorker(QRunnable):
    def __init__(self, worker_id: str, action: ACTION):
        ...
    signals: SharedWorkerSignals
    _cancel: bool
```

Subclasses override `run()`. The `worker_id` is a UUID string used to track active workers in `Model._active_workers`.

### `SharedWorkerSignals`

```python
class SharedWorkerSignals(QObject):
    finished = Signal(str, bool, str, object)  # worker_id, success, msg, payload
    progress = Signal(str, int, str)           # worker_id, percent, msg
```

---

## Cancellation Protocol

Cancellable workers check `self._cancel` between iterations:

```python
for filename in self.files:
    if self._cancel:
        break
    ...
```

`force_cancel()` on `BaseThreadWorker` sets `_cancel = True`. `Model.cleanup()` calls `force_cancel()` on all active workers before process exit. After cancellation, the worker emits `finished(id, False, MSG.CANCELLED, {})`.

---

## Fatal Exception Handling

Each worker's `run()` is wrapped in a `try/except`. Exception types listed in `*_FATALS` tuples (from `src/constants.py`) are caught and result in `finished(id, False, str(exc), {})`. Uncaught exceptions propagate to Qt's default exception handler.

| Tuple | Covers |
|-------|--------|
| `USB_FATALS` | `serial.SerialException`, `serial.SerialTimeoutException`, `OSError` |
| `CLOUD_FATALS` | `requests.RequestException`, `google.cloud.exceptions.GoogleCloudError` |
| `AUTH_FATALS` | `requests.RequestException`, `ValueError` |

---

## Helper Functions

### `_http_error_msg(exc) -> str`

Maps `requests.HTTPError` status codes to `GATEWAY_STATUS` sentinel strings used by the Model's 401 dedup handler:

| HTTP status | Returns |
|-------------|---------|
| 401 | `GATEWAY_STATUS.UNAUTHORIZED` |
| 403 | `GATEWAY_STATUS.FORBIDDEN` |
| 404 | `GATEWAY_STATUS.NOT_FOUND` |
| 5xx | `GATEWAY_STATUS.SERVER_ERROR` |

### `_build_points(plot_spec) -> list[list[QPointF]]`

Converts gateway `PlotSpec` JSON into lists of `QPointF` objects for Qt chart injection:

```python
x_values = plot_spec["x_values"]      # list of floats (seconds)
for series in plot_spec["series"]:
    points = [QPointF(x, y) for x, y in zip(x_values, series["data"])]
```

Returns `pts_per_series: list[list[QPointF]]` passed in the `payload` dict to `Bridge.on_analysis_completed`.

---

## Full Worker Catalogue

| Worker class | `ACTION` enum | Cancellable | Calls gateway? |
|---|---|---|---|
| `DetectUSBSerialWorker` | `DETECT_USB_SERIAL_PORTS` | No | No |
| `ConnectUSBSerialWorker` | `CONNECT_USB_SERIAL_PORT` | No | No |
| `DisconnectUSBSerialWorker` | *(no action)* | No | No |
| `ListUSBSerialWorker` | `LIST_USB_FILES` | No | No |
| `ReadUSBDeviceLogFile` | *(no action)* | No | Yes (parse) |
| `AnalyzeUSBSerialWorker` | *(no action)* | No | Yes (parse) |
| `SaveAllUSBSerialWorker` | `SAVE_ALL_USB_FILES_TO_DISK` | **Yes** | No |
| `SaveOneUSBFileWorker` | `SAVE_ONE_USB_FILE_TO_DISK` | No | No |
| `GatewayWorker` | `LIST_CLOUD_FILES` / `DOWNLOAD` | No | Yes |
| `GatewaySaveFilesWorker` | `DOWNLOAD_CLOUD_FILES_TO_DISK` | No | Yes |
| `GatewaySyncToCloudWorker` | `UPLOAD_USB_FILES_TO_CLOUD` | **Yes** | Yes |
| `GatewayACLPreflightWorker` | *(no action)* | No | Yes |
| `AnalyzeCloudWorker` | *(no action)* | No | Yes |
| `AnalyzeDiskSerialWorker` | *(no action)* | No | Yes (parse) |
| `AuthWorker` | `AUTHENTICATE` | No | No |
| `MFAWorker` | `MFA_ACTION` | No | No |
| `ForgotPasswordWorker` | *(no action)* | No | No |

---

## USB Workers

### `DetectUSBSerialWorker`
- **Input:** `usb_conn: USBSerialConnection`
- **Success payload:** `{ports: list[str]}`
- Calls `usb_conn.detect_ports()`.

### `ConnectUSBSerialWorker`
- **Input:** `usb_conn, port: str`
- **Success payload:** `{}`
- Calls `usb_conn.connect_to_port(port)`. Emits failure if `SerialException`.

### `DisconnectUSBSerialWorker`
- **Input:** `usb_conn`
- **Success payload:** `{}`
- Calls `usb_conn.disconnect()`.

### `ListUSBSerialWorker`
- **Input:** `usb_conn`
- **Success payload:** `{files: list[str]}`
- Calls `usb_conn.list_all_log_files()`.

### `ReadUSBDeviceLogFile`
- **Input:** `usb_conn, gateway: GatewayClient, cache: DiskCache, mac_id: str, filename: str`
- **Success payload:** `{device_log_data: list[dict]}`
- Cache-aside read via `read_log_using_cache`; then calls `gateway.parse_device_log(mac_id, data)`.

### `AnalyzeUSBSerialWorker`
- **Input:** `usb_conn, gateway, cache, mac_id, filename`
- **Success payload:** `{plot_spec: dict, pts_per_series: list[list[QPointF]]}`
- Cache-aside read; then calls `gateway.parse_test_log(mac_id, data)` and `_build_points(plot_spec)`.

### `SaveAllUSBSerialWorker`
- **Input:** `usb_conn, files: list[str], dest: str`
- **Success payload:** `{total: int, saved_count: int}`
- **Cancellable** — checks `_cancel` between files. Emits `progress` after each file.

### `SaveOneUSBFileWorker`
- **Input:** `usb_conn, filename: str, dest: str`
- **Success payload:** `{}`
- Single file read + disk write; used by per-row Save button in FileList.

---

## Cloud / Gateway Workers

### `GatewayWorker`
- **Input:** `gateway_client, mac_id, action: ACTION`
- **Success payload:** `{files: list[str]}` (LIST) or `{data: bytes}` (DOWNLOAD)
- Dispatches to `gateway_client.list_files()` or `gateway_client.read_file()`.

### `GatewaySaveFilesWorker`
- **Input:** `gateway_client, mac_id, files: list[str], dest: str`
- **Success payload:** `{saved: list[str], failed: list[str]}`
- For each file: `get_read_signed_url` → GET signed URL → write to `dest/filename`. Emits `progress` after each file.

### `GatewaySyncToCloudWorker`
- **Input:** `usb_conn, gateway, mac_id, files: list[str]`
- **Cancellable** — checks `_cancel` after each step.
- **Steps:**
  1. ACL preflight — `gateway.list_files(mac_id)` (verifies read access).
  2. Read each file from USB via `usb_conn.read_log_file()`.
  3. Memory reflash detection via `utils.is_memory_reflash(existing_cloud_files, usb_files)`.
  4. `gateway.get_upload_signed_urls(mac_id, filenames)` → `{session_id, signed_urls}`.
  5. For each file: `gateway.upload_file(signed_url, data)`. Emits `progress`.
  6. `gateway.commit_session(mac_id, session_id, files)`.
- **Success payload:** `{}`

### `GatewayACLPreflightWorker`
- **Input:** `gateway_client, mac_id`
- Calls `gateway.list_files(mac_id)` to verify the user has read access before attempting a sync.

### `AnalyzeCloudWorker`
- **Input:** `gateway_client, mac_id, filename`
- **Success payload:** `{plot_spec: dict, pts_per_series: list[list[QPointF]]}`
- `get_read_signed_url` → GET bytes → `parse_test_log` → `_build_points`.

### `AnalyzeDiskSerialWorker`
- **Input:** `gateway_client, mac_id, path: str`
- **Success payload:** `{plot_spec, pts_per_series}`
- Opens local file at `path` → `parse_test_log` → `_build_points`.

---

## Auth Workers

### `AuthWorker`
- **Input:** `auth_manager, type: AUTH_TYPE, email, password`
- **Success payload:** varies by `AUTH_TYPE` (see [`src/login/README.md`](../login/README.md))
- Calls `auth_manager.sign_in()` or `auth_manager.sign_up()`.

### `MFAWorker`
- **Input:** `auth_manager, mode: MFA_MODE, ...`
- **Modes:**
  - `ENROLL_START` → `auth_manager.start_mfa_enrollment(id_token)` → payload `{totp_uri, session_info}`
  - `ENROLL_FINALIZE` → `auth_manager.finalize_mfa_enrollment(...)` → payload `{}`
  - `FINALIZE_SIGNIN` → `auth_manager.finalize_mfa_signin(pending_cred, code)` → payload `{token}`

### `ForgotPasswordWorker`
- **Input:** `auth_manager, email`
- Calls `auth_manager.send_password_reset(email)`. Success payload `{}`.

---

## Related Files

- [`src/mvc/README.md`](../mvc/README.md) — `Model` submits and tracks workers
- [`src/cloud_conn/README.md`](../cloud_conn/README.md) — `GatewayClient` called by cloud workers
- [`src/usb_conn/README.md`](../usb_conn/README.md) — `USBSerialConnection` called by USB workers
- [`src/disk_cache/README.md`](../disk_cache/README.md) — `DiskCache` used by analyze workers
- [`src/login/README.md`](../login/README.md) — `AuthManager` called by auth workers
