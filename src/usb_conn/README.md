# usb_conn — USB Serial Communication

## Overview

`usbserialconnection.py` wraps pyserial to communicate with XALIVA devices over a CP2102 USB-to-UART bridge. All I/O is protected by a threading lock so multiple worker threads can safely call into the same connection object.

---

## Device Identification

`detect_ports()` calls `serial.tools.list_ports.comports()` and filters to ports whose USB VID/PID match the Silicon Labs CP2102/CP2102N bridge (`constants.USB_VID` / `constants.USB_PID`, `10C4:EA60`). The driver-supplied `description` is not used because it varies by driver and OS language. The filtered list of port names (e.g. `"COM3"`, `"/dev/ttyUSB0"`) is returned to the caller.

---

## Serial Parameters

| Parameter | Value | Source |
|-----------|-------|--------|
| Baud rate | `115200` | `src/constants.py BAUDRATE` |
| Read timeout | `1.0 s` | `src/constants.py TIMEOUT` |
| Write timeout | `1.0 s` | `src/constants.py TIMEOUT` |
| Encoding | `latin-1` | Used for binary-safe decode |

---

## Protocol Reference

| Operation | Command sent | Response terminator | Notes |
|-----------|-------------|---------------------|-------|
| List files | `file_list\r\n` | `\r\r\n` | Returns `.txt` filenames, one per line |
| Read file | `file_read <name>\n` | `XALIVA>` | Binary-safe; file content may be large |
| Connect flush | `\r\n` | — | Sent immediately on `connect_to_port()` to clear stale serial buffer |

---

## `USBSerialConnection` API

```python
class USBSerialConnection(QObject):
```

### Constructor

```python
USBSerialConnection()
```

Initialises an unconnected `serial.Serial` object and a `threading.Lock` (`_io_lock`).

### Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `detect_ports` | `() -> list[str]` | Returns list of CP210x port names |
| `connect_to_port` | `(port: str) -> None` | Opens serial at 115200 baud; sends `\r\n` flush |
| `disconnect` | `() -> None` | Calls `close_port()` |
| `close_port` | `() -> None` | Closes the serial port if open |
| `is_open_bool` | `() -> bool` | Returns `True` if port is currently open |
| `list_all_log_files` | `() -> list[str]` | Sends `file_list`, reads until `\r\r\n`, returns filenames |
| `read_log_file` | `(filename: str) -> bytes` | Sends `file_read <filename>`, reads until `XALIVA>`, returns raw bytes |
| `cancel_io` | `() -> None` | Sets `_cancel = True`; active read loops check this flag |

### Thread Safety

All `list_all_log_files` and `read_log_file` calls acquire `_io_lock` before sending a command. This prevents interleaving when `SaveAllUSBSerialWorker` runs concurrent saves.

---

## Cancel Pattern

Workers that loop over multiple files (e.g. `SaveAllUSBSerialWorker`) check `_cancel` between files:

```python
def run(self):
    for filename in self.files:
        if self._cancel:
            break
        data = self.usb_conn.read_log_file(filename)
        ...
```

`cancel_io()` is called by `Model.cleanup()` on shutdown. After cancel, `close_port()` must still be called before process exit.

---

## Gotchas

- `close_port()` **must** be called before process exit to release the OS COM port handle. Failure to do so may leave the port locked until the OS detects the dead handle (varies by platform).
- `pyserial` raises `serial.SerialException` on physical disconnect (cable unplugged mid-read). Workers catch this via the `USB_FATALS` tuple in `src/constants.py`.
- On Windows, port names are `"COM3"` style; on Linux/macOS they are `"/dev/ttyUSB0"` style. The protocol is identical.
- The `XALIVA>` prompt terminator for `read_log_file` is decoded as `latin-1` to handle arbitrary byte values in binary log data.

---

## Related Files

- [`src/workflows/README.md`](../workflows/README.md) — USB workers that call these methods
- [`src/constants.py`](../constants.py) — `BAUDRATE`, `TIMEOUT`, `USB_FATALS`
- [`src/README.md`](../README.md) — startup sequence and module map
