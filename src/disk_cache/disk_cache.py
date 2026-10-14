# src/cache.py
from __future__ import annotations
from dataclasses import dataclass
from pathlib import Path
from typing import Optional
from PySide6.QtCore import QStandardPaths
import shutil, logging
from threading import Lock
from ..usb_conn import usbserialconnection
logger = logging.getLogger(__name__)

_EXCEPTIONS=(OSError, PermissionError)

@dataclass(frozen=True)
class CacheKey:
    source: str   # "usb" | "cloud"
    log_file_name: str     # filename (with extension)

class DiskCache:
    """
    Disk-based cache. Each item is a file on disk:
      <cache_dir>/<source>_<log_file_name>
    """
    def __init__(self, app_subdir: str = "XALIVA_Cache"):
        self._cache_dir: Optional[Path] = None
        self._enabled = False
        self._lock = Lock()

        # 1. Check if a valid cache location can be found.
        base_location = QStandardPaths.writableLocation(QStandardPaths.CacheLocation)
        if not base_location:
            logger.warning("[DiskCache] Could not find a writable cache location. Cache is disabled.")
            return

        self._cache_dir = Path(base_location) / app_subdir

        # 2. If the directory already exists, try to delete it.S
        if self._cache_dir.exists():
            try:
                shutil.rmtree(self._cache_dir)
            except _EXCEPTIONS as e:
                logger.error(f"[DiskCache] Failed to clear existing cache directory '{self._cache_dir}'. Cache is disabled. Reason: {e}")
                self._cache_dir = None
                return

        # 3. Create the new, empty directory.
        try:
            self._cache_dir.mkdir(parents=True, exist_ok=False)
        except _EXCEPTIONS as e:
            logger.error(f"[DiskCache] Failed to create cache directory '{self._cache_dir}'. Cache is disabled. Reason: {e}")
            self._cache_dir = None
            return
        
        # If all steps succeeded, enable the cache.
        self._enabled = True
        logger.info(f"[DiskCache] Cache enabled at: {self._cache_dir}")

    @property
    def is_enabled(self) -> bool:
        """Returns True if the cache was successfully initialized."""
        return self._enabled
    @property
    def root(self) -> Optional[Path]:
        return self._cache_dir


    def _path_for(self, key: CacheKey) -> Path:
        if not self.is_enabled or not self._cache_dir:
            return None
        safe_name  = key.log_file_name.replace("/", "_").replace("\\", "_")
        safe_src   = key.source.replace("/", "_").replace("\\", "_")
        return self._cache_dir / f"{safe_src}__{safe_name}"

    def put_bytes(self, key: CacheKey, data: bytes) -> None:
        with self._lock:
            p = self._path_for(key)
            if not p:
                return
            try:
                p.write_bytes(data)
            except _EXCEPTIONS as e:
                logger.warning(f"[DiskCache] Write failed for {p}: {e}")

    def get_bytes(self, key: CacheKey) -> Optional[bytes]:
        with self._lock:
            p = self._path_for(key)
            if not p or not p.exists():
                return None
            try:
                return p.read_bytes()
            except _EXCEPTIONS as e:
                logger.warning(f"[DiskCache] Read failed for {p}: {e}")
            return None

    def get_text(self, key: CacheKey, encoding: str = "utf-8") -> Optional[str]:
        b = self.get_bytes(key)
        return b.decode(encoding, errors="replace") if b is not None else None

    def has(self, key: CacheKey) -> bool:
        with self._lock:
            p = self._path_for(key)
            return p.exists() if p else False

    def clear(self) -> None:
        """Deletes all cached files and the directory itself."""
        with self._lock:
            if not self.is_enabled or not self._cache_dir:
                return
            try:
                if self._cache_dir.exists():
                    shutil.rmtree(self._cache_dir)
                    # After clearing, re-create the root directory for this session
                    self._cache_dir.mkdir(parents=True, exist_ok=True)
            except _EXCEPTIONS as e:
                logger.error(f"[DiskCache] Clear failed for {self._cache_dir}: {e}")

def read_log_using_cache(conn_mgr: usbserialconnection, device_log_file_name: str, cache_key: CacheKey, disk_cache: Optional[DiskCache] = None) -> bytes:
    cached_data = None
    if disk_cache is not None and cache_key is not None:
        cached_data = disk_cache.get_bytes(cache_key)

    device_log_data = None
    if cached_data:
        device_log_data = cached_data
        logger.debug(f"Fetched {device_log_file_name} from cache")
    else:
        device_log_data = conn_mgr.read_log_file(device_log_file_name)
        ## put the data in cache
        if (
            device_log_data
            and disk_cache is not None
            and cache_key is not None
        ):
            logger.debug(f"Saving {device_log_file_name} to cache")
            disk_cache.put_bytes(cache_key, device_log_data)
    return device_log_data
