"""Unit tests for USBSerialConnection.detect_ports.

The XALVIA device is found by its USB VID/PID, not by the driver-supplied
port description, so detection works whatever driver or OS language is in use.
"""
from unittest.mock import patch

import pytest
from serial.tools.list_ports_common import ListPortInfo

from src.usb_conn.usbserialconnection import USBSerialConnection


def _port(device, vid=None, pid=None, description="n/a"):
    info = ListPortInfo(device, skip_link_detection=True)
    info.vid = vid
    info.pid = pid
    info.description = description
    return info


@pytest.fixture
def conn():
    return USBSerialConnection()


def _detect(conn, ports):
    with patch("src.usb_conn.usbserialconnection.list_ports.comports", return_value=ports):
        return conn.detect_ports()


@pytest.mark.parametrize("description", [
    "Silicon Labs CP210x USB to UART Bridge (COM3)",
    "CP2102N USB to UART Bridge Controller",
    "USB Serial Device (COM3)",  # generic/other driver
    "Pont USB vers UART CP210x (COM3)",  # localized Windows
])
def test_detects_cp210x_regardless_of_description(conn, description):
    ports = [_port("COM3", vid=0x10C4, pid=0xEA60, description=description)]
    assert _detect(conn, ports) == ["COM3"]


def test_ignores_other_usb_serial_devices(conn):
    ports = [
        _port("COM1"),  # built-in, no USB ids
        _port("COM4", vid=0x0403, pid=0x6001, description="FTDI"),
        _port("COM5", vid=0x10C4, pid=0xEA70, description="CP2105"),
    ]
    assert _detect(conn, ports) == []


def test_ignores_non_usb_port_named_like_cp210x(conn):
    ports = [_port("COM9", description="CP210x USB to UART Bridge")]
    assert _detect(conn, ports) == []


def test_returns_all_matching_ports_in_order(conn):
    ports = [
        _port("/dev/ttyUSB0", vid=0x10C4, pid=0xEA60),
        _port("/dev/ttyS0"),
        _port("/dev/ttyUSB1", vid=0x10C4, pid=0xEA60),
    ]
    assert _detect(conn, ports) == ["/dev/ttyUSB0", "/dev/ttyUSB1"]
