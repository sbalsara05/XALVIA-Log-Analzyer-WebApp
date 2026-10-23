# disk_cache — File-Based Log Cache

## Overview

`disk_cache.py` prevents re-reading large files from USB serial within a session. It is a simple read-through cache keyed by `(source, log_file_name)` that stores raw bytes on disk in `QStandardPaths.CacheLocation`.

---

## `CacheKey`

Frozen dataclass used as the cache key:

```python
@dataclass(frozen=True)
class CacheKey:
    source: str           # e.g. "usb", "cloud", "disk"
    log_file_name: str    # e.g. "10352920_test_log.txt"
```

Before writing to disk, the key is sanitised to a filesystem-safe filename (replacing characters that are illegal on Windows/macOS).

---

## `DiskCache`

### Constructor

```python
DiskCache(cache_dir: str | None = None)
```

If `cache_dir` is `None`, resolves to `QStandardPaths.writableLocation(QStandardPaths.CacheLocation) / "xaliva_logs"`. If the directory cannot be created, `_enabled` is set to `False` and all operations become no-ops.

### Properties

| Property | Type | Description |
|----------|------|-------------|
| `root` | `Path` | Absolute path to the cache directory |
| `is_enabled` | `bool` | `False` if the cache location is unwritable |

### Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `put_bytes` | `(key: CacheKey, data: bytes) -> None` | Write bytes to `root/{sanitised_key}` |
| `get_bytes` | `(key: CacheKey) -> bytes` | Read and return cached bytes; raises `KeyError` if not cached |
| `get_text` | `(key: CacheKey, encoding="utf-8") -> str` | `get_bytes` decoded as text |
| `has` | `(key: CacheKey) -> bool` | Returns `True` if a cache file exists for the key |
| `clear` | `() -> None` | Deletes all files in `root`; called by `Model.cleanup()` on shutdown |

---

## `read_log_using_cache`

Top-level helper function used by workers:

```python
def read_log_using_cache(
    cache: DiskCache,
    key: CacheKey,
    fetch_fn: Callable[[], bytes]
) -> bytes
```

Cache-aside pattern:
1. `cache.has(key)` → if `True`, return `cache.get_bytes(key)`.
2. Otherwise call `fetch_fn()` (e.g. `usb_conn.read_log_file(filename)`).
3. `cache.put_bytes(key, result)`.
4. Return the fetched bytes.

---

## Eviction

There is **no automatic eviction**. Cache files accumulate until one of:
- `cache.clear()` is called (done by `Model.cleanup()` on application shutdown).
- The user manually clears the OS cache directory.

Files that are always re-read (device log, `track.txt`) bypass the cache via `is_always_fresh()` in `src/utils.py`.

---

## Disabled State

If `QStandardPaths.CacheLocation` is unwritable (rare on desktop systems), `_enabled = False`. All `put_bytes`, `get_bytes`, `has`, and `clear` calls become silent no-ops — the application works correctly but reads from USB on every request.

---

## Gotchas

- Cache files survive across app restarts within the same OS session. If a device's file changes (e.g. after a new test run), the stale cache file will be returned until `Model.cleanup()` clears it.
- The cache is not shared across device MAC IDs; the `source` field in `CacheKey` is used to namespace entries (e.g. `"usb"` vs `"cloud"`).

---

## Related Files

- [`src/workflows/README.md`](../workflows/README.md) — workers that call `read_log_using_cache`
- [`src/README.md`](../README.md) — full startup sequence and module map
