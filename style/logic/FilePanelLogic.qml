import QtQuick 2.15

// ─────────────────────────────────────────────────────────────────────────────
// FilePanelLogic.qml - Logic layer for FilePanel
// ─────────────────────────────────────────────────────────────────────────────

QtObject {
    id: logic

    property var filePanel: null  // FilePanel root Rectangle

    // -- 1.2  Connect / Disconnect toggle --------------------------------
    function handleConnectToggle(isConnected, port) {
        if (!isConnected)
            filePanel.connectClicked(port)
        else
            filePanel.disconnectClicked()
    }

    // -- 1.3 / 1.4  Map selected indices → file names --------------------
    function getSelectedFileNames(selectedIndices, allLogs) {
        return selectedIndices.map(function(i) { return allLogs[i] })
    }

    // -- 1.5  Trim device-ID input ---------------------------------------
    function trimmedDeviceId(rawText) {
        return rawText.trim()
    }

    // -- 1.6  Whether device-ID field has content after trim -------------
    function hasDeviceId(rawText) {
        return rawText.trim().length > 0
    }

    // -- 1.7  ComboBox selection → port label ----------------------------
    function onPortSelected(newText, portLabel) {
        portLabel.text = newText
    }
}
