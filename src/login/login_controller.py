# src/login/login_controller.py
#
# LoginController — auth state machine extracted from old controller.py + signup_dialog.py.
# Wired by ApplicationController: bridge signals → handlers here, model signals → slots here.

from __future__ import annotations
import base64
import io
import logging
from typing import TYPE_CHECKING
import qrcode

from PySide6.QtCore import QObject, Slot

from ..constants import MSG, AUTH_TYPE

if TYPE_CHECKING:
    from ..mvc.bridge import Bridge
    from ..mvc.model import Model

logger = logging.getLogger(__name__)


class LoginController(QObject):
    """Owns all authentication state and logic.

    Constructor args:
        bridge : Bridge instance (Python→QML communication)
        model  : Model instance (auth operations)
    """

    def __init__(self, bridge: "Bridge", model: "Model", parent=None) -> None:
        super().__init__(parent)
        self._bridge = bridge
        self._model = model
        # True when an MFA challenge arrived during a login attempt (not enrollment)
        self._is_login_challenge: bool = False

    # ------------------------------------------------------------------
    # 3.2  handle_login
    # ------------------------------------------------------------------

    @Slot(str, str, bool)
    def handle_login(self, email: str, password: str, remember: bool) -> None:
        email = email.strip()
        password = password.strip()
        if not email or not password:
            self._bridge.login_error(MSG.Auth.EMAIL_PASSWORD_REQUIRED)
            return
        self._bridge.set_status(MSG.Auth.VERIFYING_CREDENTIALS, busy=True)
        self._model.sign_in_user(email, password)

    # ------------------------------------------------------------------
    # 3.4  handle_mfa_verify
    # ------------------------------------------------------------------

    @Slot(str)
    def handle_mfa_verify(self, code: str) -> None:
        code = code.strip()
        if len(code) != 6 or not code.isdigit():
            self._bridge.login_error(MSG.Auth.MFA_CODE_INVALID)
            return
        self._bridge.set_status(MSG.Auth.VERIFYING_CODE, busy=True)
        if self._is_login_challenge:
            # Case A: MFA challenge during login
            session_ticket = self._model._temp_mfa_session
            self._model.verify_mfa_login(session_ticket, code)
        else:
            # Case B: MFA enrollment finalization
            self._model.finalize_mfa_setup(code)

    # ------------------------------------------------------------------
    # 3.4b  handle_forgot_password (navigation + submission)
    # ------------------------------------------------------------------

    @Slot()
    def handle_forgot_password_nav(self) -> None:
        """Navigate the overlay to the forgot-password email entry form."""
        self._bridge._call("showForgotPasswordForm")

    @Slot(str)
    def handle_forgot_password_submit(self, email: str) -> None:
        email = email.strip()
        logger.debug("handle_forgot_password_submit: email=%r", email)
        if not email:
            self._bridge.login_error(MSG.Auth.EMAIL_REQUIRED)
            return
        self._bridge.set_status(MSG.Auth.SENDING_RESET_EMAIL, busy=True)
        self._model.send_password_reset(email)

    # ------------------------------------------------------------------
    # 3.5  handle_signout
    # ------------------------------------------------------------------

    @Slot()
    def handle_verify_email_done(self) -> None:
        """User clicked 'I've verified my email' — re-check with Firebase."""
        self._bridge.set_status(MSG.Auth.CHECKING_EMAIL_VERIFICATION, busy=True)
        self._model.recheck_email_verification()

    @Slot()
    def handle_resend_verification_email(self) -> None:
        """User clicked 'Resend' on the verify-email page."""
        self._bridge.set_status(MSG.Auth.SENDING_VERIFICATION_EMAIL, busy=True)
        self._model.resend_verification_email()

    @Slot()
    def handle_signout(self) -> None:
        self._model.auth_manager.sign_out()
        self._bridge.clear_user()
        self._bridge.show_login()

    # ------------------------------------------------------------------
    # 3.6  on_auth_finished — full auth state machine
    # ------------------------------------------------------------------

    @Slot(bool, str, dict)
    def on_auth_finished(self, success: bool, msg: str, payload: dict) -> None:
        # 1. MFA challenge arrived during a login attempt
        if msg == MSG.Auth.MFA_REQUIRED:
            self._model._temp_mfa_session = payload
            self._is_login_challenge = True
            # Show MFA page with no QR (user already has authenticator app)
            self._bridge.login_show_mfa("")
            return

        # Reset challenge flag for all other outcomes
        self._is_login_challenge = False

        # 1b. Password reset email sent successfully
        if msg == MSG.Auth.RESET_EMAIL_SENT:
            logger.debug("on_auth_finished: password reset email sent to %r", payload.get("email", ""))
            self._bridge._call("showForgotPasswordSuccess", payload.get("email", ""))
            self._bridge.set_status("", busy=False)
            return

        # 2. Successful login
        if success and payload.get("type") == AUTH_TYPE.LOGIN:
            email = self._model.auth_manager.email or ""
            name = getattr(self._model.auth_manager, "display_name", "") or email
            self._bridge.hide_login()
            self._bridge.set_user(email, name)
            self._bridge.set_status(MSG.Status.SIGNED_IN, busy=False)
            return

        # 3. Auth failure
        if not success:
            self._bridge.login_error(msg)
            self._bridge.set_status("", busy=False)

    # ------------------------------------------------------------------
    # 3.7  on_mfa_qr_ready — QR code generation for enrollment
    # ------------------------------------------------------------------

    @Slot(str, str)
    def on_mfa_qr_ready(self, secret: str, email: str) -> None:
        uri = f"otpauth://totp/XALIVA:{email}?secret={secret}&issuer=XALIVA"
        img = qrcode.make(uri)

        buf = io.BytesIO()
        img.save(buf, format="PNG")
        b64 = base64.b64encode(buf.getvalue()).decode("ascii")
        data_url = f"data:image/png;base64,{b64}"

        self._bridge.login_show_mfa(data_url)
