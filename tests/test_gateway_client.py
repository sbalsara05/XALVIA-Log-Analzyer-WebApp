"""Unit tests for GatewayClient.

Covers the three gateway operations:
  1. list_files       — GET /api/files?mac_id=X
  2. read_file        — GET /api/signed-url/read  → GCS GET (two requests.get calls)
  3. upload_file      — GET /api/signed-url/upload → GCS PUT (requests.get + requests.put)

No external HTTP traffic; every request is intercepted with unittest.mock.patch.
"""
import pytest
import requests
from unittest.mock import MagicMock, patch, call

from src.cloud_conn.gateway_client import (
    GatewayClient,
    GatewayError,
    GatewayUnauthorizedError,
    GatewayForbiddenError,
    GatewayMfaRequiredError,
    _raise_for_status,
)

# ---------------------------------------------------------------------------
# Constants shared across tests
# ---------------------------------------------------------------------------
GATEWAY_URL = "http://localhost:8080"
TOKEN       = "test-id-token"
MAC_ID      = "AABBCCDDEEFF"
FILENAME    = "device_log.txt"
GCS_READ_URL   = "https://storage.googleapis.com/xaliva_logs/signed_read"
GCS_UPLOAD_URL = "https://storage.googleapis.com/xaliva_logs/signed_upload"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
def _ok_response(json_data=None, content=b""):
    """Build a mock response that does NOT raise on raise_for_status()."""
    r = MagicMock()
    r.status_code = 200
    r.raise_for_status.return_value = None
    r.json.return_value = json_data
    r.content = content
    return r


def _error_response(status: int):
    """Build a mock response whose raise_for_status() raises HTTPError."""
    r = MagicMock()
    r.status_code = status
    exc = requests.HTTPError(response=r)
    r.raise_for_status.side_effect = exc
    return r


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------
@pytest.fixture
def auth():
    m = MagicMock()
    m.get_token.return_value = TOKEN
    return m


@pytest.fixture
def client(auth):
    return GatewayClient(auth_manager=auth, base_url=GATEWAY_URL)


# ---------------------------------------------------------------------------
# 1. list_files
# ---------------------------------------------------------------------------
class TestListFiles:
    def test_success_returns_list(self, client):
        # Gateway returns base filenames; session_id is resolved server-side.
        filenames = ["a.txt", "b.txt"]
        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=_ok_response(json_data=filenames)) as mock_get:
            result = client.list_files(MAC_ID)

        assert result == filenames
        mock_get.assert_called_once_with(
            f"{GATEWAY_URL}/api/files",
            params={"mac_id": MAC_ID},
            headers={"Authorization": f"Bearer {TOKEN}"},
            timeout=30,
        )

    def test_empty_list(self, client):
        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=_ok_response(json_data=[])):
            assert client.list_files(MAC_ID) == []

    def test_401_raises_http_error(self, client):
        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=_error_response(401)):
            with pytest.raises(requests.HTTPError) as exc_info:
                client.list_files(MAC_ID)
        assert exc_info.value.response.status_code == 401

    def test_403_raises_http_error(self, client):
        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=_error_response(403)):
            with pytest.raises(requests.HTTPError):
                client.list_files(MAC_ID)

    def test_bearer_token_sent(self, client, auth):
        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=_ok_response(json_data=[])) as mock_get:
            client.list_files(MAC_ID)

        _, kwargs = mock_get.call_args
        assert kwargs["headers"]["Authorization"] == f"Bearer {TOKEN}"


# ---------------------------------------------------------------------------
# 2. get_read_signed_url
# ---------------------------------------------------------------------------
class TestGetReadSignedUrl:
    def test_success_returns_url(self, client):
        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=_ok_response(json_data={"url": GCS_READ_URL})) as mock_get:
            url = client.get_read_signed_url(MAC_ID, FILENAME)

        assert url == GCS_READ_URL
        mock_get.assert_called_once_with(
            f"{GATEWAY_URL}/api/signed-url/read",
            params={"mac_id": MAC_ID, "filename": FILENAME},
            headers={"Authorization": f"Bearer {TOKEN}"},
            timeout=15,
        )

    def test_401_raises_http_error(self, client):
        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=_error_response(401)):
            with pytest.raises(requests.HTTPError) as exc_info:
                client.get_read_signed_url(MAC_ID, FILENAME)
        assert exc_info.value.response.status_code == 401

    def test_404_raises_http_error(self, client):
        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=_error_response(404)):
            with pytest.raises(requests.HTTPError):
                client.get_read_signed_url(MAC_ID, FILENAME)


