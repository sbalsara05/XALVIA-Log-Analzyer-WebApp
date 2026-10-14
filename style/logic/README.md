# style/logic — JavaScript Behaviour Objects

## Overview

Each `*Logic.qml` is a `QtObject` that holds all JavaScript behaviour for its host panel. Panels contain only declarative layout; logic objects contain only imperative JS. The two sides communicate via **property injection** — the logic object receives its host panel as a property and calls back via its injected reference. Logic objects never reference element IDs from the parent file.

### `callBridge(fn)` Null Guard

All logic objects use a shared `callBridge` helper to protect against the bridge being `null` before QML fully loads:

```qml
function callBridge(fn) {
    if (window.bridge) fn()
}
```

Every bridge call is wrapped: `callBridge(() => window.bridge.detectClicked())`.

---

## `MainWindowLogic.qml`

**Host panel:** `MainWindow.qml`

**Injected properties:** `window` (the `ApplicationWindow` itself), `bridge` (via `window.bridge`)

### Functions

| Function | Args | Bridge signal triggered | Effect |
|----------|------|------------------------|--------|
| `handleTabChange(tab)` | `int` | — | Sets `window.activeTab`; blocks tab switch if not logged in |
| `handleDetect()` | — | `bridge.detectClicked()` | Initiates USB port scan |
| `handleConnect(port)` | `string` | `bridge.connectClicked(port)` | Opens serial connection |
| `handleDisconnect()` | — | `bridge.disconnectClicked()` | Closes serial connection |
| `handleListUsb()` | — | `bridge.listUsbFilesClicked()` | Lists files on USB device |
| `handleListCloud(mac_id)` | `string` | `bridge.listCloudFilesClicked(mac_id)` | Lists cloud files for device |
| `handleAnalyze(tab, filename)` | `int, string` | `bridge.analyzeClicked(tab, filename)` | Opens log for analysis |
| `handleSyncToCloud(files)` | `list` | `bridge.syncUsbToCloudClicked(files)` | Syncs selected USB files to GCS |
| `handleDownloadCloud(files)` | `list` | `bridge.downloadCloudFilesToDiskClicked(files)` | Downloads cloud files to disk |
| `handleSaveAllUsb()` | — | `bridge.saveAllUsbFilesClicked()` | Saves all selected USB files |
| `handleSaveOneUsb(filename)` | `string` | `bridge.saveOneUsbFileClicked(filename)` | Saves single USB file |
| `handleBrowsePc()` | — | `bridge.browsePcClicked()` | Opens folder picker for PC tab |
| `handleExportCsv()` | — | `bridge.exportCsvClicked()` | Exports plot data as CSV |
| `handleScreenshot()` | — | `bridge.screenshotClicked()` | Saves chart screenshot |
| `handleSignOut()` | — | `bridge.signOutClicked()` | Signs out current user |

### State Setters (called by Python → QML)

These JS functions are called by `bridge._call(fn, args)` to update `MainWindow` state properties:

| JS function | Sets on `window` |
|-------------|-----------------|
| `setStatus(msg, isError)` | `statusMessage`, `statusIsError` |
| `setUser(email, name)` | `userEmail`, `userName` |
| `setUsbPorts(ports)` | `usbPorts` |
| `setUsbLogs(files)` | `usbLogs` |
| `setCloudLogs(files)` | `cloudLogs` |
| `setPcFiles(files)` | `pcFiles` |
| `setUsbBusy(active, msg)` | `usbBusy`, `usbBusyMessage` |
| `setCloudBusy(active, msg)` | `cloudBusy`, `cloudBusyMessage` |
| `showError(msg)` | `toastMessage`, `toastLevel = "error"` + starts dismiss timer |
| `showSuccess(msg)` | `toastMessage`, `toastLevel = "success"` |
| `showWarning(msg)` | `toastMessage`, `toastLevel = "warning"` |
| `setDeviceInfo(entries)` | `deviceInfo` |
| `setPlotConfig(specJson)` | `plotSpec`, `plotVisible = true` |
| `hideLoginOverlay(success)` | `loginVisible = false` |
| `showMFAPage(qrUri)` | `loginMode = 2`, `mfaQrUri = qrUri` |
| `showLoginExpiredOverlay()` | `loginVisible = true`, `loginExpired = true` |
| `showForgotPasswordForm()` | `loginMode = 3` |
| `showForgotPasswordSuccess(email)` | `loginMode = 4`, `resetEmail = email` |

