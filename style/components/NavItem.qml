import QtQuick 2.15
import QtQuick.Layouts 1.15
import ".."

// ─────────────────────────────────────────────────────────────────────────────
// NavItem.qml - Sidebar navigation item (NO EMOJIS)
// Props: iconText (simple text like "USB"), label, isActive, onClicked signal
// ─────────────────────────────────────────────────────────────────────────────

Rectangle {
    id: root
    
    property string iconText: ""
    property string label: ""
    property bool isActive: false
    property bool hovered: false

    signal clicked()

    implicitHeight: 40
    radius: 8
    color: isActive ? Theme.brandPill : (hovered ? Theme.hoverSubtle : "transparent")

    RowLayout {
        anchors {
            fill: parent
            leftMargin: 12
            rightMargin: 12
        }
        spacing: 10
        
        // Simple text icon (no emoji)
        Text {
            text: root.iconText
            font.pixelSize: 11
            font.weight: Font.Bold
            color: root.isActive ? Theme.brandPillText : Theme.textPrimary
            visible: root.iconText.length > 0
        }

        Text {
            text: root.label
            font.pixelSize: 13
            font.weight: Font.Medium
            color: root.isActive ? Theme.brandPillText : Theme.textPrimary
            Layout.fillWidth: true
        }
    }
    
    MouseArea {
        id: mouseArea
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onEntered: root.hovered = true
        onExited: root.hovered = false
        onClicked: root.clicked()
    }
}
