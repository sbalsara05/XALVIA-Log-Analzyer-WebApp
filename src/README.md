# src — Python Application Source

## Module Map

| Sub-package | Purpose |
|-------------|---------|
| `mvc/` | `Model`, `Bridge`, `ApplicationController` — data layer and signal wiring |
| `login/` | `AuthManager`, `LoginController` — Firebase auth state machine |
| `workflows/` | 17 `QRunnable` worker classes for USB, cloud, and auth operations |
| `cloud_conn/` | `GatewayClient` — authenticated HTTP client for the Go Gateway |
| `usb_conn/` | `USBSerialConnection` — CP2102 serial protocol |
| `disk_cache/` | `DiskCache` — file-based log read cache |
| `constants.py` | Enums, message strings, device log codes |
| `dataclasses.py` | Parameter dataclasses for workers |
| `utils.py` | Pure helper functions |
| `main.py` | Qt bootstrap and logging setup |

---

## Startup Sequence

```
run.py
  └─ load_dotenv()                          # reads GO_GATEWAY_URL from .env
  └─ src.main.main()
        └─ get_base_path()                  # sys._MEIPASS (bundle) or __file__ (dev)
        └─ get_directories(base)            # resolves AppLocalData, Documents, temp
        └─ setup_logging(log_dir)           # rotating file + console handlers
        └─ QT_QUICK_CONTROLS_STYLE = "Basic"  # must be before QApplication
        └─ QApplication(sys.argv)
        └─ AuthManager()
        └─ Model(auth_manager, directories)
        └─ Bridge(model)
        └─ LoginController(bridge, model)
        └─ ApplicationController(bridge, model, login_controller)
              └─ _wire_qml_to_model()
              └─ _wire_auth_signals()
              └─ _wire_model_to_bridge()
        └─ QQmlApplicationEngine()
        └─ engine.rootContext().setContextProperty("bridge", bridge)
        └─ engine.load(base_path / "style/MainWindow.qml")
        └─ bridge.set_root(engine.rootObjects()[0])
        └─ bridge.show_login_overlay()      # login overlay appears
        └─ app.exec()                       # Qt event loop
```

---

## `constants.py`

### `ACTION` Enum

| Value | Integer | Description |
|-------|---------|-------------|
| `DETECT_USB_SERIAL_PORTS` | 10 | Scan for CP210x ports |
| `CONNECT_USB_SERIAL_PORT` | 11 | Open serial port |
| `DISCONNECT_USB_SERIAL_PORT` | 12 | Close serial port |
| `LIST_USB_FILES` | 13 | List files on USB device |
| `SAVE_ALL_USB_FILES_TO_DISK` | 14 | Save all selected USB files to disk |
| `STAGE_USB_FILES_FOR_CLOUD` | 15 | Read files from device into temp dir for upload |
| `UPLOAD_USB_FILES_TO_CLOUD` | 16 | Sync USB files to GCS via gateway |
| `PARSE_USB_DEVICE_LOG` | 17 | Parse binary device event log via gateway |
| `ANALYZE_USB_LOG` | 18 | Parse test log from USB device |
| `SAVE_ONE_USB_FILE_TO_DISK` | 19 | Save single USB file to disk |
| `LIST_CLOUD_FILES` | 20 | List committed cloud files |
| `ANALYZE_CLOUD_LOG` | 21 | Parse test log from cloud (signed-URL fetch + gateway parse) |
| `DOWNLOAD_CLOUD_FILES_TO_DISK` | 23 | Download cloud files to local disk |
| `GATEWAY_LIST_FILES` | 24 | Gateway ACL-aware file listing |
| `GATEWAY_READ_FILE` | 25 | Gateway signed-URL read + local save |
| `GATEWAY_ACL_PREFLIGHT` | 27 | Check gateway access before USB sync |
| `ANALYZE_DISK_LOG` | 30 | Parse test log from local disk |
| `AUTHENTICATE` | 40 | Firebase sign in |
| `FORGOT_PASSWORD` | 41 | Send password reset email |
| `MFA_ACTION` | 50 | TOTP enroll or verify |

### `TAB` Enum

| Value | Meaning |
|-------|---------|
| `USB` | USB tab — reads from serial connection |
| `CLOUD` | Cloud tab — reads from GCS via gateway |
| `PC` | PC tab — reads from local disk |

### `AUTH_TYPE` Enum

| Value | Meaning |
|-------|---------|
| `LOGIN` | Email + password sign in |

### `MFA_MODE` Enum

| Value | Meaning |
|-------|---------|
| `START` | Begin TOTP enrollment (get QR URI) |
| `FINALIZE` | Complete TOTP enrollment with code |
| `FINALIZE_SIGNIN` | Complete MFA signin with TOTP code |

