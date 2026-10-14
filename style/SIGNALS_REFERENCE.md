# QML ↔ Python Architecture Reference

This document describes how the QML GUI and Python backend communicate, how QML is
structured, and how logic is separated from declarative UI.

---

## 1. File Structure

```
style/
├── MainWindow.qml              ← ApplicationWindow — state owner, thin delegators
├── Sidebar.qml                 ← Left nav (declarative only)
├── FilePanel.qml               ← Middle panel: USB / Cloud / PC tabs
├── LoginOverlay.qml            ← Login / Register / MFA overlay (declarative + public API)
├── VisualizationPanel.qml      ← Right panel: device info, log text, chart
├── Theme.qml                   ← Singleton design tokens (colors, spacing)
├── qmldir                      ← Registers Theme as a singleton module
├── components/
│   ├── NavItem.qml             ← Sidebar nav row
│   ├── FileList.qml            ← Multi-select file list
│   ├── BusyOverlay.qml         ← Translucent progress overlay (USB/Cloud tabs)
│   ├── XButton.qml             ← Standard button
│   ├── XInput.qml              ← Text input
│   ├── XInputPassword.qml      ← Password input with show/hide toggle
│   └── SSOButton.qml           ← Google / Microsoft SSO button
└── logic/
    ├── MainWindowLogic.qml     ← All bridge functions + tab-nav guard + timers
    ├── LoginOverlayLogic.qml   ← Form state: show/hide, error, MFA, field reset
    ├── FilePanelLogic.qml      ← Connect/disconnect toggle, index→name mapping
    ├── SidebarLogic.qml        ← Stub (state is pure property bindings)
    └── VisualizationPanelLogic.qml  ← Stub (state is pure property bindings)
```

---

## 2. Architecture Principle: GUI/Logic Separation

Every panel is split into two QML files.

```
PanelName.qml          ← declarative UI only
  - property declarations (the "data model" for this panel)
  - signal declarations
  - alias properties (so logic can read/write inputs without touching internal IDs)
  - one Logic object instantiated with a back-reference injected into it
  - thin public function wrappers that delegate to the logic object

logic/PanelNameLogic.qml    ← all JavaScript behaviour
  - one QtObject with a property var pointing back to the panel root
  - all functions operate on panel.someProperty or call panel.someSignal()
```

This keeps QML files readable as layout descriptors and keeps all conditional logic
in the `logic/` files.

### Dependency injection pattern

Logic objects receive their host via property injection at instantiation:

```qml
// In MainWindow.qml
MainWindowLogic {
    id: mainLogic
    window: root           // ← ApplicationWindow
    loginOverlay: loginOverlay  // ← LoginOverlay instance
}

// In LoginOverlay.qml
LoginOverlayLogic {
    id: overlayLogic
    overlay: root          // ← LoginOverlay root Item
}

// In FilePanel.qml
FilePanelLogic {
    id: panelLogic
    filePanel: root        // ← FilePanel root Rectangle
}
```

Logic objects never reference element IDs from the parent file — they only read/write
the properties and aliases declared on the panel root. This is the equivalent of
constructor injection.

---

## 3. Bootstrap — How Python Wires Into QML

```
main.py
  │
  ├─ Creates: AuthManager, Model, Bridge, LoginController, ApplicationController
  │
  ├─ engine.setInitialProperties({"bridge": bridge, "xaliva_logo": ...})
  │       ↑ Bridge QObject is injected as a QML root property BEFORE the file loads.
  │       QML immediately has access to `bridge` at the window level.
  │
  ├─ engine.load("style/MainWindow.qml")
  │
  └─ bridge.set_root(engine.rootObjects()[0])
          ↑ Python side stores a handle to the QML root object so it can
            call JS functions on it (setStatus, setUsbLogs, etc.).
```

`ApplicationController` wires all signals during construction (before `engine.load`):

```
_wire_qml_to_model()    bridge signals → model slots
_wire_auth_signals()    bridge auth signals → LoginController slots
_wire_model_to_bridge() model signals → bridge @Slot handlers
```

---

## 4. QML → Python Signal Flow

```
User interaction in a child component
    └─► Component emits its own signal   (e.g. FilePanel emits detectClicked)
            └─► MainWindow.qml onXxx handler fires
                    └─► mainLogic.handleXxx()           (MainWindowLogic)
                            └─► window.bridge.signalName()   ← emits Qt Signal
                                    └─► ApplicationController slot / Model method
```

The `bridge` is null-guarded in every `handleXxx` via `callBridge(fn)`:

