import QtQuick 2.15
import QtCharts 2.15

// ─────────────────────────────────────────────────────────────────────────────
// VisualizationPanelLogic.qml - Logic layer for VisualizationPanel
// References to chart items are injected by VisualizationPanel.qml so that
// all rendering decisions live here rather than inline in the UI file.
// ─────────────────────────────────────────────────────────────────────────────

QtObject {
    id: logic

    property var panel: null     // VisualizationPanel root Item
    property var chartView: null // ChartView { id: chartView }
    property var axisX: null     // ValueAxis { id: axisX }
    property var axisY: null     // ValueAxis { id: axisY }
    
    // Bounds cache for handling custom zoom/pan resets natively
    property var originalXRange: []
    property var originalYRange: []

    function clearChart() {
        if (chartView) chartView.removeAllSeries()
        panel.plotFileName = ""
        panel.plotSource = ""
        originalXRange = []
        originalYRange = []
    }

    function setupConfig(spec) {
        if (!chartView) return
        chartView.removeAllSeries()
        if (!spec || !spec.series || spec.series.length === 0) return

        // Chart title
        if (spec.title) chartView.title = spec.title

        // Legend — Go drives visibility and position
        if (spec.legend !== undefined) {
            chartView.legend.visible = spec.legend.visible !== false
            var posMap = {
                "top":    Qt.AlignTop,
                "bottom": Qt.AlignBottom,
                "left":   Qt.AlignLeft,
                "right":  Qt.AlignRight
            }
            chartView.legend.alignment = posMap[spec.legend.position] || Qt.AlignBottom
        }

        var created = []
        for (var i = 0; i < spec.series.length; i++) {
            var s = spec.series[i]
            var ls = chartView.createSeries(ChartView.SeriesTypeLine, s.label, axisX, axisY)
            ls.color         = s.color       || "#1f77b4"
            ls.width         = s.line_width  || 1.5
            ls.objectName    = "series_" + i
            ls.pointsVisible = (s.marker_style !== undefined && s.marker_style !== "none")
            if (ls.pointsVisible && s.marker_size) ls.markerSize = s.marker_size
            created.push(ls)
        }

        // Axis labels — Go sends fully-formatted strings, render as-is
        if (spec.x_axis)                        axisX.titleText = spec.x_axis.label
        if (spec.y_axes && spec.y_axes.length > 0) axisY.titleText = spec.y_axes[0].label
        // Qt Charts does not auto-range after replace() — must set min/max explicitly
        if (spec.x_range && spec.x_range.length === 2) {
            axisX.min = spec.x_range[0]
            axisX.max = spec.x_range[1]
            originalXRange = [spec.x_range[0], spec.x_range[1]]
        } else {
            originalXRange = []
        }
        
        if (spec.y_range && spec.y_range.length === 2) {
            axisY.min = spec.y_range[0]
            axisY.max = spec.y_range[1]
            originalYRange = [spec.y_range[0], spec.y_range[1]]
        } else {
            originalYRange = []
        }

        // Apply file metadata to panel header
        panel.plotFileName = spec._file_name || ""
        panel.plotSource   = spec._source    || ""

        // Emit series refs to Python — cast to QXYSeries on the Python side via shiboken6
        panel.chartSeriesReady(created)
    }
    
    // ══════════════ Chart Interaction Wrappers (Zoom + Pan)

    function handleZoomIn() {
        if (chartView) chartView.zoomIn()
    }

    function handleZoomOut() {
        if (chartView) chartView.zoomOut()
    }

    function handleZoomReset() {
        if (!chartView || !axisX || !axisY) return
        
        // Qt's native zoomReset only pops the internal rectangle zoom stack,
        // it doesn't revert our custom properties if we scrolled physically.
        chartView.zoomReset()
        
        if (originalXRange && originalXRange.length === 2) {
            axisX.min = originalXRange[0]
            axisX.max = originalXRange[1]
        }
        if (originalYRange && originalYRange.length === 2) {
            axisY.min = originalYRange[0]
            axisY.max = originalYRange[1]
        }
    }

    function handlePan(lastX, lastY, currentX, currentY) {
        if (!chartView || !axisX || !axisY) return
        
        if (chartView.series(0)) {
            var s0 = chartView.series(0)
            
            // Convert raw physical pixels into logical absolute chart coordinates
            var oldVal = chartView.mapToValue(Qt.point(lastX, lastY), s0)
            var newVal = chartView.mapToValue(Qt.point(currentX, currentY), s0)
            
            var dx_val = newVal.x - oldVal.x
            var dy_val = newVal.y - oldVal.y
            
            // Bypass Qt's slow scroll methods and translate boundaries directly
            axisX.min -= dx_val
            axisX.max -= dx_val
            axisY.min -= dy_val
            axisY.max -= dy_val
        }
    }
}
