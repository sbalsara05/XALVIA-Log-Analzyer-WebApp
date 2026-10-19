"""Unit tests for Task C-2 — MFA routing layer.

Tests the three things added in C-2 that are verifiable without a Qt event loop:

  1. GATEWAY_STATUS.MFA_REQUIRED constant exists and is distinct from FORBIDDEN.
  2. _gateway_error_msg() maps typed GatewayError subclasses to the correct sentinels.
  3. The sentinel strings produced by _gateway_error_msg() match what Model handlers
     compare against, so routing never silently misses.

No real network traffic or Qt objects are created.
"""
import pytest
from unittest.mock import MagicMock

from src.constants import GATEWAY_STATUS
from src.cloud_conn.gateway_client import (
    GatewayMfaRequiredError,
    GatewayForbiddenError,
    GatewayUnauthorizedError,
    GatewayError,
)
from src.workflows.workers import _gateway_error_msg, GatewayWorker


# ---------------------------------------------------------------------------
# 1. GATEWAY_STATUS constant
# ---------------------------------------------------------------------------

class TestGatewayStatusConstants:
    def test_mfa_required_constant_exists(self):
        assert hasattr(GATEWAY_STATUS, "MFA_REQUIRED")

    def test_mfa_required_is_string(self):
        assert isinstance(GATEWAY_STATUS.MFA_REQUIRED, str)

    def test_mfa_required_distinct_from_forbidden(self):
        assert GATEWAY_STATUS.MFA_REQUIRED != GATEWAY_STATUS.FORBIDDEN

    def test_mfa_required_distinct_from_unauthorized(self):
        assert GATEWAY_STATUS.MFA_REQUIRED != GATEWAY_STATUS.UNAUTHORIZED

    def test_all_sentinels_are_unique(self):
        sentinels = [
            GATEWAY_STATUS.UNAUTHORIZED,
            GATEWAY_STATUS.FORBIDDEN,
            GATEWAY_STATUS.NOT_FOUND,
            GATEWAY_STATUS.FILE_NOT_FOUND,
            GATEWAY_STATUS.MFA_REQUIRED,
        ]
        assert len(sentinels) == len(set(sentinels)), "Duplicate sentinel detected"


# ---------------------------------------------------------------------------
# 2. _gateway_error_msg helper
# ---------------------------------------------------------------------------

class TestGatewayErrorMsg:
    def test_mfa_required_maps_to_mfa_sentinel(self):
        exc = GatewayMfaRequiredError(403, "MFA required")
        assert _gateway_error_msg(exc) == GATEWAY_STATUS.MFA_REQUIRED

    def test_forbidden_maps_to_forbidden_sentinel(self):
        exc = GatewayForbiddenError(403, "no permission")
        assert _gateway_error_msg(exc) == GATEWAY_STATUS.FORBIDDEN

    def test_unauthorized_maps_to_unauthorized_sentinel(self):
        exc = GatewayUnauthorizedError(401, "token expired")
        assert _gateway_error_msg(exc) == GATEWAY_STATUS.UNAUTHORIZED

    def test_mfa_required_does_not_map_to_forbidden(self):
        exc = GatewayMfaRequiredError(403, "MFA required")
        assert _gateway_error_msg(exc) != GATEWAY_STATUS.FORBIDDEN

    def test_mfa_required_does_not_map_to_unauthorized(self):
        exc = GatewayMfaRequiredError(403, "MFA required")
        assert _gateway_error_msg(exc) != GATEWAY_STATUS.UNAUTHORIZED

    def test_forbidden_does_not_map_to_mfa_sentinel(self):
        # A plain forbidden (e.g. device not in org) must NOT trigger enrollment.
        exc = GatewayForbiddenError(403, "device not registered")
        assert _gateway_error_msg(exc) != GATEWAY_STATUS.MFA_REQUIRED


# ---------------------------------------------------------------------------
# 3. Sentinel round-trip — what workers emit must match what model handlers read
# ---------------------------------------------------------------------------

