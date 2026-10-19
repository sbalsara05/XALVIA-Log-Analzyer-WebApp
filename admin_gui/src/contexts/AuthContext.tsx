import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { type User, onAuthStateChanged, multiFactor } from "firebase/auth";
import { auth } from "@/lib/firebase";
import { apiClient, ApiError } from "@/lib/api";

interface AuthContextValue {
  user: User | null;
  loading: boolean;
  // null = onAuthStateChanged not yet fired.
  emailVerified: boolean | null;
  // null = check in-flight (show spinner); false = confirmed no admin access.
  hasAdminAccess: boolean | null;
  // null = check in-flight; false = not enrolled (must complete TOTP setup).
  mfaEnrolled: boolean | null;
  // true when /api/me/access returned a server/network error (not a 403 denial).
  accessCheckError: boolean;
  reloadUser: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue>({
  user: null,
  loading: true,
  emailVerified: null,
  hasAdminAccess: null,
  mfaEnrolled: null,
  accessCheckError: false,
  reloadUser: async () => {},
});

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  const [emailVerified, setEmailVerified] = useState<boolean | null>(null);
  const [hasAdminAccess, setHasAdminAccess] = useState<boolean | null>(null);
  const [mfaEnrolled, setMfaEnrolled] = useState<boolean | null>(null);
  const [accessCheckError, setAccessCheckError] = useState(false);

  useEffect(() => {
    return onAuthStateChanged(auth, async (firebaseUser) => {
      setUser(firebaseUser);
      setLoading(false);

      if (firebaseUser) {
        setEmailVerified(firebaseUser.emailVerified);
        setAccessCheckError(false);
        const enrolled = multiFactor(firebaseUser).enrolledFactors.length > 0;
        setMfaEnrolled(enrolled);

        // Gate is strictly ordered: email must be verified before MFA enrollment
        // is checked, which must pass before the gateway access check fires.
        // Each step short-circuits — AuthGuard renders the correct blocking view
        // and no further checks run for that stage.
        if (firebaseUser.emailVerified && enrolled) {
          try {
            await apiClient.get("/api/me/access");
            setHasAdminAccess(true);
          } catch (err) {
            // Distinguish a genuine 403 denial from a server/network error so
            // AuthGuard can show the right message to the user.
            if (err instanceof ApiError && err.status === 403) {
              setHasAdminAccess(false);
            } else {
              setAccessCheckError(true);
              setHasAdminAccess(false);
            }
          }
        }
        // unverified or unenrolled: skip the access check entirely.
        // AuthGuard will render the appropriate blocking view before
        // ever consulting hasAdminAccess.
      } else {
        setEmailVerified(null);
        setHasAdminAccess(null);
        setMfaEnrolled(null);
        setAccessCheckError(false);
      }
    });
  }, []);

  // reload() mutates auth.currentUser in place without triggering onAuthStateChanged,
  // so we read and set emailVerified manually after the reload.
  async function reloadUser() {
    if (!auth.currentUser) return;
    await auth.currentUser.reload();
    setEmailVerified(auth.currentUser.emailVerified);
  }

  return (
    <AuthContext.Provider value={{ user, loading, emailVerified, hasAdminAccess, mfaEnrolled, accessCheckError, reloadUser }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}
