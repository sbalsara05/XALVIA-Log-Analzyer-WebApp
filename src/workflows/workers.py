from __future__ import annotations
from pathlib import Path
from typing import List, Optional, Iterator, Tuple
import requests
from PySide6.QtCore import QObject, QRunnable, Signal, Slot
from serial import Serial, SerialException, SerialTimeoutException
from serial.tools import list_ports
from platform import system
from time import sleep
from zipfile import ZipFile, ZIP_DEFLATED


from ..disk_cache.disk_cache import DiskCache, CacheKey, read_log_using_cache
from ..usb_conn.usbserialconnection import USBSerialConnection
from .. import constants, utils
from ..constants import SOURCE, AUTH_TYPE, MFA_MODE, MFA_PAYLOAD_TYPE
from ..utils import safe_del_dir, mac_from_filename, enrich_plot_spec, is_always_fresh
from ..cloud_conn.gateway_client import (
    GatewayMfaRequiredError,
    GatewayForbiddenError,
    GatewayUnauthorizedError,
)

import logging

logger = logging.getLogger(__name__)



def _build_points(plot_spec: dict) -> list:
    """Build one list[QPointF] per series in the background thread.

    QPointF is a value class (no QObject), so construction here is thread-safe.
    The main thread then only needs to call series.replace(pts) — one C++ swap.
    """
    from PySide6.QtCore import QPointF
    x_vals = plot_spec.get("x_values")
    result = []
    for s in plot_spec.get("series", []):
        data = s.get("data", [])
        if x_vals:
            pts = [QPointF(x_vals[j], data[j]) for j in range(len(data))]
        else:
            pts = [QPointF(float(j), data[j]) for j in range(len(data))]
        result.append(pts)
    return result



class BaseThreadWorker(QRunnable):
    def __init__(self, worker_id: int, signals: QObject, parent=None) -> None:
        super().__init__(parent)
        self._worker_id = worker_id
        self._signals = signals
        self._cancel = False

    def force_cancel(self) -> None:
        """Called from Model when user cancels."""
        self._cancel = True

    # Optional, but makes progress emission consistent
    def emit_progress(self, current: int, total: int, phase: int = 0) -> None:
        self._signals.progress.emit(self._worker_id, current, total, phase)

    def run(self) -> None:  # pragma: no cover
        """Subclasses *must* implement this."""
        raise NotImplementedError("Subclasses must implement run()")


class DetectUSBSerialWorker(BaseThreadWorker):
    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        usb_serial_connection: USBSerialConnection,
    ) -> None:
        super().__init__(
            worker_id=worker_id,
            signals=signals,
        )
        self._usb_serial_connection = usb_serial_connection

    def run(self) -> None:
        try:
            filtered_ports = self._usb_serial_connection.detect_ports()

            if filtered_ports is None or not filtered_ports:
                self._signals.finished.emit(
                    self._worker_id,
                    False,
                    constants.MSG.USB.NO_DEVICE,
                    dict(),
                )
            else:
                self._signals.finished.emit(
                    self._worker_id, True, "", {"filtered_ports": filtered_ports}
                )

        except constants.USB_SERIAL_FATALS as exc:
            logger.debug("DetectUSBSerialWorker: serial error: %r", exc)
            self._usb_serial_connection.close_port()
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.UNEXPECTED_ERROR.format(exc), None
            )


class ConnectUSBSerialWorker(BaseThreadWorker):
    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        usb_serial_connection: USBSerialConnection,
        port_device: str,
        baudrate: int,
        timeout: float,
    ) -> None:
        super().__init__(
            worker_id=worker_id,
            signals=signals,
        )

        self._usb_serial_connection = usb_serial_connection
        self._port_device = port_device
        self._baudrate = baudrate
        self._timeout = timeout

    def run(self) -> None:
        try:
            success = self._usb_serial_connection.connect_to_port(self._port_device, self._baudrate, self._timeout)
            if success:
                self._signals.finished.emit(
                    self._worker_id, True, "", {"port_device": self._port_device}
                )
            else:
                self._signals.finished.emit(
                    self._worker_id, False, "", {"port_device": self._port_device}
                )

        except constants.USB_SERIAL_FATALS as exc:
            logger.debug("ConnectUSBSerialWorker: serial error port=%r: %r", self._port_device, exc)
            self._usb_serial_connection.close_port()
            self._signals.finished.emit(
                self._worker_id,
                False,
                constants.MSG.USB.UNEXPECTED_ERROR.format(exc),
                {"port_device": self._port_device},
            )


class DisconnectUSBSerialWorker(BaseThreadWorker):
    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        usb_serial_connection: USBSerialConnection,
    ) -> None:
        super().__init__(
            worker_id=worker_id,
            signals=signals,
        )

        self._usb_serial_connection = usb_serial_connection

    def run(self) -> None:
        try:
            self._usb_serial_connection.close_port()
            self._signals.finished.emit(self._worker_id, True, "", {})

        except constants.USB_SERIAL_FATALS as exc:
            logger.debug("DisconnectUSBSerialWorker: serial error: %r", exc)
            self._usb_serial_connection.close_port()
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.UNEXPECTED_ERROR.format(exc), {}
            )


class ListUSBSerialWorker(BaseThreadWorker):
    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        usb_serial_connection,

    ) -> None:
        super().__init__(
            worker_id=worker_id,
            signals=signals,
        )

        self._usb_serial_connection = usb_serial_connection

    def run(self) -> None:
        try:
            logs = self._usb_serial_connection.list_all_log_files()
            if logs is None or not logs:
                self._signals.finished.emit(
                    self._worker_id, False, constants.MSG.USB.NO_LOG_FILES, {}
                )
            else:
                potential_device_logs = [
                    file_name for file_name in logs if utils.is_devicelog_fname(file_name)
                ]
                device_log_file_name = ""
                if potential_device_logs:
                    device_log_file_name = potential_device_logs[0]
                self._signals.finished.emit(
                    self._worker_id,
                    True,
                    None,
                    {
                        "logfilenames": logs,
                        "device_log_file_name": device_log_file_name,
                    },
                )

        except constants.USB_SERIAL_FATALS as exc:
            logger.debug("ListUSBSerialWorker: serial error: %r", exc)
            self._usb_serial_connection.close_port()
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.UNEXPECTED_ERROR.format(exc), {}
            )


