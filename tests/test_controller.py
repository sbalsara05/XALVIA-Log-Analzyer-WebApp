# tests/test_usb_detect.py
from pathlib import Path
import pytest
from PySide6.QtWidgets import QMessageBox
from PySide6.QtCore import Qt

# ---- import your real application classes ----
from src.view import View
from src.model import Model
from src.controller import ApplicationController
# ---------- reusable fixture: build MVC but monkey-patch detect_ports ---------- #
@pytest.fixture
def mvc(monkeypatch, qtbot, tmp_path):
    """
    Build the real View / Model / Controller graph but intercept the
    model.detect_ports() call so we can inject canned results without
    starting a real QRunnable or touching serial hardware.
    """
    LOGS_DEFAULT = tmp_path / "logs"
    LOGS_DEFAULT.mkdir()
    CLOUD_KEY = "dummy"

    view = View(
        STYLE_PATH=Path(__file__),  # dummy, unused in tests
        APP_ICON=Path(__file__),
        COMPANY_LOGO=Path(__file__),
        LOGS_DEFAULT=LOGS_DEFAULT,
    )
    model = Model(LOGS_DEFAULT=LOGS_DEFAULT, CLOUD_KEY=CLOUD_KEY)
    ctl   = ApplicationController(view_obj=view, model_obj=model)

    qtbot.addWidget(view)      # let pytest-qt manage cleanup
    view.show()                # needed so widgets have sizes

    return view, model, ctl, qtbot, monkeypatch




# ══════════════════════════════  TC-001  ══════════════════════════════ #
def test_detect_ports_success(mvc):
    view, model, ctl, qtbot, mp = mvc

    fake_ports = ["COM3", "COM4"]

    # Monkey-patch the model.detect_ports so that it immediately emits
    # portsDetected(list[str]) synchronously.
    def fake_detect():
        model.portsDetected.emit(fake_ports)

    mp.setattr(model, "detect_ports", fake_detect)

    # Click the Detect button
    qtbot.mouseClick(view.btn_detect, Qt.LeftButton)

    # Wait until combo count becomes len(fake_ports)
    qtbot.waitUntil(lambda: view.combo_ports.count() == len(fake_ports), timeout=500)

    # Assertions
    assert [view.combo_ports.itemText(i) for i in range(view.combo_ports.count())] == fake_ports
    assert view.combo_ports.isEnabled()
    assert view.statusBar().currentMessage() == "Detected 2 port(s)"


# ══════════════════════════════  TC-002  ══════════════════════════════ #
def test_detect_ports_none(mvc):
    view, model, ctl, qtbot, mp = mvc

    # Fake detect emits an empty list
    mp.setattr(model, "detect_ports", lambda: model.portsDetected.emit([]))

    # monkey-patch QMessageBox.critical to flag that it was called
    called = {"flag": False}

    def fake_critical(*args, **kwargs):
        called["flag"] = True
        return QMessageBox.Ok

    mp.setattr(QMessageBox, "critical", fake_critical)

    qtbot.mouseClick(view.btn_detect, Qt.LeftButton)

    # Wait until combo count stabilises
    qtbot.waitUntil(lambda: view.combo_ports.count() == 0, timeout=500)

    # Assertions
    assert view.combo_ports.count() == 0
    assert not view.combo_ports.isEnabled()
    assert called["flag"] is True                   # QMessageBox displayed
    assert view.statusBar().currentMessage() == "No ports found"


def test_plot_data_calculates_ms_correctly(mvc):
    view, model, ctl, qtbot, mp = mvc

    # 1. SETUP: Define known inputs
    # 100 Hz means 10ms per point (1000ms / 100)
    frequency = 100
    y_data = [10, 20, 30, 40, 50]
    
    # Expected X axis in ms: [0.0, 10.0, 20.0, 30.0, 40.0]
    expected_x_ms = [0.0, 10.0, 20.0, 30.0, 40.0]

    payload = {
        "frequency": frequency,
        "data_points": y_data,
        "log_file_name": "test.log"
    }

    # 2. TRIGGER: Emit the MODEL signal directly
    # Because the Controller successfully ran _connect_signals(), 
    # this should automatically trigger view.on_plot_data()
    model.logAnalyzed.emit(True, "OK", payload)

    # 3. ASSERTIONS: Inspect the View to confirm the wiring worked
    x_actual, y_actual = view.hf_curve.getData()
    
    assert np.allclose(x_actual, expected_x_ms), "X-axis (time in ms) is incorrect"
    assert np.array_equal(y_actual, y_data), "Y-axis data is incorrect"

    # C) Verify other side effects
    assert view.btn_export_csv.isEnabled() is True