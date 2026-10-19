"""
GatewayClient — authenticated HTTP client for the Go Gateway.

All requests inject the Firebase idToken as a Bearer header so the gateway
can validate it and enforce ACL rules.

Typed exceptions:
    GatewayUnauthorizedError (401) — token expired or missing; caller should
        trigger re-login and retry.
    GatewayForbiddenError    (403) — authenticated but insufficient permissions;
        caller should surface a clear message to the user, do NOT retry.
"""
from __future__ import annotations

import requests

from .. import constants


# ── Typed gateway errors ─────────────────────────────────────────────────────

class GatewayError(Exception):
    """Base class for gateway HTTP errors."""
    def __init__(self, status_code: int, message: str) -> None:
        super().__init__(message)
        self.status_code = status_code


class GatewayUnauthorizedError(GatewayError):
    """Raised on HTTP 401 — token invalid or expired. Trigger re-login."""


class GatewayForbiddenError(GatewayError):
    """Raised on HTTP 403 — authenticated but no permission for this resource."""


class GatewayMfaRequiredError(GatewayForbiddenError):
    """Raised on HTTP 403 with code 'mfa_required'.

    The user is authenticated but has not completed TOTP enrollment.
    The caller must route to the MFA enrollment flow — do NOT surface
    a generic 'forbidden' message to the user.
    """


def _raise_for_status(resp: requests.Response) -> None:
    """Raise a typed exception on 4xx/5xx, with 401 and 403 distinguished."""
    if resp.status_code == 401:
        raise GatewayUnauthorizedError(401, "Unauthorized: token invalid or expired")
    if resp.status_code == 403:
        try:
            body = resp.json()
        except Exception:
            body = {}
        if body.get("code") == "mfa_required":
            raise GatewayMfaRequiredError(403, body.get("error", "MFA required"))
        raise GatewayForbiddenError(403, body.get("error", resp.text or "Forbidden"))
    resp.raise_for_status()  # re-raise any other 4xx/5xx as requests.HTTPError


# ── Client ───────────────────────────────────────────────────────────────────

class GatewayClient:
    def __init__(self, auth_manager, base_url: str = constants.GO_GATEWAY_URL) -> None:
        self._auth_manager = auth_manager
        self._base_url = base_url.rstrip("/")

    def _headers(self) -> dict:
        token = self._auth_manager.get_token()
        return {"Authorization": f"Bearer {token}"}

    # ── File listing ─────────────────────────────────────────────────────────

    def list_files(self, mac_id: str) -> list[str]:
        """GET /api/files?mac_id=X → list of filenames for the device."""
        resp = requests.get(
            f"{self._base_url}/api/files",
            params={"mac_id": mac_id},
            headers=self._headers(),
            timeout=30,
        )
        _raise_for_status(resp)
        return resp.json()

    # ── Signed-URL read ───────────────────────────────────────────────────────

    def get_read_signed_url(self, mac_id: str, filename: str) -> str:
        """GET /api/signed-url/read?mac_id=X&filename=Y → GCS signed read URL."""
        resp = requests.get(
            f"{self._base_url}/api/signed-url/read",
            params={"mac_id": mac_id, "filename": filename},
            headers=self._headers(),
            timeout=15,
        )
        _raise_for_status(resp)
        return resp.json()["url"]

    def read_file(self, mac_id: str, filename: str) -> bytes:
        """Resolve a gateway signed read URL then download the file from GCS directly."""
        url = self.get_read_signed_url(mac_id, filename)
        resp = requests.get(url, timeout=60)
        resp.raise_for_status()
        return resp.content

    # ── Signed-URL upload ─────────────────────────────────────────────────────

    def get_upload_signed_urls(
        self, mac_id: str, filenames: list[str]
    ) -> tuple[str, dict[str, str], dict[str, str]]:
        """POST /api/signed-url/upload → (session_id, {filename: signed_put_url}, required_headers).

        Step 1 of the 3-step upload flow.
        required_headers must be forwarded verbatim in every upload_file call.
        """
        resp = requests.post(
            f"{self._base_url}/api/signed-url/upload",
            json={"mac_id": mac_id, "files": filenames},
            headers=self._headers(),
            timeout=15,
        )
        _raise_for_status(resp)
        body = resp.json()
        return body["session_id"], body["urls"], body.get("required_headers", {})

    def upload_file(self, signed_url: str, data: bytes, required_headers: dict[str, str] | None = None) -> None:
        """PUT file content directly to GCS using a pre-signed URL.

        Step 2 of the 3-step upload flow. required_headers must be the dict
        returned by get_upload_signed_urls — they are part of the signed URL
        and GCS will reject the request if they are absent.
        """
        headers = {"Content-Type": "application/octet-stream", **(required_headers or {})}
        resp = requests.put(
            signed_url,
            data=data,
            headers=headers,
            timeout=60,
        )
        resp.raise_for_status()

    # ── Commit ────────────────────────────────────────────────────────────────

    def commit_session(self, mac_id: str, session_id: str, files: list[str]) -> str:
        """POST /api/commit — tells the gateway to write _COMMIT.json to GCS.

        Step 3 of the 3-step upload flow. Returns the committed GCS path on success.
        """
        resp = requests.post(
            f"{self._base_url}/api/commit",
            params={"mac_id": mac_id, "session_id": session_id},
            headers=self._headers(),
            json=files,
            timeout=30,
        )
        _raise_for_status(resp)
        return resp.json()["committed"]

    # ── Parse endpoints ───────────────────────────────────────────────────────

    def parse_device_log(self, mac_id: str, data: bytes) -> list[dict]:
        """POST /api/parse/device-log?mac_id=X → list of {timestamp, description} dicts."""
        headers = {**self._headers(), "Content-Type": "application/octet-stream"}
        resp = requests.post(
            f"{self._base_url}/api/parse/device-log",
            params={"mac_id": mac_id},
            data=data,
            headers=headers,
            timeout=30,
        )
        _raise_for_status(resp)
        return resp.json()

    def parse_test_log(self, mac_id: str, data: bytes) -> tuple[bool, dict]:
        """POST /api/parse/test-log?mac_id=X → (success, plot_spec dict)."""
        headers = {**self._headers(), "Content-Type": "application/octet-stream"}
        resp = requests.post(
            f"{self._base_url}/api/parse/test-log",
            params={"mac_id": mac_id},
            data=data,
            headers=headers,
            timeout=30,
        )
        _raise_for_status(resp)
        body = resp.json()
        return body.get("success", False), body