class ReadUSBDeviceLogFile(BaseThreadWorker):
    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        usb_serial_connection: USBSerialConnection,
        device_log_file_name: str,
        mac_id: str,
        client,
        cache: Optional[DiskCache] = None,
    ) -> None:
        super().__init__(
            worker_id=worker_id,
            signals=signals,
        )

        self._usb_serial_connection = usb_serial_connection
        self._device_log_file_name = device_log_file_name
        self._mac_id = mac_id
        self._client = client
        self._cache = cache
        self._cache_key = None
        if cache is not None:
            self._cache_key = CacheKey(source="usb", log_file_name=device_log_file_name)

    

    def run(self) -> None:
        ## check in cache
        try:
            device_log_data = read_log_using_cache(conn_mgr=self._usb_serial_connection,
                                                       device_log_file_name=self._device_log_file_name,
                                                       cache_key=self._cache_key,
                                                       disk_cache=self._cache)
            if device_log_data is None or len(device_log_data) == 0:
                self._signals.finished.emit(
                    self._worker_id, False, constants.MSG.USB.DEVICE_LOG_UNREADABLE, {}
                )
            else:
                parsed_device_log = self._client.parse_device_log(self._mac_id, device_log_data)
                self._signals.finished.emit(
                    self._worker_id, True, "", {"device_log_data": parsed_device_log}
                )

        except (GatewayMfaRequiredError, GatewayForbiddenError, GatewayUnauthorizedError) as exc:
            logger.debug("ReadUSBDeviceLogFile: gateway access error: %r", exc)
            self._signals.finished.emit(
                self._worker_id, False, _gateway_error_msg(exc), {}
            )
        except (requests.ConnectionError, requests.Timeout) as exc:
            logger.debug("ReadUSBDeviceLogFile: server unreachable mac_id=%r: %r", self._mac_id, exc)
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.SERVER_UNREACHABLE, {}
            )
        except requests.HTTPError as exc:
            logger.debug("ReadUSBDeviceLogFile: HTTP error mac_id=%r file=%r: %r", self._mac_id, self._device_log_file_name, exc)
            self._signals.finished.emit(
                self._worker_id, False, _http_error_msg(exc, file_op=True), {}
            )
        except constants.USB_SERIAL_FATALS as exc:
            logger.debug("ReadUSBDeviceLogFile: serial error mac_id=%r file=%r: %r", self._mac_id, self._device_log_file_name, exc)
            self._usb_serial_connection.close_port()
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.UNEXPECTED_ERROR.format(exc), {}
            )


class AnalyzeUSBSerialWorker(BaseThreadWorker):
    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        usb_serial_connection: USBSerialConnection,
        log_file_name: str,
        mac_id: str,
        client,
        cache: Optional[DiskCache] = None,
    ) -> None:
        super().__init__(
            worker_id=worker_id,
            signals=signals,
        )

        self._usb_serial_connection = usb_serial_connection
        self._log_file_name = log_file_name
        self._mac_id = mac_id
        self._client = client

        self._cache = cache
        self._cache_key = None
        if cache is not None:
            self._cache_key = CacheKey(
                source="usb",
                log_file_name=log_file_name,
            )



    def run(self) -> None:
        try:
            log_data = read_log_using_cache(conn_mgr=self._usb_serial_connection,
                                                       device_log_file_name=self._log_file_name,
                                                       cache_key=self._cache_key,
                                                       disk_cache=self._cache)

            if log_data is None or len(log_data) == 0:
                self._signals.finished.emit(
                    self._worker_id,
                    False,
                    constants.MSG.USB.LOG_UNREADABLE,
                    {"log_file_name": self._log_file_name, "source": SOURCE.USB},
                )
            else:
                success, plot_spec = self._client.parse_test_log(self._mac_id, log_data)

                if not success:
                    self._signals.finished.emit(
                        self._worker_id,
                        False,
                        constants.MSG.USB.NO_OPTICAL_VALUES.format(self._log_file_name),
                        {"log_file_name": self._log_file_name, "source": SOURCE.USB},
                    )
                    return

                enrich_plot_spec(plot_spec, self._log_file_name, "usb")
                self._signals.finished.emit(
                    self._worker_id,
                    True,
                    "",
                    {
                        "log_file_name": self._log_file_name,
                        "source": SOURCE.USB,
                        "plot_spec": plot_spec,
                        "pts_per_series": _build_points(plot_spec),
                    },
                )
        except (GatewayMfaRequiredError, GatewayForbiddenError, GatewayUnauthorizedError) as exc:
            logger.debug("AnalyzeUSBSerialWorker: gateway access error: %r", exc)
            self._signals.finished.emit(
                self._worker_id, False, _gateway_error_msg(exc), {"source": SOURCE.USB, "log_file_name": self._log_file_name}
            )
        except (requests.ConnectionError, requests.Timeout) as exc:
            logger.debug("AnalyzeUSBSerialWorker: server unreachable: %r", exc)
            self._usb_serial_connection.close_port()
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.SERVER_UNREACHABLE, {"source": SOURCE.USB, "log_file_name": self._log_file_name}
            )
        except requests.HTTPError as exc:
            logger.debug("AnalyzeUSBSerialWorker: caught exception %r", exc)
            status = exc.response.status_code if exc.response is not None else 0
            self._usb_serial_connection.close_port()
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.UNEXPECTED_ERROR.format(f"Server error (HTTP {status})"), {"source": SOURCE.USB, "log_file_name": self._log_file_name}
            )
        except constants.USB_SERIAL_FATALS + (requests.RequestException,) as exc:
            logger.debug("AnalyzeUSBSerialWorker: serial/request error mac_id=%r file=%r: %r", self._mac_id, self._log_file_name, exc)
            self._usb_serial_connection.close_port()
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.UNEXPECTED_ERROR.format(exc), {"source": SOURCE.USB, "log_file_name": self._log_file_name}
            )