### `MFA_PAYLOAD_TYPE`

Discriminates the payload type emitted by `MFAWorker`:

| Value | Meaning |
|-------|---------|
| `START` (`"mfa_start"`) | Payload contains QR secret from enrollment-start response |
| `FINALIZE` (`"mfa_finalize"`) | Payload contains result of enrollment or sign-in finalization |

### `SOURCE`

Log source provenance constants used internally to label where a log file came from:

| Value | Meaning |
|-------|---------|
| `USB` (`"usb"`) | Read from the connected XALIVA device over serial |
| `CLOUD` (`"cloud"`) | Downloaded from GCS via the gateway |
| `DISK` (`"disk"`) | Read from the local filesystem (PC tab) |

### `GATEWAY_STATUS` Sentinel Values

String constants returned by workers when the gateway responds with a known error. These are control-flow sentinels — not displayed to users.

| Constant | Value | Meaning |
|----------|-------|---------|
| `GATEWAY_STATUS.UNAUTHORIZED` | `"UNAUTHORIZED"` | Firebase token expired or invalid (401) |
| `GATEWAY_STATUS.FORBIDDEN` | `"FORBIDDEN"` | User lacks permission (403, generic) |
| `GATEWAY_STATUS.MFA_REQUIRED` | `"MFA_REQUIRED"` | 403 with `code=mfa_required` — user must complete TOTP enrollment |
| `GATEWAY_STATUS.NOT_FOUND` | `"NOT_FOUND"` | Device unknown — 404 from `/api/files` or ACL preflight |
| `GATEWAY_STATUS.FILE_NOT_FOUND` | `"FILE_NOT_FOUND"` | File missing from storage — 404 from `/api/signed-url/read` |

### `MSG` Hierarchy

All UI-facing strings live in nested classes under `MSG`:

| Namespace | Key constants |
|-----------|--------------|
| `MSG.Status` | `READY`, `CONNECTED`, `DISCONNECTED`, `SIGNED_IN`, `ANALYSIS_COMPLETE`, `PORT_DETECT_FAILED`, `PORTS_DETECTED`, `FETCH_FAILED`, `FETCHED_USB_LOGS`, `FETCHED_CLOUD_LOGS`, `SAVE_FAILED`, `SYNC_FAILED`, `DOWNLOAD_FAILED`, `CANNOT_PLOT`, `ANALYZING` |
| `MSG.Auth` | `SUCCESS`, `MFA_READY`, `MFA_START_FAILED`, `MFA_REQUIRED`, `NO_TOKEN`, `LOGIN_FAILED`, `MFA_VERIFY_FAILED`, `NO_SESSION`, `EMAIL_SENT`, `EMAIL_FAILED`, `SESSION_EXPIRED`, `SIGN_IN_REQUIRED`, `MFA_SESSION_EXPIRED`, `LOGIN_SUCCESSFUL`, `INTERNAL_ERROR`, `WORKER_ERROR`, `EMAIL_PASSWORD_REQUIRED`, `VERIFYING_CREDENTIALS`, `VERIFYING_CODE`, `MFA_CODE_INVALID`, `RESET_EMAIL_SENT`, `RESET_FAILED`, `SENDING_RESET_EMAIL`, `EMAIL_REQUIRED`, `MFA_ENROLLMENT_REQUIRED`, `MFA_ENROLLED`, `MFA_ENROLL_FAILED` |
| `MSG.USB` | `NO_DEVICE`, `PORT_NOT_OPEN`, `NO_LOG_FILES`, `DEVICE_LOG_UNREADABLE`, `LOG_UNREADABLE`, `FILE_NOT_FOUND`, `MEMORY_REFLASH`, `SAVE_STOPPED`, `SERIAL_DISCONNECTED`, `ALL_FILES_SAVED`, `FILES_SAVED_COUNT`, `FILE_SAVED`, `NO_OPTICAL_VALUES`, `UNEXPECTED_ERROR`, `SERVER_UNREACHABLE` |
| `MSG.Cloud` | `NO_DEVICE_ID`, `FETCH_FAILED`, `FETCH_FILES_FAILED`, `NO_FILES_TO_SYNC`, `NO_MAC_ID`, `ACCESS_DENIED`, `ACCESS_DENIED_WRITE`, `DEVICE_NOT_REGISTERED`, `FILE_NOT_FOUND`, `GATEWAY_ACCESS_DENIED`, `UPLOAD_STOPPED`, `ALL_FILES_UPLOADED` |
| `MSG.Export` | `NO_PLOT_DATA`, `EXPORTED`, `EXPORT_FAILED`, `SCREENSHOT_SAVED` |

