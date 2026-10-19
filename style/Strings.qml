pragma Singleton
import QtQuick 2.15

// ─────────────────────────────────────────────────────────────────────────────
// Strings.qml - All UI copy for the login overlay
// ─────────────────────────────────────────────────────────────────────────────

QtObject {
    // Titles & subtitles
    readonly property string loginTitle:              "Log in"
    readonly property string loginSubtitle:           "Sign in to XALIVA Log Analyzer"
    readonly property string mfaTitle:                "Two-Factor Authentication"
    readonly property string mfaSubtitle:             "Enter the code from your authenticator app"

    // Placeholders
    readonly property string emailPlaceholder:           "name@company.com"
    readonly property string passwordPlaceholder:        "••••••••"
    readonly property string mfaPlaceholder:             "000000"

    // Button labels
    readonly property string signInLabel:          "Sign In"
    readonly property string verifyCodeLabel:      "Verify Code"
    readonly property string showLabel:            "Show"
    readonly property string hideLabel:            "Hide"

    // Forgot password
    readonly property string forgotPasswordTitle:       "Reset password"
    readonly property string forgotPasswordSubtitle:    "Enter your email to receive a reset link"
    readonly property string sendResetLinkLabel:        "Send Reset Link"
    readonly property string resetSuccessTitle:         "Check your email"
    readonly property string resetSuccessSubtitle:      "We sent a password reset link to"
    readonly property string backToSignInLabel:         "Back to Sign In"

    // Verify email page (formMode 5)
    readonly property string verifyEmailTitle:          "Verify Your Email"
    readonly property string verifyEmailSubtitle:       "We sent a verification link to your inbox. Click it, then return here."
    readonly property string verifyEmailDoneLabel:      "I've Verified My Email"
    readonly property string verifyEmailResendLabel:    "Resend verification email"

    // Links & nav copy
    readonly property string forgotPasswordLabel:      "Forgot password?"
    readonly property string orDivider:                "or"

    // USB busy messages
    readonly property string usbDetecting:     "Detecting USB ports…"
    readonly property string usbConnecting:    "Connecting…"
    readonly property string usbFetching:      "Fetching log list…"
    readonly property string usbSaving:        "Saving files…"
    readonly property string usbSyncing:       "Syncing to cloud…"
    readonly property string usbBusyGeneric:   "USB operation in progress…"

    // Cloud busy messages
    readonly property string cloudFetching:    "Fetching log list…"
    readonly property string cloudDownloading: "Downloading files…"
    readonly property string cloudBusyGeneric: "Cloud operation in progress…"

    // Analysis state
    readonly property string analysisInProgress: "Analysis already in progress"

    // Status
    readonly property string statusReady: "Ready"

    // App / window
    readonly property string appTitle:              "XALIVA Log Analyzer"
    readonly property string errorDialogTitle:      "Error"

    // Sidebar nav
    readonly property string navUsb:                "USB Connection"
    readonly property string navCloud:              "From Cloud"
    readonly property string navPc:                 "This PC"
    readonly property string recentlyViewedLabel:   "RECENTLY VIEWED"
    readonly property string noRecentFiles:         "No recent files"
    readonly property string settingsLabel:         "Settings"
    readonly property string logOutLabel:           "Log out"
    readonly property string loginSignUpLabel:      "Login"
    readonly property string notSignedIn:           "Not signed in"

    // File Panel — USB tab
    readonly property string usbConnectionHeader:   "USB Connection"
    readonly property string serialPortLabel:       "SERIAL PORT"
    readonly property string noPortsDetected:       "No ports detected"
    readonly property string connectLabel:          "Connect"
    readonly property string disconnectLabel:       "Disconnect"
    readonly property string fetchLogsLabel:        "Fetch Logs"
    readonly property string connectedLabel:        "Connected"
    readonly property string disconnectedLabel:     "Disconnected"
    readonly property string saveAllToDiskLabel:    "Save All to Disk"
    readonly property string syncToCloudLabel:      "Sync All to Cloud"

    // File Panel — Cloud tab
    readonly property string fromCloudHeader:       "From Cloud"
    readonly property string deviceIdLabel:         "DEVICE ID"
    readonly property string deviceIdPlaceholder:   "Enter device ID"
    readonly property string fetchLabel:            "Fetch"
    readonly property string saveSelectedLabel:     "Save Selected"

    // File Panel — This PC tab
    readonly property string thisPcHeader:          "This PC"
    readonly property string folderPathLabel:       "FOLDER PATH"
    readonly property string noFolderSelected:      "No folder selected"

    // File list
    readonly property string noFiles:              "No files"

    // Visualization panel
    readonly property string deviceInfoHeader:      "Device Information"
    readonly property string logInfoHeader:         "Log Information"
    readonly property string dataVizHeader:         "Data Visualization"
    readonly property string selectLogPrompt:       "Select a log file and click Analyze"
    readonly property string analyzingLabel:        "Analyzing…"
    readonly property string exportCsvLabel:        "Export CSV"
    readonly property string tooltipZoomReset:      "Default Zoom"
    readonly property string tooltipZoomOut:        "Zoom Out"
    readonly property string tooltipZoomIn:         "Zoom In"
    readonly property string tooltipScreenshot:     "Save Screenshot"

    // SSO buttons
    readonly property string continueWithGoogle:    "Continue with Google"
    readonly property string continueWithMicrosoft: "Continue with Microsoft"

    // MFA back link
    readonly property string backLabel:             "← Back"
}
