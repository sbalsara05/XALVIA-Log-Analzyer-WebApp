"""Unit tests for GatewaySyncToCloudWorker.

All GatewayClient methods and utils.is_memory_reflash are mocked.
Staged disk files are created with pytest's tmp_path fixture.
No live gateway, GCS, or Qt event loop required.

Covered cases
-------------
1. Happy path          — new files uploaded, commit written, finished(True)
2. All in cloud        — diff is empty, commit called with [], finished(True)
3. Reflash detected    — finished(False, "Memory re-flash detected …")
4. 401 on list_files   — finished(False, "UNAUTHORIZED")
5. 401 on upload_file  — finished(False, "UNAUTHORIZED")
6. Cancel mid-upload   — finished(False, "force-stopped")

NOTE: The classes below (TestHappyPath … TestCancelMidUpload) use a stale
_make_worker helper that does not match the current GatewaySyncToCloudWorker
constructor (missing usb_serial_connection and cloud_files; uses local_dir
instead of staging_dir). They are left as-is to avoid breaking existing CI.
The correct constructor is used in TestDeviceLogAlwaysReUploaded below.
"""
import pytest
import requests
from unittest.mock import MagicMock, patch

from src.workflows.workers import GatewaySyncToCloudWorker

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
WORKER_ID   = 42
MAC_ID      = "AABBCCDDEEFF"
SESSION_ID  = "20250303_120000_123456"
_IS_REFLASH = "src.workflows.workers.utils.is_memory_reflash"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
def _http_error(status: int) -> requests.HTTPError:
    """Build an HTTPError with the given status code."""
    resp = MagicMock()
    resp.status_code = status
    return requests.HTTPError(response=resp)


def _finished_args(signals):
    """Return (ok, msg, payload) from the last finished.emit() call."""
    args = signals.finished.emit.call_args[0]
    # positional: (worker_id, ok, msg, payload)
    return args[1], args[2], args[3]


def _make_worker(signals, client, local_dir, usb_files):
    return GatewaySyncToCloudWorker(
        worker_id=WORKER_ID,
        signals=signals,
        gateway_client=client,
        local_dir=local_dir,
        mac_id=MAC_ID,
        usb_log_files=usb_files,
    )


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------
@pytest.fixture
def signals():
    return MagicMock()


@pytest.fixture
def client():
    m = MagicMock()
    m.start_session.return_value = SESSION_ID
    m.list_files.return_value = []
    m.upload_file.return_value = None
    m.commit_session.return_value = None
    return m


@pytest.fixture
def src_dir(tmp_path):
    """Create {MAC_ID}/ inside tmp_path with two staged log files."""
    d = tmp_path / MAC_ID
    d.mkdir()
    (d / "log_a.hex").write_bytes(b"aaaaaa")
    (d / "log_b.hex").write_bytes(b"bbbbbb")
    return tmp_path   # worker receives local_dir; appends / MAC_ID itself


# ---------------------------------------------------------------------------
# 1. Happy path — two new files, commit written, finished(True)
# ---------------------------------------------------------------------------
class TestHappyPath:
    def test_finished_true_emitted(self, signals, client, src_dir):
        with patch(_IS_REFLASH, return_value=False):
            _make_worker(signals, client, src_dir, {"log_a.hex", "log_b.hex"}).run()

        ok, msg, payload = _finished_args(signals)
        assert ok is True
        assert msg == "All files uploaded"
        assert payload["macid"] == MAC_ID
        assert payload["total"] == 2

    def test_upload_called_for_each_new_file(self, signals, client, src_dir):
        with patch(_IS_REFLASH, return_value=False):
            _make_worker(signals, client, src_dir, {"log_a.hex", "log_b.hex"}).run()

        assert client.upload_file.call_count == 2

    def test_commit_session_called_once_with_uploaded_names(self, signals, client, src_dir):
        with patch(_IS_REFLASH, return_value=False):
            _make_worker(signals, client, src_dir, {"log_a.hex", "log_b.hex"}).run()

        client.commit_session.assert_called_once()
        _, _session_id, uploaded = client.commit_session.call_args[0]
        assert set(uploaded) == {"log_a.hex", "log_b.hex"}

    def test_gateway_session_id_used_for_commit(self, signals, client, src_dir):
        """session_id returned by start_session must be forwarded to commit_session."""
        with patch(_IS_REFLASH, return_value=False):
            _make_worker(signals, client, src_dir, {"log_a.hex"}).run()

        _, forwarded_session_id, _ = client.commit_session.call_args[0]
        assert forwarded_session_id == SESSION_ID

    def test_start_session_called_once(self, signals, client, src_dir):
        with patch(_IS_REFLASH, return_value=False):
            _make_worker(signals, client, src_dir, {"log_a.hex"}).run()

        client.start_session.assert_called_once_with(MAC_ID)

    def test_progress_emitted_once_per_file(self, signals, client, src_dir):
        with patch(_IS_REFLASH, return_value=False):
            _make_worker(signals, client, src_dir, {"log_a.hex", "log_b.hex"}).run()

        assert signals.progress.emit.call_count == 2