### `DEVICE_LOG_CODE_MAP`

Maps string numeric keys (sent by the device) to human-readable event descriptions. Used by the UI when displaying raw device log entries.

| Code | Description |
|------|-------------|
| `"0"` | Device turn ON using charging cable |
| `"1"` | Device turn ON by pressing OK button |
| `"2"` | Battery % at boot-up time |
| `"3"` | POST PASS/FAIL ESP32/STM32 |
| `"4"` | Cartridge present at boot-up time |
| `"5"` | Cartridge remove which present at boot-up time |
| `"6"` | Start of Test Id |
| `"7"` | End of Test Id |
| `"8"` | Cartridge removal after end of test |
| `"9"` | Turn OFF by time-out |
| `"10"` | Turn OFF by user |
| `"11"` | Turn OFF by USB remove |
| `"12"` | Enter Settings |
| `"13"` | Exit Settings |
| `"14"` | Enter filevault |
| `"15"` | Exit filevault |

> The gateway's binary device-log decoder (binary opcode → event) is documented separately in [`Server/go_gateway/handlers/README.md`](../Server/go_gateway/handlers/README.md).

---

## `dataclasses.py`

Parameter dataclasses passed to worker constructors (avoid positional arg ordering bugs):

| Class | Fields | Used by |
|-------|--------|---------|
| `CloudListAllParams` | `mac_id: str` | `GatewayWorker(LIST)` |
| `USBListAllParams` | *(empty)* | `ListUSBSerialWorker` |
| `CloudReadLogParams` | `mac_id, target_dir, log_file_name` | `GatewayWorker(DOWNLOAD)` |
| `USBReadLogParams` | `log_file_name: str` | `ReadUSBDeviceLogFile` |

---

## `utils.py`

| Function | Signature | Description |
|----------|-----------|-------------|
| `is_memory_reflash` | `(cloud_files: list, usb_files: list) -> bool` | Returns `True` if USB file set does not include all cloud files (device memory was wiped and reflashed) |
| `is_always_fresh` | `(filename: str) -> bool` | Returns `True` for `*_devicelog.txt` and `track.txt` — these always bypass disk cache |
| `disk_test_log_files` | `(mac_id: str, base_dir: str) -> list[str]` | Lists saved test log files on disk for a given MAC ID |
| `save_file_helper` | `(data: bytes, dest_dir: str, filename: str) -> None` | Writes bytes to `dest_dir/filename`; creates dirs if needed |
| `move_files_no_overwrite` | `(src_dir: str, dest_dir: str) -> None` | Moves files from `src` to `dest`, skipping existing |
| `safe_del_dir` | `(path: str) -> None` | Deletes directory tree; swallows `FileNotFoundError` |
| `mac_from_filename` | `(filename: str) -> str \| None` | Extracts MAC ID prefix from log filenames (e.g. `"10352920_test.txt"` → `"10352920"`) |
| `enrich_plot_spec` | `(spec: dict) -> dict` | Injects axis ranges, frequency metadata, and display titles into a raw `PlotSpec` from the gateway |

---

## Logging

Configured in `main.py` via Python's `logging` module:

- **File handler:** `logs/xaliva.log` with `RotatingFileHandler` (max 5 MB × 3 backups)
- **Console handler:** `logging.StreamHandler` — `INFO` level in dev, `WARNING` in PyInstaller bundle
- **Format:** `%(asctime)s [%(levelname)s] %(name)s: %(message)s`
- Log directory is `logs/` relative to the base path (created if absent).

---

## Gotchas

- **`sys._MEIPASS`** — PyInstaller bundles set this to the temp extraction directory. `get_base_path()` checks `hasattr(sys, "_MEIPASS")` and uses it; otherwise falls back to `Path(__file__).parent.parent`. All resource paths (QML, icons) must use `get_base_path()`, not `__file__` directly.
- **`QT_QUICK_CONTROLS_STYLE = "Basic"`** — must be set as an environment variable **before** `QApplication` is created. Setting it after has no effect and the native style overrides QML control appearance. Done in `main.py` before any Qt import side-effects.
- **`.env` loading** — `python-dotenv` `load_dotenv()` is called in `run.py` before `src.main` is imported, so `GO_GATEWAY_URL` is available when `GatewayClient` is constructed in `Model.__init__`.

---

## Related Files

- [`src/mvc/README.md`](mvc/README.md)
- [`src/login/README.md`](login/README.md)
- [`src/workflows/README.md`](workflows/README.md)
- [`src/cloud_conn/README.md`](cloud_conn/README.md)
- [`src/usb_conn/README.md`](usb_conn/README.md)
- [`src/disk_cache/README.md`](disk_cache/README.md)
- [Root README](../../README.md)
