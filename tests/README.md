# tests — Test Suite

## Strategy

| Kind | Location | Network required | Speed |
|------|----------|-----------------|-------|
| Unit (mocked) | `tests/` | No | Fast |
| Integration (live) | `tests/integration/` | Yes — Firebase + Gateway + GCS | Slow |

Unit tests mock all external I/O (`requests`, `serial`, Qt classes) so they run without any running services. Integration tests perform real end-to-end round trips and are skipped automatically if required environment variables are absent.

---

## Running Tests

```bash
# Full unit suite
pytest tests/ --maxfail=1 --disable-warnings -q

# Single test
pytest tests/test_controller.py::test_detect_ports_success -v

# Integration tests (needs .env with real credentials)
pytest tests/integration/ -v

# All tests including integration
pytest tests/ tests/integration/ -v
```

---

## Fixtures (`conftest.py`)

`tests/conftest.py` appends the project root to `sys.path` so imports of `src.*` work without installing the package.

| Fixture | Scope | Provides |
|---------|-------|---------|
| *(path setup only)* | module | `src.*` importable |

`tests/integration/conftest.py` loads `.env` and skips the entire integration suite if `GO_GATEWAY_URL` or `FIREBASE_API_KEY` are absent.

---

## Per-File Catalogue

### `test_controller.py`

Tests the signal wiring in `ApplicationController` and data transformations.

| Test | What it tests | Mocking strategy |
|------|---------------|-----------------|
| `test_detect_ports_success` | `detectClicked` → `model.detect_ports()` → ports appear in USB ComboBox and status bar | `unittest.mock.MagicMock` for `Model` and `Bridge`; manual signal emit |
| `test_detect_ports_none` | Empty port list triggers `show_error()` on Bridge | Same mocks |
| `test_plot_data_calculates_ms_correctly` | 100 Hz `PlotSpec` → x-axis values are `[0.0, 10.0, 20.0, ...]` ms | No Qt; pure function test on `enrich_plot_spec` |

---

### `test_gateway_client.py`

Tests all `GatewayClient` HTTP methods with mocked `requests`.

| Test | Endpoint tested | Mock |
|------|----------------|------|
| `TestListFiles` | `GET /api/files` | `unittest.mock.patch("requests.get")` returning `["file1.txt"]` |
| `TestGetReadSignedUrl` | `GET /api/signed-url/read` | `patch("requests.get")` returning URL string |
| `TestReadFile` | Two-request flow: signed URL → GCS GET | Two chained `patch` stubs |
| `TestGetUploadSignedUrl` | `POST /api/signed-url/upload` | `patch("requests.post")` returning `{session_id, signed_urls}` |
| `TestUploadFile` | `PUT` to GCS signed URL | `patch("requests.put")` asserting correct URL and body |
| `TestCommitSession` | `POST /api/commit` | `patch("requests.post")` asserting query params |
| `TestParseDeviceLog` | `POST /api/parse/device-log` | `patch("requests.post")` returning `[{key, val}]` |
| `TestParseTestLog` | `POST /api/parse/test-log` | `patch("requests.post")` returning `PlotSpec` dict |

All tests verify that the `Authorization: Bearer` header is set correctly by asserting the `headers` kwarg passed to `requests.*`.

---

### `test_gateway_sync_worker.py`

Tests `GatewaySyncToCloudWorker` lifecycle.

| Test | What it tests |
|------|--------------|
| `test_sync_worker_success` | Full happy path: list → USB read → upload → commit → `finished(True)` |
| `test_sync_worker_memory_reflash` | `is_memory_reflash` returns `True` → `finished(False, MSG.USB.MEMORY_REFLASH)` |
| `test_sync_worker_cancel` | `force_cancel()` mid-loop → `finished(False, ...)`, no commit called |
| `test_sync_worker_gateway_401` | Gateway raises 401 → `finished(False, GATEWAY_STATUS.UNAUTHORIZED)` |

Uses `MagicMock` for both `GatewayClient` and `USBSerialConnection`.

---

### `test_login_form_clear.py`

Tests that the `LoginOverlay` QML form fields are correctly cleared when switching between login modes.

| Test | What it tests |
|------|--------------|
| `test_fields_cleared_on_mode_switch` | Switching from login → register clears email, password, confirm fields |
| `test_mfa_field_cleared_on_show` | Entering MFA mode clears the TOTP code field |
| `test_forgot_password_clears_email` | Switching to forgot-password mode clears the email field |

Uses `pytest-qt` (`qtbot`) to load `LoginOverlay.qml` in a test QML engine and simulate mode switches.

---

### `integration/test_gateway_live.py`

Full round-trip tests against a live gateway + Firebase + GCS. Auto-skipped if `GO_GATEWAY_URL`, `TEST_EMAIL`, or `TEST_PASSWORD` env vars are absent.

| Test | Flow |
|------|------|
| `test_login_and_list` | Firebase sign-in → `list_files(mac_id)` → assert HTTP 200 |
| `test_upload_and_download` | Sign-in → signed upload URL → PUT test bytes → commit → list → signed read URL → GET → assert bytes match |
| `test_parse_device_log_live` | Sign-in → POST sample device log bytes → assert `[{key, val}]` response |

---

## Adding Tests

- Unit tests go in `tests/` named `test_<module>.py`.
- Use `pytest.fixture` in `conftest.py` for shared mock objects (e.g. a pre-configured `GatewayClient` with a `MagicMock` auth manager).
- Integration tests go in `tests/integration/` and must use `pytest.mark.skipif` or environment variable guards.
- Follow existing patterns: `patch` at the `requests` level (not at `GatewayClient` method level) to test the actual HTTP construction.

---

## CI

GitHub Actions workflow is not yet configured. Placeholder: add `.github/workflows/test.yml` running:

```yaml
- name: Run tests
  run: pytest tests/ --maxfail=1 --disable-warnings -q
```

---

## Related Files

- [`src/mvc/README.md`](../src/mvc/README.md) — what `test_controller.py` tests
- [`src/cloud_conn/README.md`](../src/cloud_conn/README.md) — what `test_gateway_client.py` tests
- [`src/workflows/README.md`](../src/workflows/README.md) — what `test_gateway_sync_worker.py` tests
