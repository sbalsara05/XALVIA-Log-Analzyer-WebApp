# src/model.py
from pathlib import Path
import logging, os
from typing import List, Tuple
from serial import Serial, SerialException, SerialTimeoutException
from PySide6.QtCore import QObject, QThreadPool, Signal, Slot, QRunnable


from ..workflows.workers import *
from ..usb_conn.usbserialconnection import USBSerialConnection
from ..cloud_conn.gateway_client import GatewayClient
from ..constants import *
from ..disk_cache.disk_cache import DiskCache, CacheKey
from ..utils import safe_del_dir

import shutil

logger = logging.getLogger(__name__)


class SharedWorkerSignals(QObject):
    """
    Generic signals container for QRunnable workers.
    This creates Signal descriptors- progress and finished. Each thread gets its independent Signal instances when they access the descriptors
    """

    progress: Signal = Signal(int, int, int, int)  # (worker_id, current, total, phase)
    finished: Signal = Signal(int, bool, str, dict)  # worker id, success, msg, payload

    def __init__(self, parent=None):
        super().__init__(parent)


class Model(QObject):
    # Signals for anyone interested in model events
    portsDetected = Signal(bool, str, list)  # emits List[str]
    connectionStatusChanged = Signal(bool, str)  # emits (success, device id/name)
    usbFileNamesListed = Signal(bool, str, list)  # emits (success, message, List[str])
    usbDeviceLogParsed = Signal(bool, list)
    usbFilesSyncedToCloud = Signal(bool, str)
    cloudFileNamesListed = Signal(bool, str, list)  # emits (success, message, List[str])
    analysisCompleted = Signal(bool, str, dict)  # emits (success, message, payload)
    usbFilesSavedToDisk = Signal(bool, str, dict)  # emits (success, message, payload)
    progress = Signal(int, int, int, int)  # emits (action_id, current, total, phase)
    cloudFilesDownloadedToDisk = Signal(bool, str, int)  # emits (success, message, payload)
    authFinished = Signal(bool, str, dict)
    mfaQrReady = Signal(str, str) # Emits (secret_key, email)
    authRequired = Signal(str)         # emitted when gateway returns 401
    emailVerificationRequired = Signal()  # emitted when email is not yet verified post-login
    allWorkersDone = Signal()

    def __init__(
        self,
        LOGS_DIR: Path,
        TEMP_DIR: Path,
        auth_manager,
        parent=None,
        baudrate: int = 115_200,
        timeout: float = 1.0,
    ):
        super().__init__(parent)
        self.LOGS_DIR = LOGS_DIR
        self.TEMP_DIR = TEMP_DIR
        self.auth_manager = auth_manager
        self.serial_port = None
        self.baudrate: int = baudrate
        self.timeout: float = timeout

        self._next_worker_id = 0
        self._signals = SharedWorkerSignals(self)
        self._pool = QThreadPool.globalInstance()
        self._active_workers = dict()  ## set of active workers
        self._shutdown_in_progress = False

        self._usb_serial_connection = USBSerialConnection(parent=self)
        self._gateway_client = GatewayClient(auth_manager=self.auth_manager)
        self._current_cloud_mac_id: str = ""
        self._auth_required_emitted: bool = False

        self._temp_mfa_session = None # Model stores state temporarily

        self._signals.finished.connect(self._on_worker_finished)  # central slot
        self._signals.progress.connect(self._on_progress_received)

        # map each Action to the handler that needs it
        self._handlers = {
            ACTION.DETECT_USB_SERIAL_PORTS: self._ports_detected_from_thread,
            ACTION.CONNECT_USB_SERIAL_PORT: self._connected_to_usb_port,
            ACTION.DISCONNECT_USB_SERIAL_PORT: self._closed_port,
            ACTION.LIST_USB_FILES: self._on_usb_file_names_listed,
            ACTION.SAVE_ALL_USB_FILES_TO_DISK: self._on_usb_files_saved_to_disk,
            ACTION.SAVE_ONE_USB_FILE_TO_DISK: self._on_one_usb_file_saved_to_disk,
            ACTION.UPLOAD_USB_FILES_TO_CLOUD: self._on_usb_files_synced_to_cloud,
            ACTION.LIST_CLOUD_FILES: self._on_cloud_file_names_listed,
            ACTION.ANALYZE_USB_LOG: self._on_analysis_completed,
            ACTION.PARSE_USB_DEVICE_LOG: self._on_usb_device_log_parsed,
            ACTION.ANALYZE_CLOUD_LOG: self._on_analysis_completed,
            ACTION.ANALYZE_DISK_LOG: self._on_analysis_completed,
            ACTION.DOWNLOAD_CLOUD_FILES_TO_DISK: self._on_cloud_files_downloaded_to_disk,
            ACTION.GATEWAY_LIST_FILES:  self._on_gateway_file_names_listed,
            ACTION.GATEWAY_READ_FILE:   self._on_gateway_download_done,
            ACTION.GATEWAY_ACL_PREFLIGHT: self._on_acl_preflight_done,
            ACTION.AUTHENTICATE: self._on_auth_done,
            ACTION.FORGOT_PASSWORD: self._on_forgot_password_done,
            ACTION.MFA_ACTION: self._on_mfa_worker_done,
            ACTION.RECHECK_EMAIL_VERIFICATION: self._on_recheck_email_done,
        }

        self._disk_cache = DiskCache(app_subdir="XALIVA_Cache")

    def _delete_temp_dirs(self, parent_dir: Path):
        """
        Recursively finds and deletes all subdirectories containing "temp" in their name,
        starting from the deepest level first.
        """
        if not parent_dir.is_dir():
            logger.debug(f"Error: '{parent_dir}' is not a valid directory.")
            return

        # os.walk with topdown=False iterates from the deepest level upwards
        for dir_path, _, _ in os.walk(parent_dir, topdown=False):
            current_dir = Path(dir_path)

            # Check if the directory name contains "temp"
            if "temp" in current_dir.name.lower():
                logger.debug(f"Deleting directory: {current_dir}")
                safe_del_dir(current_dir)

    def _register_worker(self, action: constants.ACTION, runnable: BaseThreadWorker)->None:
        self._next_worker_id += 1
        runnable._worker_id = self._next_worker_id
        self._active_workers[self._next_worker_id] = (action, runnable)
    
    def cleanup(self) -> None:
        """ "
        Closes ports (if open) and deletes temp files in case there is a crash
        """
        self._usb_serial_connection.close_port()
        self._delete_temp_dirs(parent_dir=self.TEMP_DIR)
        self._disk_cache.clear()

    @Slot(int, bool, str, dict)
    def _on_worker_finished(
        self, worker_id: int, success: bool, msg: str, payload: dict
    ) -> None:
        logger.debug(
            f"Received finish sinal from worker id {worker_id}  and succes is {success} and msg is {msg}")
        try:
            worker = self._active_workers.pop(worker_id)
            action = worker[0]
            logger.debug(f"Action is {action}")
        except (KeyError, IndexError, TypeError) as exc:
            logger.debug(f"Exception: {exc}")
            return
        logger.debug("Checking all threads done")
        if self._is_all_threads_done():
            self._all_workers_done()
        logger.debug("Checked all threads done.Now getinghandler function")
        handler = self._handlers.get(action)
        if handler is not None:
            logger.debug(f"Calling handler {handler}")
            handler(success, msg, payload)

    @Slot(int, int, int, int)
    def _on_progress_received(self, worker_id: int, current: int, total: int, phase: int = 0) -> None:
        logger.debug(
            f"Sending progress signal for worker id {worker_id} and action id {self._active_workers[worker_id][0]}"
        )

        entry = self._active_workers.get(worker_id)
        if not entry:
            # late progress; ignore
            return
        self.progress.emit(entry[0], current, total, phase)

    @Slot()
    def cancel_workers(self) -> None:
        """
        Starts shutting down threads in the thread pool even if they are mid-flight
        """
        self._shutdown_in_progress = True
        self._usb_serial_connection.cancel_io()
        for worker_id, action_runnable in self._active_workers.items():
            if action_runnable and hasattr(action_runnable[1], "force_cancel"):
                logger.debug(f"Calling cancel of worker id {worker_id}")
                action_runnable[1].force_cancel()

        # If there were no workers, emit immediately
        if not self._active_workers:
            self._all_workers_done()

    @Slot()
    def detect_ports(self) -> None:
        """Probe serial ports and emit portsDetected."""
        runnable = DetectUSBSerialWorker(
            worker_id=None,
            signals=self._signals,
            usb_serial_connection=self._usb_serial_connection,
        )
        self._register_worker(
            action=ACTION.DETECT_USB_SERIAL_PORTS,
            runnable=runnable
        )
        self._pool.start(runnable)

    def _ports_detected_from_thread(
        self, success: bool, msg: str, payload: dict | None
    ) -> None:
        if success:
            self.portsDetected.emit(success, msg, payload["filtered_ports"])
        else:
            self.portsDetected.emit(success, msg, [])

    @Slot(str)
    def connect_to_port(self, port_device: str) -> None:
        
        runnable = ConnectUSBSerialWorker(
            worker_id=None,
            signals=self._signals,
            usb_serial_connection=self._usb_serial_connection,
            port_device=port_device,
            baudrate=self.baudrate,
            timeout=self.timeout,
        )

        self._register_worker(
            action=ACTION.CONNECT_USB_SERIAL_PORT,
            runnable=runnable
        )
        self._pool.start(runnable)

    def _connected_to_usb_port(
        self, success: bool, msg: str, payload: dict | None
    ) -> None:
        self.connectionStatusChanged.emit(success, payload["port_device"])

    @Slot()
    def close_port(self) -> None:
        
        runnable = DisconnectUSBSerialWorker(
            worker_id=None,
            signals=self._signals,
            usb_serial_connection=self._usb_serial_connection,
        )

        self._register_worker(
            action=ACTION.DISCONNECT_USB_SERIAL_PORT,
            runnable=runnable
        )
        self._pool.start(runnable)

    def _closed_port(self, success: bool, msg: str, payload: dict | None) -> None:
        self.connectionStatusChanged.emit(not success, msg)

    @Slot()
    def list_usb_files(self) -> None:
        """List log files over serial and emit usbFileNamesListed."""

        list_runnable = ListUSBSerialWorker(
            worker_id=None,
            signals=self._signals,
            usb_serial_connection=self._usb_serial_connection,
        )
        self._register_worker(
            action=ACTION.LIST_USB_FILES,
            runnable=list_runnable
        )
        self._pool.start(list_runnable)

    def _on_usb_file_names_listed(
        self, success: bool, msg: str, payload: dict | None
    ) -> None:
        # self._active_workers.discard(runnable)
        if success:
            self.usbFileNamesListed.emit(True, "", payload["logfilenames"])
            self._parse_usb_device_log(
                device_log_file_name=payload["device_log_file_name"]
            )
        else:
            self.usbFileNamesListed.emit(False, msg, [])

    def _parse_usb_device_log(self, device_log_file_name: str) -> list:
        mac_id = device_log_file_name.split("_")[0] if "_" in device_log_file_name else ""
        device_log_runnable = ReadUSBDeviceLogFile(
            worker_id=None,
            signals=self._signals,
            usb_serial_connection=self._usb_serial_connection,
            device_log_file_name=device_log_file_name,
            mac_id=mac_id,
            client=self._gateway_client,
            cache=self._disk_cache,
        )
        self._register_worker(
            action=ACTION.PARSE_USB_DEVICE_LOG,
            runnable=device_log_runnable
        )
        self._pool.start(device_log_runnable)

    def _on_usb_device_log_parsed(self, success: bool, msg: str, payload: dict) -> None:
        self.usbDeviceLogParsed.emit(success, payload.get("device_log_data", msg))

    @Slot(int, str)
    def analyze_log(self, tab: TAB, log_file_name: str) -> None:
        logger.debug("analyze_log called — tab=%r log_file_name=%r", tab, log_file_name)
        if tab == TAB.USB:
            usb_mac_id = log_file_name.split("_")[0] if "_" in log_file_name else ""
            analyze_runnable = AnalyzeUSBSerialWorker(
                worker_id=None,
                signals=self._signals,
                usb_serial_connection=self._usb_serial_connection,
                log_file_name=log_file_name,
                mac_id=usb_mac_id,
                client=self._gateway_client,
                cache=self._disk_cache,
            )
            self._register_worker(
                action=ACTION.ANALYZE_USB_LOG,
                runnable=analyze_runnable,
            )
            self._pool.start(analyze_runnable)

        elif tab == TAB.CLOUD:
            analyze_runnable = AnalyzeCloudWorker(
                worker_id=None,
                signals=self._signals,
                log_file_name=log_file_name,
                mac_id=self._current_cloud_mac_id,
                client=self._gateway_client,
                cache=self._disk_cache,
            )
            self._register_worker(
                action=ACTION.ANALYZE_CLOUD_LOG,
                runnable=analyze_runnable,
            )
            self._pool.start(analyze_runnable)
        elif tab == TAB.PC:
            disk_mac_id = Path(log_file_name).stem.split("_")[0] if "_" in Path(log_file_name).stem else ""
            logger.debug("PC branch — path=%r mac_id=%r file_exists=%s", log_file_name, disk_mac_id, Path(log_file_name).exists())
            analyze_runnable = AnalyzeDiskSerialWorker(
                worker_id=None,
                signals=self._signals,
                log_file_path=log_file_name,
                mac_id=disk_mac_id,
                client=self._gateway_client,
            )
            self._register_worker(
                action=ACTION.ANALYZE_DISK_LOG,
                runnable=analyze_runnable,
            )
            self._pool.start(analyze_runnable)
            

    def _on_analysis_completed(
        self, success: bool, msg: str, payload: dict | None
    ) -> None:
        if msg == GATEWAY_STATUS.UNAUTHORIZED:
            self._on_gateway_unauthorized()
            return
        log_file = payload.get("log_file_name") if payload else None
        logger.debug(f"Executing handler for {log_file}")
        self.analysisCompleted.emit(success, msg, payload)

    @Slot(list)
    def save_all_usb_files_to_disk(self, log_file_names: List[str]) -> None:
        logger.debug("Model received command to save from usb")
        if not self._usb_serial_connection.is_open_bool():
            self.usbFilesSavedToDisk.emit(
                False, MSG.USB.PORT_NOT_OPEN, None
            )
            return

        logger.debug(f"Save usb has worker id {self._next_worker_id}")
        runnable_save_from_usb = SaveAllUSBSerialWorker(
            worker_id=None,
            usb_serial_connection=self._usb_serial_connection,
            log_file_names=log_file_names,
            LOGS_DIR=self.LOGS_DIR,
            TEMP_DIR=self.TEMP_DIR,
            signals=self._signals,
        )
        # runnable_save_from_usb.signals.finished.connect(self._on_usb_files_saved_to_disk)
        self._register_worker(
            action=ACTION.SAVE_ALL_USB_FILES_TO_DISK,
            runnable=runnable_save_from_usb,
        )
        self._pool.start(runnable_save_from_usb)

    def _on_usb_files_saved_to_disk(
        self, success: bool, msg: str, payload: dict | None
    ) -> None:
        logger.debug("USB files saved")
        if payload and payload.get("serial_disconnect"):
            self.connectionStatusChanged.emit(False, msg)
            return
        self.usbFilesSavedToDisk.emit(success, msg, payload)

    @Slot(str, str)
    def save_one_usb_file_to_disk(self, filename: str, dest_dir: str) -> None:
        """Save a single file from USB to a user-chosen directory (no flash check)."""
        logger.debug("Model received command to save single file %s to %s", filename, dest_dir)
        if not self._usb_serial_connection.is_open_bool():
            self.usbFilesSavedToDisk.emit(False, MSG.USB.PORT_NOT_OPEN, None)
            return

        runnable = SaveOneUSBFileWorker(
            worker_id=None,
            signals=self._signals,
            usb_serial_connection=self._usb_serial_connection,
            log_file_name=filename,
            dest_dir=Path(dest_dir),
        )
        self._register_worker(
            action=ACTION.SAVE_ONE_USB_FILE_TO_DISK,
            runnable=runnable,
        )
        self._pool.start(runnable)

    def _on_one_usb_file_saved_to_disk(
        self, success: bool, msg: str, payload: dict | None
    ) -> None:
        if payload and payload.get("serial_disconnect"):
            self.connectionStatusChanged.emit(False, msg)
            return
        self.usbFilesSavedToDisk.emit(success, msg, payload)

    @Slot(list)
    def sync_usb_files_to_cloud(self, log_file_names: List[str]) -> None:
        """
        Handles Sync USB log files to Cloud.
        Flow: auth check → ACL pre-flight (gateway) → USB read → gateway upload.
        """
        logger.debug("starting usb to cloud sync...")

        # 1) Auth gate — must be signed in
        if not self.auth_manager.get_token():
            self.authRequired.emit(MSG.Auth.SIGN_IN_REQUIRED)
            return

        # Commenting to test -- March 4
        # 2) USB port must be open
        if not self._usb_serial_connection.is_open_bool():
            self.usbFilesSyncedToCloud.emit(
                False, MSG.USB.PORT_NOT_OPEN
            )
            return

        # 3) Extract mac_id from first filename for ACL pre-flight
        
        if not log_file_names:
            self.usbFilesSyncedToCloud.emit(False, MSG.Cloud.NO_FILES_TO_SYNC)
            return
        # Commenting to test -- March 4
        # log_file_names= ["10247072_2.txt"]
        mac_id = log_file_names[0].split("_")[0] if "_" in log_file_names[0] else ""
        if not mac_id:
            self.usbFilesSyncedToCloud.emit(False, MSG.Cloud.NO_MAC_ID)
            return

        # 4) Launch ACL pre-flight worker — verifies auth + org access via gateway
        runnable = GatewayACLPreflightWorker(
            worker_id=None,
            signals=self._signals,
            client=self._gateway_client,
            mac_id=mac_id,
            log_file_names=log_file_names,
        )
        self._register_worker(
            action=ACTION.GATEWAY_ACL_PREFLIGHT,
            runnable=runnable,
        )
        self._pool.start(runnable)

    def _on_acl_preflight_done(
        self, success: bool, msg: str, payload: dict | None
    ) -> None:
        """Handle ACL pre-flight result. On success, launch cloud sync worker directly."""
        if msg == GATEWAY_STATUS.UNAUTHORIZED:
            self._on_gateway_unauthorized()
            return
        if msg == GATEWAY_STATUS.MFA_REQUIRED:
            self._on_gateway_mfa_required()
            return
        if msg == GATEWAY_STATUS.FORBIDDEN:
            self.usbFilesSyncedToCloud.emit(False, MSG.Cloud.ACCESS_DENIED)
            return
        if msg == GATEWAY_STATUS.NOT_FOUND:
            self.usbFilesSyncedToCloud.emit(False, MSG.Cloud.DEVICE_NOT_REGISTERED)
            return
        if not success:
            self.usbFilesSyncedToCloud.emit(False, msg)
            return

        logger.debug("ACL pre-flight passed, starting cloud sync...")
        log_file_names = payload.get("log_file_names", [])
        cloud_files    = payload.get("cloud_files", [])
        mac_id         = payload.get("mac_id", "")

        staging_dir = self.TEMP_DIR / "temp_cloud_ingress"
        staging_dir.mkdir(exist_ok=True)

        runnable = GatewaySyncToCloudWorker(
            worker_id=None,
            signals=self._signals,
            gateway_client=self._gateway_client,
            usb_serial_connection=self._usb_serial_connection,
            staging_dir=staging_dir,
            mac_id=mac_id,
            usb_log_files=set(log_file_names),
            cloud_files=cloud_files,
        )
        self._register_worker(ACTION.UPLOAD_USB_FILES_TO_CLOUD, runnable)
        self._pool.start(runnable)

    def _on_usb_files_synced_to_cloud(
        self, success: bool, msg: str, payload: dict | None
    ) -> None:
        logger.debug("sync to cloud result: success=%s msg=%s", success, msg)

        # Handle gateway ACL / auth errors
        if msg == GATEWAY_STATUS.UNAUTHORIZED:
            self._cleanup_temp_cloud_dir()
            self._on_gateway_unauthorized()
            return
        if msg == GATEWAY_STATUS.MFA_REQUIRED:
            self._cleanup_temp_cloud_dir()
            self._on_gateway_mfa_required()
            return
        if msg == GATEWAY_STATUS.FORBIDDEN:
            self._cleanup_temp_cloud_dir()
            self.usbFilesSyncedToCloud.emit(False, MSG.Cloud.ACCESS_DENIED_WRITE)
            return
        if msg == GATEWAY_STATUS.NOT_FOUND:
            self._cleanup_temp_cloud_dir()
            self.usbFilesSyncedToCloud.emit(False, MSG.Cloud.DEVICE_NOT_REGISTERED)
            return
        if msg == GATEWAY_STATUS.FILE_NOT_FOUND:
            self._cleanup_temp_cloud_dir()
            self.usbFilesSyncedToCloud.emit(False, MSG.Cloud.FILE_NOT_FOUND)
            return

        self._cleanup_temp_cloud_dir()
        self.usbFilesSyncedToCloud.emit(success, msg)

    def _cleanup_temp_cloud_dir(self) -> None:
        """Remove the temp_cloud_ingress staging directory."""
        temp_dir = self.TEMP_DIR / "temp_cloud_ingress"
        try:
            if temp_dir.exists() and temp_dir.is_dir():
                shutil.rmtree(temp_dir)
        except OSError as e:
            logger.warning("Failed to clean up %s: %s", temp_dir, e.strerror)

    @Slot(str)
    def list_cloud_files(self, macid: str) -> None:
        """List cloud logs for a device via gateway, emit cloudFileNamesListed."""
        if not macid:
            self.cloudFileNamesListed.emit(False, MSG.Cloud.NO_DEVICE_ID, [])
            return

        self._current_cloud_mac_id = macid
        runnable = GatewayWorker(
            worker_id=None,
            signals=self._signals,
            client=self._gateway_client,
            mac_id=macid,
        )
        self._register_worker(action=ACTION.GATEWAY_LIST_FILES, runnable=runnable)
        self._pool.start(runnable)

    def _on_cloud_file_names_listed(
        self, success: bool, msg: str, payload: dict | None
    ) -> None:
        if success and payload:
            files = payload.get("logfilenames", [])
            self.cloudFileNamesListed.emit(True, "", files)
        else:
            self.cloudFileNamesListed.emit(False, msg or MSG.Cloud.FETCH_FAILED, [])

    def _on_gateway_file_names_listed(
        self, success: bool, msg: str, payload: dict | None
    ) -> None:
        if msg == GATEWAY_STATUS.UNAUTHORIZED:
            self._on_gateway_unauthorized()
            return
        if not success:
            self.cloudFileNamesListed.emit(False, msg or MSG.Cloud.FETCH_FILES_FAILED, [])
            return
        files = payload.get("logfilenames", []) if payload else []
        self.cloudFileNamesListed.emit(True, "", files)

    def _on_gateway_download_done(
        self, success: bool, msg: str, payload: dict | None
    ) -> None:
        if msg == GATEWAY_STATUS.UNAUTHORIZED:
            self._on_gateway_unauthorized()
            return
        if msg == GATEWAY_STATUS.FORBIDDEN:
            self.cloudFilesDownloadedToDisk.emit(False, MSG.Cloud.GATEWAY_ACCESS_DENIED, int(LOG_SRC.CLOUD))
            return
        if msg == GATEWAY_STATUS.FILE_NOT_FOUND:
            self.cloudFilesDownloadedToDisk.emit(False, MSG.Cloud.FILE_NOT_FOUND, int(LOG_SRC.CLOUD))
            return
        src = payload.get("src", LOG_SRC.CLOUD) if payload else LOG_SRC.CLOUD
        self.cloudFilesDownloadedToDisk.emit(success, msg, int(src))

    def _on_gateway_unauthorized(self) -> None:
        """Handle a 401 from the gateway — sign the user out and prompt re-login.

        The `_auth_required_emitted` flag prevents multiple concurrent 401s
        from triggering the dialog more than once.
        """
        if self._auth_required_emitted:
            return
        self._auth_required_emitted = True
        self.auth_manager.id_token = None  # invalidate cached token
        if self._active_workers:
            self.cancel_workers()
        self._usb_serial_connection.close_port()  # release the OS port handle immediately
        self.authRequired.emit(MSG.Auth.SESSION_EXPIRED)

    @Slot(int, list, str)
    def download_cloud_files_to_disk(self, src: int, log_file_names: list, dest_path: str) -> None:
        logger.debug(f"Saving {len(log_file_names)} log files from src {src}")
        if src == LOG_SRC.CLOUD:
            logger.debug("Creating gateway save worker")
            runnable = GatewaySaveFilesWorker(
                worker_id=None,
                signals=self._signals,
                client=self._gateway_client,
                mac_id=self._current_cloud_mac_id,
                log_file_names=log_file_names,
                dest_path=Path(dest_path),
            )
            self._register_worker(
                action=ACTION.GATEWAY_READ_FILE,
                runnable=runnable,
            )
            self._pool.start(runnable)
            
    
    def _on_cloud_files_downloaded_to_disk(self, success: bool, msg: str, payload: dict | None
    ) -> None:
        self.cloudFilesDownloadedToDisk.emit(success, msg, payload["src"])


    def _on_auth_done(self, success: bool, msg: str, payload: dict | None) -> None:
        if success and payload.get("type") == AUTH_TYPE.LOGIN:
            status_ok, _, status_data = self.auth_manager.get_account_info()

            if not status_ok:
                # Could not verify account status — fail closed.
                logger.warning("Account info check failed — rejecting login to fail closed")
                self.authFinished.emit(False, MSG.Auth.INTERNAL_ERROR.format("Could not verify account status"), {})
                return

            # Gate 1: email must be verified before MFA enrollment is attempted.
            if not status_data.get("email_verified", False):
                logger.debug("User logged in but email not verified — sending verification email")
                self.auth_manager.send_verification_email()
                self.emailVerificationRequired.emit()
                return

            # Gate 2: MFA enrollment check.
            if not status_data["enrolled"]:
                logger.debug("User logged in but not enrolled — starting TOTP setup")
                self.start_mfa_enrollment()
                return

            # Fully verified and enrolled — normal login path.
            self._auth_required_emitted = False
            self.authFinished.emit(success, msg, payload)
            return

        # Login failure or any other non-login/signup result.
        self.authFinished.emit(success, msg, payload)

    def _on_forgot_password_done(self, success: bool, msg: str, payload: dict) -> None:
        self.authFinished.emit(success, msg, payload)

    @Slot(str, str)
    def sign_in_user(self, email, password):
        # Re-use the SAME worker class, just flip the flag
        runnable = AuthWorker(
            worker_id=None,
            signals=self._signals,
            auth_manager=self.auth_manager,
            email=email,
            password=password,
        )
        self._register_worker(ACTION.AUTHENTICATE, runnable)
        self._pool.start(runnable)

    @Slot(str)
    def send_password_reset(self, email):
        runnable = ForgotPasswordWorker(
            worker_id=None,
            signals=self._signals,
            auth_manager=self.auth_manager,
            email=email,
        )
        self._register_worker(ACTION.FORGOT_PASSWORD, runnable)
        self._pool.start(runnable)

    @Slot()
    def recheck_email_verification(self) -> None:
        """Re-check emailVerified status after user clicks 'I've verified my email'."""
        runnable = RecheckEmailVerificationWorker(
            worker_id=None, signals=self._signals, auth_manager=self.auth_manager
        )
        self._register_worker(ACTION.RECHECK_EMAIL_VERIFICATION, runnable)
        self._pool.start(runnable)

    @Slot()
    def resend_verification_email(self) -> None:
        """Resend the verification email without re-checking status."""
        runnable = RecheckEmailVerificationWorker(
            worker_id=None, signals=self._signals, auth_manager=self.auth_manager,
            resend_only=True,
        )
        self._register_worker(ACTION.RECHECK_EMAIL_VERIFICATION, runnable)
        self._pool.start(runnable)

    def _on_recheck_email_done(self, success: bool, msg: str, payload: dict) -> None:
        if payload.get("resend_only"):
            # Resend finished — re-show the verify email page (error banner on failure).
            if not success:
                self.authFinished.emit(False, msg, {})
            self.emailVerificationRequired.emit()
            return

        if not success:
            self.authFinished.emit(False, MSG.Auth.INTERNAL_ERROR.format("Could not verify email status"), {})
            return

        if not payload.get("email_verified", False):
            # Still unverified — show error banner then re-show the gate page.
            self.authFinished.emit(False, MSG.Auth.EMAIL_STILL_UNVERIFIED, {})
            self.emailVerificationRequired.emit()
            return

        # Email is now verified — proceed to MFA enrollment check.
        if not payload.get("enrolled", False):
            self.start_mfa_enrollment()
            return

        self._auth_required_emitted = False
        self.authFinished.emit(True, MSG.Auth.SUCCESS, {"type": AUTH_TYPE.LOGIN})

    def start_mfa_enrollment(self) -> None:
        """Spawn the MFA START worker — triggers auth_manager.start_mfa_enrollment()
        on a background thread and emits mfaQrReady on success."""
        runnable = MFAWorker(
            worker_id=None, signals=self._signals, auth_manager=self.auth_manager,
            mode=MFA_MODE.START
        )
        self._register_worker(ACTION.MFA_ACTION, runnable)
        self._pool.start(runnable)

    def _on_gateway_mfa_required(self) -> None:
        """Handle a mid-session 403 mfa_required from the gateway.

        This fires when a user's token lacks the sign_in_second_factor claim —
        either they bypassed enrollment or MFA enforcement was just turned on.
        Route them through the same enrollment flow used after login.
        """
        logger.debug("Gateway MFA required — routing to TOTP enrollment")
        self.start_mfa_enrollment()



    # Update your submit slot to handle both Enrollment and Sign-in