class TestSentinelRoundTrip:
    """Verify that the strings _gateway_error_msg produces are byte-for-byte
    identical to the GATEWAY_STATUS constants the Model handlers compare against.
    This catches any typo between the two sides without running the Qt stack.
    """

    def test_mfa_sentinel_matches_gateway_status(self):
        exc = GatewayMfaRequiredError(403, "MFA required")
        assert _gateway_error_msg(exc) == GATEWAY_STATUS.MFA_REQUIRED

    def test_unauthorized_sentinel_matches_gateway_status(self):
        exc = GatewayUnauthorizedError(401, "expired")
        assert _gateway_error_msg(exc) == GATEWAY_STATUS.UNAUTHORIZED

    def test_forbidden_sentinel_matches_gateway_status(self):
        exc = GatewayForbiddenError(403, "denied")
        assert _gateway_error_msg(exc) == GATEWAY_STATUS.FORBIDDEN

    def test_mfa_subclass_caught_as_forbidden_in_broad_catch(self):
        """GatewayMfaRequiredError IS a GatewayForbiddenError subclass.
        Workers catch (GatewayMfaRequiredError, GatewayForbiddenError, ...) as a
        tuple. The MFA case must be listed FIRST so it is matched before the
        broader forbidden case. This test documents the dependency.
        """
        exc = GatewayMfaRequiredError(403, "MFA required")
        assert isinstance(exc, GatewayForbiddenError)
        # Simulates: except (GatewayMfaRequiredError, GatewayForbiddenError) — first match wins.
        matched = None
        try:
            raise exc
        except GatewayMfaRequiredError:
            matched = "mfa"
        except GatewayForbiddenError:
            matched = "forbidden"
        assert matched == "mfa", (
            "GatewayMfaRequiredError must be listed before GatewayForbiddenError in except tuples"
        )


# ---------------------------------------------------------------------------
# 4. GatewayWorker.run() — end-to-end routing through the worker
#
# These tests close the gap between the signal-mapping tests above and the
# actual worker: they verify that when the gateway client raises a typed
# exception, GatewayWorker.run() catches it and emits the correct sentinel
# via finished(), rather than falling into the generic Exception handler.
# ---------------------------------------------------------------------------

_WORKER_ID = 7
_MAC_ID    = "AABBCCDDEEFF"


def _finished_args(signals):
    """Return (ok, msg, payload) from the last finished.emit() call."""
    args = signals.finished.emit.call_args[0]
    return args[1], args[2], args[3]


class TestGatewayWorkerRouting:

    @pytest.fixture
    def signals(self):
        return MagicMock()

    @pytest.fixture
    def client(self):
        return MagicMock()

    @pytest.fixture
    def worker(self, signals, client):
        return GatewayWorker(
            worker_id=_WORKER_ID,
            signals=signals,
            client=client,
            mac_id=_MAC_ID,
        )

    # N2 — plain forbidden (e.g. device not in org) → FORBIDDEN, not MFA
    def test_forbidden_emits_forbidden_sentinel(self, worker, client, signals):
        client.list_files.side_effect = GatewayForbiddenError(403, "device not registered")
        worker.run()
        ok, msg, _ = _finished_args(signals)
        assert ok is False
        assert msg == GATEWAY_STATUS.FORBIDDEN

    def test_forbidden_does_not_emit_mfa_sentinel(self, worker, client, signals):
        client.list_files.side_effect = GatewayForbiddenError(403, "device not registered")
        worker.run()
        _, msg, _ = _finished_args(signals)
        assert msg != GATEWAY_STATUS.MFA_REQUIRED

    # N3 — mfa_required → MFA_REQUIRED, not plain FORBIDDEN
    def test_mfa_required_emits_mfa_sentinel(self, worker, client, signals):
        client.list_files.side_effect = GatewayMfaRequiredError(403, "MFA required")
        worker.run()
        ok, msg, _ = _finished_args(signals)
        assert ok is False
        assert msg == GATEWAY_STATUS.MFA_REQUIRED

    def test_mfa_required_does_not_emit_forbidden_sentinel(self, worker, client, signals):
        client.list_files.side_effect = GatewayMfaRequiredError(403, "MFA required")
        worker.run()
        _, msg, _ = _finished_args(signals)
        assert msg != GATEWAY_STATUS.FORBIDDEN

    # finished() must be called exactly once regardless of which exception fires
    def test_finished_called_once_on_forbidden(self, worker, client, signals):
        client.list_files.side_effect = GatewayForbiddenError(403, "no access")
        worker.run()
        assert signals.finished.emit.call_count == 1

    def test_finished_called_once_on_mfa_required(self, worker, client, signals):
        client.list_files.side_effect = GatewayMfaRequiredError(403, "MFA required")
        worker.run()
        assert signals.finished.emit.call_count == 1
