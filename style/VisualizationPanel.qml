import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15
import QtCharts 2.15
import "." as App
import "logic"

// ─────────────────────────────────────────────────────────────────────────────
// VisualizationPanel.qml - Right panel (UI only)
// Logic layer: logic/VisualizationPanelLogic.qml
// Layout: vertical SplitView (top info row | data viz)
//         top info row: horizontal SplitView (device info | log info)
// ─────────────────────────────────────────────────────────────────────────────

Item {
    id: root

    VisualizationPanelLogic {
        id: vizLogic
        panel: root
        chartView: chartView
        axisX: axisX
        axisY: axisY
    }

    property var deviceInfo: []
    property string logInfo: "(select a log file to inspect)"
    property bool plotVisible: false
    property var plotSpec: null
    property string plotFileName: ""
    property string plotSource: ""
    property bool isAnalyzing: false

    signal exportCsvClicked()
    signal screenshotClicked()
    signal chartSeriesReady(var seriesList)

    // Public API — called by MainWindowLogic to configure chart from a PlotSpec
    function setupConfig(spec) { vizLogic.setupConfig(spec) }
    function clearChart()      { vizLogic.clearChart() }

    function saveScreenshot(filename) {
        if (chartView) {
            chartView.grabToImage(function(result) {
                result.saveToFile(filename);
            });
        }
    }

    // ── Outer vertical SplitView: top info row | data viz
    SplitView {
        id: outerSplit
        anchors {
            fill: parent
            topMargin: 14
            leftMargin: 14
            rightMargin: 14
            bottomMargin: 14
        }
        orientation: Qt.Vertical

        handle: Rectangle {
            implicitWidth: outerSplit.width
            implicitHeight: 7
            color: "transparent"
            Rectangle {
                anchors.centerIn: parent
                width: parent.width
                height: 1
                color: SplitHandle.hovered || SplitHandle.pressed ? App.Theme.borderAccent : App.Theme.border
            }
            HoverHandler { cursorShape: Qt.SizeVerCursor }
        }

        // ── Top info row: horizontal SplitView (device info | log info)
        SplitView {
            id: innerSplit
            SplitView.preferredHeight: Math.max(200, Math.min(250, outerSplit.height * 0.28))
            SplitView.minimumHeight: 150
            orientation: Qt.Horizontal
            spacing: 12

            handle: Rectangle {
                implicitWidth: 7
                implicitHeight: innerSplit.height
                color: "transparent"
                Rectangle {
                    anchors.centerIn: parent
                    width: 1
                    height: parent.height
                    color: SplitHandle.hovered || SplitHandle.pressed ? App.Theme.borderAccent : App.Theme.border
                }
                HoverHandler { cursorShape: Qt.SizeHorCursor }
            }

            // ── Device Information
            Rectangle {
                SplitView.preferredWidth: 310
                SplitView.minimumWidth: 180
                radius: 10
                color: App.Theme.card
                border.color: App.Theme.borderAccent
                border.width: 1

                Text {
                    anchors {
                        top: parent.top
                        left: parent.left
                        topMargin: 14
                        leftMargin: 16
                    }
                    text: App.Strings.deviceInfoHeader
                    font.pixelSize: 13
                    font.weight: Font.Bold
                    color: App.Theme.foreground
                }

                Rectangle {
                    anchors {
                        top: parent.top
                        left: parent.left
                        right: parent.right
                        bottom: parent.bottom
                        topMargin: 42
                        leftMargin: 14
                        rightMargin: 14
                        bottomMargin: 14
                    }
                    color: App.Theme.muted
                    radius: 6
                    border.color: App.Theme.border
                    border.width: 1
                    clip: true

                    ScrollView {
                        anchors {
                            fill: parent
                            topMargin: 8
                            leftMargin: 12
                            rightMargin: 12
                            bottomMargin: 8
                        }
                        ScrollBar.horizontal.policy: ScrollBar.AsNeeded
                        clip: true

                        ColumnLayout {
                            spacing: 0

                            Repeater {
                                model: root.deviceInfo

                                delegate: RowLayout {
                                    height: 26
                                    spacing: 12

                                    Text {
                                        text: modelData.key
                                        font.pixelSize: 12
                                        color: App.Theme.textMuted
                                        Layout.preferredWidth: 80
                                    }

                                    Text {
                                        text: modelData.val
                                        font.pixelSize: 12
                                        font.weight: Font.DemiBold
                                        color: App.Theme.textPrimary
                                    }
                                }
                            }
                        }
                    }
                }
            }

            // ── Log Information
            Rectangle {
                SplitView.fillWidth: true
                SplitView.minimumWidth: 180
                radius: 10
                color: App.Theme.card
                border.color: App.Theme.borderAccent
                border.width: 1

                Text {
                    anchors {
                        top: parent.top
                        left: parent.left
                        topMargin: 14
                        leftMargin: 16
                    }
                    text: App.Strings.logInfoHeader
                    font.pixelSize: 13
                    font.weight: Font.Bold
                    color: App.Theme.foreground
                }

                Rectangle {
                    anchors {
                        top: parent.top
                        left: parent.left
                        right: parent.right
                        bottom: parent.bottom
                        topMargin: 42
                        leftMargin: 14
                        rightMargin: 14
                        bottomMargin: 14
                    }
                    color: App.Theme.muted
                    radius: 6
                    border.color: App.Theme.border
                    border.width: 1
                    clip: true

                    ScrollView {
                        anchors.fill: parent
                        anchors.margins: 12
                        contentWidth: availableWidth

                        Text {
                            width: parent.width
                            text: root.logInfo
                            font.pixelSize: 11
                            font.family: "Courier New, monospace"
                            color: App.Theme.foreground
                            wrapMode: Text.WordWrap
                            lineHeight: 1.5
                        }
                    }
                }
            }
        }

        // ── Data Visualization
        Rectangle {
            SplitView.fillHeight: true
            SplitView.minimumHeight: 200
            radius: 10
            color: App.Theme.card
            border.color: App.Theme.borderAccent
            border.width: 1

            Text {
                id: headerTitle
                anchors {
                    top: parent.top
                    left: parent.left
                    topMargin: 14
                    leftMargin: 16
                }
                text: App.Strings.dataVizHeader
                font.pixelSize: 13
                font.weight: Font.Bold
                color: App.Theme.foreground
            }

            Text {
                anchors {
                    left: headerTitle.right
                    verticalCenter: headerTitle.verticalCenter
                    leftMargin: 10
                }
                visible: root.plotFileName !== ""
                text: root.plotFileName + (root.plotSource !== "" ? "  ·  " + root.plotSource : "")
                font.pixelSize: 11
                color: App.Theme.textMuted
                elide: Text.ElideMiddle
                width: Math.min(implicitWidth, parent.width - headerTitle.width - 160)
            }

            // Toolbar (right aligned)
            RowLayout {
                anchors {
                    top: parent.top
                    right: parent.right
                    topMargin: 8
                    rightMargin: 12
                }
                height: 30
                spacing: 8
                visible: root.plotVisible

                // Zoom Reset
                Rectangle {
                    width: 30
                    height: 30
                    radius: 6
                    color: zoomResetMouse.containsMouse ? App.Theme.hoverSubtle : App.Theme.card
                    border.color: App.Theme.input
                    border.width: 1
                    Text {
                        anchors.centerIn: parent
                        text: "🏠"
                        font.pixelSize: 14
                    }
                    MouseArea {
                        id: zoomResetMouse
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: vizLogic.handleZoomReset()
                    }
                    ToolTip.visible: zoomResetMouse.containsMouse
                    ToolTip.text: App.Strings.tooltipZoomReset
                }

                // Zoom Out
                Rectangle {
                    width: 30
                    height: 30
                    radius: 6
                    color: zoomOutMouse.containsMouse ? App.Theme.hoverSubtle : App.Theme.card
                    border.color: App.Theme.input
                    border.width: 1
                    Text {
                        anchors.centerIn: parent
                        text: "➖"
                        font.pixelSize: 14
                    }
                    MouseArea {
                        id: zoomOutMouse
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: vizLogic.handleZoomOut()
                    }
                    ToolTip.visible: zoomOutMouse.containsMouse
                    ToolTip.text: App.Strings.tooltipZoomOut
                }

                // Zoom In
                Rectangle {
                    width: 30
                    height: 30
                    radius: 6
                    color: zoomInMouse.containsMouse ? App.Theme.hoverSubtle : App.Theme.card
                    border.color: App.Theme.input
                    border.width: 1
                    Text {
                        anchors.centerIn: parent
                        text: "➕"
                        font.pixelSize: 14
                    }
                    MouseArea {
                        id: zoomInMouse
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: vizLogic.handleZoomIn()
                    }
                    ToolTip.visible: zoomInMouse.containsMouse
                    ToolTip.text: App.Strings.tooltipZoomIn
                }

                // Screenshot
                Rectangle {
                    width: 30
                    height: 30
                    radius: 6
                    color: screenshotMouse.containsMouse ? App.Theme.hoverSubtle : App.Theme.card
                    border.color: App.Theme.input
                    border.width: 1
                    Text {
                        anchors.centerIn: parent
                        text: "📸"
                        font.pixelSize: 14
                    }
                    MouseArea {
                        id: screenshotMouse
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: root.screenshotClicked()
                    }
                    ToolTip.visible: screenshotMouse.containsMouse
                    ToolTip.text: App.Strings.tooltipScreenshot
                }

                // Export CSV button
                Rectangle {
                    height: 30
                    width: exportText.implicitWidth + 20
                    radius: 6
                    color: exportMouse.containsMouse ? App.Theme.hoverSubtle : App.Theme.card
                    border.color: App.Theme.input
                    border.width: 1

                    RowLayout {
                        anchors.centerIn: parent
                        spacing: 6
                        Text {
                            id: exportText
                            text: App.Strings.exportCsvLabel
                            font.pixelSize: 12
                            font.weight: Font.Medium
                            color: App.Theme.foreground
                        }
                    }

                    MouseArea {
                        id: exportMouse
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: root.exportCsvClicked()
                    }
                }
            }

            // Chart area
            Rectangle {
                objectName: "plotArea"
                anchors {
                    fill: parent
                    topMargin: 44
                    leftMargin: 14
                    rightMargin: 14
                    bottomMargin: 14
                }
                color: App.Theme.card
                radius: 6
                border.color: App.Theme.border
                border.width: 1

                // Placeholder
                ColumnLayout {
                    anchors.centerIn: parent
                    spacing: 8
                    visible: !root.plotVisible && !root.isAnalyzing

                    Text {
                        text: App.Strings.selectLogPrompt
                        font.pixelSize: 12
                        color: App.Theme.textMuted
                    }
                }

                // Busy spinner
                ColumnLayout {
                    anchors.centerIn: parent
                    spacing: 12
                    visible: root.isAnalyzing

                    BusyIndicator {
                        Layout.alignment: Qt.AlignHCenter
                        running: root.isAnalyzing
                        width: 40
                        height: 40
                        palette.dark: App.Theme.statusOnline
                    }

                    Text {
                        Layout.alignment: Qt.AlignHCenter
                        text: App.Strings.analyzingLabel
                        font.pixelSize: 12
                        color: App.Theme.textMuted
                    }
                }

                // Live chart
                ChartView {
                    id: chartView
                    anchors.fill: parent
                    visible: root.plotVisible
                    antialiasing: true
                    backgroundColor: "transparent"
                    plotAreaColor: "transparent"
                    legend.alignment: Qt.AlignBottom
                    legend.visible: true

                    ValueAxis {
                        id: axisX
                        labelFormat: "%.2f"
                    }

                    ValueAxis {
                        id: axisY
                        labelFormat: "%.0f"
                    }

                    MouseArea {
                        anchors.fill: parent
                        acceptedButtons: Qt.LeftButton

                        property real lastX: -1
                        property real lastY: -1

                        onPressed: (mouse) => {
                            lastX = mouse.x
                            lastY = mouse.y
                        }

                        onPositionChanged: (mouse) => {
                            if (pressed) {
                                vizLogic.handlePan(lastX, lastY, mouse.x, mouse.y)
                                lastX = mouse.x
                                lastY = mouse.y
                            }
                        }

                        onWheel: (wheel) => {
                            if (wheel.angleDelta.y > 0) {
                                vizLogic.handleZoomIn()
                            } else {
                                vizLogic.handleZoomOut()
                            }
                        }
                    }
                }
            }
        }
    }
}
