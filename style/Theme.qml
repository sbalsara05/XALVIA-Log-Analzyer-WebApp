pragma Singleton
import QtQuick 2.15

// ─────────────────────────────────────────────────────────────────────────────
// Theme.qml - Exact Lovable design tokens (converted from HSL to RGB)
// Source: index.css from Lovable
// ─────────────────────────────────────────────────────────────────────────────

QtObject {
    // ── Colors
    readonly property color background:  "#F0F2F5"
    readonly property color foreground:  "#1C2026"  // Pantone 20-0200-TPM (near-black)
    readonly property color card:        "#FFFFFF"

    // Brand palette
    readonly property color accent:      "#6ECEB2"  // Pantone 936-C  (mint/teal)
    readonly property color primary:     "#5B8DB8"  // Pantone 7688-C (steel blue)
    readonly property color primaryFg:   "#FFFFFF"

    readonly property color brandDark:   "#1C2026"  // Pantone 20-0200-TPM - dark buttons
    readonly property color brandPill:   "#EBF5FB"  // light blue tint of primary - active nav bg
    readonly property color brandPillText: "#5B8DB8" // same as primary

    readonly property color border:       "#E1E4E8"
    readonly property color borderAccent: "#6ECEB2"  // accent (brand mint) — full opacity
    readonly property color input:        "#E1E4E8"

    readonly property color textPrimary:   "#1C2026"  // Pantone 20-0200-TPM
    readonly property color textSecondary: "#667085"
    readonly property color textMuted:     "#98A2B3"

    readonly property color muted:       "#F8F9FB"  // log bg
    readonly property color mutedFg:     "#667085"

    // Status
    readonly property color statusOnline:  "#6ECEB2"  // Pantone 936-C (brand mint = connected)
    readonly property color statusOffline: "#D0D5DD"

    // File icon
    readonly property color fileIcon:    "#6ECEB2"  // Pantone 936-C (mint)
    
    // Hover states
    readonly property color hoverSubtle: "#F5F6F8"  // hsl(220 14% 96%)
    readonly property color hoverDanger: "#FEF3F2"  // hsl(0 85% 97%)
    readonly property color dangerText:  "#D92D20"  // hsl(0 72% 51%)
    
    // Error
    readonly property color errorBg:     "#FEF3F2"  // hsl(0 85% 97%)
    readonly property color errorBorder: "#FDA29B"  // hsl(0 70% 80%)
    readonly property color errorText:   "#B42318"  // hsl(0 70% 45%)
    
    // Disabled
    readonly property color disabled:    "#CED4DA"  // used for disabled analyze button
    readonly property color disabledText:"#8D98A4"
    
    // ── Typography
    readonly property string fontFamily: "Inter, 'Segoe UI', sans-serif"
    
    // ── Exact pixel sizes from Lovable
    // Sidebar
    readonly property int sidebarWidth:        240
    readonly property int sidebarHeaderHeight: 56
    readonly property int logoSize:            32
    readonly property int navItemHeight:       40
    readonly property int analyzeButtonHeight: 42
    readonly property int recentFileHeight:    32
    readonly property int settingsRowHeight:   38
    readonly property int userCardHeight:      48
    readonly property int avatarSize:          30
    
    // File Panel
    readonly property int filePanelWidth:      300
    readonly property int panelHeaderHeight:   48
    readonly property int inputHeight:         36
    readonly property int buttonHeight:        34
    readonly property int fileRowHeight:       34
    
    // Right Panel
    readonly property int deviceInfoWidth:     310
    readonly property int topRowHeight:        220
    readonly property int infoRowHeight:       26
    readonly property int exportButtonHeight:  30
    
    // Login
    readonly property int loginCardWidth:      460
    readonly property int loginInputHeight:    46
    readonly property int loginButtonHeight:   48
    readonly property int loginLogoSize:       36
    readonly property int mfaInputHeight:      56
    
    // ── Border radius (exact from Lovable)
    readonly property int radius5:  5   // file rows
    readonly property int radius6:  6   // buttons, inputs
    readonly property int radius8:  8   // nav items, cards
    readonly property int radius10: 10  // panels
    readonly property int radius16: 16  // login modal
    
    // ── Spacing (exact from Lovable)
    readonly property int spacing2:  2
    readonly property int spacing4:  4
    readonly property int spacing6:  6
    readonly property int spacing8:  8
    readonly property int spacing10: 10
    readonly property int spacing12: 12
    readonly property int spacing14: 14
    readonly property int spacing16: 16
    readonly property int spacing20: 20
    
    // ── Shadows
    readonly property string cardShadow: "0 1px 3px rgba(0, 0, 0, 0.1)"
}