```qml
// MainWindowLogic.qml
function callBridge(method) {
    if (window.bridge) method()
}
```

### All Bridge signals (declared in `src/mvc/bridge.py`)

#### USB
| Bridge Signal | Signature | Wired to |
|---|---|---|
| `detectClicked` | `()` | `model.detect_ports` |
| `connectClicked` | `(port: str)` | `model.connect_to_port` |
| `disconnectClicked` | `()` | `model.close_port` |
| `listUsbFilesClicked` | `()` | `model.list_usb_files` |
| `saveAllUsbToDiskClicked` | `(files: list)` | `model.save_all_usb_files_to_disk` |
| `saveOneUsbFileToDiskClicked` | `()` | `model.save_one_usb_file_to_disk` |
| `syncUsbToCloudClicked` | `(files: list)` | `model.sync_usb_files_to_cloud` |

#### Cloud
| Bridge Signal | Signature | Wired to |
|---|---|---|
| `listCloudFilesClicked` | `(device_id: str)` | `model.list_cloud_files` |
| `downloadCloudFilesToDiskClicked` | `(files: list)` | `controller._handle_download_cloud_files_to_disk` (opens OS dialog) |

#### PC
| Bridge Signal | Signature | Wired to |
|---|---|---|
| `browsePcClicked` | `(current_path: str)` | `controller._handle_browse_pc` (opens dir dialog) |

#### Analysis
| Bridge Signal | Signature | Wired to |
|---|---|---|
| `analyzeClicked` | `(tab_id: int, filename: str)` | `controller._translate_and_analyze` |
| `exportCsvClicked` | `()` | `controller._handle_export_csv` |

#### Per-Tab Busy State (Python → QML)
| Bridge Signal | Signature | Listens in |
|---|---|---|
| `usbBusyChanged` | `(busy: bool, msg: str)` | `MainWindowLogic._bridgeConn` → `setUsbBusy` |
| `cloudBusyChanged` | `(busy: bool, msg: str)` | `MainWindowLogic._bridgeConn` → `setCloudBusy` |

#### Auth
| Bridge Signal | Signature | Wired to |
|---|---|---|
| `loginClicked` | `(email: str, password: str, remember: bool)` | `login_ctrl.handle_login` |
| `registerClicked` | `(email: str, password: str)` | `login_ctrl.handle_register` |
| `mfaVerifyClicked` | `(code: str)` | `login_ctrl.handle_mfa_verify` |
| `signOutClicked` | `()` | `login_ctrl.handle_signout` |
| `googleSSOClicked` | `()` | not yet wired |
| `microsoftSSOClicked` | `()` | not yet wired |
| `forgotPasswordClicked` | `()` | not yet wired |

#### Window
| Bridge Signal | Signature | Wired to |
|---|---|---|
| `closeRequested` | `()` | `model.cancel_workers` |

> **Gap:** `settingsClicked` and `recentFileClicked` are called from
> `MainWindowLogic.qml` but are not declared as Signals in `bridge.py` yet.
> `tabChanged` is handled entirely within QML (see §5).

---

## 5. QML-Internal Logic (no Python involved)

Some interactions are handled entirely in QML without ever reaching Python.

### Tab navigation guard
When the user clicks the Cloud tab (tab index 1) while `userEmail` is empty,
`MainWindowLogic.handleTabChanged` intercepts the change, stores the intended
destination in `pendingTab`, and shows the login overlay instead of switching.
After successful login, `hideLoginOverlay()` checks `pendingTab` and completes
the navigation.

```qml
// MainWindowLogic.qml
function handleTabChanged(newTab) {
    if (newTab === 1 && window.userEmail.length === 0) {
        pendingTab = newTab
        loginOverlay.show()
    } else {
        window.activeTab = newTab
    }
}
```

### Login overlay form switching
Switching between Login / Register / MFA pages is done by `LoginOverlayLogic`
toggling `overlay.formMode` (0 / 1 / 2). The `StackLayout` in `LoginOverlay.qml`
uses `currentIndex: formMode` as its binding — no Python call needed.

### Status bar auto-reset
`showSuccess(msg)` in `MainWindowLogic` sets the status text and starts a 5-second
`Timer`. When it fires, status resets to `"Ready"`. Python never polls this.

### Toast auto-dismiss
`setStatus(msg, busy)` shows the info toast and starts a 4-second `Timer`.
The `✕` dismiss button calls `mainLogic.dismissToast()` directly.

---

## 6. Python → QML Call Flow