# ---------------------------------------------------------------------------
# 3. read_file  (two requests.get calls: gateway → GCS)
# ---------------------------------------------------------------------------
class TestReadFile:
    def test_success_downloads_bytes(self, client):
        file_bytes = b"hello device log"
        gateway_resp = _ok_response(json_data={"url": GCS_READ_URL})
        gcs_resp     = _ok_response(content=file_bytes)

        with patch("src.cloud_conn.gateway_client.requests.get",
                   side_effect=[gateway_resp, gcs_resp]) as mock_get:
            result = client.read_file(MAC_ID, FILENAME)

        assert result == file_bytes
        # First call must hit the gateway signed-url endpoint
        assert mock_get.call_args_list[0] == call(
            f"{GATEWAY_URL}/api/signed-url/read",
            params={"mac_id": MAC_ID, "filename": FILENAME},
            headers={"Authorization": f"Bearer {TOKEN}"},
            timeout=15,
        )
        # Second call must hit the GCS signed URL directly (no auth header needed)
        assert mock_get.call_args_list[1] == call(GCS_READ_URL, timeout=60)

    def test_gateway_401_propagates(self, client):
        """If the gateway rejects the signed-URL request, HTTPError is raised."""
        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=_error_response(401)):
            with pytest.raises(requests.HTTPError) as exc_info:
                client.read_file(MAC_ID, FILENAME)
        assert exc_info.value.response.status_code == 401

    def test_gcs_download_error_propagates(self, client):
        """If the GCS download itself fails (e.g. expired URL), HTTPError is raised."""
        gateway_resp = _ok_response(json_data={"url": GCS_READ_URL})
        gcs_resp     = _error_response(403)   # GCS returns 403 on expired signed URL

        with patch("src.cloud_conn.gateway_client.requests.get",
                   side_effect=[gateway_resp, gcs_resp]):
            with pytest.raises(requests.HTTPError):
                client.read_file(MAC_ID, FILENAME)

    def test_two_get_calls_made(self, client):
        gateway_resp = _ok_response(json_data={"url": GCS_READ_URL})
        gcs_resp     = _ok_response(content=b"data")

        with patch("src.cloud_conn.gateway_client.requests.get",
                   side_effect=[gateway_resp, gcs_resp]) as mock_get:
            client.read_file(MAC_ID, FILENAME)

        assert mock_get.call_count == 2


SESSION_ID  = "20250303_120000_123456"


# ---------------------------------------------------------------------------
# 4. start_session
# ---------------------------------------------------------------------------
class TestStartSession:
    def test_success_returns_session_id(self, client):
        with patch("src.cloud_conn.gateway_client.requests.post",
                   return_value=_ok_response(json_data={"session_id": SESSION_ID})) as mock_post:
            result = client.start_session(MAC_ID)

        assert result == SESSION_ID
        mock_post.assert_called_once_with(
            f"{GATEWAY_URL}/api/session/start",
            params={"mac_id": MAC_ID},
            headers={"Authorization": f"Bearer {TOKEN}"},
            timeout=15,
        )

    def test_401_raises_http_error(self, client):
        with patch("src.cloud_conn.gateway_client.requests.post",
                   return_value=_error_response(401)):
            with pytest.raises(requests.HTTPError) as exc_info:
                client.start_session(MAC_ID)
        assert exc_info.value.response.status_code == 401

    def test_403_raises_http_error(self, client):
        with patch("src.cloud_conn.gateway_client.requests.post",
                   return_value=_error_response(403)):
            with pytest.raises(requests.HTTPError):
                client.start_session(MAC_ID)


