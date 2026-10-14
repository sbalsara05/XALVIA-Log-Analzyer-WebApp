# src/main.py
import argparse
import os
import sys
import tempfile
from pathlib import Path
import logging
from datetime import datetime
from traceback import format_exception

# Must be set before QApplication is created to prevent Windows native style
# from overriding QML Quick Controls colors and appearances.
os.environ.setdefault("QT_QUICK_CONTROLS_STYLE", "Basic")

from PySide6.QtWidgets import QApplication
from PySide6.QtQml import QQmlApplicationEngine
from PySide6.QtGui import QIcon
from PySide6.QtCore import QStandardPaths

from .mvc.model           import Model
from .mvc.bridge          import Bridge
from .mvc.controller      import ApplicationController
from .login.auth_manager  import AuthManager
from .login.login_controller import LoginController

from . import constants

logger = logging.getLogger(__name__)


def get_base_path() -> Path:
    """Get absolute path to directory where logo, style, and theme dirs are located."""
    if hasattr(sys, "_MEIPASS"):
        return Path(sys._MEIPASS).resolve()
    return Path(__file__).parent.parent.resolve()


def _is_writable(path: Path) -> bool:
    """Create the directory (if needed) and verify we can actually write to it."""
    try:
        path.mkdir(parents=True, exist_ok=True)
        probe = path / ".xaliva_write_test"
        probe.write_text("ok")
        probe.unlink()
        return True
    except OSError:
        return False


def get_directories() -> tuple[Path, Path]:
    """Return (hidden_app_data, user_workspace) paths.

    Must be called AFTER QApplication creation and setApplicationName/setOrganizationName.

    hidden_app_data  — temp staging dirs, internal state (never shown to user)
    user_workspace   — error traces, saved log files, exports (user browses this)
    """
    parser = argparse.ArgumentParser()
    parser.add_argument("--workspace", type=str,
                        help="Path for user-facing logs and traces")
    args, _ = parser.parse_known_args()

    hidden_app_data = Path(
        QStandardPaths.writableLocation(QStandardPaths.StandardLocation.AppLocalDataLocation))
    if not _is_writable(hidden_app_data):
        hidden_app_data = Path(tempfile.gettempdir()) / constants.APP_NAME
        hidden_app_data.mkdir(parents=True, exist_ok=True)
        logger.warning("AppLocalDataLocation not writable, falling back to %s", hidden_app_data)

    docs = Path(QStandardPaths.writableLocation(QStandardPaths.StandardLocation.DocumentsLocation))
    default_workspace = docs / f"{constants.APP_NAME}_Workspace"

    if args.workspace:
        user_workspace = Path(args.workspace)
        if not _is_writable(user_workspace):
            logger.warning("--workspace %s is not writable, falling back to %s",
                           user_workspace, default_workspace)
            user_workspace = default_workspace
    else:
        user_workspace = default_workspace

    if not _is_writable(user_workspace):
        user_workspace = Path(tempfile.gettempdir()) / f"{constants.APP_NAME}_Workspace"
        user_workspace.mkdir(parents=True, exist_ok=True)
        logger.warning("Documents not writable, falling back to %s", user_workspace)

    return hidden_app_data, user_workspace


def global_exception_hook(exctype, value, tb) -> None:
    """Catches unhandled exceptions and logs them."""
    error_message = "".join(format_exception(exctype, value, tb))
    logging.error("An unhandled exception occurred:\n%s", error_message)
    sys.__excepthook__(exctype, value, tb)


