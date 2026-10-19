import QtQuick 2.15
import QtQuick.Controls 2.15
import ".."
import QtQuick.Layouts 1.15

// ─────────────────────────────────────────────────────────────────────────────
// FileList.qml - Multi-select scrollable file list (Lovable exact match)
// Props: model (array of filenames)
// Signals: selectionChanged(selectedNames array)
// ─────────────────────────────────────────────────────────────────────────────

Item {
    id: root

    property var model: []
    property int count: model.length
    property var selectedIndices: []
    property int selectionRevision: 0
    property int lastClickedIndex: -1
    property bool showSaveButton: true
    property bool analysisBusy: false

    signal selectionChanged(var selectedNames)
    signal analyzeFileClicked(string filename)
    signal saveFileClicked(string filename)

    function clearSelection() {
        selectedIndices = []
        selectionRevision++
    }

    onModelChanged: {
        selectedIndices = []
        lastClickedIndex = -1
        selectionRevision++
    }

    FileListLogic { id: fileListLogic; fileList: root }
    
    // Background: hsl(210, 20%, 98%) ≈ #F8F9FB
    Rectangle {
        anchors.fill: parent
        radius: 6
        color: Theme.muted
        clip: true
        
        ListView {
            id: listView
            anchors {
                fill: parent
                margins: 4
            }
            model: root.model
            clip: true
            
            ScrollBar.vertical: ScrollBar {
                policy: ScrollBar.AsNeeded
            }
            
            delegate: Rectangle {
                width: listView.width
                height: 34
                radius: 5
                
                property bool isSelected: {
                    root.selectionRevision  // dependency trigger
                    return root.selectedIndices.indexOf(index) >= 0
                }
                
                // Selected: #EBF5FB (brand-pill), Hover: #F5F6F8
                color: isSelected ? Theme.brandPill : (rowMouse.containsMouse ? Theme.hoverSubtle : "transparent")

                MouseArea {
                    id: rowMouse
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor

                    onClicked: function(mouse) {
                        if (mouse.modifiers & Qt.ControlModifier) {
                            fileListLogic.handleCtrlClick(index)
                        } else if ((mouse.modifiers & Qt.ShiftModifier) && root.lastClickedIndex >= 0) {
                            fileListLogic.handleShiftClick(index)
                            // anchor (lastClickedIndex) is NOT updated on shift-click
                        } else {
                            fileListLogic.handleSingleClick(index)
                        }
                    }
                }

                RowLayout {
                    anchors {
                        fill: parent
                        leftMargin: 8
                        rightMargin: 8
                    }
                    spacing: 8

                    // Purple file icon "≡" 13px
                    Text {
                        text: "≡"
                        font.pixelSize: 13
                        color: Theme.fileIcon
                    }

                    Text {
                        Layout.fillWidth: true
                        text: modelData
                        font.pixelSize: 12
                        // Selected: medium weight blue, unselected: normal black
                        font.weight: isSelected ? Font.Medium : Font.Normal
                        color: isSelected ? Theme.brandPillText : Theme.foreground
                        elide: Text.ElideRight
                    }

                    // Save button — visible only when exactly one row is selected
                    Rectangle {
                        id: saveBtn
                        width: 28
                        height: 24
                        radius: 4
                        visible: root.showSaveButton && isSelected && root.selectedIndices.length === 1
                        Layout.alignment: Qt.AlignVCenter
                        color: saveBtnMouse.containsMouse ? Theme.primary : "transparent"
                        border.color: Theme.primary
                        border.width: 1

                        Text {
                            anchors.centerIn: parent
                            text: "💾"
                            font.pixelSize: 11
                            color: saveBtnMouse.containsMouse ? "white" : Theme.primary
                        }

                        MouseArea {
                            id: saveBtnMouse
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: function(mouse) {
                                mouse.accepted = true
                                root.saveFileClicked(modelData)
                            }
                        }
                    }

                    // Analyze button — visible only when exactly one row is selected
                    Rectangle {
                        id: analyzeBtn
                        width: 28
                        height: 24
                        radius: 4
                        visible: isSelected && root.selectedIndices.length === 1
                        Layout.alignment: Qt.AlignVCenter
                        opacity: root.analysisBusy ? 0.4 : 1.0
                        color: !root.analysisBusy && analyzeMouse.containsMouse ? Theme.primary : "transparent"
                        border.color: Theme.primary
                        border.width: 1

                        Text {
                            anchors.centerIn: parent
                            text: "▶"
                            font.pixelSize: 11
                            color: !root.analysisBusy && analyzeMouse.containsMouse ? "white" : Theme.primary
                        }

                        MouseArea {
                            id: analyzeMouse
                            anchors.fill: parent
                            hoverEnabled: !root.analysisBusy
                            enabled: !root.analysisBusy
                            cursorShape: root.analysisBusy ? Qt.ArrowCursor : Qt.PointingHandCursor
                            onClicked: function(mouse) {
                                mouse.accepted = true
                                root.analyzeFileClicked(modelData)
                            }
                        }
                    }
                }
            }
            
            // Empty state
            Text {
                anchors.centerIn: parent
                text: Strings.noFiles
                font.pixelSize: 12
                color: Theme.textMuted
                visible: listView.count === 0
            }
        }
    }
}
