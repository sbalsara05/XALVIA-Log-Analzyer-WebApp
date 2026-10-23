from typing import Set, Callable
from pathlib import Path
import logging
import os
import shutil
import re 



logger = logging.getLogger(__name__)

DEVICELOG_RE = re.compile("^[0-9]+_devicelog.txt$")

def is_memory_reflash(
    read_src_a: Callable, log_files_a:set, read_src_b: Callable, log_files_b: set, 
) -> bool:
    # Step 2.1 If mac_id directory exists, get intersection of file names listed on GUI and file names saved in the existing dir

    intersection = log_files_a & log_files_b
    logger.debug("intersection of files is %s", intersection)
    if not intersection:
        # no common reference file => no basis for comparison
        return False
    
    else:
        reference_file = intersection.pop()

        data_src_a = read_src_a(reference_file)
        data_src_b = read_src_b(reference_file)

        return data_src_a.replace(b"\r", b"").replace(b"\n", b"") != data_src_b.replace(b"\r", b"").replace(b"\n", b"")

def is_always_fresh(fname: str) -> bool:
    """Return True for log files that are always re-read/re-uploaded and never
    used as a memory-reflash reference.

    Matches:
      - any file whose name contains the keyword ``devicelog`` (e.g. ``10247072_devicelog.txt``)
      - endswith("track.txt")
    """
    return "devicelog" in fname or fname.endswith("track.txt")


def is_devicelog_fname(fname: str) -> bool:
    return True if DEVICELOG_RE.match(fname) else False


def disk_test_log_files(disk_dir: Path, mac_id: str) -> Set:
    on_disk_files = {
        file.name
        for file in disk_dir.iterdir()
        if file.name.startswith(f"{mac_id}_")
        and not is_always_fresh(file.name)
        and file.is_file()
    }
    return on_disk_files


def save_file_helper(
        log_filename: str, log_file_data: bytes, save_path: Path
    ) -> None:
        save_path.mkdir(exist_ok=True)
        save_path = save_path / log_filename
        with open(save_path.as_posix(), "wb") as file:
            file.write(log_file_data)


def move_files_no_overwrite(source_dir: Path, destination_dir: Path) -> None:
    """
    Moves all files and subdirectories from a source directory to a destination,
    skipping any items that already exist in the destination. Deletes the
    source directory afterward only if it has become empty.

    Args:
        source_dir: A pathlib.Path object for the source directory.
        destination_dir: A pathlib.Path object for the destination directory.
    """
    # --- 1. Pre-flight checks and setup ---
    if not source_dir.is_dir():
        print(f"Error: Source '{source_dir}' is not a valid directory.")
        return

    print(f"Moving items from '{source_dir}' to '{destination_dir}'...")

    # --- 2. Iterate and move items ---
    for source_item in source_dir.iterdir():
        destination_item = destination_dir / source_item.name

        # Check if an item with the same name already exists in the destination
        if (not "device" in source_item.name) and destination_item.exists():
            print(f"- Skipping '{source_item.name}', it already exists.")
            continue

        # If it doesn't exist, move the item
        shutil.move(source_item.as_posix(), destination_item.as_posix())
        print(f"- Moved '{source_item.name}'.")


def safe_del_dir(dir: Path) -> bool:
    """Delete a directory and its contents. Returns True on success or if not found."""
    try:
        if dir.is_dir():
            shutil.rmtree(dir)
        return True
    except FileNotFoundError:
        return True
    except (PermissionError, OSError):
        return False


def mac_from_filename(name: str) -> str:
    """Extract the MAC ID prefix from a log filename (e.g. 'AABBCC_...' -> 'AABBCC')."""
    i = name.find("_")
    return name[:i] if i != -1 else ""


def enrich_plot_spec(plot_spec: dict, log_file_name: str, source: str) -> None:
    """Inject display metadata and axis ranges into plot_spec in-place.

    Called in the worker thread — pure data transforms, no Qt GUI objects.
    """
    plot_spec["_file_name"] = os.path.basename(log_file_name)
    plot_spec["_source"] = source

    series_list = plot_spec.get("series", [])
    x_values = plot_spec.get("x_values") or []
    all_y = [s.get("data", []) for s in series_list]
    ref_y = all_y[0] if all_y else []
    x = x_values if x_values else list(range(len(ref_y)))
    flat_y = [v for y in all_y for v in y]

    if x and flat_y:
        y_range = max(flat_y) - min(flat_y)
        padding = y_range * 0.05 if y_range > 0 else 1.0
        plot_spec["x_range"] = [min(x), max(x)]
        plot_spec["y_range"] = [min(flat_y) - padding, max(flat_y) + padding]