def main() -> None:
    base_path = get_base_path()

    # --- Phase 1: console-only logging (before QApp) ---
    root_logger = logging.getLogger()
    root_logger.setLevel(logging.DEBUG)
    if not root_logger.handlers:
        console_handler = logging.StreamHandler(sys.stdout)
        console_handler.setLevel(logging.DEBUG)
        console_handler.setFormatter(logging.Formatter("%(levelname)s: %(message)s"))
        root_logger.addHandler(console_handler)
    sys.excepthook = global_exception_hook

    logger.debug("Logging configured (console only). base_path=%s", base_path)

    QML_PATH = base_path / "style" / "MainWindow.qml"
    logger.debug("Paths resolved — QML=%s (exists=%s)", QML_PATH, QML_PATH.exists())

    # ------------------------------------------------------------------
    # QApplication — superset of QGuiApplication; required for QFileDialog
    # ------------------------------------------------------------------
    app = QApplication(sys.argv)
    app.setApplicationName(constants.APP_NAME)
    app.setOrganizationName(constants.APP_AUTHOR)
    app_icon_path = base_path / constants.SVG_APP_ICON_DIR_STUB
    app.setWindowIcon(QIcon(str(app_icon_path)))
    logger.debug("QApplication created, window icon set to %s", app_icon_path)

    # --- Phase 2: resolve data directories and add file logging ---
    hidden_dir, workspace_dir = get_directories()

    ERROR_TRACE = workspace_dir / "error_trace"
    ERROR_TRACE.mkdir(exist_ok=True)
    if not any(isinstance(h, logging.FileHandler) for h in root_logger.handlers):
        ts = datetime.now().strftime("%Y-%m-%d_%H-%M-%S")
        file_handler = logging.FileHandler(ERROR_TRACE / f"{ts}.log", mode="a")
        file_handler.setLevel(logging.DEBUG)
        file_handler.setFormatter(
            logging.Formatter("%(asctime)s - %(name)s - %(levelname)s - %(message)s"))
        root_logger.addHandler(file_handler)

    logger.debug("Directories resolved — workspace=%s hidden=%s", workspace_dir, hidden_dir)

    LOGS_DIR = workspace_dir / "logs"
    LOGS_DIR.mkdir(exist_ok=True)

    TEMP_DIR = hidden_dir / "temp"
    TEMP_DIR.mkdir(exist_ok=True)

    model_obj = None
    try:
        logger.debug("Creating AuthManager…")
        auth_manager  = AuthManager()
        logger.debug("AuthManager created")

        logger.debug("Creating Model…")
        model_obj     = Model(
            LOGS_DIR=LOGS_DIR,
            TEMP_DIR=TEMP_DIR,
            auth_manager=auth_manager,
            baudrate=constants.BAUDRATE,
            timeout=constants.TIMEOUT,
        )
        logger.debug("Model created")

        bridge        = Bridge()
        logger.debug("Bridge created")

        login_ctrl    = LoginController(bridge=bridge, model=model_obj)
        logger.debug("LoginController created")

        _controller   = ApplicationController(
            bridge=bridge,
            model=model_obj,
            login_controller=login_ctrl,
        )
        logger.debug("ApplicationController created — all signals wired")

        # ------------------------------------------------------------------
        # QML engine — pass bridge before load so QML sees it immediately
        # ------------------------------------------------------------------
        engine = QQmlApplicationEngine()
        logger.debug("QQmlApplicationEngine created")

        # Pipe QML warnings/errors into our logger (they default to stderr only)
        def _on_qml_warnings(warnings):
            for w in warnings:
                logger.warning("QML warning: %s  [%s line %d]",
                               w.description(), w.url().toString(), w.line())

        engine.warnings.connect(_on_qml_warnings)
        logger.debug("QML warnings handler connected")

        logger.debug("Setting initial QML property: bridge=%r", bridge)
        engine.setInitialProperties({"bridge": bridge,
                                     "xaliva_logo": (base_path / constants.COMPANY_LOGO_DIR_STUB).as_uri()})

        logger.debug("Loading QML file: %s", QML_PATH)
        engine.load(str(QML_PATH))
        logger.debug("engine.load() returned — rootObjects count: %d", len(engine.rootObjects()))

        if not engine.rootObjects():
            logging.error("Failed to load QML: %s", QML_PATH)
            sys.exit(1)

        root = engine.rootObjects()[0]
        logger.debug("QML root object obtained: %r", root)

        bridge.set_root(root)
        logger.debug("Bridge root set")

        bridge.show_login()           # always require login on startup
        logger.debug("Login overlay shown on startup")

        logger.debug("Entering Qt event loop (app.exec())")
        sys.exit(app.exec())

    finally:
        if model_obj is not None:
            model_obj.cleanup()


if __name__ == "__main__":
    main()
