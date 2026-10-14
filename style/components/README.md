# style/components — Reusable QML Primitives

## Overview

All components import `Theme` for design tokens and emit signals instead of performing logic directly. They are stateless — state lives in `MainWindow.qml` or the host panel.

---

## Component Catalogue

### `XButton.qml`

General-purpose action button.

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `label` | `string` | `""` | Button text |
| `enabled` | `bool` | `true` | Disables interaction and dims when `false` |

| Signal | Description |
|--------|-------------|
| `clicked()` | Emitted on mouse press (inherited from `Button`) |

**Theming:** Background uses `Theme.accent`; hover darkens via `ColorAnimation`. Height 34 px, corner radius 6 px, font 12 px medium.

**Usage:**
```qml
XButton {
    label: "Connect"
    onClicked: mainLogic.handleConnectToggle()
}
```

---

### `XInput.qml`

Single-line text input.

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `text` | `string` | `""` | Alias for `TextInput.text` (two-way bindable) |
| `placeholderText` | `string` | `""` | Greyed-out hint when empty |
| `inputMethodHints` | `Qt.InputMethodHint` | `Qt.ImhNone` | Keyboard type hints |
| `maximumLength` | `int` | `32767` | Character cap |
| `font` | `font` | Theme default | Font override |

| Signal | Args | Description |
|--------|------|-------------|
| `accepted()` | — | Emitted on Enter/Return key |
| `keyPressed(event)` | `KeyEvent` | Forwards all key events to host |

**Theming:** Height 36 px, border colour changes from `Theme.border` to `Theme.accent` on focus, background `Theme.inputBg`, corner radius 6 px.

**Usage:**
```qml
XInput {
    placeholderText: "Device MAC ID"
    onAccepted: logic.handleSearch()
}
```

---

### `XInputPassword.qml`

Password input — wraps `XInput` with a show/hide toggle button.

| Property | Type | Description |
|----------|------|-------------|
| `text` | `string` | Password text (alias) |
| `placeholderText` | `string` | Hint text |

Internally toggles `echoMode` between `TextInput.Password` and `TextInput.Normal` via an eye icon button.

---

### `FileList.qml`

Multi-select scrollable log file list. Used in all three tabs of `FilePanel`.

| Property | Type | Description |
|----------|------|-------------|
| `model` | `list<string>` | Filenames to display |
| `selectedFiles` | `list<string>` | Currently selected filenames (read-only from outside) |
| `selectedIndices` | `list<int>` | Internal index set; drives highlight and deduplication |

| Signal | Args | Description |
|--------|------|-------------|
| `selectionChanged()` | — | Any selection change |
| `analyzeFileClicked(filename)` | `string` | Per-row Analyze button pressed |
| `saveFileClicked(filename)` | `string` | Per-row Save button pressed |

**Selection behaviour:**
- Single click — select only that item
- Ctrl+click — toggle item in/out of selection
- Shift+click — range-select from last selected to clicked item
- Clicking a selected item deselects it (toggles)

**Coupling:** `FileList.qml` uses `FileListLogic.qml` (a sibling file in `components/`) as its behaviour object, injected via property. The logic handles click events and maintains `selectedIndices`.

**Usage:**
```qml
FileList {
    model: window.usbLogs
    onAnalyzeFileClicked: function(filename) {
        logic.handleAnalyze(filename)
    }
}
```

---

### `BusyOverlay.qml`

Semi-transparent blocking overlay with animated spinner.

| Property | Type | Description |
|----------|------|-------------|
| `active` | `bool` | Show (`true`) or hide (`false`) the overlay |
| `message` | `string` | Status text shown below the spinner |

When `active` is `true`:
- Covers its parent with a semi-transparent dark fill.
- Renders a `Canvas`-drawn arc that rotates continuously (360° / 1.2 s, `NumberAnimation` on `rotation`).
- Blocks all mouse and touch events via `MouseArea { anchors.fill: parent }`.

**Pattern — placement inside `StackLayout` tabs:**

Wrap the tab's `ColumnLayout` and `BusyOverlay` in a plain `Item` so the overlay's `anchors.fill: parent` targets the `Item` (not the `StackLayout` which ignores anchors):

```qml
Item {
    Layout.fillWidth: true
    Layout.fillHeight: true

    ColumnLayout { ... }          // tab content

    BusyOverlay {
        anchors.fill: parent
        active: window.usbBusy
        message: window.usbBusyMessage
    }
}
```

---

### `NavItem.qml`

Sidebar navigation button.

| Property | Type | Description |
|----------|------|-------------|
| `iconText` | `string` | Icon glyph (icon font character) |
| `label` | `string` | Nav item label text |
| `isActive` | `bool` | Highlights item when it matches the active tab |

| Signal | Description |
|--------|-------------|
| `clicked()` | Tab switch requested |

Hover changes background colour via `ColorAnimation`. `cursorShape: Qt.PointingHandCursor` is set on the `MouseArea`.

---

### `SSOButton.qml`

Social sign-in button (Google / Microsoft layout). Shows provider icon and label. Emits `clicked()`. Currently used for UI layout only — SSO flows are not yet implemented.

---

## Theming Pattern

All components reference `Theme` via the singleton import:

```qml
import "." as App  // or via qmldir

color: App.Theme.accent
font.pixelSize: App.Theme.fontSizeSm
```

Never hardcode colours, font sizes, or spacing inside components — always use a `Theme` token.

---

## Related Files

- [`style/README.md`](../README.md) — panel structure and signal wiring
- [`style/logic/README.md`](../logic/README.md) — logic objects that consume component signals
- [`style/Theme.qml`](../Theme.qml) — design token singleton
