from __future__ import annotations
from pathlib import Path
import shutil, logging, threading
from typing import List, Optional
from PySide6.QtCore import QObject, QRunnable, Signal, Slot
from serial import Serial, SerialException, SerialTimeoutException
from serial.tools import list_ports
from platform import system
from time import sleep 

from .. import constants

logger = logging.getLogger(__name__)


class USBSerialConnection(QObject):
    """
    Class to talk to XALIVA device over USB Serial connection
    """


    def __init__(
        self,
        parent=None
    ) -> None:
        super().__init__(parent)
        self.serial_port=Serial()
        self._cancel=False
        self._io_lock = threading.Lock()
    
    def _reset_cpu_buffers(self):
    
        ## Clearing ipput and output buffers of the host machine/PC
        self.serial_port.reset_input_buffer() 
        self.serial_port.reset_output_buffer()
        

    def _pre_command(self):
        self._reset_cpu_buffers()        

        ## sending CR LF to activate ESP 32
        self.serial_port.write(b"\r\n")
        sleep(0.5)

    def is_open_bool(self)-> bool:
        return self.serial_port and self.serial_port.is_open
    
    def detect_ports(self) -> list:
        """
        Check available serial ports
        """
        ports = list_ports.comports()
        filtered_ports = []
        for port in ports:
            if system() == 'Windows':
                if 'CP210x USB to UART Bridge' in port.description:
                    filtered_ports.append(port.device)
            else:
                if 'CP2102N USB to UART Bridge Controller' in port.description:
                    filtered_ports.append(port.device)
        return filtered_ports
    
    def connect_to_port(self, port_device: str, baudrate:int, timeout:float)->bool:
        self.close_port()
        self._cancel = False
        self.serial_port = Serial(port_device, baudrate = baudrate, timeout=timeout)
        self._pre_command()

        return True


    def cancel_io(self) -> None:
        """Signal any in-progress serial read loops to stop."""
        self._cancel = True

    def close_port(self):
        if self.serial_port and self.serial_port.is_open:
            self.serial_port.close()
            logger.debug(f"Serial port is now closed: {not self.serial_port.is_open}")


    def list_all_log_files(self,mac_id: str=None)->list:
        """Send 'file_list' and parse *.txt names delimited by CR CR LF."""
        # with self._open_port() as ser:
        if not(self.serial_port and self.serial_port.is_open):
            return []
        with self._io_lock:
            self._reset_cpu_buffers()


            command = b"file_list\r\n"
            self.serial_port.write(command)

            logs = []
            org_timeout = self.serial_port.timeout
            self.serial_port.timeout = 3

            nb_log_files = 0

            while not self._cancel: ## force stop when application emits aboutToQuit
                data = self.serial_port.read_until(b"\r\r\n")
                if not data:                 # timeout or end-of-stream
                    break

                # strip leading 0x0d (CR) and trailing "\r\r\n"
                decoded = data[1:-3].decode(errors="ignore")
                for item in decoded.split():
                    if item.endswith(".txt"):
                        logs.append(item)
                        nb_log_files += 1

            self.serial_port.timeout = org_timeout

        return logs

    def read_log_file(self, log_file_name: str) -> bytes:
        with self._io_lock:

            self._reset_cpu_buffers()
            # with self._open_port() as ser:
            logger.debug(f"Attempting read {log_file_name} from serial {self.serial_port}")
            command = b"file_read " + log_file_name[:-4].encode(encoding='utf-8') + b" \n" ## bytes in python is ASCII and it is compatible with UTF-8
            self.serial_port.write(command)

            log_file_data = bytearray()
            reading = False
            org_timeout = self.serial_port.timeout
            self.serial_port.timeout = 3
            starting_index_text = f"data = {log_file_name[:-4]}".encode('utf-8')
            ending_index_text = b"XALIVA>"
            while not (self._cancel):
                data = self.serial_port.readline().strip()
                start_index = data.find(starting_index_text)
                end_index = data.find(ending_index_text)
                if start_index >= 0:
                    reading = True

                    beg = start_index + len(starting_index_text)
                    if beg == len(data):
                        continue
                    else:
                        log_file_data.extend(data[beg:] + b'\n')

                elif end_index >= 0 and reading:
                    if end_index > 0:
                        log_file_data.extend(data[:end_index] + b'\n')
                    break

                elif reading:
                    log_file_data.extend(data + b'\n')

            self.serial_port.timeout = org_timeout

        logger.debug(f"\t\tlog file data is  {bytes(log_file_data)[:5]}")
        return None if self._cancel else bytes(log_file_data)
        