class AnalyzeCloudWorker(BaseThreadWorker):
    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        log_file_name: str,
        mac_id: str,
        client,
        cache: Optional[DiskCache] = None,
    ) -> None:
        super().__init__(
            worker_id=worker_id,
            signals=signals,
        )

        self._log_file_name = log_file_name
        self._mac_id = mac_id
        self._client = client

        self._cache = cache
        self._cache_key = None
        if cache is not None:
            self._cache_key = CacheKey(
                source="cloud",
                log_file_name=log_file_name,
            )

    def run(self) -> None:
        try:
            # Extract mac_id from filename (e.g. "AABBCCDD_001_test.txt" → "AABBCCDD")
            cloud_mac_id = self._log_file_name.split("_")[0] if "_" in self._log_file_name else self._mac_id

            # Check disk cache first; otherwise read via gateway signed URL
            cached_data = self._cache.get_bytes(self._cache_key) if (self._cache and self._cache_key) else None
            if cached_data:
                log_data = cached_data
            else:
                log_data = self._client.read_file(cloud_mac_id, self._log_file_name)
                if log_data and self._cache and self._cache_key:
                    self._cache.put_bytes(self._cache_key, log_data)

            if log_data is None or len(log_data) == 0:
                self._signals.finished.emit(
                    self._worker_id,
                    False,
                    constants.MSG.USB.LOG_UNREADABLE,
                    {"log_file_name": self._log_file_name, "source": SOURCE.CLOUD},
                )
            else:
                success, plot_spec = self._client.parse_test_log(cloud_mac_id, log_data)

                if not success:
                    self._signals.finished.emit(
                        self._worker_id,
                        False,
                        constants.MSG.USB.NO_OPTICAL_VALUES.format(self._log_file_name),
                        {"log_file_name": self._log_file_name, "source": SOURCE.CLOUD},
                    )
                    return

                enrich_plot_spec(plot_spec, self._log_file_name, "cloud")
                self._signals.finished.emit(
                    self._worker_id,
                    True,
                    "",
                    {
                        "log_file_name": self._log_file_name,
                        "source": SOURCE.CLOUD,
                        "plot_spec": plot_spec,
                        "pts_per_series": _build_points(plot_spec),
                    },
                )
        except (GatewayMfaRequiredError, GatewayForbiddenError, GatewayUnauthorizedError) as exc:
            logger.debug("AnalyzeCloudWorker: gateway access error: %r", exc)
            self._signals.finished.emit(
                self._worker_id, False, _gateway_error_msg(exc), {"source": SOURCE.CLOUD, "log_file_name": self._log_file_name}
            )
        except (requests.ConnectionError, requests.Timeout) as exc:
            logger.debug("AnalyzeCloudWorker: server unreachable: %r", exc)
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.SERVER_UNREACHABLE, {"source": SOURCE.CLOUD, "log_file_name": self._log_file_name}
            )
        except requests.HTTPError as exc:
            logger.debug("AnalyzeCloudWorker: caught exception %r", exc)
            self._signals.finished.emit(
                self._worker_id, False, _http_error_msg(exc, file_op=True), {"source": SOURCE.CLOUD, "log_file_name": self._log_file_name}
            )
        except constants.FILE_FATALS + (requests.RequestException,) as exc:
            logger.debug("AnalyzeCloudWorker: file/request error mac_id=%r file=%r: %r", self._mac_id, self._log_file_name, exc)
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.UNEXPECTED_ERROR.format(exc), {"source": SOURCE.CLOUD, "log_file_name": self._log_file_name}
            )


class AnalyzeDiskSerialWorker(BaseThreadWorker):
    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        log_file_path: str,
        mac_id: str,
        client,
        cache: Optional[DiskCache] = None,
    ) -> None:
        super().__init__(
            worker_id=worker_id,
            signals=signals,
        )

        self._log_file_path = Path(log_file_path)
        self._mac_id = mac_id
        self._client = client

    def run(self) -> None:
        logger.debug("AnalyzeDiskSerialWorker.run — path=%r mac_id=%r", self._log_file_path, self._mac_id)
        try:
            ## read log file string
            if not self._log_file_path.exists() or not self._log_file_path.is_file():
                logger.debug("AnalyzeDiskSerialWorker: file not found at %r", self._log_file_path)
                self._signals.finished.emit(
                    self._worker_id,
                    False,
                    constants.MSG.USB.FILE_NOT_FOUND,
                    {"log_file_name": self._log_file_path.as_posix(), "source": SOURCE.DISK},
                )
                return

            log_data = self._log_file_path.read_bytes()
            logger.debug("AnalyzeDiskSerialWorker: read %d bytes from %r", len(log_data), self._log_file_path)

            if log_data is None or len(log_data) == 0:
                self._signals.finished.emit(
                    self._worker_id,
                    False,
                    constants.MSG.USB.LOG_UNREADABLE,
                    {"log_file_name": self._log_file_path.as_posix(), "source": SOURCE.DISK},
                )
            else:
                logger.debug("AnalyzeDiskSerialWorker: calling parse_test_log mac_id=%r", self._mac_id)
                success, plot_spec = self._client.parse_test_log(self._mac_id, log_data)
                logger.debug("AnalyzeDiskSerialWorker: parse_test_log success=%s", success)

                if not success:
                    logger.debug("AnalyzeDiskSerialWorker: no optical values in %r", self._log_file_path)
                    self._signals.finished.emit(
                        self._worker_id,
                        False,
                        constants.MSG.USB.NO_OPTICAL_VALUES.format(self._log_file_path.as_posix()),
                        {"log_file_name": self._log_file_path.as_posix(), "source": SOURCE.DISK},
                    )
                    return

                logger.debug("AnalyzeDiskSerialWorker: emitting success with plot_spec keys=%s", list(plot_spec.keys()) if plot_spec else None)
                enrich_plot_spec(plot_spec, self._log_file_path.as_posix(), "disk")
                self._signals.finished.emit(
                    self._worker_id,
                    True,
                    "",
                    {
                        "log_file_name": self._log_file_path.as_posix(),
                        "source": SOURCE.DISK,
                        "plot_spec": plot_spec,
                        "pts_per_series": _build_points(plot_spec),
                    },
                )
        except (GatewayMfaRequiredError, GatewayForbiddenError, GatewayUnauthorizedError) as exc:
            logger.debug("AnalyzeDiskSerialWorker: gateway access error: %r", exc)
            self._signals.finished.emit(
                self._worker_id, False, _gateway_error_msg(exc), {"source": SOURCE.DISK, "log_file_name": self._log_file_path.as_posix()}
            )
        except (requests.ConnectionError, requests.Timeout) as exc:
            logger.debug("AnalyzeDiskSerialWorker: server unreachable: %r", exc)
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.SERVER_UNREACHABLE, {"source": SOURCE.DISK, "log_file_name": self._log_file_path.as_posix()}
            )
        except requests.HTTPError as exc:
            logger.debug("AnalyzeDiskSerialWorker: caught exception %r", exc)
            status = exc.response.status_code if exc.response is not None else 0
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.UNEXPECTED_ERROR.format(f"Server error (HTTP {status})"), {"source": SOURCE.DISK, "log_file_name": self._log_file_path.as_posix()}
            )
        except constants.FILE_FATALS + (requests.RequestException,) as exc:
            logger.debug("AnalyzeDiskSerialWorker: caught exception %r", exc)
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.UNEXPECTED_ERROR.format(exc), {"source": SOURCE.DISK, "log_file_name": self._log_file_path.as_posix()}
            )