```
Python (Bridge method)
    └─► self._call("jsFunctionName", *args)      ← Bridge._call()
            └─► getattr(self._root, js_func)(*args)
                    └─► MainWindow.qml delegator function
                            └─► mainLogic.implementation()
                                    └─► window.someProperty = value
                                     or loginOverlay.show() / showError() / etc.
```

### Bridge Python methods → QML JS functions

| Bridge method (Python) | QML function called on root | What it updates |
|---|---|---|
| `set_status(msg, busy)` | `setStatus(msg, busy)` | `statusMessage`, `isBusy`, toast |
| `set_user(email, name)` | `setUserEmail(email)` + `setUserName(name)` | `userEmail`, `userName` |
| `clear_user()` | `setUserEmail("")` | `userEmail` → `""` |
| `set_usb_ports(ports)` | `setUsbPorts(ports)` | `usbPorts` |
| `set_usb_connected(ok, id)` | `setUsbConnected(ok, id)` | `usbConnected` |
| `set_usb_logs(logs)` | `setUsbLogs(logs)` | `usbLogs` (sorted), clears `selectedFiles` |
| `set_cloud_logs(logs)` | `setCloudLogs(logs)` | `cloudLogs` (sorted), clears `selectedFiles` |
| `set_local_path(path)` | `setLocalPath(path)` | `pcPath` |
| `set_local_files(files)` | `setLocalFiles(files)` | `pcFiles` (sorted), clears `selectedFiles` |
| `set_device_info(pairs)` | `setDeviceInfo(pairs)` | `deviceInfo` list of `{key, val}` |
| `set_log_info(text)` | `setLogInfo(text)` | `logInfo` |
| `set_plot_visible(v)` | `setPlotVisible(v)` | `plotVisible` |
| `set_recent_files(files)` | `setRecentFiles(files)` | `recentFiles` |
| `show_login()` | `showLoginOverlay()` | opens `LoginOverlay` |
| `hide_login()` | `hideLoginOverlay()` | closes overlay, resolves `pendingTab` |
| `login_error(msg)` | `loginShowError(msg)` | error banner in `LoginOverlay` |
| `login_show_mfa(url)` | `loginShowMFA(url)` | switches overlay to MFA page + QR image |
| `show_success(msg)` | `showSuccess(msg)` | status + toast, auto-resets after 5 s |
| `show_error(msg)` | `showError(msg)` | modal error dialog |
| `show_login_expired(msg)` | _(composite)_ | `clear_user` + `login_error` + `show_login` |

---

## 7. Model → Bridge Signal Flow

```
Worker thread (QRunnable)
    └─► emits Model Qt Signal
            └─► Bridge @Slot handler (wired by ApplicationController)
                    └─► calls one of the Bridge Python→QML methods above
```

### Model signals → Bridge slots

| Model signal | Signature | Bridge slot | Effect |
|---|---|---|---|
| `portsDetected` | `(bool, str, list)` | `on_ports_detected` | `set_usb_ports` or error status |
| `connectionStatusChanged` | `(bool, str)` | `on_connection_status` | `set_usb_connected` + status |
| `usbFileNamesListed` | `(bool, str, list)` | `on_usb_file_names_listed` | `set_usb_logs` + status |
| `usbDeviceLogParsed` | `(bool, list)` | `on_usb_device_log_parsed` | `set_device_info` |
| `usbFilesSavedToDisk` | `(bool, str, dict)` | `on_usb_files_saved_to_disk` | `show_success` or status |
| `usbFilesSyncedToCloud` | `(bool, str)` | `on_usb_files_synced_to_cloud` | `show_success` or status |
| `cloudFileNamesListed` | `(bool, str, list)` | `on_cloud_file_names_listed` | `set_cloud_logs` + status |
| `cloudFilesDownloadedToDisk` | `(bool, str, dict)` | `on_cloud_files_downloaded_to_disk` | `show_success` or status |
| `progress` | `(action_id, cur, tot)` | `on_progress` | progress status string (busy=True) |
| `allWorkersDone` | `()` | `on_all_workers_done` | sets `_safe_to_close`, calls `root.close()` |
| `analysisCompleted` | `(bool, str, dict)` | `on_analysis_completed` | calls `root.setPlotData(x, y, freq, ...)` |
| `authFinished` | `(bool, str, dict)` | `login_ctrl.on_auth_finished` | auth state machine |
| `mfaQrReady` | `(secret, email)` | `login_ctrl.on_mfa_qr_ready` | generates QR, calls `login_show_mfa` |
| `authRequired` | `(msg: str)` | `bridge.show_login_expired` | 401 guard: clears user + shows login |

---

## 8. Auth Flow Detail

