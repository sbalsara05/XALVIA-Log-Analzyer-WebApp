// Centralised user-visible strings — mirrors the MSG / constants pattern in src/constants.py.
// Views import from here; no inline string literals for user-facing copy.

export const MSG = {
  Orgs: {
    CREATED:           "Organization created",
    DELETED:           "Organization deleted",
    CREATE_FAILED:     "Failed to create org",
    DELETE_FAILED:     "Failed to delete org",
    EMPTY_TITLE:       "No organizations yet",
    EMPTY_BODY:        "Create your first sub-organization to get started.",
    COUNT:             (n: number) => `${n} organization${n !== 1 ? "s" : ""} in your admin tree`,
    DELETE_BODY:       "This will soft-delete the organization and all its sub-orgs, devices, and memberships. This action cannot be undone.",
  },

  Users: {
    SAVED:             "User permissions saved",
    REMOVED:           "User removed from organization",
    SAVE_FAILED:       "Failed to save user",
    INVITE_EMAIL_WARNING: "Invite email failed",
    REMOVE_FAILED:     "Failed to remove user",
    EMPTY_TITLE:       "No users in this organization",
    EMPTY_BODY:        "Add a user to grant them access.",
    SELECT_ORG_PROMPT: "Select an organization to view its members",
    REVOKE_BODY:       "This will revoke their membership from the selected organization.",
    ADMIN_PERMS_NOTE:  "(admins always have full access)",
  },

  Devices: {
    REGISTERED:        "Device registered",
    REMOVED:           "Device removed",
    REGISTER_FAILED:   "Failed to add device",
    REMOVE_FAILED:     "Failed to remove device",
    EMPTY_TITLE:       "No devices registered",
    EMPTY_BODY:        "Register a device to associate it with this organization.",
    SELECT_ORG_PROMPT: "Select an organization to view its devices",
    REMOVE_BODY:       (mac: string) => `This will soft-delete ${mac} from the organization. It can be re-registered later.`,
    MAC_HINT:          "Enter an alphanumeric device ID (letters and numbers only, e.g. DEVICE001)",
  },

  Auth: {
    SESSION_EXPIRED:   "Your session expired. Please sign in again.",
    NO_ADMIN_ACCESS:   "This account does not have admin access to any organization. Contact a Ricovr administrator.",
    ACCESS_CHECK_ERROR: "Could not verify your access — this may be a temporary server issue. Try again or sign out and back in.",
    SIGN_IN_TITLE:     "Ricovr Admin",
    SIGN_IN_SUBTITLE:  "Sign in to your admin account",
    MFA_SUBTITLE:      "Enter the 6-digit code from your authenticator app",
    MFA_SETUP_TITLE:   "Secure your account",
    MFA_SETUP_BODY:    "Scan this QR code with your authenticator app (Google Authenticator, Authy, etc.), then enter the 6-digit code to confirm.",
    MFA_ENROLLED:      "Authenticator app connected. You can now access the dashboard.",
    MFA_ENROLLED_BANNER: "Authenticator connected. Sign in with your 6-digit code to continue.",
    MFA_ENROLL_FAILED: "Code verification failed. Check your authenticator app and try again.",
    VERIFY_EMAIL_TITLE:           "Verify Your Email",
    VERIFY_EMAIL_BODY:            "We sent a verification link to your inbox. Click it, then return here.",
    VERIFY_EMAIL_DONE:            "I've Verified My Email",
    VERIFY_EMAIL_RESEND:          "Resend verification email",
    VERIFY_EMAIL_STILL_UNVERIFIED: "Email not yet verified. Please check your inbox and try again.",
  },
} as const;