class SaveAllUSBSerialWorker(BaseThreadWorker):
    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        usb_serial_connection: USBSerialConnection,
        log_file_names: List[str],
        LOGS_DIR: Path,
        TEMP_DIR: Path,
    ) -> None:
        super().__init__(
            worker_id=worker_id,
            signals=signals,
        )

        self._usb_serial_connection = usb_serial_connection
        self._log_file_names = log_file_names
        self._LOGS_DIR = LOGS_DIR
        self._TEMP_DIR = TEMP_DIR

    def run(self) -> None:
        try:
            mac_id = mac_from_filename(self._log_file_names[0])
            logger.debug("mac id is %s", mac_id)

            # Step 2. Check if the mac_id's directory exists in the logs directory
            disk_dir = self._LOGS_DIR / mac_id
            self._log_file_names = set(self._log_file_names)
            intersection = set()
            if disk_dir.is_dir():
                logger.debug("device dir exists on disk")
                on_disk_files = utils.disk_test_log_files(disk_dir=disk_dir, mac_id=mac_id)

                def read_log_from_disk(log_file_name: str) -> bytes:
                    return (disk_dir / log_file_name).read_bytes()
                
                if utils.is_memory_reflash(
                    read_src_a=read_log_from_disk, log_files_a=on_disk_files,
                      read_src_b=self._usb_serial_connection.read_log_file, log_files_b=self._log_file_names
                ):
                    self._signals.finished.emit(self._worker_id, False, constants.MSG.USB.MEMORY_REFLASH, {})
                    return

                else:
                    logger.debug("Writing newer files to device dir")
                    intersection = on_disk_files & self._log_file_names

            # Step 2.2.2/Step 3. Re-write all logs (device and test)
            print("Creating a temp device dir")
            temp_disk_dir = self._TEMP_DIR / f"temp_{mac_id}"

            old_temp_deleted = safe_del_dir(dir=temp_disk_dir)
            if not old_temp_deleted:
                self._signals.finished.emit(
                    self._worker_id, False, constants.MSG.USB.UNEXPECTED_ERROR.format(""), {}
                )
            temp_disk_dir.mkdir(exist_ok=False)

            logger.debug("Creating device dir if not exists")
            disk_dir.mkdir(exist_ok=True)

            diff = list(self._log_file_names.difference(intersection))
            logger.debug("difference to be read is %s", diff)
            empty_log_files = set()
            nb_saved = 0
            total = len(diff)
            for fname in diff:  ## device log is always read afresh
                if self._cancel:
                    break
                log_file_data = self._usb_serial_connection.read_log_file(fname)
                ## TO DO: INSEAD OF TERMINATING SAVING, GENERATE A STATUS REPORT"
                if not log_file_data:
                    empty_log_files.add(fname)
                utils.save_file_helper(
                        log_filename=fname,
                        log_file_data=log_file_data,
                        save_path=temp_disk_dir,
                    )
                nb_saved += 1
                self._signals.progress.emit(self._worker_id, nb_saved, total, 0)

            if self._cancel:
                self._signals.finished.emit(
                    self._worker_id, False, constants.MSG.USB.SAVE_STOPPED, {}
                )
            else:
                utils.move_files_no_overwrite(
                    source_dir=temp_disk_dir, destination_dir=disk_dir
                )
                self._signals.finished.emit(
                    self._worker_id,
                    True,
                    constants.MSG.USB.ALL_FILES_SAVED,
                    {
                        "save_path": disk_dir,
                        "macid": mac_id,
                        "total": nb_saved,
                        "empty": empty_log_files,
                        "usb_log_files": self._log_file_names - empty_log_files,
                    },
                )

            safe_del_dir(dir=temp_disk_dir)

            if not any(disk_dir.iterdir()):
                safe_del_dir(dir=disk_dir)

        except constants.USB_SERIAL_FATALS as exc:
            logger.debug("SaveAllUSBSerialWorker: serial error: %r", exc)
            self._usb_serial_connection.close_port()
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.SERIAL_DISCONNECTED.format(exc),
                {"serial_disconnect": True}
            )
        except constants.FILE_FATALS as exc:
            logger.debug("SaveAllUSBSerialWorker: disk I/O error: %r", exc)
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.UNEXPECTED_ERROR.format(exc), {}
            )


class SaveOneUSBFileWorker(BaseThreadWorker):
    """Read a single file from USB and write it to a user-chosen directory.

    No memory-flash check, no delta logic — just a straight read+write.
    """

    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        usb_serial_connection: USBSerialConnection,
        log_file_name: str,
        dest_dir: Path,
    ) -> None:
        super().__init__(worker_id=worker_id, signals=signals)
        self._usb = usb_serial_connection
        self._filename = log_file_name
        self._dest_dir = dest_dir

    def run(self) -> None:
        try:
            data = self._usb.read_log_file(self._filename)
            if not data:
                self._signals.finished.emit(
                    self._worker_id, False, constants.MSG.USB.LOG_UNREADABLE, {}
                )
                return
            utils.save_file_helper(
                log_filename=self._filename,
                log_file_data=data,
                save_path=self._dest_dir,
            )
            self.emit_progress(1, 1, 0)
            self._signals.finished.emit(
                self._worker_id, True, constants.MSG.USB.FILE_SAVED,
                {"save_path": str(self._dest_dir / self._filename)},
            )
        except constants.USB_SERIAL_FATALS as exc:
            logger.debug("SaveOneUSBFileWorker: serial error file=%r: %r", self._filename, exc)
            self._usb.close_port()
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.SERIAL_DISCONNECTED.format(exc),
                {"serial_disconnect": True}
            )
        except constants.FILE_FATALS as exc:
            logger.debug("SaveOneUSBFileWorker: disk I/O error file=%r dest=%r: %r", self._filename, self._dest_dir, exc)
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.UNEXPECTED_ERROR.format(exc), {}
            )


