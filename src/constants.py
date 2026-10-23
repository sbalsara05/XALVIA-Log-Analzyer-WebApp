from serial import SerialException, SerialTimeoutException
from enum import IntEnum, auto


import os

FIREBASE_API_KEY = os.environ.get("FIREBASE_API", "AIzaSyB8PdU9NmRBuoslHEU8rsdKrzBcOhZ2WPo")
_DEFAULT_GATEWAY_URL = "https://xaliva-gateway-468534916638.us-east1.run.app"
GO_GATEWAY_URL = os.environ.get("XALIVA_GATEWAY_URL", _DEFAULT_GATEWAY_URL)

print("Go Gateway URL is ", GO_GATEWAY_URL)

APP_NAME   = "XalivaLog"
APP_AUTHOR = "Ricovr"

## --- used in main.py ---

MAIN_WINDOW_STYLESHEET_DIR_STUB = "style/main_window.qss"
LOGIN_DIALOG_STYLESHEET_DIR_STUB = "style/login_dialog.qss"
APP_ICON_DIR_STUB = "logo/RICOVR_LOGO.ico"
SVG_APP_ICON_DIR_STUB = "logo/RICOVR_LOGO.svg"
COMPANY_LOGO_DIR_STUB = "logo/RICOVR.svg"

BAUDRATE=115_200
TIMEOUT=1.0


## --- used in view.py ---

class LOG_SRC(IntEnum):
    """
    Enum for source of log files
    """
    USB = 1
    CLOUD= 2
    DISK=3

ACTION_ID_MAP = {10: "USB Tab: Detect USB Ports",
                 11: "USB Tab: List Log File Names from USB port",
                12: "USB Tab: Save All Log Files to disk",
                 13: "USB Tab: Stage USB Files for Cloud Upload",
                 14: "USB Tab: Upload log files to Cloud",
                 15: "USB Tab: List log files from Cloud for a specific MAC ID",
                 16: "USB Tab: List all Log File Names from USB port TEMPORARILY to support Upload to Cloud",
            20: "cloud",
            21:"Cloud Tab: List log files present on cloud storage",
            22: "Cloud Tab: TEMPORARILY Download a specific log file from cloud storage to a temp folder",
            30: "disk",
            99: "Analyze Log File"}





## used in model.py

class ACTION(IntEnum):
    DETECT_USB_SERIAL_PORTS          = 10
    CONNECT_USB_SERIAL_PORT = 11
    DISCONNECT_USB_SERIAL_PORT= 12
    LIST_USB_FILES=13
    SAVE_ALL_USB_FILES_TO_DISK = 14
    SAVE_ONE_USB_FILE_TO_DISK = 19
    STAGE_USB_FILES_FOR_CLOUD = 15
    UPLOAD_USB_FILES_TO_CLOUD = 16
    PARSE_USB_DEVICE_LOG= 17
    ANALYZE_USB_LOG=18
    LIST_CLOUD_FILES        = 20
    ANALYZE_CLOUD_LOG=21
    ANALYZE_DISK_LOG=30
    DOWNLOAD_CLOUD_FILES_TO_DISK = 23
    GATEWAY_LIST_FILES   = 24
    GATEWAY_READ_FILE    = 25
    GATEWAY_ACL_PREFLIGHT = 27
    AUTHENTICATE = 40
    FORGOT_PASSWORD = 41
    MFA_ACTION = 50
    RECHECK_EMAIL_VERIFICATION = 42


class TAB(IntEnum):
    USB   = 0
    CLOUD = 1
    PC    = 2

# --- Sentinel strings for control flow (not displayed to users) ---

class GATEWAY_STATUS:
    """HTTP error status codes from gateway."""
    UNAUTHORIZED   = "UNAUTHORIZED"
    FORBIDDEN      = "FORBIDDEN"
    NOT_FOUND      = "NOT_FOUND"       # device unknown (404 from /api/files or ACL preflight)
    FILE_NOT_FOUND = "FILE_NOT_FOUND"  # file missing from storage (404 from /api/signed-url/read)
    MFA_REQUIRED   = "MFA_REQUIRED"    # 403 with code=mfa_required — user must complete TOTP enrollment


class AUTH_TYPE:
    """Auth flow type selector."""
    LOGIN  = "login"


