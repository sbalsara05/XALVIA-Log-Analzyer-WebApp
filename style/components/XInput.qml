import QtQuick 2.15
import QtQuick.Controls 2.15
import ".."

// ─────────────────────────────────────────────────────────────────────────────
// XInput.qml - Reusable text input field (Lovable exact match)
// Props: placeholderText, text (alias)
// Height: 36px, radius: 6px, text: 12px
// ─────────────────────────────────────────────────────────────────────────────

Item {
    id: root
    
    property alias text: textInput.text
    property string placeholderText: ""
    property alias inputMethodHints: textInput.inputMethodHints
    property alias maximumLength: textInput.maximumLength
    property alias horizontalAlignment: textInput.horizontalAlignment
    property alias font: textInput.font

    signal accepted()
    signal keyPressed(var event)

    implicitHeight: 36
    
    Rectangle {
        anchors.fill: parent
        radius: 6
        color: Theme.card
        border.color: textInput.activeFocus ? Theme.primary : Theme.input
        border.width: textInput.activeFocus ? 1 : 1
    }
    
    // Input
    TextInput {
        id: textInput
        anchors {
            fill: parent
            leftMargin: 12
            rightMargin: 12
        }
        verticalAlignment: TextInput.AlignVCenter
        font.pixelSize: 12
        color: Theme.foreground

        onAccepted: root.accepted()
        Keys.onPressed: function(event) { root.keyPressed(event) }
        
        // Placeholder
        Text {
            anchors.fill: parent
            verticalAlignment: Text.AlignVCenter
            text: root.placeholderText
            color: Theme.textMuted
            font.pixelSize: 12
            visible: !parent.text.length
        }
    }
}
