import QtQuick 2.15
import ".." as App

// ─────────────────────────────────────────────────────────────────────────────
// MainWindowLogic.qml - All state management logic for MainWindow
// Receives references to the root ApplicationWindow and the LoginOverlay item.
// MainWindow.qml keeps thin one-liner delegators so the Python bridge API
// (root.setStatus, root.setUsbPorts, etc.) is unchanged.
// ─────────────────────────────────────────────────────────────────────────────

QtObject {
    id: logic

    // ── References injected by MainWindow.qml on instantiation
    property var window: null        // root ApplicationWindow
    property var loginOverlay: null  // LoginOverlay { id: loginOverlay }
    property var vizPanel: null      // VisualizationPanel { id: vizPanel }

    // ══════════════ Python → QML bridge implementations

    function setStatus(msg, busy) {
        window.statusMessage = msg
        window.isBusy = busy
        // Note: toast is no longer shown by setStatus. Use showToast, showSuccess, showWarning, showInfo instead.
    }

    function showToast(msg, level) {
        window.toastMessage = msg
        window.toastLevel = level
        window.toastVisible = true
        toastTimer.interval = window.getToastTimeout(level)
        toastTimer.restart()
    }

    function setUserEmail(email) {
        window.userEmail = email
    }

    function setUserName(name) {
        window.userName = name
    }

    function setUsbPorts(ports) {
        window.usbPorts = ports
    }

    function setUsbConnected(connected, deviceId) {
        window.usbConnected = connected
        if (!connected) {
            window.usbLogs = []
            window.selectedFiles = []
            window.deviceInfo = []
        }
    }

    function setUsbLogs(logs) {
        window.usbLogs = logs.slice().sort()
        window.selectedFiles = []
    }

    function setCloudLogs(logs) {
        window.cloudLogs = logs.slice().sort()
        window.selectedFiles = []
    }

    function setLocalPath(path) {
        window.pcPath = path
    }

    function setLocalFiles(files) {
        window.pcFiles = files.slice().sort()
        window.selectedFiles = []
    }

    function setDeviceInfo(pairs) {
        window.deviceInfo = pairs
    }

    function setLogInfo(text) {
        window.logInfo = text
    }

    function setPlotVisible(visible) {
        window.plotVisible = visible
    }

    function setPlotConfig(spec) {
        window.plotVisible = true   // show ChartView before setupConfig fires
        if (vizPanel) vizPanel.setupConfig(spec)
    }

    function setRecentFiles(files) {
        window.recentFiles = files
    }

    function showLoginOverlay() {
        loginOverlay.show()
    }

    function hideLoginOverlay() {
        loginOverlay.hide()
        // Only switch tabs if the user was trying to reach a specific tab
        // (e.g. clicked Cloud while logged out). Otherwise stay on current tab.
        if (pendingTab >= 0) {
            window.activeTab = pendingTab
            pendingTab = -1
        }
    }

    function loginShowError(msg) {
        loginOverlay.showError(msg)
    }

    function loginShowMFA(qrUrl) {
        loginOverlay.showMFA(qrUrl)
    }

    function showSuccess(msg) {
        window.statusMessage = msg
        window.isBusy = false
        showToast(msg, "success")
        successTimer.restart()
    }

    function showWarning(msg) {
        window.statusMessage = msg
        window.isBusy = false
        showToast(msg, "warning")
    }

    function showInfo(msg) {
        window.statusMessage = msg
        window.isBusy = false
        showToast(msg, "info")
    }

    function showError(msg) {
        window.errorMessage = msg
        window.errorDialog.open()
    }

    function setUsbBusy(busy, msg) {
        window.usbBusy = busy
        window.usbBusyMessage = msg
    }

    function setCloudBusy(busy, msg) {
        window.cloudBusy = busy
        window.cloudBusyMessage = msg
    }

    // ══════════════ QML → Python bridge forwarding (null-guarded)

    function callBridge(method) {
        if (window.bridge) method()
    }

    // Sidebar actions
    function handleAnalyzeFileClicked(filename)    {
        if (window.currentlyAnalyzing) return
        if (vizPanel) vizPanel.clearChart()
        window.plotVisible = false   // disable Export CSV until new plot data arrives
        setStatus("Analyzing " + filename + "…", true)
        callBridge(function() { window.bridge.analyzeClicked(window.activeTab, filename) })
    }
    function handleSettingsClicked()               { callBridge(function() { window.bridge.settingsClicked() }) }
    function handleSignOutClicked()                { callBridge(function() { window.bridge.signOutClicked() }) }
    function handleLoginSignupClicked()            { loginOverlay.show() }
    function handleRecentFileClicked(filename)     { pendingTab = -1; callBridge(function() { window.bridge.recentFileClicked(filename) }) }

    // FilePanel actions
    function handleDetectClicked()                     { setStatus(App.Strings.usbDetecting, true); callBridge(function() { window.bridge.detectClicked() }) }
    function handleConnectClicked(port)                { setStatus(App.Strings.usbConnecting, true); callBridge(function() { window.bridge.connectClicked(port) }) }
    function handleDisconnectClicked()                 { callBridge(function() { window.bridge.disconnectClicked() }) }
    function handleListUsbFilesClicked()               { setUsbBusy(true, App.Strings.usbFetching); callBridge(function() { window.bridge.listUsbFilesClicked() }) }
    function handleSaveAllUsbToDiskClicked()            { setStatus(App.Strings.usbSaving, true); setUsbBusy(true, App.Strings.usbSaving); callBridge(function() { window.bridge.saveAllUsbToDiskClicked(window.usbLogs) }) }
    function handleSaveOneUsbFileToDiskClicked(filename) { setStatus(App.Strings.usbSaving, true); setUsbBusy(true, App.Strings.usbSaving); callBridge(function() { window.bridge.saveOneUsbFileToDiskClicked(filename) }) }
    function handleSyncUsbToCloudClicked(files)        { setUsbBusy(true, App.Strings.usbSyncing); callBridge(function() { window.bridge.syncUsbToCloudClicked(files) }) }
    function handleListCloudFilesClicked(deviceId)     { setCloudBusy(true, App.Strings.cloudFetching); callBridge(function() { window.bridge.listCloudFilesClicked(deviceId) }) }
    function handleDownloadCloudFilesToDiskClicked(files) { setStatus(App.Strings.cloudDownloading, true); setCloudBusy(true, App.Strings.cloudDownloading); callBridge(function() { window.bridge.downloadCloudFilesToDiskClicked(files) }) }
    function handleBrowsePcClicked(currentPath)    { callBridge(function() { window.bridge.browsePcClicked(currentPath) }) }
    function handleFilesSelected(files)            { window.selectedFiles = files }

    // VisualizationPanel actions
    function handleExportCsvClicked()              { callBridge(function() { window.bridge.exportCsvClicked() }) }
    function handleScreenshotClicked()             { callBridge(function() { window.bridge.screenshotClicked() }) }

    // LoginOverlay actions
    function handleLoginClicked(email, password)   { callBridge(function() { window.bridge.loginClicked(email, password, false) }) }
    function handleMfaVerifyClicked(code)          { callBridge(function() { window.bridge.mfaVerifyClicked(code) }) }
    function handleGoogleSSOClicked()              { callBridge(function() { window.bridge.googleSSOClicked() }) }
    function handleMicrosoftSSOClicked()           { callBridge(function() { window.bridge.microsoftSSOClicked() }) }
    function handleForgotPasswordClicked()         { callBridge(function() { window.bridge.forgotPasswordClicked() }) }
    function handleForgotPasswordSubmitClicked(email) { callBridge(function() { window.bridge.forgotPasswordSubmitClicked(email) }) }
    function handleVerifyEmailDoneClicked()        { callBridge(function() { window.bridge.verifyEmailDoneClicked() }) }
    function handleVerifyEmailResendClicked()      { callBridge(function() { window.bridge.verifyEmailResendClicked() }) }

    // Toast dismiss
    function dismissToast() { window.toastVisible = false }

    // ══════════════ Navigation logic

    // Tab the user was trying to reach when login was required (-1 = none)
    property int pendingTab: -1

    function handleTabChanged(newTab) {
        if (window.userEmail.length === 0) {
            pendingTab = newTab
            loginOverlay.show()
        } else {
            window.activeTab = newTab
        }
    }

    // ══════════════ Bridge signal listeners (analysis lifecycle + per-tab busy state)

    property var _bridgeConn: Connections {
        target: window ? window.bridge : null
        function onAnalysisStarted() { window.currentlyAnalyzing = true  }
        function onAnalysisEnded()   { window.currentlyAnalyzing = false }
        function onUsbBusyChanged(busy, msg) { logic.setUsbBusy(busy, msg) }
        function onCloudBusyChanged(busy, msg) { logic.setCloudBusy(busy, msg) }
    }

    // ══════════════ Internal timer — resets status bar after showSuccess()

    property var successTimer: Timer {
        interval: 5000
        onTriggered: logic.setStatus(App.Strings.statusReady, false)
    }

    property var toastTimer: Timer {
        interval: 4000
        onTriggered: window.toastVisible = false
    }
}
