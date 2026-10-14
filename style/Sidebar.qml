import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15
import "components" as Components
import "." as App
import "logic"

// ─────────────────────────────────────────────────────────────────────────────
// Sidebar.qml - Left navigation sidebar (UI only)
// Logic layer: logic/SidebarLogic.qml
// ─────────────────────────────────────────────────────────────────────────────

Rectangle {
    id: root

    SidebarLogic { id: sidebarLogic; sidebar: root }
    
    property int activeTab: 0
    property string userEmail: ""
    property string userName: "Guest"
    property var recentFiles: []
    property var xaliva_logo: null
    
    signal tabChanged(int newTab)
    signal settingsClicked()
    signal signOutClicked()
    signal loginSignupClicked()
    signal recentFileClicked(string filename)
    
    color: App.Theme.card

    // Right border
    Rectangle {
        anchors { top: parent.top; bottom: parent.bottom; right: parent.right }
        width: 1
        color: App.Theme.border
    }
    
    ColumnLayout {
        anchors.fill: parent
        spacing: 0
        
        // ── Header (56px — matches FilePanel header height so horizontal rules align)
        Rectangle {
            Layout.fillWidth: true
            height: 56
            color: "transparent"
            
            RowLayout {
                anchors { fill: parent; leftMargin: 16; rightMargin: 16 }
                spacing: 10
                
                // Logo 
                Image {
                    Layout.preferredWidth: 200
                    Layout.preferredHeight: 30 
                    Layout.alignment: Qt.AlignHCenter
                    
                    // Path to your svg file
                    source: xaliva_logo
                    
                    // Critical for SVGs: defines the render buffer size to keep it sharp
                    sourceSize.width: 200
                    sourceSize.height: 30
                    
                    smooth: true
                    mipmap: true
                    asynchronous: true
                    fillMode: Image.PreserveAspectFit
                }
                
            }
            
            Rectangle {
                anchors { bottom: parent.bottom; left: parent.left; right: parent.right }
                height: 1
                color: App.Theme.border
            }
        }
        
        Item { Layout.preferredHeight: 8 }
        
        // ── Navigation items (40px each, 2px bottom margin)
        Components.NavItem {
            Layout.fillWidth: true
            Layout.leftMargin: 8
            Layout.rightMargin: 8
            Layout.bottomMargin: 2
            label: App.Strings.navUsb
            isActive: root.activeTab === 0
            onClicked: root.tabChanged(0)
        }
        
        Components.NavItem {
            Layout.fillWidth: true
            Layout.leftMargin: 8
            Layout.rightMargin: 8
            Layout.bottomMargin: 2
            label: App.Strings.navCloud
            isActive: root.activeTab === 1
            onClicked: root.tabChanged(1)
        }
        
        Components.NavItem {
            Layout.fillWidth: true
            Layout.leftMargin: 8
            Layout.rightMargin: 8
            label: App.Strings.navPc
            isActive: root.activeTab === 2
            onClicked: root.tabChanged(2)
        }
        
        // ── Spacer with recently viewed
        Item { Layout.preferredHeight: 16 }
        
        ColumnLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 0
            
            Text {
                Layout.leftMargin: 16
                text: App.Strings.recentlyViewedLabel
                font.pixelSize: 9
                font.weight: Font.Bold
                font.letterSpacing: 1.2
                color: App.Theme.textMuted
            }
            
            Item { Layout.preferredHeight: 8 }
            
            // Recent files list (32px rows)
            ListView {
                id: recentList
                Layout.fillWidth: true
                Layout.fillHeight: true
                Layout.leftMargin: 8
                Layout.rightMargin: 8
                clip: true
                model: root.recentFiles
                
                ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }
                
                delegate: Rectangle {
                    width: recentList.width
                    height: 32
                    radius: 6
                    color: recMouse.containsMouse ? App.Theme.hoverSubtle : "transparent"
                    Behavior on color { ColorAnimation { duration: 80 } }
                    
                    RowLayout {
                        anchors { fill: parent; leftMargin: 8; rightMargin: 8 }
                        spacing: 8
                        
                        Text {
                            text: "≡"
                            font.pixelSize: 13
                            color: App.Theme.fileIcon
                        }
                        
                        Text {
                            Layout.fillWidth: true
                            text: modelData
                            font.pixelSize: 12
                            color: App.Theme.textSecondary
                            elide: Text.ElideRight
                        }
                    }
                    
                    MouseArea {
                        id: recMouse
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: root.recentFileClicked(modelData)
                    }
                }
                
                Text {
                    anchors.centerIn: parent
                    text: App.Strings.noRecentFiles
                    font.pixelSize: 11
                    color: App.Theme.textMuted
                    visible: recentList.count === 0
                }
            }
        }
        
        // ── Divider
        Rectangle {
            Layout.fillWidth: true
            Layout.leftMargin: 12
            Layout.rightMargin: 12
            height: 1
            color: App.Theme.border
        }
        
        // ── Settings row (38px)
        Rectangle {
            id: settingsRow
            Layout.fillWidth: true
            Layout.leftMargin: 8
            Layout.rightMargin: 8
            Layout.topMargin: 4
            height: 38
            radius: 8
            property bool hovered: false
            color: hovered ? App.Theme.hoverSubtle : "transparent"

            RowLayout {
                anchors { fill: parent; leftMargin: 8; rightMargin: 8 }
                spacing: 10

                Text {
                    text: "⚙"
                    font.pixelSize: 14
                    color: App.Theme.textPrimary
                }

                Text {
                    text: App.Strings.settingsLabel
                    font.pixelSize: 12
                    color: App.Theme.textPrimary
                    Layout.fillWidth: true
                }
            }

            MouseArea {
                id: settingsMouse
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onEntered: settingsRow.hovered = true
                onExited: settingsRow.hovered = false
                onClicked: root.settingsClicked()
            }
        }

        // ── Auth row (38px) — "Log out" when signed in, "Login / Sign Up" when signed out
        Rectangle {
            id: authRow
            Layout.fillWidth: true
            Layout.leftMargin: 8
            Layout.rightMargin: 8
            Layout.bottomMargin: 4
            height: 38
            radius: 8
            property bool loggedIn: root.userEmail.length > 0
            property bool hovered: false
            color: hovered ? (loggedIn ? App.Theme.hoverDanger : App.Theme.hoverSubtle) : "transparent"

            RowLayout {
                anchors { fill: parent; leftMargin: 8; rightMargin: 8 }
                spacing: 10

                Text {
                    text: authRow.loggedIn ? "→" : "←"
                    font.pixelSize: 14
                    color: authRow.hovered && authRow.loggedIn ? App.Theme.dangerText : App.Theme.textPrimary
                }

                Text {
                    text: authRow.loggedIn ? App.Strings.logOutLabel : App.Strings.loginSignUpLabel
                    font.pixelSize: 12
                    color: authRow.hovered && authRow.loggedIn ? App.Theme.dangerText : App.Theme.textPrimary
                    Layout.fillWidth: true
                }
            }

            MouseArea {
                id: authMouse
                anchors.fill: parent
                hoverEnabled: true
                cursorShape: Qt.PointingHandCursor
                onEntered: authRow.hovered = true
                onExited: authRow.hovered = false
                onClicked: {
                    if (authRow.loggedIn)
                        root.signOutClicked()
                    else
                        root.loginSignupClicked()
                }
            }
        }
        
        // ── User card (48px) - bottom border
        Rectangle {
            Layout.fillWidth: true
            height: 1
            color: App.Theme.border
        }
        
        Rectangle {
            Layout.fillWidth: true
            Layout.leftMargin: 8
            Layout.rightMargin: 8
            Layout.topMargin: 8
            Layout.bottomMargin: 8
            height: 48
            radius: 8
            color: App.Theme.muted
            
            ColumnLayout {
                anchors { fill: parent; leftMargin: 10; rightMargin: 10 }
                spacing: 1

                Text {
                    text: root.userName
                    font.pixelSize: 12
                    font.weight: Font.Medium
                    color: App.Theme.textPrimary
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                    visible: root.userEmail.length > 0 && root.userName !== root.userEmail
                }

                Text {
                    text: root.userEmail.length > 0 ? root.userEmail : App.Strings.notSignedIn
                    font.pixelSize: 11
                    color: App.Theme.textMuted
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                }
            }
        }
    }
}