class MFA_MODE:
    """MFA enrollment/verification mode."""
    START           = "START"
    FINALIZE        = "FINALIZE"
    FINALIZE_SIGNIN = "FINALIZE_SIGNIN"


class MFA_PAYLOAD_TYPE:
    """MFA payload type selector."""
    START    = "mfa_start"
    FINALIZE = "mfa_finalize"


class SOURCE:
    """Log source provenance."""
    USB   = "usb"
    CLOUD = "cloud"
    DISK  = "disk"


# Progress message templates keyed by ACTION id.
# Each value is a list of strings; the worker's `phase` index selects which
# template to use (defaults to 0).
PROGRESS_MSG: dict[int, list[str]] = {
    ACTION.LIST_USB_FILES:                   ["Fetching log list…"],
    ACTION.SAVE_ALL_USB_FILES_TO_DISK:       ["Saving {cur}/{tot} files to disk…"],
    ACTION.SAVE_ONE_USB_FILE_TO_DISK:        ["Saving file to disk…"],
    ACTION.STAGE_USB_FILES_FOR_CLOUD:        ["Reading {cur}/{tot} files from device…"],
    ACTION.UPLOAD_USB_FILES_TO_CLOUD:        ["Reading {cur}/{tot} files from USB…",
                                              "Uploading {cur}/{tot} files to cloud…"],
    ACTION.PARSE_USB_DEVICE_LOG:             ["Parsing device log…"],
    ACTION.GATEWAY_LIST_FILES:               ["Fetching file list from cloud…"],
    ACTION.GATEWAY_READ_FILE:                ["Downloading {cur}/{tot} files…"],
    ACTION.GATEWAY_ACL_PREFLIGHT:            ["Checking access…"],
    ACTION.DOWNLOAD_CLOUD_FILES_TO_DISK:     ["Downloading {cur}/{tot} files from cloud…"],
}


CLOUD_BUCKET   = "xaliva_logs" 

FILE_FATALS = (FileNotFoundError, PermissionError,            #  filesystem errors
OSError,
UnicodeError,            # parent class of decode/encode errors
ValueError,
IndexError,
)
## used in workers.py
USB_SERIAL_FATALS = (SerialException, SerialTimeoutException)

DEVICE_LOG_CODE_MAP = {
    '0': 'Device turn ON using charging cable',
'1': 'Device turn ON  by presssing OK button',
'2': 'Battery % at boot-up time',
'3': 'POST PASS/FAIL ESP32/STM32',
'4': 'Cartridge present at boot-up time ',
'5': 'Cartridge remove which present at boot-up time ',
'6': 'Start of Test Id',
'7': 'End of Test Id',
'8': 'Cartridge removal after end of test',
'9': 'Turn OFF by time-out',
'10': 'Turn OFF by user',
'11': 'Turn OFF by usb remove',
'12': 'Enter Settings',
'13': 'Exit Settings',
'14': 'Enter filevault',
'15': 'Exit filevault',

}

# Maps TAB enum → string used by model.analyze_log
TAB_NAME = {TAB.USB: "USB", TAB.CLOUD: "CLOUD", TAB.PC: "PC"}


