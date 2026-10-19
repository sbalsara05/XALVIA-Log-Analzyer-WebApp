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

  // null = backend identity check has not run or is not applicable yet.
  // true = backend accepted the authenticated user.
  // false = backend verification did not succeed.
  //         Check accessCheckError to distinguish an explicit denial
  //         from a server/network failure.
  backendVerified: boolean | null;

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
  backendVerified: null,
  mfaEnrolled: null,
  accessCheckError: false,
  reloadUser: async () => {},
});

export function AuthProvider({
  children,
}: {
  children: ReactNode;
}) {
  const [user, setUser] = useState<User | null>(null);

  const [loading, setLoading] = useState(true);

  const [emailVerified, setEmailVerified] = useState<boolean | null>(null);

  const [backendVerified, setBackendVerified] = useState<boolean | null>(null);

  const [mfaEnrolled, setMfaEnrolled] = useState<boolean | null>(null);

  const [accessCheckError, setAccessCheckError] = useState(false);

  useEffect(() => {
    return onAuthStateChanged(
      auth,
      async (firebaseUser) => {
        // A new Firebase auth state is being processed.
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

          // Authentication gates are evaluated in order:
          // 1. email verification
          // 2. MFA enrollment
          // 3. backend identity verification
          //
          // Each step short-circuits so the UI can render
          // the correct blocking state before later checks run.
          if (
            firebaseUser.emailVerified &&
            enrolled
          ) {
            try {
              await apiClient.get(
                "/api/profile"
              );

              setBackendVerified(true);
            } catch (err) {
              // Distinguish an explicit backend denial from
              // a server/network failure so AuthGuard can
              // present the correct message.
              if (
                err instanceof ApiError &&
                err.status === 403
              ) {
                setBackendVerified(false);
              } else {
                setAccessCheckError(true);
                setBackendVerified(false);
              }
            }
          } else {
            // Backend verification should not run until the
            // Firebase-side requirements are satisfied.
            setBackendVerified(null);
          }
        } else {
          // Clear all derived auth state after sign-out.
          setEmailVerified(null);
          setBackendVerified(null);
          setMfaEnrolled(null);
          setAccessCheckError(false);
        }

        // Auth initialization is complete only after any required
        // backend verification has also finished.
        setLoading(false);
      }
    );
  }, []);

  // reload() mutates auth.currentUser in place without triggering
  // onAuthStateChanged, so emailVerified must be refreshed manually.
  async function reloadUser() {
    const currentUser = auth.currentUser;

    if (!currentUser) {
      return;
    }

    setLoading(true);
    setAccessCheckError(false);

    try {
      await currentUser.reload();

      const refreshedUser = auth.currentUser;

      if (!refreshedUser) {
        setUser(null);
        setEmailVerified(null);
        setMfaEnrolled(null);
        setBackendVerified(null);
        return;
      }

      setUser(refreshedUser);
      setEmailVerified(refreshedUser.emailVerified);

      const enrolled =
        multiFactor(refreshedUser)
          .enrolledFactors.length > 0;

      setMfaEnrolled(enrolled);

      // If the user has now completed all Firebase-side
      // requirements, immediately continue with backend
      // identity verification.
      if (
        refreshedUser.emailVerified &&
        enrolled
      ) {
        try {
          await apiClient.get("/api/profile");
          setBackendVerified(true);
        } catch (err) {
          if (
            err instanceof ApiError &&
            err.status === 403
          ) {
            setBackendVerified(false);
          } else {
            setAccessCheckError(true);
            setBackendVerified(false);
          }
        }
      } else {
        setBackendVerified(null);
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
        backendVerified,
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