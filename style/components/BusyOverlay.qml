// BusyOverlay.qml - Translucent progress overlay for file panel tabs
// Props: active (bool), message (string)
// Usage: place inside a ColumnLayout with z: 10, anchors.fill to the content area

import QtQuick
import QtQuick.Layouts

Rectangle {
    id: root

    property bool active: false
    property string message: ""

    color: Qt.rgba(1, 1, 1, 0.82)
    visible: active
    z: 10

    // Block all mouse/touch events from reaching items below the overlay
    MouseArea {
        anchors.fill: parent
        enabled: root.active
        hoverEnabled: true
        acceptedButtons: Qt.AllButtons
        onWheel: function(wheel) { wheel.accepted = true }
    }

    Behavior on opacity {
        NumberAnimation {
            duration: 150
            easing.type: Easing.InOutQuad
        }
    }

    ColumnLayout {
        anchors.centerIn: parent
        spacing: 16

        // Animated spinner
        Rectangle {
            id: spinner
            Layout.alignment: Qt.AlignHCenter
            width: 48
            height: 48
            radius: 6
            border.width: 2
            border.color: "#0066CC"  // Theme.primary equivalent
            color: "transparent"

            RotationAnimator on rotation {
                from: 0
                to: 360
                duration: 1200
                loops: Animation.Infinite
                running: root.visible
            }
        }

        // Status message
        Text {
            Layout.alignment: Qt.AlignHCenter
            text: root.message
            font.pixelSize: 12
            color: "#666666"  // Theme.textSecondary equivalent
        }
    }
}