class MSG:
    """User-facing message strings, grouped by domain."""

    class Status:
        """UI status messages."""
        READY                = "Ready"
        CONNECTED            = "Connected: {}"        # .format(device_id)
        DISCONNECTED         = "Disconnected"
        SIGNED_IN            = "Signed in"
        ANALYSIS_COMPLETE    = "Analysis complete"
        PORT_DETECT_FAILED   = "Port detection failed: {}"
        PORTS_DETECTED       = "Ports detected"
        FETCH_FAILED         = "Fetch failed: {}"
        FETCHED_USB_LOGS     = "Fetched {} logs"
        FETCHED_CLOUD_LOGS   = "Fetched {} cloud logs"
        SAVE_FAILED          = "Save failed: {}"
        SYNC_FAILED          = "Sync failed: {}"
        DOWNLOAD_FAILED      = "Download failed: {}"
        CANNOT_PLOT          = "Cannot plot: {}"
        ANALYZING            = "Analyzing {}…"

    class Auth:
        SUCCESS             = "Success"
        MFA_READY           = "MFA Ready"
        MFA_START_FAILED    = "MFA Start Failed"
        MFA_REQUIRED        = "MFA_REQUIRED"
        NO_TOKEN            = "Server returned 200 but no token found"
        LOGIN_FAILED        = "Login Failed"
        MFA_VERIFY_FAILED   = "MFA Verification Failed"
        NO_SESSION          = "No active session"
        EMAIL_SENT          = "Sent"
        EMAIL_FAILED        = "Failed"
        EMAIL_CONN_ERROR    = "Connection Error"
        SESSION_EXPIRED     = "Your session has expired. Please log in again."
        SIGN_IN_REQUIRED    = "Please sign in to sync files to the cloud."
        MFA_SESSION_EXPIRED = "MFA Session expired"
        LOGIN_SUCCESSFUL    = "Login Successful"
        INTERNAL_ERROR      = "Internal Error: {}"   # .format(exc)
        WORKER_ERROR        = "Auth Error: {}"        # .format(exc)
        EMAIL_PASSWORD_REQUIRED = "Email and password are required."
        VERIFYING_CREDENTIALS   = "Verifying credentials…"
        VERIFYING_CODE          = "Verifying code…"
        MFA_CODE_INVALID        = "Code must be exactly 6 digits."
        RESET_EMAIL_SENT        = "Password reset email sent"
        RESET_FAILED            = "Failed to send reset email"
        SENDING_RESET_EMAIL     = "Sending reset email…"
        EMAIL_REQUIRED          = "Please enter your email address."
        MFA_ENROLLMENT_REQUIRED = "mfa_enrollment_required"
        MFA_ENROLLED            = "Authenticator app connected"
        MFA_ENROLL_FAILED       = "Invalid code — check your authenticator app and try again"
        EMAIL_NOT_VERIFIED          = "email_not_verified"
        SENDING_VERIFICATION_EMAIL  = "Sending verification email…"
        CHECKING_EMAIL_VERIFICATION = "Checking email status…"
        EMAIL_STILL_UNVERIFIED      = "Email not yet verified. Please check your inbox and try again."

    class USB:
        NO_DEVICE           = "No XALIVA device connected to USB Serial Port"
        PORT_NOT_OPEN       = "Serial port is not open. Please try connecting again"
        NO_LOG_FILES        = "No log files received from device"
        DEVICE_LOG_UNREADABLE = "Device log could not be read"
        LOG_UNREADABLE      = "Log could not be read"
        FILE_NOT_FOUND      = "File not found"
        MEMORY_REFLASH      = "Memory re-flash detected, please manually archive existing logs before saving current logs."
        SAVE_STOPPED        = "File Save force-stopped"
        SERIAL_DISCONNECTED = "USB connection lost: {}"
        ALL_FILES_SAVED     = "All files Saved"
        FILES_SAVED_COUNT   = "{} log(s) saved to disk"  # new: n filled by bridge
        FILE_SAVED          = "File saved to disk"
        NO_OPTICAL_VALUES   = "File {} has no optical values to plot"
        UNEXPECTED_ERROR    = "An unexpcted error occured:\n{}"  # .format(exc)
        SERVER_UNREACHABLE  = "Cannot reach server"

    class Cloud:
        NO_DEVICE_ID        = "No device ID entered"
        FETCH_FAILED        = "Failed to fetch cloud logs"
        FETCH_FILES_FAILED  = "Failed to fetch files"
        NO_FILES_TO_SYNC    = "No files to sync"
        NO_MAC_ID           = "Could not determine device ID from file names"
        ACCESS_DENIED       = "Access denied: you don't have permission for this device"
        ACCESS_DENIED_WRITE = "Access denied: you don't have write permission for this device"
        DEVICE_NOT_REGISTERED = "Device not registered in the system"
        FILE_NOT_FOUND        = "File not found in cloud storage"
        GATEWAY_ACCESS_DENIED = "Access denied by gateway"
        UPLOAD_STOPPED      = "File Upload to Cloud force-stopped"
        ALL_FILES_UPLOADED  = "{} files uploaded"

    class Export:
        NO_PLOT_DATA     = "No plot data to export."
        EXPORTED         = "Exported to {}"            # .format(dest)
        EXPORT_FAILED    = "Export failed: {}"          # .format(exc)
        SCREENSHOT_SAVED = "Screenshot saved to {}"    # .format(dest)