class GatewaySyncToCloudWorker(BaseThreadWorker):
    """Self-contained USB-to-cloud sync worker with fail-fast reflash check.

    Owns the full pipeline: reflash check → USB diff read → cloud upload.
    No pre-staged local directory required — files are read directly from USB
    into a per-run temp dir under ``staging_dir/<mac_id>/`` and cleaned up on
    every exit path.

    I/O operations:
    1. **List cloud files**  — GET /api/files?mac_id=X
    2. **Reflash check**     — reads one cloud file + one USB file (fail-fast,
       zero bulk reads if reflash detected)
    3. **Read USB diff**     — serial read of only the files not already in cloud
    4. **Get signed URLs**   — GET /api/signed-url/upload?mac_id=X&filename=Y
    5. **Upload**            — PUT directly to GCS signed URL per file
    6. **Commit session**    — gateway writes _COMMIT.json

    On HTTP 401/403/404 the canonical message string produced by
    ``_http_error_msg`` is forwarded so Model handlers can react.
    """

    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        gateway_client,                        # GatewayClient
        usb_serial_connection: USBSerialConnection,
        staging_dir: Path,                     # base for temp files (caller: LOGS_DEFAULT / "temp_cloud_ingress")
        mac_id: str,
        usb_log_files: set,
        cloud_files: list,                     # cloud file list from ACL preflight — avoids duplicate list_files call
    ) -> None:
        super().__init__(worker_id=worker_id, signals=signals)
        self._client = gateway_client
        self._usb_serial_connection = usb_serial_connection
        self._staging_dir = staging_dir
        self._mac_id = mac_id
        self._usb_log_files = set(usb_log_files)
        self._cloud_files = list(cloud_files)

    def run(self) -> None:
        if not self._mac_id or not self._usb_log_files:
            self._signals.finished.emit(
                self._worker_id, False,
                "macid and files required for sync", {},
            )
            return

        temp_dir = self._staging_dir / self._mac_id

        # 1) Cloud file list was fetched by ACL preflight — no duplicate HTTP call needed.
        cloud_log_files = set(self._cloud_files)
        logger.debug("cloud_log_files (from preflight) %s", list(cloud_log_files)[:5])

        # Exclude always-fresh files from reflash comparison (see utils.is_always_fresh).
        test_log_usb   = {f for f in self._usb_log_files if not is_always_fresh(f)}
        test_log_cloud = {f for f in cloud_log_files      if not is_always_fresh(f)}

        # 2) Reflash check (fail-fast) — reads only ONE cloud file + ONE USB file.
        #    Operates only on test-log files; devicelog/track files are never compared.
        if test_log_usb and test_log_cloud:
            try:
                if utils.is_memory_reflash(
                    read_src_a=lambda fname: self._client.read_file(self._mac_id, fname),
                    log_files_a=test_log_cloud,
                    read_src_b=self._usb_serial_connection.read_log_file,
                    log_files_b=test_log_usb,
                ):
                    logger.debug(constants.MSG.USB.MEMORY_REFLASH)
                    self._signals.finished.emit(
                        self._worker_id, False, constants.MSG.USB.MEMORY_REFLASH, {}
                    )
                    return
            except (GatewayMfaRequiredError, GatewayForbiddenError, GatewayUnauthorizedError) as exc:
                logger.debug("GatewaySyncToCloudWorker: reflash check — gateway auth error mac_id=%r: %r", self._mac_id, exc)
                self._signals.finished.emit(
                    self._worker_id, False, _gateway_error_msg(exc), {},
                )
                return
            except requests.HTTPError as exc:
                logger.debug("GatewaySyncToCloudWorker: reflash check — HTTP error mac_id=%r: %r", self._mac_id, exc)
                self._signals.finished.emit(
                    self._worker_id, False, _http_error_msg(exc, file_op=True), {},
                )
                return
            except constants.USB_SERIAL_FATALS as exc:
                logger.debug("GatewaySyncToCloudWorker: reflash check — serial error mac_id=%r: %r", self._mac_id, exc)
                self._usb_serial_connection.close_port()
                self._signals.finished.emit(
                    self._worker_id, False,
                    constants.MSG.USB.SERIAL_DISCONNECTED.format(exc),
                    {"serial_disconnect": True},
                )
                return

        # 3) Compute diff — always-fresh files are always included regardless of what's
        #    in cloud; test-log files only upload if not already there.
        diff = list(
            {f for f in self._usb_log_files if is_always_fresh(f)}
            | (test_log_usb - test_log_cloud)
        )
        logger.debug("difference to be uploaded is %s", diff)

        if not diff:
            self._signals.finished.emit(
                self._worker_id, True,
                constants.MSG.Cloud.ALL_FILES_UPLOADED.format(0),
                {"macid": self._mac_id, "total": 0, "session_id": ""},
            )
            return

        # 4) Read diff files from USB into temp dir (atomic).
        safe_del_dir(temp_dir)
        try:
            temp_dir.mkdir(parents=True, exist_ok=False)
        except constants.FILE_FATALS as exc:
            logger.debug("GatewaySyncToCloudWorker: failed to create temp dir %r: %r", temp_dir, exc)
            self._signals.finished.emit(
                self._worker_id, False,
                constants.MSG.USB.UNEXPECTED_ERROR.format(exc), {},
            )
            return

        empty_log_files: set = set()
        nb_read = 0
        total = len(diff)

        try:
            for fname in diff:
                if self._cancel:
                    break
                data = self._usb_serial_connection.read_log_file(fname)
                if not data:
                    empty_log_files.add(fname)
                utils.save_file_helper(
                    log_filename=fname,
                    log_file_data=data,
                    save_path=temp_dir,
                )
                nb_read += 1
                self._signals.progress.emit(self._worker_id, nb_read, total, 0)
        except constants.USB_SERIAL_FATALS as exc:
            logger.debug("GatewaySyncToCloudWorker: USB read error mac_id=%r diff=%r: %r", self._mac_id, diff, exc)
            self._usb_serial_connection.close_port()
            safe_del_dir(temp_dir)
            self._signals.finished.emit(
                self._worker_id, False,
                constants.MSG.USB.SERIAL_DISCONNECTED.format(exc),
                {"serial_disconnect": True},
            )
            return
        except constants.FILE_FATALS as exc:
            logger.debug("GatewaySyncToCloudWorker: disk I/O error during USB read mac_id=%r: %r", self._mac_id, exc)
            safe_del_dir(temp_dir)
            self._signals.finished.emit(
                self._worker_id, False,
                constants.MSG.USB.UNEXPECTED_ERROR.format(exc), {},
            )
            return

        if self._cancel:
            safe_del_dir(temp_dir)
            self._signals.finished.emit(
                self._worker_id, False,
                constants.MSG.Cloud.UPLOAD_STOPPED, {},
            )
            return

        # 5) Get gateway-owned session_id + signed PUT URLs for all diff files.
        try:
            session_id, signed_urls, required_headers = self._client.get_upload_signed_urls(self._mac_id, diff)
        except (GatewayMfaRequiredError, GatewayForbiddenError, GatewayUnauthorizedError) as exc:
            logger.debug("GatewaySyncToCloudWorker: get upload URLs — gateway auth error mac_id=%r: %r", self._mac_id, exc)
            safe_del_dir(temp_dir)
            self._signals.finished.emit(
                self._worker_id, False, _gateway_error_msg(exc), {},
            )
            return
        except requests.HTTPError as exc:
            logger.debug("GatewaySyncToCloudWorker: get upload URLs — HTTP error mac_id=%r: %r", self._mac_id, exc)
            safe_del_dir(temp_dir)
            self._signals.finished.emit(
                self._worker_id, False, _http_error_msg(exc), {},
            )
            return
        except Exception as exc:
            logger.debug("GatewaySyncToCloudWorker: get upload URLs — unexpected error mac_id=%r: %r", self._mac_id, exc)
            safe_del_dir(temp_dir)
            self._signals.finished.emit(
                self._worker_id, False,
                f"Failed to get upload URLs: {exc}", {},
            )
            return

        uploaded: list[str] = []
        nb_uploaded = 0

        # 6) Upload each new file directly to GCS using pre-signed URLs.
        for fname in diff:
            if self._cancel:
                break
            try:
                data = (temp_dir / fname).read_bytes()
                self._client.upload_file(signed_urls[fname], data, required_headers)
            except requests.HTTPError as exc:
                logger.debug("GatewaySyncToCloudWorker: GCS upload HTTP error mac_id=%r file=%r: %r", self._mac_id, fname, exc)
                safe_del_dir(temp_dir)
                self._signals.finished.emit(
                    self._worker_id, False, _http_error_msg(exc), {},
                )
                return
            except Exception as exc:
                msg = f"An unexpected error occurred while uploading {fname}: {exc}"
                logger.error(msg)
                safe_del_dir(temp_dir)
                self._signals.finished.emit(self._worker_id, False, msg, {})
                return

            uploaded.append(fname)
            nb_uploaded += 1
            self._signals.progress.emit(self._worker_id, nb_uploaded, total, 1)

        if self._cancel:
            safe_del_dir(temp_dir)
            self._signals.finished.emit(
                self._worker_id, False,
                constants.MSG.Cloud.UPLOAD_STOPPED, {},
            )
            return

        # 7) Write commit marker via gateway.
        try:
            committed_path = self._client.commit_session(self._mac_id, session_id, uploaded)
        except (GatewayMfaRequiredError, GatewayForbiddenError, GatewayUnauthorizedError) as exc:
            logger.debug("GatewaySyncToCloudWorker: commit — gateway auth error mac_id=%r session=%r: %r", self._mac_id, session_id, exc)
            safe_del_dir(temp_dir)
            self._signals.finished.emit(
                self._worker_id, False, _gateway_error_msg(exc), {},
            )
            return
        except requests.HTTPError as exc:
            logger.debug("GatewaySyncToCloudWorker: commit — HTTP error mac_id=%r session=%r: %r", self._mac_id, session_id, exc)
            safe_del_dir(temp_dir)
            self._signals.finished.emit(
                self._worker_id, False, _http_error_msg(exc), {},
            )
            return
        except Exception as exc:
            msg = f"Files uploaded but commit failed — upload may be incomplete: {exc}"
            logger.error(msg)
            safe_del_dir(temp_dir)
            self._signals.finished.emit(self._worker_id, False, msg, {})
            return

        if not committed_path:
            safe_del_dir(temp_dir)
            self._signals.finished.emit(
                self._worker_id, False,
                "Files uploaded but server did not confirm the commit — upload may be incomplete",
                {},
            )
            return

        # 8) Cleanup temp dir and emit success.
        safe_del_dir(temp_dir)
        self._signals.finished.emit(
            self._worker_id,
            True,
            constants.MSG.Cloud.ALL_FILES_UPLOADED.format(nb_uploaded),
            {"macid": self._mac_id, "total": nb_uploaded, "session_id": session_id, "committed": committed_path},
        )