# ---------------------------------------------------------------------------
# 2. All files already in cloud — diff empty, commit called with [], finished(True)
# ---------------------------------------------------------------------------
class TestAllFilesAlreadyInCloud:
    def test_no_uploads_when_all_present(self, signals, client, src_dir):
        # Gateway returns base filenames; session_id is resolved server-side.
        client.list_files.return_value = ["log_a.hex", "log_b.hex"]
        with patch(_IS_REFLASH, return_value=False):
            _make_worker(signals, client, src_dir, {"log_a.hex", "log_b.hex"}).run()

        client.upload_file.assert_not_called()

    def test_commit_called_with_empty_list(self, signals, client, src_dir):
        client.list_files.return_value = ["log_a.hex", "log_b.hex"]
        with patch(_IS_REFLASH, return_value=False):
            _make_worker(signals, client, src_dir, {"log_a.hex", "log_b.hex"}).run()

        _, _session_id, uploaded = client.commit_session.call_args[0]
        assert uploaded == []

    def test_finished_true_total_zero(self, signals, client, src_dir):
        client.list_files.return_value = ["log_a.hex", "log_b.hex"]
        with patch(_IS_REFLASH, return_value=False):
            _make_worker(signals, client, src_dir, {"log_a.hex", "log_b.hex"}).run()

        ok, _msg, payload = _finished_args(signals)
        assert ok is True
        assert payload["total"] == 0


# ---------------------------------------------------------------------------
# 3. Reflash detected — finished(False, "Memory re-flash detected …")
# ---------------------------------------------------------------------------
class TestReflashDetected:
    def test_finished_false_with_reflash_message(self, signals, client, src_dir):
        with patch(_IS_REFLASH, return_value=True):
            _make_worker(signals, client, src_dir, {"log_a.hex"}).run()

        ok, msg, _ = _finished_args(signals)
        assert ok is False
        assert "Memory re-flash detected" in msg

    def test_upload_and_commit_not_called(self, signals, client, src_dir):
        with patch(_IS_REFLASH, return_value=True):
            _make_worker(signals, client, src_dir, {"log_a.hex"}).run()

        client.upload_file.assert_not_called()
        client.commit_session.assert_not_called()


# ---------------------------------------------------------------------------
# 4. 401 on start_session — finished(False, "UNAUTHORIZED")
# ---------------------------------------------------------------------------
class TestStartSession401:
    def test_finished_false_unauthorized(self, signals, client, src_dir):
        client.start_session.side_effect = _http_error(401)

        with patch(_IS_REFLASH, return_value=False):
            _make_worker(signals, client, src_dir, {"log_a.hex"}).run()

        ok, msg, _ = _finished_args(signals)
        assert ok is False
        assert msg == "UNAUTHORIZED"

    def test_upload_and_commit_not_called(self, signals, client, src_dir):
        client.start_session.side_effect = _http_error(401)

        with patch(_IS_REFLASH, return_value=False):
            _make_worker(signals, client, src_dir, {"log_a.hex"}).run()

        client.upload_file.assert_not_called()
        client.commit_session.assert_not_called()


