import QtQuick 2.15
import QtQuick.Controls 2.15
import ".."
import QtQuick.Layouts 1.15

// ─────────────────────────────────────────────────────────────────────────────
// SSOButton.qml - SSO button (Google / Microsoft) - Lovable exact match
// Props: provider ("google" or "microsoft"), label
// Height: 46px, radius: 8px, text: 13px medium
// Icons: 18×18px SVG (Google G, Microsoft 4-squares)
// ─────────────────────────────────────────────────────────────────────────────

Button {
    id: root
    
    property string provider: "google"  // "google" or "microsoft"
    property string label: ""
    
    implicitHeight: 46
    
    contentItem: RowLayout {
        spacing: 12
        
        // Icon area (18×18)
        Item {
            width: 18
            height: 18
            
            // Google G (simplified - just the letter since we don't have SVG rendering)
            Text {
                anchors.centerIn: parent
                text: "G"
                font.pixelSize: 16
                font.weight: Font.Bold
                color: "#4285F4"
                visible: root.provider === "google"
            }
            
            // Microsoft squares (8×8 each)
            Grid {
                anchors.centerIn: parent
                columns: 2
                spacing: 2
                rowSpacing: 2
                visible: root.provider === "microsoft"
                
                Rectangle { width: 8; height: 8; color: "#F25022" }
                Rectangle { width: 8; height: 8; color: "#7FBA00" }
                Rectangle { width: 8; height: 8; color: "#00A4EF" }
                Rectangle { width: 8; height: 8; color: "#FFB900" }
            }
        }
        
        Text {
            text: root.label
            font.pixelSize: 13
            font.weight: Font.Medium
            color: Theme.foreground
        }
    }
    
    background: Rectangle {
        radius: 8
        color: root.hovered ? Theme.hoverSubtle : Theme.card
        border.color: Theme.input
        border.width: 1
        Behavior on color { ColorAnimation { duration: 120 } }
    }
}
