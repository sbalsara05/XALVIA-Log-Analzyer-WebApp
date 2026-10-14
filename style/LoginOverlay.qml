import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15
import "components" as Components
import "." as App
import "logic"

// ─────────────────────────────────────────────────────────────────────────────
// LoginOverlay.qml - Login/Register/MFA overlay (UI only)
// All logic lives in logic/LoginOverlayLogic.qml.
// Card: 460px wide, 16px radius, 40px padding
// Inputs: 46px, Button: 48px, MFA: 56px
// ─────────────────────────────────────────────────────────────────────────────

Item {
    id: root

    anchors.fill: parent
    visible: false

    property int formMode: 0  // 0=login, 2=mfa, 3=forgotPassword, 4=resetSent, 5=verifyEmail
    property int mfaPreviousMode: 0  // formMode before entering MFA
    // When true the ✕ close button is hidden; only successful auth can dismiss the overlay.
    property bool mandatory: false

    signal loginClicked(string email, string password)
    signal mfaVerifyClicked(string code)
    signal googleSSOClicked()
    signal microsoftSSOClicked()
    signal forgotPasswordClicked()
    signal forgotPasswordSubmitClicked(string email)
    signal verifyEmailDoneClicked()
    signal verifyEmailResendClicked()
    signal closed()

    // ── State properties consumed by the UI and set by LoginOverlayLogic
    property bool errorBannerVisible: false
    property string errorMessage: ""
    property url qrImageSource: ""

    // ── Aliases so logic can read/write input field text without referencing
    //    internal element IDs directly
    property alias emailLoginText:   emailLogin.text
    property alias passLoginText:    passLogin.text
    property alias mfaCodeText:      mfaCode.text
    property alias emailForgotText:  emailForgot.text
    property alias resetEmailText:   resetEmailLabel.text

    // ── Logic object
    LoginOverlayLogic {
        id: overlayLogic
        overlay: root
    }

    // ── Public API (called from MainWindow / MainWindowLogic)
    function show()           { overlayLogic.show() }
    function hide()           { overlayLogic.hide() }
    function showError(msg)   { overlayLogic.showError(msg) }
    function showMFA(qrUrl)   { overlayLogic.showMFA(qrUrl) }
    function clearForm()      { overlayLogic.clearForm() }
    function showForgotPasswordForm()         { overlayLogic.switchToForgotPasswordMode() }
    function showForgotPasswordSuccess(email) { overlayLogic.showResetSuccess(email) }
    function showVerifyEmail()                { overlayLogic.showVerifyEmail() }

    // Backdrop (rgba(0,0,0,0.45))
    Rectangle {
        anchors.fill: parent
        color: "#000000"
        opacity: 0.45

        MouseArea { anchors.fill: parent }
    }

    // Card (460px, 16px radius, 40px padding)
    Rectangle {
        anchors.centerIn: parent
        width: Math.min(parent.width - 48, 460)
        height: contentCol.implicitHeight + 80  // 40px top + 40px bottom
        radius: 16
        clip: true
        color: App.Theme.card
        border.color: App.Theme.border
        border.width: 1

        // Close button (16px margins)
        Text {
            anchors { top: parent.top; right: parent.right; margins: 16 }
            text: "✕"
            font.pixelSize: 16
            color: App.Theme.textMuted
            visible: !root.mandatory

            MouseArea {
                anchors.fill: parent
                cursorShape: Qt.PointingHandCursor
                onClicked: root.hide()
            }
        }

        ColumnLayout {
            id: contentCol
            anchors { top: parent.top; left: parent.left; right: parent.right; margins: 40 }
            spacing: 0

            // Title (24px bold)
            Text {
                text: formMode === 5 ? App.Strings.verifyEmailTitle
                    : formMode === 4 ? App.Strings.resetSuccessTitle
                    : formMode === 3 ? App.Strings.forgotPasswordTitle
                    : formMode === 2 ? App.Strings.mfaTitle
                    : App.Strings.loginTitle
                font.pixelSize: 24
                font.weight: Font.Bold
                color: App.Theme.textPrimary
                Layout.bottomMargin: 4
            }

            // Subtitle (13px secondary)
            Text {
                text: formMode === 5 ? App.Strings.verifyEmailSubtitle
                    : formMode === 4 ? ""
                    : formMode === 3 ? App.Strings.forgotPasswordSubtitle
                    : formMode === 2 ? App.Strings.mfaSubtitle
                    : App.Strings.loginSubtitle
                font.pixelSize: 13
                color: App.Theme.textSecondary
                Layout.bottomMargin: 24
                wrapMode: Text.WordWrap
                Layout.fillWidth: true
            }

            // Error banner
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: errorRow.implicitHeight + 16
                radius: 8
                color: App.Theme.errorBg
                border.color: App.Theme.errorBorder
                border.width: 1
                visible: root.errorBannerVisible
                Layout.bottomMargin: root.errorBannerVisible ? 12 : 0

                RowLayout {
                    id: errorRow
                    anchors { left: parent.left; right: parent.right; verticalCenter: parent.verticalCenter; leftMargin: 12; rightMargin: 12 }

                    Text {
                        text: root.errorMessage
                        Layout.fillWidth: true
                        font.pixelSize: 12
                        color: App.Theme.errorText
                        wrapMode: Text.WordWrap
                    }

                    Text {
                        text: "✕"
                        color: App.Theme.errorText
                        font.pixelSize: 12

                        MouseArea {
                            anchors.fill: parent
                            onClicked: overlayLogic.dismissError()
                        }
                    }
                }
            }

            // ── Page 0: Login
            ColumnLayout {
                visible: formMode === 0
                Layout.fillWidth: true
                spacing: 14

                    // Email input (46px)
                    Item {
                        Layout.fillWidth: true
                        height: 46

                        Rectangle {
                            anchors.fill: parent
                            radius: 8
                            color: App.Theme.card
                            border.color: emailLogin.activeFocus ? App.Theme.primary : App.Theme.input
                            border.width: 1
                        }

                        TextInput {
                            id: emailLogin
                            anchors { fill: parent; leftMargin: 12; rightMargin: 12 }
                            verticalAlignment: TextInput.AlignVCenter
                            font.pixelSize: 13
                            color: App.Theme.foreground
                            Keys.onTabPressed: function(event) { event.accepted = true; passLogin.forceActiveFocus() }

                            Text {
                                anchors.fill: parent
                                verticalAlignment: Text.AlignVCenter
                                text: App.Strings.emailPlaceholder
                                font.pixelSize: 13
                                color: App.Theme.textMuted
                                visible: !parent.text.length
                            }
                        }

                        MouseArea {
                            anchors.fill: parent
                            acceptedButtons: Qt.NoButton
                            cursorShape: Qt.IBeamCursor
                        }
                    }

                    // Password input (46px with show/hide toggle)
                    Components.XInputPassword {
                        id: passLogin
                        Layout.fillWidth: true
                        onAccepted: overlayLogic.doLogin()
                        onTabPressed: signInBtn.forceActiveFocus()
                    }

                    // Forgot password
                    Text {
                        text: App.Strings.forgotPasswordLabel
                        font.pixelSize: 12
                        color: App.Theme.primary
                        font.weight: Font.Medium

                        MouseArea {
                            anchors.fill: parent
                            cursorShape: Qt.PointingHandCursor
                            onClicked: root.forgotPasswordClicked()
                        }
                    }

                    // Sign In button (48px)
                    Rectangle {
                        id: signInBtn
                        Layout.fillWidth: true
                        height: 48
                        radius: 8
                        activeFocusOnTab: true
                        color: signInMouse.pressed ? "#000000" : (signInMouse.containsMouse ? "#1D2939" : App.Theme.brandDark)
                        border.color: signInBtn.activeFocus ? App.Theme.primary : "transparent"
                        border.width: 2
                        Behavior on color { ColorAnimation { duration: 100 } }

                        Keys.onReturnPressed: overlayLogic.doLogin()
                        Keys.onSpacePressed: overlayLogic.doLogin()
                        Keys.onTabPressed: function(event) { event.accepted = true; emailLogin.forceActiveFocus() }

                        Text {
                            anchors.centerIn: parent
                            text: App.Strings.signInLabel
                            font.pixelSize: 14
                            font.weight: Font.Bold
                            color: "white"
                        }

                        MouseArea {
                            id: signInMouse
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: overlayLogic.doLogin()
                        }
                    }

                    // OR divider
                    RowLayout {
                        Layout.fillWidth: true
                        Layout.topMargin: 2
                        spacing: 12

                        Rectangle { Layout.fillWidth: true; height: 1; color: App.Theme.border }
                        Text { text: App.Strings.orDivider; font.pixelSize: 12; color: App.Theme.textMuted }
                        Rectangle { Layout.fillWidth: true; height: 1; color: App.Theme.border }
                    }

                    // SSO buttons
                    Components.SSOButton {
                        Layout.fillWidth: true
                        provider: "google"
                        label: App.Strings.continueWithGoogle
                        onClicked: root.googleSSOClicked()
                    }

                    Components.SSOButton {
                        Layout.fillWidth: true
                        provider: "microsoft"
                        label: App.Strings.continueWithMicrosoft
                        onClicked: root.microsoftSSOClicked()
                    }

                    Item { height: 4 }
                }

            // ── Page 2: MFA
            ColumnLayout {
                visible: formMode === 2
                Layout.fillWidth: true
                spacing: root.qrImageSource != "" ? 24 : 16

                    // QR code — only shown during register MFA enrollment
                    Image {
                        Layout.alignment: Qt.AlignHCenter
                        Layout.preferredWidth: 150
                        Layout.preferredHeight: 150
                        fillMode: Image.PreserveAspectFit
                        source: root.qrImageSource
                        visible: root.qrImageSource != ""
                    }

                    // MFA code input (56px, 24px mono)
                    Rectangle {
                        Layout.fillWidth: true
                        height: 56
                        radius: 8
                        color: App.Theme.card
                        border.color: mfaCode.activeFocus ? App.Theme.primary : App.Theme.input
                        border.width: 1

                        TextInput {
                            id: mfaCode
                            anchors { fill: parent; leftMargin: 12; rightMargin: 12 }
                            verticalAlignment: TextInput.AlignVCenter
                            horizontalAlignment: TextInput.AlignHCenter
                            font.pixelSize: 24
                            font.family: "monospace"
                            font.letterSpacing: 12
                            maximumLength: 6
                            inputMethodHints: Qt.ImhDigitsOnly
                            color: App.Theme.foreground
                            Keys.onReturnPressed: overlayLogic.doMFA()

                            Text {
                                anchors.centerIn: parent
                                text: App.Strings.mfaPlaceholder
                                font.pixelSize: 24
                                font.family: "monospace"
                                font.letterSpacing: 12
                                color: App.Theme.textMuted
                                visible: !parent.text.length
                            }
                        }
                    }

                    Rectangle {
                        Layout.fillWidth: true
                        height: 48
                        radius: 8
                        color: mfaMouse.pressed ? "#000000" : (mfaMouse.containsMouse ? "#1D2939" : App.Theme.brandDark)
                        Behavior on color { ColorAnimation { duration: 100 } }

                        Text {
                            anchors.centerIn: parent
                            text: App.Strings.verifyCodeLabel
                            font.pixelSize: 14
                            font.weight: Font.Bold
                            color: "white"
                        }

                        MouseArea {
                            id: mfaMouse
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: overlayLogic.doMFA()
                        }
                    }

                    // Back button (bottom-left)
                    Text {
                        text: App.Strings.backLabel
                        font.pixelSize: 13
                        color: backMouse.containsMouse ? Qt.darker(App.Theme.primary, 1.2) : App.Theme.primary
                        font.weight: Font.Medium
                        Layout.topMargin: 4

                        MouseArea {
                            id: backMouse
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: overlayLogic.goBackFromMFA()
                        }
                    }

                    Item { height: 4 }
                }

            // ── Page 3: Forgot Password
            ColumnLayout {
                visible: formMode === 3
                Layout.fillWidth: true
                spacing: 14

                // Email input (46px) — same style as login email
                Item {
                    Layout.fillWidth: true
                    height: 46

                    Rectangle {
                        anchors.fill: parent; radius: 8
                        color: App.Theme.card
                        border.color: emailForgot.activeFocus ? App.Theme.primary : App.Theme.input
                        border.width: 1
                    }

                    TextInput {
                        id: emailForgot
                        anchors { fill: parent; leftMargin: 12; rightMargin: 12 }
                        verticalAlignment: TextInput.AlignVCenter
                        font.pixelSize: 13
                        color: App.Theme.foreground
                        Keys.onReturnPressed: overlayLogic.doForgotPassword()

                        Text {
                            anchors.fill: parent
                            verticalAlignment: Text.AlignVCenter
                            text: App.Strings.emailPlaceholder
                            font.pixelSize: 13
                            color: App.Theme.textMuted
                            visible: !parent.text.length
                        }
                    }

                    MouseArea {
                        anchors.fill: parent
                        acceptedButtons: Qt.NoButton
                        cursorShape: Qt.IBeamCursor
                    }
                }

                // Send Reset Link button (48px)
                Rectangle {
                    Layout.fillWidth: true
                    height: 48; radius: 8
                    color: resetMouse.pressed ? "#000000"
                         : (resetMouse.containsMouse ? "#1D2939" : App.Theme.brandDark)
                    Behavior on color { ColorAnimation { duration: 100 } }

                    Text {
                        anchors.centerIn: parent
                        text: App.Strings.sendResetLinkLabel
                        font.pixelSize: 14; font.weight: Font.Bold; color: "white"
                    }

                    MouseArea {
                        id: resetMouse
                        anchors.fill: parent; hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: overlayLogic.doForgotPassword()
                    }
                }

                // Back to Sign In link
                Text {
                    text: "\u2190 " + App.Strings.backToSignInLabel
                    font.pixelSize: 13; color: App.Theme.primary; font.weight: Font.Medium
                    Layout.topMargin: 4

                    MouseArea {
                        anchors.fill: parent; cursorShape: Qt.PointingHandCursor
                        onClicked: overlayLogic.switchToLoginMode()
                    }
                }

                Item { height: 4 }
            }

            // ── Page 5: Verify Email
            ColumnLayout {
                visible: formMode === 5
                Layout.fillWidth: true
                spacing: 14

                // Envelope icon
                Text {
                    text: "✉"
                    font.pixelSize: 40; color: App.Theme.primary
                    Layout.alignment: Qt.AlignHCenter
                }

                // Primary action: "I've Verified" button
                Rectangle {
                    Layout.fillWidth: true
                    height: 48; radius: 8
                    color: verifyDoneMouse.pressed ? "#000000"
                         : (verifyDoneMouse.containsMouse ? "#1D2939" : App.Theme.brandDark)
                    Behavior on color { ColorAnimation { duration: 100 } }

                    Text {
                        anchors.centerIn: parent
                        text: App.Strings.verifyEmailDoneLabel
                        font.pixelSize: 14; font.weight: Font.Bold; color: "white"
                    }

                    MouseArea {
                        id: verifyDoneMouse
                        anchors.fill: parent; hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: root.verifyEmailDoneClicked()
                    }
                }

                // Resend link
                Text {
                    text: App.Strings.verifyEmailResendLabel
                    font.pixelSize: 12; color: App.Theme.primary; font.weight: Font.Medium
                    Layout.alignment: Qt.AlignHCenter

                    MouseArea {
                        anchors.fill: parent; cursorShape: Qt.PointingHandCursor
                        onClicked: root.verifyEmailResendClicked()
                    }
                }

                // Back to sign in
                Text {
                    text: "← " + App.Strings.backToSignInLabel
                    font.pixelSize: 13; color: App.Theme.primary; font.weight: Font.Medium
                    Layout.topMargin: 4

                    MouseArea {
                        anchors.fill: parent; cursorShape: Qt.PointingHandCursor
                        onClicked: overlayLogic.switchToLoginMode()
                    }
                }

                Item { height: 4 }
            }

            // ── Page 4: Reset Email Sent
            ColumnLayout {
                visible: formMode === 4
                Layout.fillWidth: true
                spacing: 14

                // Success icon
                Text {
                    text: "\u2713"
                    font.pixelSize: 40; color: "#12B76A"
                    Layout.alignment: Qt.AlignHCenter
                }

                // "We sent a reset link to <email>"
                Text {
                    text: App.Strings.resetSuccessSubtitle
                    font.pixelSize: 13; color: App.Theme.textSecondary
                    Layout.alignment: Qt.AlignHCenter
                }

                Text {
                    id: resetEmailLabel
                    font.pixelSize: 14; font.weight: Font.Bold
                    color: App.Theme.textPrimary
                    Layout.alignment: Qt.AlignHCenter
                }

                // Back to Sign In button (48px)
                Rectangle {
                    Layout.fillWidth: true; Layout.topMargin: 10
                    height: 48; radius: 8
                    color: backSignInMouse.pressed ? "#000000"
                         : (backSignInMouse.containsMouse ? "#1D2939" : App.Theme.brandDark)
                    Behavior on color { ColorAnimation { duration: 100 } }

                    Text {
                        anchors.centerIn: parent
                        text: App.Strings.backToSignInLabel
                        font.pixelSize: 14; font.weight: Font.Bold; color: "white"
                    }

                    MouseArea {
                        id: backSignInMouse
                        anchors.fill: parent; hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: overlayLogic.switchToLoginMode()
                    }
                }

                Item { height: 4 }
            }
        }
    }
}
