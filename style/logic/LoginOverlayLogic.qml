import QtQuick 2.15
import ".." as App

// ─────────────────────────────────────────────────────────────────────────────
// LoginOverlayLogic.qml - Form state management for LoginOverlay
// Receives a reference to the LoginOverlay root Item.
// Accesses form state via properties / aliases declared on that root:
//   overlay.errorBannerVisible  overlay.errorMessage  overlay.qrImageSource
//   overlay.emailLoginText      overlay.passLoginText
//   overlay.mfaCodeText         overlay.emailForgotText  overlay.resetEmailText
// ─────────────────────────────────────────────────────────────────────────────

QtObject {
    id: logic

    // ── Reference injected by LoginOverlay.qml on instantiation
    property var overlay: null  // LoginOverlay root Item

    // ══════════════ Overlay visibility / mode

    function show() {
        overlay.visible = true
        overlay.formMode = 0
        overlay.errorBannerVisible = false
        overlay.qrImageSource = ""
        overlay.mandatory = true
        clearForm()
    }

    function hide() {
        overlay.visible = false
        overlay.mandatory = false
        overlay.closed()
    }

    // ══════════════ Error banner

    function showError(msg) {
        overlay.errorMessage = msg
        overlay.errorBannerVisible = true
    }

    // ══════════════ MFA page

    function showMFA(qrUrl) {
        overlay.mfaPreviousMode = overlay.formMode
        overlay.qrImageSource = qrUrl
        overlay.formMode = 2
    }

    function goBackFromMFA() {
        overlay.mfaCodeText = ""
        overlay.qrImageSource = ""
        overlay.errorBannerVisible = false
        overlay.formMode = overlay.mfaPreviousMode
    }

    // ══════════════ Form reset

    function clearForm() {
        overlay.emailLoginText   = ""
        overlay.passLoginText    = ""
        overlay.mfaCodeText      = ""
        overlay.emailForgotText  = ""
        overlay.errorBannerVisible = false
    }

    // ══════════════ Form mode switching

    function switchToLoginMode() {
        overlay.errorBannerVisible = false
        overlay.formMode = 0
    }

    function switchToForgotPasswordMode() {
        overlay.errorBannerVisible = false
        overlay.formMode = 3
        // Pre-fill email from login form if available
        overlay.emailForgotText = overlay.emailLoginText
    }

    function showResetSuccess(email) {
        overlay.errorBannerVisible = false
        overlay.formMode = 4
        overlay.resetEmailText = email
    }

    function showVerifyEmail() {
        overlay.formMode = 5
    }

    // ══════════════ Error banner dismiss

    function dismissError() {
        overlay.errorBannerVisible = false
    }

    // ══════════════ Form submission

    function doLogin() {
        overlay.loginClicked(overlay.emailLoginText, overlay.passLoginText)
    }

    function doMFA() {
        overlay.mfaVerifyClicked(overlay.mfaCodeText)
    }

    function doForgotPassword() {
        overlay.forgotPasswordSubmitClicked(overlay.emailForgotText)
    }
}
