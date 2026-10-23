import QtQuick 2.15

// ─────────────────────────────────────────────────────────────────────────────
// SidebarLogic.qml - Logic layer for Sidebar
// Sidebar.qml has no complex functions — all state is simple property bindings
// (activeTab, analyzeEnabled, userEmail, userName, recentFiles) driven by
// MainWindow. Tab navigation logic lives in MainWindowLogic.handleTabChanged().
// This file establishes the pattern for future extraction.
// ─────────────────────────────────────────────────────────────────────────────

QtObject {
    id: logic

    property var sidebar: null  // Sidebar root Rectangle
}
