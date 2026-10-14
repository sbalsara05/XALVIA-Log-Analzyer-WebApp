"""Unit tests for AuthManager — Task C-4 coverage.

Tests finalize_mfa_enrollment after the C-4 fix:
  - Removed invalid mfaEnrollmentId field from the payload.
  - displayName changed to "XALIVA TOTP".
  - Success returns MSG.Auth.MFA_ENROLLED (not the generic SUCCESS string).
  - Failure returns MSG.Auth.MFA_ENROLL_FAILED (not a raw Firebase message or None).

No real network traffic; every requests.post call is mocked.
"""
import pytest
from unittest.mock import MagicMock, patch

from src.login.auth_manager import AuthManager
from src.constants import MSG


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _ok_response(id_token="new-id-token"):
    r = MagicMock()
    r.status_code = 200
    r.json.return_value = {"idToken": id_token}
    return r


def _error_response(status: int, json_body=None):
    r = MagicMock()
    r.status_code = status
    if json_body is not None:
        r.json.return_value = json_body
    else:
        r.json.side_effect = ValueError("no JSON")
    return r


# ---------------------------------------------------------------------------
# Fixture
# ---------------------------------------------------------------------------

@pytest.fixture
def manager():
    am = AuthManager()
    am.id_token = "current-id-token"
    am.email = "test@example.com"
    return am


SESSION_INFO = "opaque-session-info-string"
USER_CODE    = "123456"


# ---------------------------------------------------------------------------
# 1. Success path
# ---------------------------------------------------------------------------

class TestFinalizeMfaEnrollmentSuccess:
    def test_returns_true_on_200(self, manager):
        with patch("src.login.auth_manager.requests.post",
                   return_value=_ok_response()):
            success, _, _ = manager.finalize_mfa_enrollment(SESSION_INFO, USER_CODE)
        assert success is True

    def test_message_is_mfa_enrolled(self, manager):
        with patch("src.login.auth_manager.requests.post",
                   return_value=_ok_response()):
            _, msg, _ = manager.finalize_mfa_enrollment(SESSION_INFO, USER_CODE)
        assert msg == MSG.Auth.MFA_ENROLLED

    def test_message_is_not_generic_success(self, manager):
        with patch("src.login.auth_manager.requests.post",
                   return_value=_ok_response()):
            _, msg, _ = manager.finalize_mfa_enrollment(SESSION_INFO, USER_CODE)
        assert msg != MSG.Auth.SUCCESS

    def test_payload_type_is_mfa_finalize(self, manager):
        with patch("src.login.auth_manager.requests.post",
                   return_value=_ok_response()):
            _, _, payload = manager.finalize_mfa_enrollment(SESSION_INFO, USER_CODE)
        assert payload.get("type") == "mfa_finalize"

    def test_id_token_is_updated(self, manager):
        new_token = "refreshed-id-token"
        with patch("src.login.auth_manager.requests.post",
                   return_value=_ok_response(id_token=new_token)):
            manager.finalize_mfa_enrollment(SESSION_INFO, USER_CODE)
        assert manager.id_token == new_token


# ---------------------------------------------------------------------------
# 2. Payload structure — the C-4 bug fixes
# ---------------------------------------------------------------------------

class TestFinalizeMfaEnrollmentPayload:
    def _captured_payload(self, manager):
        """Run finalize_mfa_enrollment and return the JSON body that was posted."""
        with patch("src.login.auth_manager.requests.post",
                   return_value=_ok_response()) as mock_post:
            manager.finalize_mfa_enrollment(SESSION_INFO, USER_CODE)
        _, kwargs = mock_post.call_args
        return kwargs["json"]

    def test_payload_excludes_mfa_enrollment_id(self, manager):
        payload = self._captured_payload(manager)
        assert "mfaEnrollmentId" not in payload, (
            "mfaEnrollmentId belongs to mfaSignIn:finalize, not mfaEnrollment:finalize"
        )

    def test_payload_display_name_is_xaliva_totp(self, manager):
        payload = self._captured_payload(manager)
        assert payload.get("displayName") == "XALIVA TOTP"

    def test_payload_includes_session_info(self, manager):
        payload = self._captured_payload(manager)
        totp_info = payload.get("totpVerificationInfo", {})
        assert totp_info.get("sessionInfo") == SESSION_INFO

    def test_payload_includes_verification_code(self, manager):
        payload = self._captured_payload(manager)
        totp_info = payload.get("totpVerificationInfo", {})
        assert totp_info.get("verificationCode") == USER_CODE

    def test_payload_includes_id_token(self, manager):
        payload = self._captured_payload(manager)
        assert payload.get("idToken") == "current-id-token"


# ---------------------------------------------------------------------------
# 3. Failure paths — message is never None
# ---------------------------------------------------------------------------

class TestFinalizeMfaEnrollmentFailure:
    def test_returns_false_on_400(self, manager):
        resp = _error_response(400, json_body={"error": {"message": "INVALID_CODE"}})
        with patch("src.login.auth_manager.requests.post", return_value=resp):
            success, _, _ = manager.finalize_mfa_enrollment(SESSION_INFO, USER_CODE)
        assert success is False

    def test_message_is_mfa_enroll_failed_on_400(self, manager):
        resp = _error_response(400, json_body={"error": {"message": "INVALID_CODE"}})
        with patch("src.login.auth_manager.requests.post", return_value=resp):
            _, msg, _ = manager.finalize_mfa_enrollment(SESSION_INFO, USER_CODE)
        assert msg == MSG.Auth.MFA_ENROLL_FAILED

    def test_message_is_not_none_when_no_error_field(self, manager):
        resp = _error_response(400, json_body={})
        with patch("src.login.auth_manager.requests.post", return_value=resp):
            _, msg, _ = manager.finalize_mfa_enrollment(SESSION_INFO, USER_CODE)
        assert msg is not None

    def test_message_is_not_none_when_no_json_body(self, manager):
        resp = _error_response(400, json_body=None)
        with patch("src.login.auth_manager.requests.post", return_value=resp):
            _, msg, _ = manager.finalize_mfa_enrollment(SESSION_INFO, USER_CODE)
        assert msg is not None

    def test_id_token_unchanged_on_failure(self, manager):
        original_token = manager.id_token
        resp = _error_response(400, json_body={"error": {"message": "INVALID_CODE"}})
        with patch("src.login.auth_manager.requests.post", return_value=resp):
            manager.finalize_mfa_enrollment(SESSION_INFO, USER_CODE)
        assert manager.id_token == original_token

    def test_network_error_returns_false(self, manager):
        import requests as req
        with patch("src.login.auth_manager.requests.post",
                   side_effect=req.ConnectionError("unreachable")):
            success, _, _ = manager.finalize_mfa_enrollment(SESSION_INFO, USER_CODE)
        assert success is False

    def test_network_error_message_is_not_none(self, manager):
        import requests as req
        with patch("src.login.auth_manager.requests.post",
                   side_effect=req.ConnectionError("unreachable")):
            _, msg, _ = manager.finalize_mfa_enrollment(SESSION_INFO, USER_CODE)
        assert msg is not None
        assert len(msg) > 0