class AuthWorker(BaseThreadWorker):
    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        auth_manager,
        email: str,
        password: str,
    ) -> None:
        super().__init__(worker_id=worker_id, signals=signals)
        self.auth_manager = auth_manager
        self.email = email
        self.password = password

    def run(self):
        try:
            success, msg, payload = self.auth_manager.sign_in(self.email, self.password)
            self._signals.finished.emit(self._worker_id, success, msg, payload)
        except Exception as e:
            logger.debug("AuthWorker: unexpected error email=%r: %r", self.email, e)
            self._signals.finished.emit(self._worker_id, False, constants.MSG.Auth.WORKER_ERROR.format(e), {})
# src/workflows/workers.py

# src/workflows/workers.py

class MFAWorker(BaseThreadWorker):
    def __init__(self, worker_id, signals, auth_manager, mode, session_info=None, user_code=None):
        """
        Handles MFA background tasks.
        :param mode: "START", "FINALIZE" (Enrollment), or "FINALIZE_SIGNIN" (Login)
        :param session_info: Dict containing 'mfa_pending_cred' and 'mfa_enrollment_id' (for login)
        :param user_code: The 6-digit TOTP code
        """
        super().__init__(worker_id, signals)
        self.auth_manager = auth_manager
        self.mode = mode
        self.session_info = session_info
        self.user_code = user_code

    def run(self):
        try:
            # --- 1. START ENROLLMENT (Get QR Code) ---
            if self.mode == MFA_MODE.START:
                success, msg, data = self.auth_manager.start_mfa_enrollment()
                payload = {"type": MFA_PAYLOAD_TYPE.START}
                if success:
                    payload.update(data)
                
                self._signals.finished.emit(self._worker_id, success, msg, payload)

            # --- 2. FINALIZE ENROLLMENT (Setup) ---
            elif self.mode == MFA_MODE.FINALIZE:
                success, msg, payload = self.auth_manager.finalize_mfa_enrollment(
                    self.session_info, self.user_code
                )
                self._signals.finished.emit(self._worker_id, success, msg, payload)

            # --- 3. FINALIZE SIGNIN (Login Challenge) ---
            elif self.mode == MFA_MODE.FINALIZE_SIGNIN:
                # A. Unpack the dictionary safely
                cred = None
                eid = None
                
                if isinstance(self.session_info, dict):
                    cred = self.session_info.get("mfa_pending_cred")
                    eid = self.session_info.get("mfa_enrollment_id")
                    logger.debug(f"DEBUG WORKER: Cred length={len(cred) if cred else 0}, EID={eid}, Code={self.user_code}")
                else:
                    logger.debug(f"DEBUG WORKER: Session info is not a dict? Type: {type(self.session_info)}")
                    cred = self.session_info
                    eid = None

                # B. Call AuthManager (catches raises internally in AuthManager or here)
                try:
                    user_data = self.auth_manager.verify_mfa_challenge(
                        cred, 
                        self.user_code, 
                        mfa_enrollment_id=eid
                    )
                    
                    # C. Emit Success (using consistent 'finished' signal)
                    self._signals.finished.emit(self._worker_id, True, constants.MSG.Auth.LOGIN_SUCCESSFUL, {
                        "type": AUTH_TYPE.LOGIN,
                        "user": user_data
                    })
                except Exception as e:
                    logger.debug("MFAWorker: verify_mfa_challenge error mode=%r: %r", self.mode, e)
                    self._signals.finished.emit(self._worker_id, False, str(e), {"type": AUTH_TYPE.LOGIN})

        except Exception as e:
            logger.debug("MFAWorker: unexpected error mode=%r: %r", self.mode, e)
            self._signals.finished.emit(self._worker_id, False, constants.MSG.Auth.WORKER_ERROR.format(e), {})


