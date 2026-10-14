"""Fixtures for live integration tests.

Required env vars:
  TEST_EMAIL     — Firebase account email
  TEST_PASSWORD  — Firebase account password
  TEST_MAC_ID    — MAC ID of a device you control (default: see below)
  GATEWAY_URL    — override gateway base URL (default: constants.GO_GATEWAY_URL)

Run:
  TEST_EMAIL=you@example.com TEST_PASSWORD=secret \\
    pytest tests/integration/ -m integration -v -s
"""
import os
import sys
import pathlib

import pytest

# Ensure project root is on sys.path when running this sub-package directly
sys.path.append((pathlib.Path(__file__).resolve().parents[2]).as_posix())

from src.login.auth_manager import AuthManager
from src.cloud_conn.gateway_client import GatewayClient
import src.constants as constants


def pytest_configure(config):
    config.addinivalue_line(
        "markers",
        "integration: live gateway + GCS tests (opt-in, requires env vars)",
    )


@pytest.fixture(scope="session")
def auth():
    """Authenticate once per pytest session using env-var credentials."""
    email    = os.environ.get("TEST_EMAIL")
    password = os.environ.get("TEST_PASSWORD")
    if not email or not password:
        pytest.skip("TEST_EMAIL / TEST_PASSWORD env vars not set")

    am = AuthManager()
    ok, msg, _ = am.login(email, password)
    if not ok:
        pytest.skip(f"Firebase login failed: {msg}")
    return am


@pytest.fixture(scope="session")
def gateway(auth):
    """Real GatewayClient pointed at the running gateway."""
    base_url = os.environ.get("GATEWAY_URL", constants.GO_GATEWAY_URL)
    return GatewayClient(auth_manager=auth, base_url=base_url)


@pytest.fixture(scope="session")
def mac_id():
    return os.environ.get("TEST_MAC_ID", "AABBCCDDEEFF")
