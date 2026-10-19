# src/mvc/bridge.py
#
# Bridge(QObject) — the single Python↔QML communication layer.
#
#   QML → Python : signals declared below (QML calls bridge.signalName(...))
#   Python → QML : methods that call JS functions on self._root
#
# Usage (main.py):
#   bridge = Bridge()
#   engine.setInitialProperties({"bridge": bridge})
#   engine.load(qml_path)
#   bridge.set_root(engine.rootObjects()[0])

from __future__ import annotations
import logging
from typing import Optional

import shiboken6
from PySide6.QtCore import QObject, Signal, Slot, Property
from PySide6.QtCharts import QXYSeries

from ..constants import ACTION, PROGRESS_MSG, MSG, GATEWAY_STATUS

logger = logging.getLogger(__name__)

# Action routing: which actions belong to which tab's overlay
_USB_ACTIONS: frozenset = frozenset([
    ACTION.LIST_USB_FILES,
    ACTION.SAVE_ALL_USB_FILES_TO_DISK,
    ACTION.SAVE_ONE_USB_FILE_TO_DISK,
    ACTION.UPLOAD_USB_FILES_TO_CLOUD,
    ACTION.PARSE_USB_DEVICE_LOG,
    ACTION.ANALYZE_USB_LOG,
    ACTION.GATEWAY_ACL_PREFLIGHT,  # Part of USB sync flow
])

_CLOUD_ACTIONS: frozenset = frozenset([
    ACTION.GATEWAY_LIST_FILES,
    ACTION.GATEWAY_READ_FILE,
    ACTION.DOWNLOAD_CLOUD_FILES_TO_DISK,
    ACTION.ANALYZE_CLOUD_LOG,
])


