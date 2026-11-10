import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";

import {
  type User,
  onAuthStateChanged,
  multiFactor,
} from "firebase/auth";

import { auth } from "@/lib/firebase";
import { apiClient, ApiError } from "@/lib/api";

interface AuthContextValue {
  user: User | null;

  // true while Firebase auth state and any required backend
  // verification are still being resolved.
  loading: boolean;

  // null = authentication state has not been resolved yet.
  emailVerified: boolean | null;

  // null = backend access check has not run yet.
  // true = backend confirmed admin access.
  // false = backend explicitly denied access, or the check failed.
  // accessCheckError distinguishes server/network failures.
  hasAdminAccess: boolean | null;

  // null = authentication state has not been resolved yet.
  // false = user still needs to complete TOTP enrollment.
  mfaEnrolled: boolean | null;

  // true when backend verification failed because of a
  // server/network problem rather than an explicit denial.
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

export function AuthProvider({
  children,
}: {
  children: ReactNode;
}) {
  const [user, setUser] =
    useState<User | null>(null);

  const [loading, setLoading] =
    useState(true);

  const [emailVerified, setEmailVerified] =
    useState<boolean | null>(null);

  const [hasAdminAccess, setHasAdminAccess] =
    useState<boolean | null>(null);

  const [mfaEnrolled, setMfaEnrolled] =
    useState<boolean | null>(null);

  const [accessCheckError, setAccessCheckError] =
    useState(false);

  useEffect(() => {
    return onAuthStateChanged(
      auth,
      async (firebaseUser) => {
        setLoading(true);
        setUser(firebaseUser);
        setAccessCheckError(false);

        if (firebaseUser) {
          setEmailVerified(
            firebaseUser.emailVerified
          );

          const enrolled =
            multiFactor(firebaseUser)
              .enrolledFactors.length > 0;

          setMfaEnrolled(enrolled);

          // Authentication gates:
          // 1. email verification
          // 2. MFA enrollment
          // 3. backend access verification
          if (
            firebaseUser.emailVerified &&
            enrolled
          ) {
            try {
              await apiClient.get(
                "/api/me/access"
              );

              setHasAdminAccess(true);
            } catch (err) {
              if (
                err instanceof ApiError &&
                err.status === 403
              ) {
                setHasAdminAccess(false);
              } else {
                setAccessCheckError(true);
                setHasAdminAccess(false);
              }
            }
          } else {
            // Do not check backend access until the
            // Firebase-side requirements are complete.
            setHasAdminAccess(null);
          }
        } else {
          // Clear all derived auth state after sign-out.
          setEmailVerified(null);
          setHasAdminAccess(null);
          setMfaEnrolled(null);
          setAccessCheckError(false);
        }

        setLoading(false);
      }
    );
  }, []);

  // Firebase reload() updates auth.currentUser in place
  // without necessarily triggering onAuthStateChanged,
  // so refresh all derived authentication state here.
  async function reloadUser() {
    const currentUser = auth.currentUser;

    if (!currentUser) {
      return;
    }

    setLoading(true);
    setAccessCheckError(false);

    try {
      await currentUser.reload();

      const refreshedUser =
        auth.currentUser;

      if (!refreshedUser) {
        setUser(null);
        setEmailVerified(null);
        setMfaEnrolled(null);
        setHasAdminAccess(null);
        return;
      }

      setUser(refreshedUser);
      setEmailVerified(
        refreshedUser.emailVerified
      );

      const enrolled =
        multiFactor(refreshedUser)
          .enrolledFactors.length > 0;

      setMfaEnrolled(enrolled);

      if (
        refreshedUser.emailVerified &&
        enrolled
      ) {
        try {
          await apiClient.get(
            "/api/me/access"
          );

          setHasAdminAccess(true);
        } catch (err) {
          if (
            err instanceof ApiError &&
            err.status === 403
          ) {
            setHasAdminAccess(false);
          } else {
            setAccessCheckError(true);
            setHasAdminAccess(false);
          }
        }
      } else {
        setHasAdminAccess(null);
      }
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthContext.Provider
      value={{
        user,
        loading,
        emailVerified,
        hasAdminAccess,
        mfaEnrolled,
        accessCheckError,
        reloadUser,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}