class RecheckEmailVerificationWorker(BaseThreadWorker):
    """Re-checks account info to confirm email verification, or resends the verification email."""

    def __init__(self, worker_id, signals, auth_manager, resend_only: bool = False):
        super().__init__(worker_id=worker_id, signals=signals)
        self.auth_manager = auth_manager
        self.resend_only = resend_only

    def run(self):
        try:
            if self.resend_only:
                success, msg, _ = self.auth_manager.send_verification_email()
                self._signals.finished.emit(self._worker_id, success, msg, {"resend_only": True})
                return
            success, msg, payload = self.auth_manager.get_account_info()
            self._signals.finished.emit(self._worker_id, success, msg, payload)
        except Exception as e:
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.Auth.WORKER_ERROR.format(e), {}
            )


class ForgotPasswordWorker(BaseThreadWorker):
    def __init__(self, worker_id, signals, auth_manager, email):
        super().__init__(worker_id=worker_id, signals=signals)
        self.auth_manager = auth_manager
        self.email = email

    def run(self):
        try:
            success, msg, payload = self.auth_manager.send_password_reset(self.email)
            self._signals.finished.emit(self._worker_id, success, msg, payload)
        except Exception as e:
            logger.debug("ForgotPasswordWorker: unexpected error email=%r: %r", self.email, e)
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.Auth.WORKER_ERROR.format(e), {}
            )


# ---------------------------------------------------------------------------
# Gateway workers — communicate with the Go Gateway over HTTP
# ---------------------------------------------------------------------------

class GatewayACLPreflightWorker(BaseThreadWorker):
    """Pre-flight ACL check via gateway before starting USB-to-cloud sync.

    Calls GET /api/files?mac_id=X which validates:
      - Auth (401 if token missing/expired)
      - User exists in ACL (403 if not)
      - Org access for device (403/404)

    On success emits finished(True, "", {"mac_id": ..., "log_file_names": ..., **extra}).
    On HTTP error emits finished(False, <UNAUTHORIZED|FORBIDDEN|...>, {}).
    """

    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        client,               # GatewayClient
        mac_id: str,
        log_file_names: list,
        extra: dict | None = None,
    ) -> None:
        super().__init__(worker_id=worker_id, signals=signals)
        self._client = client
        self._mac_id = mac_id
        self._log_file_names = log_file_names
        self._extra = extra or {}

    def run(self) -> None:
        try:
            cloud_files = self._client.list_files(self._mac_id)
            self._signals.finished.emit(
                self._worker_id,
                True,
                "",
                {
                    "mac_id": self._mac_id,
                    "log_file_names": self._log_file_names,
                    "cloud_files": cloud_files,
                    **self._extra,
                },
            )
        except (GatewayMfaRequiredError, GatewayForbiddenError, GatewayUnauthorizedError) as exc:
            logger.debug("GatewayACLPreflightWorker: gateway auth error mac_id=%r: %r", self._mac_id, exc)
            self._signals.finished.emit(
                self._worker_id, False, _gateway_error_msg(exc), {},
            )
        except requests.HTTPError as exc:
            logger.debug("GatewayACLPreflightWorker: HTTP error mac_id=%r: %r", self._mac_id, exc)
            self._signals.finished.emit(
                self._worker_id, False, _http_error_msg(exc), {},
            )
        except (requests.ConnectionError, requests.Timeout) as exc:
            logger.debug("GatewayACLPreflightWorker: server unreachable mac_id=%r: %r", self._mac_id, exc)
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.SERVER_UNREACHABLE, {},
            )
        except Exception as exc:
            logger.debug("GatewayACLPreflightWorker: unexpected error mac_id=%r: %r", self._mac_id, exc)
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.UNEXPECTED_ERROR.format(exc), {},
            )