class Bridge(QObject):
    """Pure QObject bridge between QML and the Python backend.

    Signals  : fired by QML interactions, wired to model slots by controller.
    Methods  : called by Python to push state updates into QML via root JS functions.
    Slots    : decorated with @Slot, wired from model signals by controller.
    """

    # ------------------------------------------------------------------
    # 2.1 / 2.2  Internal state
    # ------------------------------------------------------------------

    def __init__(self, parent: Optional[QObject] = None) -> None:
        super().__init__(parent)
        self._root = None               # set after engine.rootObjects()[0] is available
        self._safe_to_close: bool = False

        # Cached plot_spec for CSV export (set by on_plot_data on success, cleared on start)
        self._last_plot_spec: dict = {}

        # Pending QPointF lists waiting for chartSeriesReady callback
        self._pending_pts: list = []

        # Per-tab busy state tracking
        self._usb_busy: bool = False
        self._cloud_busy: bool = False

    def set_root(self, root) -> None:
        """Call once after the QML engine has loaded MainWindow."""
        self._root = root

    # ------------------------------------------------------------------
    # 2.3  safeToClose property (read by QML onClosing guard)
    # ------------------------------------------------------------------

    def _get_safe_to_close(self) -> bool:
        return self._safe_to_close

    safeToClose = Property(bool, _get_safe_to_close, constant=False)

    # ------------------------------------------------------------------
    # 2.4  USB signals (QML → Python)
    # ------------------------------------------------------------------

    detectClicked     = Signal()
    connectClicked    = Signal(str)   # port: str
    disconnectClicked = Signal()
    listUsbFilesClicked   = Signal()
    saveAllUsbToDiskClicked = Signal(list)  # files: list
    saveOneUsbFileToDiskClicked = Signal(str)  # filename: str (single file save with dialog)
    syncUsbToCloudClicked = Signal(list) # files: list (all usbList.model)

    # ------------------------------------------------------------------
    # 2.5  Cloud signals
    # ------------------------------------------------------------------

    listCloudFilesClicked    = Signal(str)   # device_id: str
    downloadCloudFilesToDiskClicked  = Signal(list)  # files: list (selectedFiles)

    # ------------------------------------------------------------------
    # 2.6  PC signals
    # ------------------------------------------------------------------

    browsePcClicked = Signal(str)  # current_path: str

    # ------------------------------------------------------------------
    # 2.7  Analysis signals
    # ------------------------------------------------------------------

    analyzeClicked    = Signal(int, str)  # tab_id: int, file: str
    exportCsvClicked  = Signal()
    screenshotClicked = Signal()
    chartSeriesReady  = Signal(list)       # QML → Python: list of QLineSeries refs

    # Analysis lifecycle signals (listened to by QML Connections in MainWindowLogic)
    analysisStarted   = Signal()
    analysisEnded     = Signal()

    # Per-tab busy state signals
    usbBusyChanged   = Signal(bool, str)   # (is_busy, message)
    cloudBusyChanged = Signal(bool, str)   # (is_busy, message)

    # ------------------------------------------------------------------
    # 2.8  Auth signals
    # ------------------------------------------------------------------

    loginClicked        = Signal(str, str, bool)  # email, password, remember
    mfaVerifyClicked    = Signal(str)              # code
    signOutClicked      = Signal()
    googleSSOClicked    = Signal()
    microsoftSSOClicked = Signal()
    forgotPasswordClicked = Signal()
    forgotPasswordSubmitClicked = Signal(str)      # email
    verifyEmailDoneClicked   = Signal()            # "I've verified my email" button
    verifyEmailResendClicked = Signal()            # "Resend verification email" link

    # ------------------------------------------------------------------
    # 2.9  Window signal
    # ------------------------------------------------------------------

    closeRequested = Signal()

    # ------------------------------------------------------------------
    # 2.10  Per-tab busy state methods
    # ------------------------------------------------------------------

    def set_usb_busy(self, busy: bool, msg: str = "") -> None:
        """Emit usbBusyChanged signal with busy state and message."""
        self._usb_busy = busy
        self.usbBusyChanged.emit(busy, msg)

    def set_cloud_busy(self, busy: bool, msg: str = "") -> None:
        """Emit cloudBusyChanged signal with busy state and message."""
        self._cloud_busy = busy
        self.cloudBusyChanged.emit(busy, msg)

    def _clear_usb_busy(self) -> None:
        """Clear USB busy state."""
        self.set_usb_busy(False, "")

    def _clear_cloud_busy(self) -> None:
        """Clear Cloud busy state."""
        self.set_cloud_busy(False, "")

    def _clear_session_state(self) -> None:
        """Reset all transient UI state when a session ends or expires.

        Called before showing the login overlay so no spinners or stale
        busy indicators remain visible while the user re-authenticates.
        Uses existing per-subsystem clear methods rather than duplicating logic.
        """
        self.on_connection_status(False, "")  # USB visual disconnect + clears USB busy + status bar
        self._clear_cloud_busy()

    # ------------------------------------------------------------------
    # 2.11  Python → QML methods
    # ------------------------------------------------------------------

    def _call(self, js_func: str, *args):
        """Safely invoke a JS function on the QML root object."""
        if not self._root:
            return
        fn = getattr(self._root, js_func, None)
        if fn is None:
            logger.warning("QML root has no function: %s", js_func)
            return
        try:
            fn(*args)
        except Exception:
            logger.exception("Error calling QML %s", js_func)

    def set_status(self, msg: str, busy: bool = False) -> None:
        self._call("setStatus", msg, busy)

    def set_user(self, email: str, name: str) -> None:
        self._call("setUserEmail", email)
        self._call("setUserName", name)

    def clear_user(self) -> None:
        self._call("setUserEmail", "")

    def set_usb_ports(self, ports: list) -> None:
        self._call("setUsbPorts", ports)

    def set_usb_connected(self, ok: bool, device_id: str) -> None:
        self._call("setUsbConnected", ok, device_id)

    def set_usb_logs(self, logs: list) -> None:
        self._call("setUsbLogs", logs)

    def set_cloud_logs(self, logs: list) -> None:
        self._call("setCloudLogs", logs)

    def set_local_path(self, path: str) -> None:
        self._call("setLocalPath", path)

    def set_local_files(self, files: list) -> None:
        self._call("setLocalFiles", files)

    def set_device_info(self, pairs: list) -> None:
        self._call("setDeviceInfo", pairs)

    def set_log_info(self, text: str) -> None:
        self._call("setLogInfo", text)

    def set_plot_visible(self, v: bool) -> None:
        self._call("setPlotVisible", v)

    def set_recent_files(self, files: list) -> None:
        self._call("setRecentFiles", files)

    def show_login(self) -> None:
        self._call("showLoginOverlay")

    def hide_login(self) -> None:
        self._call("hideLoginOverlay")

    def login_error(self, msg: str) -> None:
        self._call("loginShowError", msg)

    def login_show_mfa(self, url: str) -> None:
        self._call("loginShowMFA", url)

    def login_show_verify_email(self) -> None:
        self._call("loginShowVerifyEmail")

    def show_success(self, msg: str) -> None:
        self._call("showSuccess", msg)

    def show_warning(self, msg: str) -> None:
        self._call("showWarning", msg)

    def show_info(self, msg: str) -> None:
        self._call("showInfo", msg)

    def show_error(self, msg: str) -> None:
        self._call("showError", msg)

    def show_login_expired(self, msg: str) -> None:
        """Called when the gateway returns 401 — clear the UI and show the login overlay."""
        self._clear_session_state()
        self.clear_user()
        self.login_error(msg)
        self.show_login()

    def show_forgot_password_success(self, email: str) -> None:
        self._call("showForgotPasswordSuccess", email)

    # ------------------------------------------------------------------
    # 2.11–2.14  Bridge @Slot handlers (wired from model signals by controller)
    # ------------------------------------------------------------------

    @Slot(bool, str, list)
    def on_ports_detected(self, success: bool, msg: str, ports: list) -> None:
        if not success:
            self.show_warning(MSG.Status.PORT_DETECT_FAILED.format(msg))
            return
        self.set_usb_ports(ports)
        self.show_success(MSG.Status.PORTS_DETECTED)

    @Slot(bool, str)
    def on_connection_status(self, success: bool, device_id: str) -> None:
        self.set_usb_connected(success, device_id)
        if success:
            self.set_status(MSG.Status.CONNECTED.format(device_id), busy=False)
        else:
            self.set_status(MSG.Status.DISCONNECTED, busy=False)
            if device_id:
                self.show_warning(device_id)   # device_id carries the error msg when success=False
            self._clear_usb_busy()         # dismiss any in-progress busy overlay

    @Slot(bool, str, list)
    def on_usb_file_names_listed(self, success: bool, msg: str, logs: list) -> None:
        if not success:
            self.show_warning(MSG.Status.FETCH_FAILED.format(msg))
            self._clear_usb_busy()
            return
        self.set_usb_logs(logs)
        self.show_success(MSG.Status.FETCHED_USB_LOGS.format(len(logs)))
        self._clear_usb_busy()

    @Slot(bool, list)
    def on_usb_device_log_parsed(self, success: bool, items: list) -> None:
        if not success:
            return
        self.set_device_info(items)

    @Slot(bool, str, dict)
    def on_usb_files_saved_to_disk(self, success: bool, msg: str, payload: dict) -> None:
        if success:
            n = payload.get("total") if payload else None
            msg = MSG.USB.FILES_SAVED_COUNT.format(n) if n is not None else msg
            self.show_success(msg)
        else:
            self.show_warning(MSG.Status.SAVE_FAILED.format(msg))
        self._clear_usb_busy()

    @Slot(bool, str)
    def on_usb_files_synced_to_cloud(self, success: bool, msg: str) -> None:
        if success:
            self.show_success(msg)
        else:
            self.show_warning(MSG.Status.SYNC_FAILED.format(msg))
        self._clear_usb_busy()

    @Slot(bool, str, list)
    def on_cloud_file_names_listed(self, success: bool, msg: str, logs: list) -> None:
        self.set_cloud_logs(logs)
        if success:
            self.show_success(MSG.Status.FETCHED_CLOUD_LOGS.format(len(logs)))
        else:
            self.show_warning(msg)
        self._clear_cloud_busy()

    @Slot(bool, str, dict)
    def on_cloud_files_downloaded_to_disk(self, success: bool, msg: str, src: dict) -> None:
        if success:
            self.show_success(msg)
        else:
            self.show_warning(MSG.Status.DOWNLOAD_FAILED.format(msg))
        self._clear_cloud_busy()

    @Slot(int, int, int, int)
    def on_progress(self, action_id: int, current: int, total: int, phase: int = 0) -> None:
        # 2.15: translate ACTION enum → human-readable status string
        templates = PROGRESS_MSG.get(action_id, ["Processing {cur}/{tot}…"])
        tmpl = templates[min(phase, len(templates) - 1)]
        status_msg = tmpl.format(cur=current, tot=total)
        self.set_status(status_msg, busy=True)

        # Route progress to per-tab busy state
        if action_id in _USB_ACTIONS:
            self.set_usb_busy(True, status_msg)
        elif action_id in _CLOUD_ACTIONS:
            self.set_cloud_busy(True, status_msg)

    @Slot()
    def on_all_workers_done(self) -> None:
        # 2.14: allow the window close that was deferred by onClosing handler
        self._safe_to_close = True
        if self._root:
            self._root.close()

    @Slot(bool, str, dict)
    def on_analysis_completed(self, success: bool, msg: str, payload: dict) -> None:
        # 2.12: stash pts and spec (already enriched by worker), push config to QML.
        # Data injection happens in on_chart_series_ready after QML emits series refs.
        self._pending_pts = []
        self._last_plot_spec = {}

        if not success:
            if msg == GATEWAY_STATUS.FILE_NOT_FOUND:
                display = MSG.Cloud.FILE_NOT_FOUND
            elif msg == GATEWAY_STATUS.FORBIDDEN:
                display = MSG.Cloud.ACCESS_DENIED
            else:
                display = msg or MSG.Status.CANNOT_PLOT.format(payload.get('log_file_name', ''))
            self.show_warning(display)
            self.analysisEnded.emit()
            return

        plot_spec = payload.get("plot_spec", {})
        self._last_plot_spec = plot_spec
        self._pending_pts = payload.get("pts_per_series", [])

        if self._root and plot_spec:
            self._root.setPlotConfig(plot_spec)
        # Status is set in on_chart_series_ready after data is injected

    @Slot(list)
    def on_chart_series_ready(self, series_list: list) -> None:
        # Phase 2: QML passes series refs as QML JS wrappers. Cast each one to the raw
        # C++ QXYSeries via shiboken6 so we can call replace(list[QPointF]), which
        # atomically clears the previous log's data and draws the new one in one repaint.
        pts = self._pending_pts
        self._pending_pts = []
        for i, wrapper in enumerate(series_list):
            if i >= len(pts):
                break
            try:
                ptr = shiboken6.getCppPointer(wrapper)[0]
                series = shiboken6.wrapInstance(ptr, QXYSeries)
                series.replace(pts[i])
            except Exception:
                logger.exception("on_chart_series_ready: replace failed for series_%d", i)
        self.set_status(MSG.Status.ANALYSIS_COMPLETE, busy=False)
        self.analysisEnded.emit()


    @Slot(bool, str, dict)
    def on_auth_finished(self, success: bool, msg: str, payload: dict) -> None:
        # Delegated to login_controller via controller wiring.
        # This slot is a relay — controller connects model.authFinished to
        # login_controller.on_auth_finished directly (see controller.py).
        pass

    @Slot()
    def on_email_verification_required(self) -> None:
        self.set_status("", busy=False)
        self.login_show_verify_email()