# src/model.py

    @Slot(str)
    def finalize_mfa_setup(self, user_code):
        if not self._temp_mfa_session:
            self.authFinished.emit(False, MSG.Auth.MFA_SESSION_EXPIRED, {})
            return

        # If we have an id_token, we are setting up (Enrollment).
        # If we don't, we are halfway through a login (Sign-in Challenge).
        mode = MFA_MODE.FINALIZE if self.auth_manager.id_token else MFA_MODE.FINALIZE_SIGNIN

        runnable = MFAWorker(
            worker_id=None, signals=self._signals, auth_manager=self.auth_manager,
            mode=mode, session_info=self._temp_mfa_session, user_code=user_code
        )
        self._register_worker(ACTION.MFA_ACTION, runnable)
        self._pool.start(runnable)

    def _on_mfa_worker_done(self, success: bool, msg: str, payload: dict | None) -> None:
        mfa_type = payload.get("type")
        if mfa_type == MFA_PAYLOAD_TYPE.START and success:
            self._temp_mfa_session = payload["session"]
            self.mfaQrReady.emit(payload["secret"], self.auth_manager.email)
        elif mfa_type == AUTH_TYPE.LOGIN:
            # FINALIZE_SIGNIN emits type=AUTH_TYPE.LOGIN. Route through _on_auth_done
            # so the email verification gate runs — it is not checked before MFA login.
            self._on_auth_done(success, msg, {"type": AUTH_TYPE.LOGIN})
        else:
            # FINALIZE_ENROLL (type=MFA_PAYLOAD_TYPE.FINALIZE): email was already
            # verified to reach the enrollment screen, so emit directly.
            self.authFinished.emit(success, msg, {"type": AUTH_TYPE.LOGIN})
    @Slot(str)
    def verify_mfa_login(self, session_ticket, user_code):
        """
        Specific slot for handling MFA Login Challenges (SignIn).
        """
        # We explicitly set the mode to FINALIZE_SIGNIN
        runnable = MFAWorker(
            worker_id=None,
            signals=self._signals,
            auth_manager=self.auth_manager,
            mode=MFA_MODE.FINALIZE_SIGNIN,  # <--- Forces the correct worker logic
            session_info=session_ticket,
            user_code=user_code
        )
        self._register_worker(ACTION.MFA_ACTION, runnable)
        self._pool.start(runnable)

    def _is_all_threads_done(self) -> bool:
        return self._shutdown_in_progress and (not self._active_workers)

    def _all_workers_done(self) -> None:
        self._usb_serial_connection.close_port()
        self.allWorkersDone.emit()