# ---------------------------------------------------------------------------
# 5. get_upload_signed_url
# ---------------------------------------------------------------------------
class TestGetUploadSignedUrl:
    def test_success_returns_url(self, client):
        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=_ok_response(json_data={"url": GCS_UPLOAD_URL})) as mock_get:
            url = client.get_upload_signed_url(MAC_ID, SESSION_ID, FILENAME)

        assert url == GCS_UPLOAD_URL
        mock_get.assert_called_once_with(
            f"{GATEWAY_URL}/api/signed-url/upload",
            params={"mac_id": MAC_ID, "session_id": SESSION_ID, "filename": FILENAME},
            headers={"Authorization": f"Bearer {TOKEN}"},
            timeout=15,
        )

    def test_401_raises_http_error(self, client):
        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=_error_response(401)):
            with pytest.raises(requests.HTTPError):
                client.get_upload_signed_url(MAC_ID, SESSION_ID, FILENAME)

    def test_403_raises_http_error(self, client):
        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=_error_response(403)):
            with pytest.raises(requests.HTTPError):
                client.get_upload_signed_url(MAC_ID, SESSION_ID, FILENAME)


# ---------------------------------------------------------------------------
# 6. upload_file  (requests.get for signed URL + requests.put to GCS)
# ---------------------------------------------------------------------------
class TestUploadFile:
    def test_success_puts_to_gcs(self, client):
        data = b"raw log bytes"
        get_resp = _ok_response(json_data={"url": GCS_UPLOAD_URL})
        put_resp = _ok_response()

        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=get_resp) as mock_get, \
             patch("src.cloud_conn.gateway_client.requests.put",
                   return_value=put_resp) as mock_put:
            client.upload_file(MAC_ID, SESSION_ID, FILENAME, data)

        # Gateway GET must include session_id and use auth header
        mock_get.assert_called_once_with(
            f"{GATEWAY_URL}/api/signed-url/upload",
            params={"mac_id": MAC_ID, "session_id": SESSION_ID, "filename": FILENAME},
            headers={"Authorization": f"Bearer {TOKEN}"},
            timeout=15,
        )
        # GCS PUT must carry the raw data to the signed URL (no auth header)
        mock_put.assert_called_once_with(GCS_UPLOAD_URL, data=data, timeout=60)

    def test_gateway_401_propagates(self, client):
        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=_error_response(401)):
            with pytest.raises(requests.HTTPError) as exc_info:
                client.upload_file(MAC_ID, SESSION_ID, FILENAME, b"data")
        assert exc_info.value.response.status_code == 401

    def test_gcs_put_error_propagates(self, client):
        """If GCS rejects the PUT (e.g. signed URL expired), HTTPError is raised."""
        get_resp = _ok_response(json_data={"url": GCS_UPLOAD_URL})
        put_resp = _error_response(403)

        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=get_resp), \
             patch("src.cloud_conn.gateway_client.requests.put",
                   return_value=put_resp):
            with pytest.raises(requests.HTTPError):
                client.upload_file(MAC_ID, SESSION_ID, FILENAME, b"data")

    def test_put_not_called_when_gateway_rejects(self, client):
        """requests.put should never be called if the gateway refuses the URL request."""
        with patch("src.cloud_conn.gateway_client.requests.get",
                   return_value=_error_response(403)), \
             patch("src.cloud_conn.gateway_client.requests.put") as mock_put:
            with pytest.raises(requests.HTTPError):
                client.upload_file(MAC_ID, SESSION_ID, FILENAME, b"data")

        mock_put.assert_not_called()


# ---------------------------------------------------------------------------
# _raise_for_status — typed exception dispatch (C-1)
# ---------------------------------------------------------------------------

def _mock_resp(status: int, json_body=None, text: str = ""):
    """Build a minimal mock response for _raise_for_status tests."""
    r = MagicMock()
    r.status_code = status
    r.text = text
    if json_body is not None:
        r.json.return_value = json_body
    else:
        r.json.side_effect = ValueError("no JSON")
    return r