def _http_error_msg(exc: requests.HTTPError, file_op: bool = False) -> str:
    """Map HTTP status code to a canonical sentinel string used by Model handlers.

    Pass file_op=True when the request was a signed-URL read/upload — a 404 there
    means the file is absent from storage, not that the device is unregistered.
    """
    status = exc.response.status_code if exc.response is not None else 0
    if exc.response is not None:
        logger.debug("HTTP %s from %s — body: %s", status, exc.response.url, exc.response.text[:500])
    if status == 401:
        return constants.GATEWAY_STATUS.UNAUTHORIZED
    if status == 403:
        return constants.GATEWAY_STATUS.FORBIDDEN
    if status == 404:
        return constants.GATEWAY_STATUS.FILE_NOT_FOUND if file_op else constants.GATEWAY_STATUS.NOT_FOUND
    return f"GATEWAY_ERROR_{status}"


def _gateway_error_msg(exc) -> str:
    """Map a typed GatewayError to the same sentinel strings used by _http_error_msg.

    GatewayClient raises GatewayError subclasses (not requests.HTTPError) for
    401 and 403 responses. Workers must catch these separately so Model handlers
    receive the expected sentinel strings rather than falling into the generic
    Exception fallback.
    """
    if isinstance(exc, GatewayMfaRequiredError):
        return constants.GATEWAY_STATUS.MFA_REQUIRED
    if isinstance(exc, GatewayUnauthorizedError):
        return constants.GATEWAY_STATUS.UNAUTHORIZED
    return constants.GATEWAY_STATUS.FORBIDDEN


class GatewayWorker(BaseThreadWorker):
    """Lists log files for a device via GET /api/files?mac_id=X.

    On success emits finished with payload={"logfilenames": [...]}.
    On HTTP error emits finished(False, <UNAUTHORIZED|FORBIDDEN|...>, {}).
    """

    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        client,               # GatewayClient
        mac_id: str,
    ) -> None:
        super().__init__(worker_id=worker_id, signals=signals)
        self._client = client
        self._mac_id = mac_id

    def run(self) -> None:
        try:
            files = self._client.list_files(self._mac_id)
            self._signals.finished.emit(
                self._worker_id, True, "", {"logfilenames": files}
            )
        except (GatewayMfaRequiredError, GatewayForbiddenError, GatewayUnauthorizedError) as exc:
            logger.debug("GatewayWorker: gateway auth error mac_id=%r: %r", self._mac_id, exc)
            self._signals.finished.emit(
                self._worker_id, False, _gateway_error_msg(exc), {}
            )
        except requests.HTTPError as exc:
            logger.debug("GatewayWorker: HTTP error mac_id=%r: %r", self._mac_id, exc)
            self._signals.finished.emit(
                self._worker_id, False, _http_error_msg(exc), {}
            )
        except (requests.ConnectionError, requests.Timeout) as exc:
            logger.debug("GatewayWorker: server unreachable mac_id=%r: %r", self._mac_id, exc)
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.USB.SERVER_UNREACHABLE, {}
            )
        except Exception as exc:
            logger.debug("GatewayWorker: unexpected error: %r", exc)
            self._signals.finished.emit(
                self._worker_id, False, constants.MSG.Cloud.FETCH_FILES_FAILED, {}
            )


class GatewaySaveFilesWorker(BaseThreadWorker):
    """Downloads one or more log files via the gateway and saves them to disk.

    Single file  → written as-is to dest_path.
    Multiple files (or dest_path ends in .zip) → saved as a ZIP archive.

    On success emits finished with payload={"src": LOG_SRC.CLOUD}.
    On HTTP error emits finished(False, <UNAUTHORIZED|FORBIDDEN|...>, {}).
    """

    def __init__(
        self,
        worker_id: int,
        signals: QObject,
        client,               # GatewayClient
        mac_id: str,
        log_file_names: List[str],
        dest_path: Path,
    ) -> None:
        super().__init__(worker_id=worker_id, signals=signals)
        self._client = client
        self._mac_id = mac_id
        self._log_file_names = log_file_names
        self._dest_path = Path(dest_path)

    def run(self) -> None:
        from .. import constants as _c   # avoid top-level circular import risk

        if not self._log_file_names:
            self._signals.finished.emit(
                self._worker_id, False, "No log files selected", {}
            )
            return

        self._dest_path.parent.mkdir(parents=True, exist_ok=True)

        try:
            if len(self._log_file_names) == 1 and self._dest_path.suffix.lower() != ".zip":
                data = self._client.read_file(self._mac_id, self._log_file_names[0])
                self._dest_path.write_bytes(data)
            else:
                with ZipFile(self._dest_path, "w", compression=ZIP_DEFLATED) as zf:
                    for name in self._log_file_names:
                        data = self._client.read_file(self._mac_id, name)
                        zf.writestr(name, data)

            self._signals.finished.emit(
                self._worker_id,
                True,
                f"{len(self._log_file_names)} Log(s) saved from cloud",
                {"src": _c.LOG_SRC.CLOUD},
            )

        except requests.HTTPError as exc:
            logger.debug("GatewaySaveFilesWorker: HTTP error mac_id=%r files=%r: %r", self._mac_id, self._log_file_names, exc)
            self._signals.finished.emit(
                self._worker_id, False, _http_error_msg(exc, file_op=True), {}
            )
        except Exception as exc:
            logger.debug("GatewaySaveFilesWorker: unexpected error mac_id=%r files=%r: %r", self._mac_id, self._log_file_names, exc)
            self._signals.finished.emit(
                self._worker_id, False, f"Download failed: {exc}",
                {"src": _c.LOG_SRC.CLOUD},
            )
