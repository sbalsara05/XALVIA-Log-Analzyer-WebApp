from dataclasses import dataclass


@dataclass
class BaseListAllParams: # A base class for all param objects
    pass

@dataclass
class CloudListAllParams(BaseListAllParams):
    mac_id: str

@dataclass
class USBListAllParams(BaseListAllParams):
    pass 

@dataclass
class BaseReadLogParams:
    """Common fields for *read_log_file* operations."""

    log_file_name: str                      # "log_20250710_1523.txt", etc.

@dataclass
class CloudReadLogParams(BaseReadLogParams):
    """Read one file from GCS to a local directory."""

    mac_id: str
    target_dir: Path                        # where to download the blob

@dataclass
class USBReadLogParams(BaseReadLogParams):
    """Read one file over the serial connection."""
    # No extra fields required for USB at the moment, but we keep the child
    # class in case we later add chunk‑size, checksum, etc.
    pass