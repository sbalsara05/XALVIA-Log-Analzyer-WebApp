"""Live integration tests for GatewayClient + GatewaySyncToCloudWorker.

These tests hit the real gateway and GCS — run opt-in only:

  TEST_EMAIL=you@example.com TEST_PASSWORD=secret \\
    pytest tests/integration/ -m integration -v -s
"""
import pytest
from unittest.mock import MagicMock

from src.cloud_conn.gateway_client import GatewayClient
from src.workflows.workers import GatewaySyncToCloudWorker

pytestmark = pytest.mark.integration


# ---------------------------------------------------------------------------
# GatewayClient — live HTTP
# ---------------------------------------------------------------------------

class TestListFilesLive:
    def test_returns_list(self, gateway, mac_id):
        files = gateway.list_files(mac_id)
        print(f"\ncloud files for {mac_id}: {files}")
        assert isinstance(files, list)

    def test_all_entries_are_strings(self, gateway, mac_id):
        files = gateway.list_files(mac_id)
        assert all(isinstance(f, str) for f in files)


class TestUploadCommitRoundtrip:
    FNAME = "integ_test.hex"
    DATA  = b"\x5A\x01\x02\x03\xDE\xAD\xBE\xEF"

    def test_full_upload_cycle(self, gateway, mac_id):
        """start_session → upload → commit → file appears in list."""
        # 1. Start session
        session_id, urls = gateway.get_upload_signed_urls(mac_id, [self.FNAME])
        print(f"\nsession_id: {session_id}")
        assert session_id
        assert self.FNAME in urls

        # 2. Upload directly to GCS via signed URL
        gateway.upload_file(urls[self.FNAME], self.DATA)

        # 3. Commit session
        gateway.commit_session(mac_id, session_id, [self.FNAME])

        # 4. Verify file is visible
        files = gateway.list_files(mac_id)
        print(f"files after commit: {files}")
        assert self.FNAME in files


class TestReadFileLive:
    def test_read_existing_file(self, gateway, mac_id):
        """Read the first file listed for the device."""
        files = gateway.list_files(mac_id)
        if not files:
            pytest.skip("No files in cloud for this device — run upload test first")

        fname = files[0]
        data = gateway.read_file(mac_id, fname)
        print(f"\nread {fname}: {len(data)} bytes")
        assert isinstance(data, bytes)
        assert len(data) > 0


# ---------------------------------------------------------------------------
# GatewaySyncToCloudWorker — end-to-end with real gateway
# ---------------------------------------------------------------------------

class TestSyncWorkerLive:
    def test_sync_new_files(self, gateway, mac_id, tmp_path):
        """Worker uploads staged files and emits finished(True)."""
        # Create staged files on disk
        device_dir = tmp_path / mac_id
        device_dir.mkdir()
        (device_dir / "live_a.hex").write_bytes(b"\x5A" * 16)
        (device_dir / "live_b.hex").write_bytes(b"\x5A" * 32)

        signals = MagicMock()

        worker = GatewaySyncToCloudWorker(
            worker_id=99,
            signals=signals,
            gateway_client=gateway,
            local_dir=tmp_path,
            mac_id=mac_id,
            usb_log_files={"live_a.hex", "live_b.hex"},
        )
        worker.run()

        args = signals.finished.emit.call_args[0]
        ok, msg, payload = args[1], args[2], args[3]
        print(f"\nworker finished: ok={ok}, msg={msg}, payload={payload}")
        assert ok is True
