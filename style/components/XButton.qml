import QtQuick 2.15
import QtQuick.Controls 2.15
import ".."

// ─────────────────────────────────────────────────────────────────────────────
// XButton.qml - Reusable button component (Lovable exact match)
// Props: label, enabled
// Height: 34px, radius: 6px, text: 12px medium
// ─────────────────────────────────────────────────────────────────────────────

Button {
    id: root
    
    property string label: ""
    
    implicitHeight: 34
    
    contentItem: Text {
        text: root.label
        font.pixelSize: 12
        font.weight: Font.Medium
        color: !root.enabled ? Theme.textMuted : Theme.foreground
        horizontalAlignment: Text.AlignHCenter
        verticalAlignment: Text.AlignVCenter
    }
    
    background: Rectangle {
        radius: 6
        color: root.hovered ? Theme.hoverSubtle : Theme.card
        border.color: !root.enabled ? Theme.border : (root.hovered ? Theme.border : Theme.input)
        border.width: 1
        Behavior on color { ColorAnimation { duration: 100 } }
    }
}