### Toast Auto-Dismiss Timers

| Level | TTL |
|-------|-----|
| `"success"` | 3 000 ms |
| `"warning"` | 5 000 ms |
| `"error"` | 8 000 ms |

A `Timer` resets `toastMessage` to `""` after the TTL, hiding the toast.

---

## `FilePanelLogic.qml`

**Host panel:** `FilePanel.qml`

**Injected properties:** `panel` (the `FilePanel` item), `window` (for tab and file access)

### Functions

| Function | Args | Effect |
|----------|------|--------|
| `handleConnectToggle()` | — | Calls `mainLogic.handleConnect(port)` or `handleDisconnect()` depending on `window.usbConnected` |
| `getSelectedFileNames(tab)` | `int` | Maps `selectedIndices` from the active `FileList` to filenames |
| `trimmedDeviceId()` | — | Returns `panel.deviceIdInput.text.trim()` |
| `hasDeviceId()` | — | Returns `trimmedDeviceId() !== ""` |
| `onPortSelected(index)` | `int` | Updates `panel.portLabel` from `window.usbPorts[index]` |

---

## `LoginOverlayLogic.qml`

**Host panel:** `LoginOverlay.qml`

**Injected properties:** `overlay` (the `LoginOverlay` item), `window`

### Mode Constants

| Value | Mode |
|-------|------|
| `0` | Login |
| `1` | Register |
| `2` | MFA (enrollment or signin) |
| `3` | Forgot password form |
| `4` | Reset email sent (success screen) |

### Functions

| Function | Args | Effect |
|----------|------|--------|
| `show()` | — | Sets `overlay.visible = true` |
| `hide()` | — | Sets `overlay.visible = false` |
| `showError(msg)` | `string` | Shows error banner inside overlay |
| `dismissError()` | — | Hides error banner |
| `switchToLoginMode()` | — | Sets mode 0, clears all fields |
| `switchToRegisterMode()` | — | Sets mode 1, clears all fields |
| `switchToMFAMode(qrUri)` | `string` | Sets mode 2; sets QR image source if `qrUri` provided |
| `switchToForgotPasswordMode()` | — | Sets mode 3, clears email field |
| `validatePassword(pw)` | `string` | Returns `true` if length ≥ 8, contains upper, lower, digit, and special char |
| `doLogin()` | — | Validates fields; emits `bridge.loginClicked(email, pw, remember)` |
| `doRegister()` | — | Validates fields + `validatePassword`; emits `bridge.registerClicked(email, pw, confirm)` |
| `doMFA()` | — | Validates 6-digit code; emits `bridge.mfaVerifyClicked(code)` |
| `doForgotPassword()` | — | Validates email; emits `bridge.forgotPasswordSubmitClicked(email)` |

---

## `SidebarLogic.qml`

**Host panel:** `Sidebar.qml`

**Injected properties:** `sidebar`, `window`

### Functions

| Function | Effect |
|----------|--------|
| `handleSignOut()` | Calls `mainLogic.handleSignOut()` |
| `handleNavClick(tab)` | Calls `mainLogic.handleTabChange(tab)` |

Sidebar displays `window.userEmail` and `window.userName` directly via property binding — no setter function needed.

---

## `VisualizationPanelLogic.qml`

**Host panel:** `VisualizationPanel.qml`

**Injected properties:** `panel`, `window`

### Functions

| Function | Effect |
|----------|--------|
| `handleExportCsv()` | Calls `mainLogic.handleExportCsv()` |
| `handleScreenshot()` | Calls `mainLogic.handleScreenshot()` |

The panel itself is mostly declarative — it binds `window.deviceInfo` to the device info table model and `window.plotSpec` to the chart config. The logic object handles the two user-triggered export actions.

---

## Related Files

- [`style/README.md`](../README.md) — panel structure and signal wiring overview
- [`style/components/README.md`](../components/README.md) — reusable primitives used by panels
- [`src/mvc/README.md`](../../src/mvc/README.md) — bridge signals that logic objects trigger
