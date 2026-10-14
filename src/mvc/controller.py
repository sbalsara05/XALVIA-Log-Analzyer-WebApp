# src/mvc/controller.py
#
# ApplicationController — pure signal wiring + thin OS-dialog adapters.
# No business logic lives here. All auth logic is in LoginController.

from __future__ import annotations
import csv
import logging
import re
from pathlib import Path
from typing import TYPE_CHECKING

from PySide6.QtWidgets import QFileDialog

from ..constants import TAB, LOG_SRC, MSG

if TYPE_CHECKING:
    from .bridge import Bridge
    from .model import Model
    from ..login.login_controller import LoginController

logger = logging.getLogger(__name__)


class ApplicationController:
    """Wires bridge signals ↔ model slots and login_controller slots.

    All entries here follow the pattern:
        source.signal.connect(destination.slot)

    Adapter functions (_handle_*) are the only methods that contain code;
    they exist solely because QML cannot open native OS dialogs.
    """

    def __init__(
        self,
        bridge: "Bridge",
        model: "Model",
        login_controller: "LoginController",
    ) -> None:
        self._bridge = bridge
        self._model = model
        self._login_ctrl = login_controller
        self._pc_folder: str = ""   # 4.6: tracks current PC tab directory

        self._wire_qml_to_model()   # 4.2
        self._wire_auth_signals()   # 4.3
        self._wire_model_to_bridge()# 4.4 + 4.5

    # ------------------------------------------------------------------
    # 4.2  QML→Python: bridge signals → model slots
    # ------------------------------------------------------------------

    def _wire_qml_to_model(self) -> None:
        b, m = self._bridge, self._model

        b.chartSeriesReady.connect(b.on_chart_series_ready)

        b.detectClicked.connect(m.detect_ports)
        b.connectClicked.connect(m.connect_to_port)
        b.disconnectClicked.connect(m.close_port)
        b.listUsbFilesClicked.connect(m.list_usb_files)
        b.saveAllUsbToDiskClicked.connect(m.save_all_usb_files_to_disk)
        b.saveOneUsbFileToDiskClicked.connect(self._handle_save_one_usb_file_to_disk)
        b.syncUsbToCloudClicked.connect(m.sync_usb_files_to_cloud)
        b.listCloudFilesClicked.connect(m.list_cloud_files)

        # Signals that need adapter translation
        b.analyzeClicked.connect(self._translate_and_analyze)       # 4.7
        b.browsePcClicked.connect(self._handle_browse_pc)           # 4.8
        b.downloadCloudFilesToDiskClicked.connect(self._handle_download_cloud_files_to_disk)   # 4.9
        b.exportCsvClicked.connect(self._handle_export_csv)         # 4.10
        b.screenshotClicked.connect(self._handle_screenshot_clicked)

        b.closeRequested.connect(m.cancel_workers)

    # ------------------------------------------------------------------
    # 4.3  Auth signals → login_controller
    # ------------------------------------------------------------------

    def _wire_auth_signals(self) -> None:
        b, lc = self._bridge, self._login_ctrl

        b.loginClicked.connect(lc.handle_login)
        b.mfaVerifyClicked.connect(lc.handle_mfa_verify)
        b.signOutClicked.connect(lc.handle_signout)
        b.forgotPasswordClicked.connect(lc.handle_forgot_password_nav)
        b.forgotPasswordSubmitClicked.connect(lc.handle_forgot_password_submit)
        b.verifyEmailDoneClicked.connect(lc.handle_verify_email_done)
        b.verifyEmailResendClicked.connect(lc.handle_resend_verification_email)

    # ------------------------------------------------------------------
    # 4.4 + 4.5  Model signals → bridge slots (and login_controller)
    # ------------------------------------------------------------------

    def _wire_model_to_bridge(self) -> None:
        m, b, lc = self._model, self._bridge, self._login_ctrl

        m.portsDetected.connect(b.on_ports_detected)
        m.connectionStatusChanged.connect(b.on_connection_status)
        m.usbFileNamesListed.connect(b.on_usb_file_names_listed)
        m.usbDeviceLogParsed.connect(b.on_usb_device_log_parsed)
        m.usbFilesSavedToDisk.connect(b.on_usb_files_saved_to_disk)
        m.usbFilesSyncedToCloud.connect(b.on_usb_files_synced_to_cloud)
        m.cloudFileNamesListed.connect(b.on_cloud_file_names_listed)
        m.progress.connect(b.on_progress)
        m.allWorkersDone.connect(b.on_all_workers_done)
        m.analysisCompleted.connect(b.on_analysis_completed)
        m.cloudFilesDownloadedToDisk.connect(b.on_cloud_files_downloaded_to_disk)

        # Auth signal goes to login_controller (4.5)
        m.authFinished.connect(lc.on_auth_finished)
        m.mfaQrReady.connect(lc.on_mfa_qr_ready)
        m.emailVerificationRequired.connect(b.on_email_verification_required)

        # Gateway 401 → show login overlay with expiry message
        m.authRequired.connect(b.show_login_expired)

    # ------------------------------------------------------------------
    # 4.7  _translate_and_analyze
    # ------------------------------------------------------------------

    def _translate_and_analyze(self, tab_id: int, filename: str) -> None:
        logger.debug("analyzeClicked received — tab_id=%s filename=%r", tab_id, filename)
        self._bridge.analysisStarted.emit()
        tab = TAB(tab_id)
        if tab == TAB.PC:
            full_path = str(Path(self._pc_folder) / filename)
            logger.debug("PC analyze: pc_folder=%r → full_path=%r", self._pc_folder, full_path)
            self._model.analyze_log(tab, full_path)
        else:
            logger.debug("Non-PC analyze: tab=%r filename=%r", tab, filename)
            self._model.analyze_log(tab, filename)

    # ------------------------------------------------------------------
    # 4.8a  _handle_save_one_usb_file
    # ------------------------------------------------------------------

    def _handle_save_one_usb_file_to_disk(self, filename: str) -> None:
        folder = QFileDialog.getExistingDirectory(
            None, "Save File To", str(Path.home())
        )
        if not folder:
            self._bridge.set_usb_busy(False, "")
            return
        self._model.save_one_usb_file_to_disk(filename, folder)

    # ------------------------------------------------------------------
    # 4.8  _handle_browse_pc
    # ------------------------------------------------------------------

    def _handle_browse_pc(self, current_path: str) -> None:
        folder = QFileDialog.getExistingDirectory(
            None, "Select Directory", current_path or str(Path.home())
        )
        if not folder:
            return
        self._pc_folder = folder
        self._bridge.set_local_path(folder)
        files = sorted(
            f.name for f in Path(folder).iterdir()
            if f.is_file() and f.suffix.lower() == ".txt"
        )
        self._bridge.set_local_files(files)

    # ------------------------------------------------------------------
    # 4.9  _handle_download_cloud_files_to_disk
    # ------------------------------------------------------------------

    _MAC_RE = re.compile(r"^([a-zA-Z0-9]+)_\d+\.txt$")

    def _handle_download_cloud_files_to_disk(self, files: list) -> None:
        if not files:
            self._bridge.set_cloud_busy(False, "")
            return

        if len(files) == 1:
            dest, _ = QFileDialog.getSaveFileName(
                None, "Save Log File", files[0], "Text Files (*.txt)"
            )
            if not dest:
                logger.debug("User canceled save dialog")
                self._bridge.set_cloud_busy(False, "")
                return
            self._model.download_cloud_files_to_disk(LOG_SRC.CLOUD, files, dest)
        else:
            # Multiple files → suggest a .zip name derived from MAC ID
            m = self._MAC_RE.match(files[0])
            default_name = f"{m.group(1)}.zip" if m else "logs.zip"
            dest, _ = QFileDialog.getSaveFileName(
                None, "Save Logs as ZIP", default_name, "ZIP Archives (*.zip)"
            )
            if not dest:
                logger.debug("User canceled save dialog")
                self._bridge.set_cloud_busy(False, "")
                return
            self._model.download_cloud_files_to_disk(LOG_SRC.CLOUD, files, dest)

    # ------------------------------------------------------------------
    # 4.10  _handle_export_csv
    # ------------------------------------------------------------------

    def _handle_export_csv(self) -> None:
        plot_spec = self._bridge._last_plot_spec
        series_list = plot_spec.get("series", []) if plot_spec else []
        x_values = plot_spec.get("x_values") or [] if plot_spec else []
        all_y = [s.get("data", []) for s in series_list]
        ref_y = all_y[0] if all_y else []
        x = x_values if x_values else list(range(len(ref_y)))
        if not x or not all_y:
            self._bridge.show_warning(MSG.Export.NO_PLOT_DATA)
            return

        folder = QFileDialog.getExistingDirectory(None, "Export CSV to Folder")
        if not folder:
            return

        base = plot_spec.get("_file_name", "xaliva_export")
        if base.endswith(".txt"):
            base = base[:-4]
        dest = Path(folder) / (base + ".csv")
        try:
            labels = [s.get("label", f"y{i}") for i, s in enumerate(series_list)]
            with open(dest, "w", newline="") as f:
                writer = csv.writer(f)
                writer.writerow(["x", *labels])
                for i, xi in enumerate(x):
                    row = [xi] + [y[i] if i < len(y) else "" for y in all_y]
                    writer.writerow(row)
            self._bridge.show_success(MSG.Export.EXPORTED.format(dest))
        except OSError as e:
            self._bridge.show_warning(MSG.Export.EXPORT_FAILED.format(e))

    def _handle_screenshot_clicked(self) -> None:
        plot_spec = self._bridge._last_plot_spec
        if not plot_spec:
            self._bridge.show_warning(MSG.Export.NO_PLOT_DATA)
            return

        suggested_name = plot_spec.get("_file_name", "screenshot")
        if suggested_name.endswith(".txt"):
            suggested_name = suggested_name[:-4]
        suggested_name += ".png"

        dest, _ = QFileDialog.getSaveFileName(
            None, "Save Screenshot", suggested_name, "Images (*.png *.jpg)"
        )
        if dest:
            # We call the QML forwarder to trigger grabToImage in the active chart
            self._bridge._call("saveScreenshot", dest)
            self._bridge.show_success(MSG.Export.SCREENSHOT_SAVED.format(dest))
