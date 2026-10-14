import QtQuick 2.15
import QtQuick.Controls 2.15
import ".."

// ─────────────────────────────────────────────────────────────────────────────
// XInputPassword.qml - Password input with show/hide toggle (Lovable exact match)
// Props: text (alias), accepted signal, keyPressed signal
// Height: 46px (login), radius: 8px, text: 13px
// Eye toggle: 16×16px at right 12px
// ─────────────────────────────────────────────────────────────────────────────

FocusScope {
    id: root

    property alias text: passField.text
    property bool showPassword: false
    property string placeholderOverride: ""

    signal accepted()
    signal keyPressed(var event)
    signal tabPressed()
    
    implicitHeight: 46
    
    Rectangle {
        anchors.fill: parent
        radius: 8
        color: Theme.card
        border.color: passField.activeFocus ? Theme.primary : Theme.input
        border.width: 1
    }

    // IBeam cursor over the text input area (excludes show/hide toggle)
    MouseArea {
        anchors { fill: parent; rightMargin: 52 }
        acceptedButtons: Qt.NoButton
        cursorShape: Qt.IBeamCursor
    }

    // Input field
    TextInput {
        id: passField
        focus: true
        anchors {
            fill: parent
            leftMargin: 12
            rightMargin: 52
        }
        verticalAlignment: TextInput.AlignVCenter
        font.pixelSize: 13
        color: Theme.foreground
        echoMode: root.showPassword ? TextInput.Normal : TextInput.Password

        onAccepted: root.accepted()
        Keys.onPressed: function(event) { root.keyPressed(event) }
        Keys.onTabPressed: function(event) { event.accepted = true; root.tabPressed() }

        // Placeholder
        Text {
            anchors.fill: parent
            verticalAlignment: Text.AlignVCenter
            text: root.placeholderOverride.length > 0 ? root.placeholderOverride : Strings.passwordPlaceholder
            color: Theme.textMuted
            font.pixelSize: 13
            visible: !parent.text.length && !root.showPassword
        }
    }

    // Show/hide toggle (text "Show"/"Hide" at right 12px)
    Text {
        anchors {
            right: parent.right
            rightMargin: 12
            verticalCenter: parent.verticalCenter
        }
        text: root.showPassword ? Strings.hideLabel : Strings.showLabel
        font.pixelSize: 12
        font.weight: Font.Medium
        color: Theme.primary

        MouseArea {
            anchors.fill: parent
            cursorShape: Qt.PointingHandCursor
            onClicked: root.showPassword = !root.showPassword
        }
    }
}
