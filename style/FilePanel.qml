import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15
import "components" as Components
import "." as App
import "logic"

// ─────────────────────────────────────────────────────────────────────────────
// FilePanel.qml - Middle panel with USB/Cloud/PC tabs (UI only)
// Logic layer: logic/FilePanelLogic.qml
// ─────────────────────────────────────────────────────────────────────────────

Rectangle {
    id: root

    FilePanelLogic { id: filePanelLogic; filePanel: root }
    
    property int activeTab: 0
    property bool usbConnected: false
    property var usbPorts: []
    property var usbLogs: []
    property var cloudLogs: []
    property string pcPath: "No folder selected"
    property var pcFiles: []
    property bool usbBusy: false
    property string usbBusyMessage: ""
    property bool cloudBusy: false
    property string cloudBusyMessage: ""
    property bool currentlyAnalyzing: false
    
    signal detectClicked()
    signal connectClicked(string port)
    signal disconnectClicked()
    signal listUsbFilesClicked()
    signal saveAllUsbToDiskClicked()
    signal saveOneUsbFileToDiskClicked(string filename)
    signal syncUsbToCloudClicked(var allFiles)
    signal listCloudFilesClicked(string deviceId)
    signal downloadCloudFilesToDiskClicked(var selectedFiles)
    signal browsePcClicked(string currentPath)
    signal filesSelected(var selectedNames)
    signal analyzeFileClicked(string filename)
    
    color: App.Theme.card
    
    // Right border
    Rectangle {
        anchors { top: parent.top; bottom: parent.bottom; right: parent.right }
        width: 1
        color: App.Theme.borderAccent
    }
    
    StackLayout {
        anchors.fill: parent
        currentIndex: root.activeTab
        
        // ══════════════ USB TAB
        Item {
        ColumnLayout {
            anchors.fill: parent
            spacing: 0
            
            // Header (56px — matches Sidebar header height so horizontal rules align)
            Rectangle {
                Layout.fillWidth: true
                height: 56
                color: "transparent"
                
                RowLayout {
                    anchors { fill: parent; leftMargin: 14; rightMargin: 14 }
                    spacing: 8
                    
                    Text {
                        text: App.Strings.usbConnectionHeader
                        font.pixelSize: 13
                        font.weight: Font.Bold
                        color: App.Theme.foreground
                    }
                }
                
                Rectangle {
                    anchors { bottom: parent.bottom; left: parent.left; right: parent.right }
                    height: 1
                    color: App.Theme.border
                }
            }
            
            // Content
            ColumnLayout {
                Layout.fillWidth: true
                Layout.fillHeight: true
                Layout.margins: 14
                spacing: 12
                
                // Section label (10px bold uppercase, 0.8px spacing)
                Text {
                    text: App.Strings.serialPortLabel
                    font.pixelSize: 10
                    font.weight: Font.Bold
                    font.letterSpacing: 0.8
                    color: App.Theme.textMuted
                }
                
                // Port selector row
                RowLayout {
                    Layout.fillWidth: true
                    spacing: 8
                    
                    // Dropdown (36px)
                    Rectangle {
                        Layout.fillWidth: true
                        height: 36
                        radius: 6
                        color: App.Theme.card
                        border.color: App.Theme.input
                        border.width: 1
                        
                        RowLayout {
                            anchors { fill: parent; leftMargin: 12; rightMargin: 10 }
                            
                            Text {
                                id: portLabel
                                text: root.usbPorts.length > 0 ? root.usbPorts[0] : App.Strings.noPortsDetected
                                font.pixelSize: 12
                                color: root.usbPorts.length > 0 ? App.Theme.foreground : App.Theme.textMuted
                                Layout.fillWidth: true
                            }
                            
                            Text {
                                text: "⌄"
                                font.pixelSize: 14
                                color: App.Theme.textSecondary
                            }
                        }
                        
                        ComboBox {
                            id: portCombo
                            anchors.fill: parent
                            opacity: 0
                            model: root.usbPorts
                            onCurrentTextChanged: filePanelLogic.onPortSelected(currentText, portLabel)
                        }
                    }
                    
                    // Refresh button (36×36)
                    Rectangle {
                        width: 36
                        height: 36
                        radius: 6
                        color: refreshMouse.containsMouse ? App.Theme.hoverSubtle : App.Theme.card
                        border.color: App.Theme.input
                        border.width: 1
                        
                        Text {
                            anchors.centerIn: parent
                            text: "↻"
                            font.pixelSize: 14
                            color: App.Theme.textSecondary
                        }
                        
                        MouseArea {
                            id: refreshMouse
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: root.detectClicked()
                        }
                    }
                }
                
                // Status row (8×8 dot)
                RowLayout {
                    spacing: 8
                    
                    Rectangle {
                        width: 8
                        height: 8
                        radius: 4
                        color: root.usbConnected ? App.Theme.statusOnline : App.Theme.statusOffline
                    }
                    
                    Text {
                        text: root.usbConnected ? App.Strings.connectedLabel : App.Strings.disconnectedLabel
                        font.pixelSize: 12
                        color: App.Theme.textSecondary
                    }
                }
                
                // Connect / Fetch row (34px buttons)
                RowLayout {
                    Layout.fillWidth: true
                    spacing: 8
                    
                    Components.XButton {
                        Layout.fillWidth: true
                        label: root.usbConnected ? App.Strings.disconnectLabel : App.Strings.connectLabel
                        enabled: root.usbPorts.length > 0 || root.usbConnected
                        onClicked: filePanelLogic.handleConnectToggle(root.usbConnected, portCombo.currentText)
                    }
                    
                    Components.XButton {
                        Layout.fillWidth: true
                        label: App.Strings.fetchLogsLabel
                        enabled: root.usbConnected
                        onClicked: root.listUsbFilesClicked()
                    }
                }
                
                // Divider
                Rectangle {
                    Layout.fillWidth: true
                    height: 1
                    color: App.Theme.border
                }
                
                // File list (fills, margins 14px horizontal, 6px vertical)
                Components.FileList {
                    id: usbFileList
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    model: root.usbLogs
                    analysisBusy: root.currentlyAnalyzing
                    onSelectionChanged: (selectedNames) => root.filesSelected(selectedNames)
                    onAnalyzeFileClicked: (filename) => root.analyzeFileClicked(filename)
                    onSaveFileClicked: (filename) => root.saveOneUsbFileToDiskClicked(filename)
                }
            }
            
            // Bottom actions (border-top, 14px padding)
            Rectangle {
                Layout.fillWidth: true
                height: 1
                color: App.Theme.border
            }
            
            RowLayout {
                Layout.fillWidth: true
                Layout.margins: 14
                spacing: 8
                
                // Save to Disk — saves ALL USB files to default logs directory
                Rectangle {
                    Layout.fillWidth: true
                    height: 34
                    radius: 6
                    color: saveMouse.containsMouse && root.usbLogs.length > 0 ? App.Theme.hoverSubtle : App.Theme.card
                    border.color: root.usbLogs.length > 0 ? App.Theme.input : App.Theme.border
                    border.width: 1

                    Text {
                        anchors.centerIn: parent
                        text: App.Strings.saveAllToDiskLabel
                        font.pixelSize: 11
                        font.weight: Font.Medium
                        color: root.usbLogs.length > 0 ? App.Theme.foreground : App.Theme.textMuted
                    }

                    MouseArea {
                        id: saveMouse
                        anchors.fill: parent
                        hoverEnabled: true
                        enabled: root.usbLogs.length > 0
                        cursorShape: enabled ? Qt.PointingHandCursor : Qt.ArrowCursor
                        onClicked: root.saveAllUsbToDiskClicked()
                    }
                }
                
                // Sync to Cloud
                Rectangle {
                    Layout.fillWidth: true
                    height: 34
                    radius: 6
                    color: syncMouse.containsMouse && root.usbConnected && root.usbLogs.length > 0 ? App.Theme.hoverSubtle : App.Theme.card
                    border.color: root.usbConnected && root.usbLogs.length > 0 ? App.Theme.input : App.Theme.border
                    border.width: 1

                    Text {
                        anchors.centerIn: parent
                        text: App.Strings.syncToCloudLabel
                        font.pixelSize: 11
                        font.weight: Font.Medium
                        color: root.usbConnected && root.usbLogs.length > 0 ? App.Theme.foreground : App.Theme.textMuted
                    }

                    MouseArea {
                        id: syncMouse
                        anchors.fill: parent
                        hoverEnabled: root.usbConnected && root.usbLogs.length > 0
                        enabled: root.usbConnected && root.usbLogs.length > 0
                        cursorShape: enabled ? Qt.PointingHandCursor : Qt.ArrowCursor
                        onClicked: root.syncUsbToCloudClicked(root.usbLogs)
                    }
                }
            }

        }

        // ── USB Busy Overlay
        Components.BusyOverlay {
            anchors.fill: parent
            active: root.usbBusy
            message: root.usbBusyMessage
            z: 10
        }
        }

        // ══════════════ CLOUD TAB
        Item {
        ColumnLayout {
            anchors.fill: parent
            spacing: 0

            Rectangle {
                Layout.fillWidth: true
                height: 56
                color: "transparent"
                
                RowLayout {
                    anchors { fill: parent; leftMargin: 14; rightMargin: 14 }
                    spacing: 8
                    
                    Text {
                        text: App.Strings.fromCloudHeader
                        font.pixelSize: 13
                        font.weight: Font.Bold
                        color: App.Theme.foreground
                    }
                }
                
                Rectangle {
                    anchors { bottom: parent.bottom; left: parent.left; right: parent.right }
                    height: 1
                    color: App.Theme.border
                }
            }
            
            ColumnLayout {
                Layout.fillWidth: true
                Layout.fillHeight: true
                Layout.margins: 14
                spacing: 12
                
                Text {
                    text: App.Strings.deviceIdLabel
                    font.pixelSize: 10
                    font.weight: Font.Bold
                    font.letterSpacing: 0.8
                    color: App.Theme.textMuted
                }
                
                RowLayout {
                    Layout.fillWidth: true
                    spacing: 8
                    
                    Components.XInput {
                        id: deviceIdInput
                        Layout.fillWidth: true
                        placeholderText: App.Strings.deviceIdPlaceholder
                    }
                    
                    Components.XButton {
                        label: App.Strings.fetchLabel
                        onClicked: root.listCloudFilesClicked(filePanelLogic.trimmedDeviceId(deviceIdInput.text))
                    }
                }
                
                Rectangle {
                    Layout.fillWidth: true
                    height: 1
                    color: App.Theme.border
                }
                
                Components.FileList {
                    id: cloudFileList
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    model: root.cloudLogs
                    analysisBusy: root.currentlyAnalyzing
                    onSelectionChanged: (selectedNames) => root.filesSelected(selectedNames)
                    onAnalyzeFileClicked: (filename) => root.analyzeFileClicked(filename)
                    onSaveFileClicked: (filename) => root.downloadCloudFilesToDiskClicked([filename])
                }
            }
            
            Rectangle {
                Layout.fillWidth: true
                height: 1
                color: App.Theme.border
            }
            
            RowLayout {
                Layout.fillWidth: true
                Layout.margins: 14
                spacing: 8

                // Save Selected
                Rectangle {
                    Layout.fillWidth: true
                    height: 34
                    radius: 6
                    color: cloudSaveMouse.containsMouse && cloudFileList.selectedIndices.length > 0 ? App.Theme.hoverSubtle : App.Theme.card
                    border.color: cloudFileList.selectedIndices.length > 0 ? App.Theme.input : App.Theme.border
                    border.width: 1

                    Text {
                        anchors.centerIn: parent
                        text: App.Strings.saveSelectedLabel
                        font.pixelSize: 11
                        font.weight: Font.Medium
                        color: cloudFileList.selectedIndices.length > 0 ? App.Theme.foreground : App.Theme.textMuted
                    }

                    MouseArea {
                        id: cloudSaveMouse
                        anchors.fill: parent
                        hoverEnabled: true
                        enabled: cloudFileList.selectedIndices.length > 0
                        cursorShape: enabled ? Qt.PointingHandCursor : Qt.ArrowCursor
                        onClicked: root.downloadCloudFilesToDiskClicked(filePanelLogic.getSelectedFileNames(cloudFileList.selectedIndices, root.cloudLogs))
                    }
                }

            }

        }

        // ── Cloud Busy Overlay
        Components.BusyOverlay {
            anchors.fill: parent
            active: root.cloudBusy
            message: root.cloudBusyMessage
            z: 10
        }
        }

        // ══════════════ THIS PC TAB
        ColumnLayout {
            spacing: 0

            Rectangle {
                Layout.fillWidth: true
                height: 56
                color: "transparent"
                
                RowLayout {
                    anchors { fill: parent; leftMargin: 14; rightMargin: 14 }
                    spacing: 8
                    
                    Text {
                        text: App.Strings.thisPcHeader
                        font.pixelSize: 13
                        font.weight: Font.Bold
                        color: App.Theme.foreground
                    }
                }
                
                Rectangle {
                    anchors { bottom: parent.bottom; left: parent.left; right: parent.right }
                    height: 1
                    color: App.Theme.border
                }
            }
            
            ColumnLayout {
                Layout.fillWidth: true
                Layout.fillHeight: true
                Layout.margins: 14
                spacing: 12
                
                Text {
                    text: App.Strings.folderPathLabel
                    font.pixelSize: 10
                    font.weight: Font.Bold
                    font.letterSpacing: 0.8
                    color: App.Theme.textMuted
                }
                
                RowLayout {
                    Layout.fillWidth: true
                    spacing: 8
                    
                    Rectangle {
                        Layout.fillWidth: true
                        height: 36
                        radius: 6
                        color: App.Theme.muted
                        border.color: App.Theme.input
                        border.width: 1
                        
                        Text {
                            anchors {
                                fill: parent
                                leftMargin: 12
                                rightMargin: 12
                            }
                            verticalAlignment: Text.AlignVCenter
                            text: root.pcPath
                            font.pixelSize: 12
                            color: App.Theme.foreground
                            elide: Text.ElideLeft
                        }
                    }
                    
                    Rectangle {
                        width: 36
                        height: 36
                        radius: 6
                        color: browseMouse.containsMouse ? App.Theme.hoverSubtle : App.Theme.card
                        border.color: App.Theme.input
                        border.width: 1
                        
                        Text {
                            anchors.centerIn: parent
                            text: "📁"
                            font.pixelSize: 14
                        }
                        
                        MouseArea {
                            id: browseMouse
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: root.browsePcClicked(root.pcPath)
                        }
                    }
                }
                
                Rectangle {
                    Layout.fillWidth: true
                    height: 1
                    color: App.Theme.border
                }
                
                Components.FileList {
                    id: pcFileList
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    model: root.pcFiles
                    analysisBusy: root.currentlyAnalyzing
                    showSaveButton: false
                    onSelectionChanged: (selectedNames) => root.filesSelected(selectedNames)
                    onAnalyzeFileClicked: (filename) => root.analyzeFileClicked(filename)
                }
            }
        }
    }
}