class TestRaiseForStatus:
    # ── 401 ──────────────────────────────────────────────────────────────────

    def test_401_raises_unauthorized(self):
        resp = _mock_resp(401)
        with pytest.raises(GatewayUnauthorizedError) as exc_info:
            _raise_for_status(resp)
        assert exc_info.value.status_code == 401

    def test_401_is_subclass_of_gateway_error(self):
        resp = _mock_resp(401)
        with pytest.raises(GatewayError):
            _raise_for_status(resp)

    # ── 403 generic ──────────────────────────────────────────────────────────

    def test_403_no_code_raises_forbidden(self):
        resp = _mock_resp(403, json_body={"error": "no permission"})
        with pytest.raises(GatewayForbiddenError) as exc_info:
            _raise_for_status(resp)
        assert exc_info.value.status_code == 403
        assert "no permission" in str(exc_info.value)

    def test_403_different_code_raises_forbidden(self):
        resp = _mock_resp(403, json_body={"error": "denied", "code": "no_admin_access"})
        with pytest.raises(GatewayForbiddenError):
            _raise_for_status(resp)

    def test_403_no_json_raises_forbidden(self):
        resp = _mock_resp(403, json_body=None, text="Forbidden")
        with pytest.raises(GatewayForbiddenError) as exc_info:
            _raise_for_status(resp)
        assert exc_info.value.status_code == 403

    def test_403_empty_json_raises_forbidden(self):
        resp = _mock_resp(403, json_body={})
        with pytest.raises(GatewayForbiddenError):
            _raise_for_status(resp)

    # ── 403 mfa_required ─────────────────────────────────────────────────────

    def test_403_mfa_required_raises_mfa_error(self):
        resp = _mock_resp(403, json_body={"error": "MFA required", "code": "mfa_required"})
        with pytest.raises(GatewayMfaRequiredError) as exc_info:
            _raise_for_status(resp)
        assert exc_info.value.status_code == 403

    def test_mfa_required_is_subclass_of_forbidden(self):
        """GatewayMfaRequiredError must be catchable as GatewayForbiddenError."""
        resp = _mock_resp(403, json_body={"code": "mfa_required"})
        with pytest.raises(GatewayForbiddenError):
            _raise_for_status(resp)

    def test_mfa_required_is_subclass_of_gateway_error(self):
        resp = _mock_resp(403, json_body={"code": "mfa_required"})
        with pytest.raises(GatewayError):
            _raise_for_status(resp)

    def test_mfa_required_message_uses_error_field(self):
        resp = _mock_resp(403, json_body={"error": "MFA required", "code": "mfa_required"})
        with pytest.raises(GatewayMfaRequiredError) as exc_info:
            _raise_for_status(resp)
        assert "MFA required" in str(exc_info.value)

    def test_mfa_required_fallback_message_when_no_error_field(self):
        resp = _mock_resp(403, json_body={"code": "mfa_required"})
        with pytest.raises(GatewayMfaRequiredError) as exc_info:
            _raise_for_status(resp)
        assert str(exc_info.value) != ""

    # ── 200 / other ──────────────────────────────────────────────────────────

    def test_200_does_not_raise(self):
        r = MagicMock()
        r.status_code = 200
        r.raise_for_status.return_value = None
        _raise_for_status(r)  # must not raise

    def test_500_delegates_to_raise_for_status(self):
        r = MagicMock()
        r.status_code = 500
        r.raise_for_status.side_effect = requests.HTTPError(response=r)
        with pytest.raises(requests.HTTPError):
            _raise_for_status(r)

    # ── type hierarchy sanity ────────────────────────────────────────────────

    def test_mfa_error_not_equal_to_plain_forbidden(self):
        assert GatewayMfaRequiredError is not GatewayForbiddenError

    def test_unauthorized_not_catchable_as_forbidden(self):
        resp = _mock_resp(401)
        with pytest.raises(GatewayUnauthorizedError):
            _raise_for_status(resp)
        # Verify it does NOT accidentally satisfy GatewayForbiddenError
        try:
            _raise_for_status(resp)
        except GatewayForbiddenError:
            pytest.fail("GatewayUnauthorizedError should not be caught as GatewayForbiddenError")
        except GatewayUnauthorizedError:
            pass
