# -*- mode: python ; coding: utf-8 -*-
# PyInstaller spec — onedir + noconsole, PySide6/QML, Windows
import sys
from pathlib import Path

project_root = Path(SPECPATH)

# ---------------------------------------------------------------------------
# Collect all QML files preserving sub-directory structure (components/, logic/)
# ---------------------------------------------------------------------------
qml_datas = []
style_root = project_root / "style"
for qml_file in style_root.rglob("*"):
    if qml_file.is_file():
        rel_dir = qml_file.parent.relative_to(project_root)
        qml_datas.append((str(qml_file), str(rel_dir)))

# ---------------------------------------------------------------------------
# Other data files
# ---------------------------------------------------------------------------
extra_datas = [
    (str(project_root / "logo" / "RICOVR_LOGO.ico"),  "logo"),
    (str(project_root / "logo" / "RICOVR_LOGO.png"),  "logo"),
    (str(project_root / "logo" / "RICOVR_LOGO.svg"),  "logo"),
    (str(project_root / "logo" / "RICOVR.svg"),        "logo"),
]

all_datas = qml_datas + extra_datas

# ---------------------------------------------------------------------------
# Hidden imports — PySide6 modules PyInstaller commonly misses for QML apps
# ---------------------------------------------------------------------------
hidden_imports = [
    # Qt Quick / QML engine
    "PySide6.QtQml",
    "PySide6.QtQuick",
    "PySide6.QtQuickControls2",
    "PySide6.QtQuickWidgets",
    # Core Qt modules used at runtime
    "PySide6.QtCore",
    "PySide6.QtGui",
    "PySide6.QtWidgets",
    "PySide6.QtNetwork",
    "PySide6.QtSvg",
    "PySide6.QtSvgWidgets",
    "PySide6.QtOpenGL",
    "PySide6.QtOpenGLWidgets",
    # Serial
    "serial",
    "serial.tools",
    "serial.tools.list_ports",
    # Other runtime deps
    "qtawesome",
    "qrcode",
    "qrcode.image.pure",
    "qrcode.image.styledpil",

    "requests",
]

a = Analysis(
    ["run.py"],
    pathex=[str(project_root)],
    binaries=[],
    datas=all_datas,
    hiddenimports=hidden_imports,
    hookspath=[],
    hooksconfig={},
    runtime_hooks=[],
    excludes=[
        "tkinter",
        "matplotlib",
        "IPython",
        "notebook",
        "pytest",
        "pytest_qt",
    ],
    noarchive=False,
    optimize=0,
)

pyz = PYZ(a.pure)

exe = EXE(
    pyz,
    a.scripts,
    [],
    exclude_binaries=True,      # required for onedir — COLLECT handles binaries
    name="log_analyzer",
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    upx=True,
    console=False,              # windowed / --noconsole
    disable_windowed_traceback=False,
    argv_emulation=False,
    target_arch=None,
    codesign_identity=None,
    entitlements_file=None,
    icon=str(project_root / "logo" / "RICOVR_LOGO.ico"),
)

# ---------------------------------------------------------------------------
# COLLECT — onedir output (dist/log_analyzer/)
# LGPL compliance: Qt shared libraries remain as separate DLLs, satisfying
# the dynamic-linking requirement for PySide6's LGPL license.
# ---------------------------------------------------------------------------
coll = COLLECT(
    exe,
    a.binaries,
    a.datas,
    strip=False,
    upx=True,
    upx_exclude=[
        # UPX can corrupt Qt DLLs; exclude them
        "Qt6*.dll",
        "PySide6/*.pyd",
        "shiboken6/*.pyd",
    ],
    name="log_analyzer",
)

# ---------------------------------------------------------------------------
# BUNDLE — macOS only: wrap the onedir output in dist/log_analyzer.app so it
# can be launched from Finder. PyInstaller converts the .ico to .icns (Pillow).
# ---------------------------------------------------------------------------
if sys.platform == "darwin":
    app = BUNDLE(
        coll,
        name="log_analyzer.app",
        icon=str(project_root / "logo" / "RICOVR_LOGO.ico"),
        bundle_identifier="com.ricovr.xalivalog",
    )
