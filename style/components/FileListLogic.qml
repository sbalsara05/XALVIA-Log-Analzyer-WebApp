import QtQuick 2.15

// ─────────────────────────────────────────────────────────────────────────────
// FileListLogic.qml - Logic layer for FileList multi-select
// Owns selection state mutations and modifier-aware handlers
// ─────────────────────────────────────────────────────────────────────────────

QtObject {
    id: logic

    property var fileList: null  // FileList root Item

    // Plain click — select only this index, deselect all others
    function handleSingleClick(index) {
        if (fileList.selectedIndices.length === 1 && fileList.selectedIndices[0] === index) {
            fileList.selectedIndices = []
            fileList.selectionRevision++
            fileList.lastClickedIndex = -1
            fileList.selectionChanged([])
            return
        }
        fileList.selectedIndices = [index]
        fileList.selectionRevision++
        fileList.lastClickedIndex = index

        // Emit selected filenames
        var names = [fileList.model[index]]
        fileList.selectionChanged(names)
    }

    // Ctrl/Cmd click — toggle index in/out of selection without touching others
    function handleCtrlClick(index) {
        var sel = fileList.selectedIndices.slice()
        var idx = sel.indexOf(index)

        if (idx >= 0) {
            sel.splice(idx, 1)
        } else {
            sel.push(index)
        }

        fileList.selectedIndices = sel
        fileList.selectionRevision++
        fileList.lastClickedIndex = index

        // Emit selected filenames
        var names = sel.map(function(i) { return fileList.model[i] })
        fileList.selectionChanged(names)
    }

    // Shift click — range select from lastClickedIndex to index (inclusive)
    // lastClickedIndex is NOT updated, so the anchor stays fixed for repeated Shift-clicks
    function handleShiftClick(index) {
        if (fileList.lastClickedIndex < 0) {
            // No anchor yet, treat as single click
            handleSingleClick(index)
            return
        }

        var start = Math.min(fileList.lastClickedIndex, index)
        var end = Math.max(fileList.lastClickedIndex, index)
        var sel = []

        for (var i = start; i <= end; i++) {
            sel.push(i)
        }

        fileList.selectedIndices = sel
        fileList.selectionRevision++
        // Intentionally do NOT update lastClickedIndex so the anchor persists

        // Emit selected filenames
        var names = sel.map(function(i) { return fileList.model[i] })
        fileList.selectionChanged(names)
    }
}