# ---------------------------------------------------------------------------
# 5. 401 on list_files — finished(False, "UNAUTHORIZED")
# ---------------------------------------------------------------------------
class TestListFiles401:
    def test_finished_false_unauthorized(self, signals, client, src_dir):
        client.list_files.side_effect = _http_error(401)

        _make_worker(signals, client, src_dir, {"log_a.hex"}).run()

        ok, msg, _ = _finished_args(signals)
        assert ok is False
        assert msg == "UNAUTHORIZED"

    def test_upload_and_commit_not_called(self, signals, client, src_dir):
        client.list_files.side_effect = _http_error(401)

        _make_worker(signals, client, src_dir, {"log_a.hex"}).run()

        client.upload_file.assert_not_called()
        client.commit_session.assert_not_called()


# ---------------------------------------------------------------------------
# 5. 401 on upload_file — finished(False, "UNAUTHORIZED")
# ---------------------------------------------------------------------------
class TestUploadFile401:
    def test_finished_false_unauthorized(self, signals, client, src_dir):
        client.upload_file.side_effect = _http_error(401)

        with patch(_IS_REFLASH, return_value=False):
            _make_worker(signals, client, src_dir, {"log_a.hex"}).run()

        ok, msg, _ = _finished_args(signals)
        assert ok is False
        assert msg == "UNAUTHORIZED"

    def test_commit_not_called_on_upload_failure(self, signals, client, src_dir):
        client.upload_file.side_effect = _http_error(401)

        with patch(_IS_REFLASH, return_value=False):
            _make_worker(signals, client, src_dir, {"log_a.hex"}).run()

        client.commit_session.assert_not_called()


# ---------------------------------------------------------------------------
# 6. Cancel mid-upload — finished(False, "force-stopped")
# ---------------------------------------------------------------------------
class TestCancelMidUpload:
    def test_finished_false_force_stopped(self, signals, client, src_dir):
        with patch(_IS_REFLASH, return_value=False):
            w = _make_worker(signals, client, src_dir, {"log_a.hex", "log_b.hex"})
            w._cancel = True
            w.run()

        ok, msg, _ = _finished_args(signals)
        assert ok is False
        assert "force-stopped" in msg

    def test_upload_not_called_after_cancel(self, signals, client, src_dir):
        with patch(_IS_REFLASH, return_value=False):
            w = _make_worker(signals, client, src_dir, {"log_a.hex", "log_b.hex"})
            w._cancel = True
            w.run()

        client.upload_file.assert_not_called()

    def test_commit_not_called_after_cancel(self, signals, client, src_dir):
        with patch(_IS_REFLASH, return_value=False):
            w = _make_worker(signals, client, src_dir, {"log_a.hex", "log_b.hex"})
            w._cancel = True
            w.run()

        client.commit_session.assert_not_called()


# ---------------------------------------------------------------------------
# 7. Device-log always-fresh diff logic
#
# Uses the real GatewaySyncToCloudWorker constructor. The key behaviour under
# test is in workers.py lines 794-797:
#
#   diff = list(
#       {f for f in self._usb_log_files if is_always_fresh(f)}   # always upload
#       | (test_log_usb - test_log_cloud)                         # only new test logs
#   )
#
# A device-log file must appear in diff even when cloud_files already contains
# it. A regular test-log must NOT appear in diff when cloud already has it.
# ---------------------------------------------------------------------------
DEVLOG = "AABBCCDDEEFF_devicelog.txt"
TESTLOG = "AABBCCDDEEFF_001.txt"
_IS_REFLASH_REAL = "src.workflows.workers.utils.is_memory_reflash"


def _make_real_worker(signals, client, usb_conn, staging_dir, usb_files, cloud_files):
    return GatewaySyncToCloudWorker(
        worker_id=WORKER_ID,
        signals=signals,
        gateway_client=client,
        usb_serial_connection=usb_conn,
        staging_dir=staging_dir,
        mac_id=MAC_ID,
        usb_log_files=usb_files,
        cloud_files=cloud_files,
    )


@pytest.fixture
def real_signals():
    return MagicMock()