```
QML LoginOverlay
    ↓ loginClicked(email, password)
Bridge.loginClicked signal
    ↓ wired by ApplicationController
LoginController.handle_login(email, password, remember)
    ↓ validates, calls
Model.sign_in_user(email, password)   [worker thread]
    ↓ emits authFinished(success, msg, payload)
LoginController.on_auth_finished(success, msg, payload)
    ├─ MFA_REQUIRED  → bridge.login_show_mfa("")  [overlay switches to MFA page]
    ├─ login success → bridge.hide_login() + bridge.set_user(email, name)
    ├─ signup success → bridge.set_status("check email…")
    └─ failure       → bridge.login_error(msg)
```

MFA enrollment (signup path) goes through `mfaQrReady` → `LoginController.on_mfa_qr_ready`
which generates a `data:image/png;base64,...` QR code URL and calls `bridge.login_show_mfa(url)`.

---

## 9. Application-State Properties on MainWindow

`MainWindow.qml` (`id: root`) is the single source of truth for all runtime state.
Child components receive state as read-only bound inputs; `MainWindowLogic` is the
only writer.

| Property | Type | Who reads it | Who writes it |
|---|---|---|---|
| `activeTab` | `int` | Sidebar, FilePanel | `mainLogic.handleTabChanged` / `hideLoginOverlay` |
| `usbConnected` | `bool` | FilePanel | `mainLogic.setUsbConnected` |
| `usbPorts` | `list` | FilePanel | `mainLogic.setUsbPorts` |
| `usbLogs` | `list` | FilePanel | `mainLogic.setUsbLogs` |
| `cloudLogs` | `list` | FilePanel | `mainLogic.setCloudLogs` |
| `pcPath` | `string` | FilePanel | `mainLogic.setLocalPath` |
| `pcFiles` | `list` | FilePanel | `mainLogic.setLocalFiles` |
| `recentFiles` | `list` | Sidebar | `mainLogic.setRecentFiles` |
| `userEmail` | `string` | Sidebar, tab-nav guard | `mainLogic.setUserEmail` |
| `userName` | `string` | Sidebar | `mainLogic.setUserName` |
| `deviceInfo` | `list` | VisualizationPanel | `mainLogic.setDeviceInfo` |
| `logInfo` | `string` | VisualizationPanel | `mainLogic.setLogInfo` |
| `plotVisible` | `bool` | VisualizationPanel | `mainLogic.setPlotVisible` |
| `selectedFiles` | `list` | Sidebar (analyzeEnabled) | `mainLogic.handleFilesSelected` / log updates |
| `statusMessage` | `string` | footer Text | `mainLogic.setStatus` |
| `isBusy` | `bool` | footer spinner | `mainLogic.setStatus` |
| `toastMessage` | `string` | info toast | `mainLogic.setStatus` |
| `toastVisible` | `bool` | info toast | `mainLogic.setStatus` / timer / dismiss |
| `errorMessage` | `string` | error dialog | `mainLogic.showError` |
| `usbBusy` | `bool` | FilePanel, BusyOverlay | `mainLogic.setUsbBusy` |
| `usbBusyMessage` | `string` | BusyOverlay | `mainLogic.setUsbBusy` |
| `cloudBusy` | `bool` | FilePanel, BusyOverlay | `mainLogic.setCloudBusy` |
| `cloudBusyMessage` | `string` | BusyOverlay | `mainLogic.setCloudBusy` |
| `currentlyAnalyzing` | `bool` | FilePanel, FileList, VisualizationPanel | `mainLogic` (from bridge.analysisStarted/End) |

---

## 10. LoginOverlay State Properties

`LoginOverlay.qml` (`id: root`) declares these for `LoginOverlayLogic` to write and
the UI to read via property bindings.

| Property | Type | Set by | Bound to |
|---|---|---|---|
| `formMode` | `int` (0/1/2) | `overlayLogic` | `StackLayout.currentIndex` (login/register/MFA) |
| `errorBannerVisible` | `bool` | `overlayLogic` | error banner `visible` |
| `errorMessage` | `string` | `overlayLogic` | error banner `Text.text` |
| `qrImageSource` | `url` | `overlayLogic` | `Image.source` in MFA page |
| `emailLoginText` | `string alias` | `overlayLogic` | `emailLogin.text` |
| `passLoginText` | `string alias` | `overlayLogic` | `passLogin.text` |
| `emailRegText` | `string alias` | `overlayLogic` | `emailReg.text` |
| `passRegText` | `string alias` | `overlayLogic` | `passReg.text` |
| `mfaCodeText` | `string alias` | `overlayLogic` | `mfaCode.text` |
