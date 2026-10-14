import QtQuick 2.15
import QtQuick.Window 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15
import "." as App
import "logic"

// ─────────────────────────────────────────────────────────────────────────────
// MainWindow.qml - Main application window (UI only)
// All logic lives in logic/MainWindowLogic.qml.
// Python bridge functions are thin delegators that forward to mainLogic.
// ─────────────────────────────────────────────────────────────────────────────

ApplicationWindow {
    id: root

    onClosing: function(close) {
        if (bridge && !bridge.safeToClose) {
            close.accepted = false
            bridge.closeRequested()
        }
    }

    visible: true
    width: 1280
    height: 800
    minimumWidth: 1024
    minimumHeight: 660
    title: App.Strings.appTitle
    color: App.Theme.background

    property var bridge: null
    property var xaliva_logo: null

    // ── Application state (bound to by UI elements; set by MainWindowLogic)
    property int activeTab: 0
    property bool usbConnected: false
    property var usbPorts: []
    property var usbLogs: []
    property var cloudLogs: []
    property string pcPath: App.Strings.noFolderSelected
    property var pcFiles: []
    property var recentFiles: []
    property string userEmail: ""
    property string userName: "Guest"
    property var deviceInfo: []
    property string logInfo: "(select a log file to inspect)"
    property bool plotVisible: false
    property var plotSpec: null
    property var selectedFiles: []
    property bool currentlyAnalyzing: false

    // ── Per-tab busy state (USB, Cloud overlays)
    property bool usbBusy: false
    property string usbBusyMessage: ""
    property bool cloudBusy: false
    property string cloudBusyMessage: ""

    // ── Status bar state (intermediate properties so logic can set them
    //    without needing a direct reference to the footer's child IDs)
    property string statusMessage: App.Strings.statusReady
    property bool isBusy: false

    // ── Error dialog state
    property string errorMessage: ""
    property alias errorDialog: errorDialogItem

    // ── Info toast state
    property string toastMessage: ""
    property bool toastVisible: false
    property string toastLevel: "info"  // "info", "success", "warning", "error"

    // ── Logic object
    MainWindowLogic {
        id: mainLogic
        window: root
        loginOverlay: loginOverlay
        vizPanel: vizPanel
    }

    // ── Status bar (26px)
    footer: Rectangle {
        height: 26
        color: "#FAFAFA"
        border.color: App.Theme.border
        border.width: 1

        RowLayout {
            anchors {
                fill: parent
                leftMargin: 14
                rightMargin: 14
            }

            Text {
                text: root.statusMessage
                font.pixelSize: 11
                color: App.Theme.textSecondary
                Layout.fillWidth: true
            }

            Rectangle {
                width: 12
                height: 12
                radius: 6
                visible: root.isBusy
                color: "transparent"
                border.color: App.Theme.primary
                border.width: 2

                RotationAnimator on rotation {
                    running: root.isBusy
                    from: 0
                    to: 360
                    duration: 800
                    loops: Animation.Infinite
                }
            }
        }
    }

    // ── Main layout (Sidebar + FilePanel + VisualizationPanel)
    RowLayout {
        anchors.fill: parent
        spacing: 0

        // ── Sidebar (17-20% of window width, min 200px, max 280px)
        App.Sidebar {
            id: sidebar
            Layout.fillHeight: true
            Layout.preferredWidth: Math.max(200, Math.min(280, root.width * 0.19))
            Layout.minimumWidth: 200
            Layout.maximumWidth: 280

            xaliva_logo: root.xaliva_logo
            activeTab: root.activeTab
            userEmail: root.userEmail
            userName: root.userName
            recentFiles: root.recentFiles

            onTabChanged:        function(newTab) { mainLogic.handleTabChanged(newTab) }
            onSettingsClicked:   mainLogic.handleSettingsClicked()
            onSignOutClicked:    mainLogic.handleSignOutClicked()
            onLoginSignupClicked: mainLogic.handleLoginSignupClicked()
            onRecentFileClicked: function(filename) { mainLogic.handleRecentFileClicked(filename) }
        }

        // ── File Panel (20-25% of window width, min 250px, max 350px)
        App.FilePanel {
            id: filePanel
            Layout.fillHeight: true
            Layout.preferredWidth: Math.max(250, Math.min(350, root.width * 0.22))
            Layout.minimumWidth: 250
            Layout.maximumWidth: 350

            activeTab: root.activeTab
            usbConnected: root.usbConnected
            usbPorts: root.usbPorts
            usbLogs: root.usbLogs
            cloudLogs: root.cloudLogs
            pcPath: root.pcPath
            pcFiles: root.pcFiles
            usbBusy: root.usbBusy
            usbBusyMessage: root.usbBusyMessage
            cloudBusy: root.cloudBusy
            cloudBusyMessage: root.cloudBusyMessage
            currentlyAnalyzing: root.currentlyAnalyzing

            onDetectClicked:                               mainLogic.handleDetectClicked()
            onConnectClicked:    function(port)          { mainLogic.handleConnectClicked(port) }
            onDisconnectClicked:                          mainLogic.handleDisconnectClicked()
            onListUsbFilesClicked:                        mainLogic.handleListUsbFilesClicked()
            onSaveAllUsbToDiskClicked:                    mainLogic.handleSaveAllUsbToDiskClicked()
            onSaveOneUsbFileToDiskClicked: function(filename)  { mainLogic.handleSaveOneUsbFileToDiskClicked(filename) }
            onSyncUsbToCloudClicked: function(files)     { mainLogic.handleSyncUsbToCloudClicked(files) }
            onListCloudFilesClicked: function(deviceId)  { mainLogic.handleListCloudFilesClicked(deviceId) }
            onDownloadCloudFilesToDiskClicked: function(files) { mainLogic.handleDownloadCloudFilesToDiskClicked(files) }
            onBrowsePcClicked: function(currentPath)  { mainLogic.handleBrowsePcClicked(currentPath) }
            onFilesSelected: function(files)          { mainLogic.handleFilesSelected(files) }
            onAnalyzeFileClicked: function(filename)  { mainLogic.handleAnalyzeFileClicked(filename) }
        }

        // ── Visualization Panel (fills remaining space)
        App.VisualizationPanel {
            id: vizPanel
            Layout.fillWidth: true
            Layout.fillHeight: true

            deviceInfo: root.deviceInfo
            logInfo: root.logInfo
            plotVisible: root.plotVisible
            plotSpec: root.plotSpec
            isAnalyzing: root.currentlyAnalyzing

            onExportCsvClicked: mainLogic.handleExportCsvClicked()
            onScreenshotClicked: mainLogic.handleScreenshotClicked()
            onChartSeriesReady: function(seriesList) {
                if (root.bridge) root.bridge.chartSeriesReady(seriesList)
            }
        }
    }

    // ── Toast level color/icon lookup
    function getToastColors(level) {
        switch (level) {
            case "success":
                return { bg: "#ECFDF5", border: "#86EFAC", icon: "✓", showSpinner: false };
            case "warning":
                return { bg: "#FFFBEB", border: "#FCD34D", icon: "⚠", showSpinner: false };
            case "error":
                return { bg: "#FEF2F2", border: "#FCA5A5", icon: "✕", showSpinner: false };
            default: // "info"
                return { bg: "#EEF2FF", border: App.Theme.primary, icon: "", showSpinner: true };
        }
    }

    function getToastTimeout(level) {
        switch (level) {
            case "warning":
                return 6000;  // 6 seconds
            case "error":
                return 8000;  // 8 seconds
            default:
                return 4000;  // 4 seconds (info, success)
        }
    }

    // ── Info Toast (auto-dismissing notification at top-center)
    Rectangle {
        id: infoToast
        z: 999
        width: Math.min(420, root.width * 0.4)
        height: toastRow.implicitHeight + 20
        radius: 8
        color: root.getToastColors(root.toastLevel).bg
        border.color: root.getToastColors(root.toastLevel).border
        border.width: 1
        anchors.horizontalCenter: parent.horizontalCenter
        y: root.toastVisible ? 12 : -height - 4
        opacity: root.toastVisible ? 1.0 : 0.0
        visible: opacity > 0

        Behavior on y   { NumberAnimation { duration: 250; easing.type: Easing.OutCubic } }
        Behavior on opacity { NumberAnimation { duration: 250 } }

        RowLayout {
            id: toastRow
            anchors { fill: parent; leftMargin: 14; rightMargin: 14 }

            Rectangle {
                width: 10; height: 10; radius: 5
                color: App.Theme.primary
                visible: root.getToastColors(root.toastLevel).showSpinner

                RotationAnimator on rotation {
                    running: root.getToastColors(root.toastLevel).showSpinner
                    from: 0; to: 360; duration: 800
                    loops: Animation.Infinite
                }
            }

            Text {
                text: root.getToastColors(root.toastLevel).icon
                font.pixelSize: 16
                color: "#1F2937"
                visible: root.getToastColors(root.toastLevel).icon !== ""
                Layout.rightMargin: 6
            }

            Text {
                text: root.toastMessage
                font.pixelSize: 12
                color: "#1F2937"
                wrapMode: Text.Wrap
                Layout.fillWidth: true
            }

            Text {
                text: "\u2715"
                font.pixelSize: 11
                color: "#9CA3AF"
                MouseArea {
                    anchors.fill: parent
                    cursorShape: Qt.PointingHandCursor
                    onClicked: mainLogic.dismissToast()
                }
            }
        }
    }

    // ── Error Dialog
    Dialog {
        id: errorDialogItem
        anchors.centerIn: parent
        modal: true
        title: App.Strings.errorDialogTitle
        standardButtons: Dialog.Ok
        width: 380

        Label {
            text: root.errorMessage
            wrapMode: Text.Wrap
            width: parent.width
            font.pixelSize: 13
            color: App.Theme.textPrimary
        }
    }

    // ── Login Overlay
    App.LoginOverlay {
        id: loginOverlay

        onLoginClicked:          function(email, password) { mainLogic.handleLoginClicked(email, password) }
        onMfaVerifyClicked:      function(code) { mainLogic.handleMfaVerifyClicked(code) }
        onGoogleSSOClicked:      mainLogic.handleGoogleSSOClicked()
        onMicrosoftSSOClicked:   mainLogic.handleMicrosoftSSOClicked()
        onForgotPasswordClicked: mainLogic.handleForgotPasswordClicked()
        onForgotPasswordSubmitClicked: function(email) { mainLogic.handleForgotPasswordSubmitClicked(email) }
        onVerifyEmailDoneClicked:   mainLogic.handleVerifyEmailDoneClicked()
        onVerifyEmailResendClicked: mainLogic.handleVerifyEmailResendClicked()
    }

    // ══════════════ PUBLIC API (Python → QML)
    // Thin delegators — implementations live in MainWindowLogic.qml

    function setStatus(msg, busy)              { mainLogic.setStatus(msg, busy) }
    function setUserEmail(email)               { mainLogic.setUserEmail(email) }
    function setUserName(name)                 { mainLogic.setUserName(name) }
    function setUsbPorts(ports)                { mainLogic.setUsbPorts(ports) }
    function setUsbConnected(connected, devId) { mainLogic.setUsbConnected(connected, devId) }
    function setUsbLogs(logs)                  { mainLogic.setUsbLogs(logs) }
    function setCloudLogs(logs)                { mainLogic.setCloudLogs(logs) }
    function setLocalPath(path)                { mainLogic.setLocalPath(path) }
    function setLocalFiles(files)              { mainLogic.setLocalFiles(files) }
    function setDeviceInfo(pairs)              { mainLogic.setDeviceInfo(pairs) }
    function setLogInfo(text)                  { mainLogic.setLogInfo(text) }
    function setPlotVisible(visible)           { mainLogic.setPlotVisible(visible) }
    function setPlotConfig(spec)               { mainLogic.setPlotConfig(spec) }
    function setRecentFiles(files)             { mainLogic.setRecentFiles(files) }
    function showLoginOverlay()                { mainLogic.showLoginOverlay() }
    function hideLoginOverlay()                { mainLogic.hideLoginOverlay() }
    function loginShowError(msg)               { mainLogic.loginShowError(msg) }
    function loginShowMFA(qrUrl)               { mainLogic.loginShowMFA(qrUrl) }
    function loginShowVerifyEmail()            { loginOverlay.showVerifyEmail() }
    function showForgotPasswordForm()          { loginOverlay.showForgotPasswordForm() }
    function showForgotPasswordSuccess(email)  { loginOverlay.showForgotPasswordSuccess(email) }
    function showSuccess(msg)                  { mainLogic.showSuccess(msg) }
    function showWarning(msg)                  { mainLogic.showWarning(msg) }
    function showInfo(msg)                     { mainLogic.showInfo(msg) }
    function showError(msg)                    { mainLogic.showError(msg) }
    function saveScreenshot(filename)          { mainLogic.saveScreenshot(filename) }
    function setUsbBusy(busy, msg)             { mainLogic.setUsbBusy(busy, msg) }
    function setCloudBusy(busy, msg)           { mainLogic.setCloudBusy(busy, msg) }
}