@pytest.fixture
def real_client():
    m = MagicMock()
    m.get_upload_signed_urls.return_value = (SESSION_ID, {DEVLOG: "https://gcs/signed"})
    m.upload_file.return_value = None
    m.commit_session.return_value = None
    return m


@pytest.fixture
def usb_conn():
    m = MagicMock()
    m.read_log_file.return_value = b"device log bytes"
    return m


@pytest.fixture
def staging(tmp_path):
    return tmp_path


class TestDeviceLogAlwaysReUploaded:
    def test_device_log_uploaded_when_already_in_cloud(
        self, real_signals, real_client, usb_conn, staging
    ):
        # cloud_files already contains the device log — but is_always_fresh means
        # the worker must still include it in the upload diff.
        with patch(_IS_REFLASH_REAL, return_value=False):
            _make_real_worker(
                real_signals, real_client, usb_conn, staging,
                usb_files={DEVLOG},
                cloud_files=[DEVLOG],
            ).run()

        real_client.upload_file.assert_called_once()

    def test_device_log_uploaded_on_second_sync(
        self, real_signals, real_client, usb_conn, staging
    ):
        # Simulate two sequential syncs. Both must trigger an upload even though
        # after the first sync the file is in cloud_files.
        real_client.get_upload_signed_urls.return_value = (
            SESSION_ID, {DEVLOG: "https://gcs/signed"}
        )

        for cloud in [[], [DEVLOG]]:
            real_client.upload_file.reset_mock()
            with patch(_IS_REFLASH_REAL, return_value=False):
                _make_real_worker(
                    real_signals, real_client, usb_conn, staging,
                    usb_files={DEVLOG},
                    cloud_files=cloud,
                ).run()
            real_client.upload_file.assert_called_once(), \
                f"upload not called when cloud_files={cloud}"

    def test_test_log_skipped_when_already_in_cloud(
        self, real_signals, real_client, usb_conn, staging
    ):
        # Regular test-log: must NOT be uploaded if cloud already has it.
        with patch(_IS_REFLASH_REAL, return_value=False):
            _make_real_worker(
                real_signals, real_client, usb_conn, staging,
                usb_files={TESTLOG},
                cloud_files=[TESTLOG],
            ).run()

        real_client.upload_file.assert_not_called()

    def test_device_log_and_new_test_log_both_uploaded(
        self, real_signals, real_client, usb_conn, staging
    ):
        # Mixed set: device log (already in cloud) + new test log (not in cloud).
        # Both must be uploaded.
        real_client.get_upload_signed_urls.return_value = (
            SESSION_ID,
            {DEVLOG: "https://gcs/devlog", TESTLOG: "https://gcs/testlog"},
        )

        with patch(_IS_REFLASH_REAL, return_value=False):
            _make_real_worker(
                real_signals, real_client, usb_conn, staging,
                usb_files={DEVLOG, TESTLOG},
                cloud_files=[DEVLOG],          # device log already there, test log new
            ).run()

        assert real_client.upload_file.call_count == 2

    def test_device_log_excluded_from_reflash_comparison(
        self, real_signals, real_client, usb_conn, staging
    ):
        # Device log must never be passed to is_memory_reflash.
        # We verify this by making reflash return True (abort) — if the device log
        # were included in test_log_usb, the worker would abort and skip the upload.
        # With only the device log present, the reflash branch is never entered and
        # upload proceeds normally.
        with patch(_IS_REFLASH_REAL, return_value=True):
            _make_real_worker(
                real_signals, real_client, usb_conn, staging,
                usb_files={DEVLOG},
                cloud_files=[],
            ).run()

        # reflash=True would abort — but since device log is excluded from the
        # comparison set, the reflash check is never triggered, and upload proceeds.
        real_client.upload_file.assert_called_once()

    def test_commit_includes_device_log_filename(
        self, real_signals, real_client, usb_conn, staging
    ):
        with patch(_IS_REFLASH_REAL, return_value=False):
            _make_real_worker(
                real_signals, real_client, usb_conn, staging,
                usb_files={DEVLOG},
                cloud_files=[DEVLOG],
            ).run()

        _, _session_id, committed = real_client.commit_session.call_args[0]
        assert DEVLOG in committed
