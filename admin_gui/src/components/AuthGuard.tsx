import { lazy, useEffect, useState } from "react";
import { useNavigate, Navigate, Outlet } from "react-router-dom";
import { signOut, sendEmailVerification } from "firebase/auth";

import { auth } from "@/lib/firebase";
import { useAuth } from "@/contexts/AuthContext";

import {
  Loader2,
  ShieldX,
  AlertCircle,
  MailCheck,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { MSG } from "@/lib/constants";

const MfaSetupView = lazy(() =>
  import("@/views/MfaSetupView").then((m) => ({
    default: m.MfaSetupView,
  }))
);

export function AuthGuard() {
  const {
    user,
    loading,
    emailVerified,
    backendVerified,
    mfaEnrolled,
    accessCheckError,
  } = useAuth();

  // AuthContext keeps loading=true until Firebase auth state
  // and any required backend verification are fully resolved.
  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <Loader2 className="h-6 w-6 animate-spin text-primary" />
      </div>
    );
  }

  // Protected routes require an authenticated Firebase user.
  if (!user) {
    return <Navigate to="/login" replace />;
  }

  // Email verification must be completed before MFA setup
  // and backend verification can proceed.
  if (emailVerified === false) {
    return <VerifyEmailView />;
  }

  // MFA enrollment is required before backend identity verification.
  if (mfaEnrolled === false) {
    return <MfaSetupView />;
  }

  // Defensive fallback: once loading is false and Firebase-side
  // requirements are satisfied, backend verification should normally
  // already be resolved.
  if (backendVerified === null && !accessCheckError) {
    return (
      <div className="flex h-screen items-center justify-center bg-background">
        <Loader2 className="h-6 w-6 animate-spin text-primary" />
      </div>
    );
  }

  if (accessCheckError) {
    return <AccessCheckErrorView />;
  }

  if (backendVerified === false) {
    return <AccessDeniedView />;
  }

  // All authentication and backend checks passed.
  // Render the protected child route selected by React Router.
  return <Outlet />;
}

function VerifyEmailView() {
  const { reloadUser } = useAuth();

  const [checking, setChecking] = useState(false);
  const [resending, setResending] = useState(false);
  const [stillUnverified, setStillUnverified] = useState(false);
  const [resendCooldown, setResendCooldown] = useState(false);

  useEffect(() => {
    if (auth.currentUser) {
      sendEmailVerification(auth.currentUser).catch(() => {});
    }
  }, []);

  async function handleVerifyDone() {
    setChecking(true);
    setStillUnverified(false);

    await reloadUser();

    if (!auth.currentUser?.emailVerified) {
      setStillUnverified(true);
    }

    setChecking(false);
  }

  async function handleResend() {
    if (!auth.currentUser || resendCooldown) {
      return;
    }

    setResending(true);

    await sendEmailVerification(auth.currentUser).catch(() => {});

    setResending(false);
    setResendCooldown(true);

    setTimeout(() => {
      setResendCooldown(false);
    }, 30_000);
  }

  function handleSignOut() {
    signOut(auth).then(() => {
      window.location.href = "/login";
    });
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-primary/10">
            <MailCheck className="h-5 w-5 text-primary" />
          </div>

          <div className="text-center">
            <h1 className="text-xl font-semibold text-foreground">
              {MSG.Auth.VERIFY_EMAIL_TITLE}
            </h1>
          </div>
        </div>

        <div className="space-y-4 rounded-xl border border-border bg-card p-6 text-center shadow-sm">
          <p className="text-sm text-muted-foreground">
            {MSG.Auth.VERIFY_EMAIL_BODY}
          </p>

          {stillUnverified && (
            <p className="text-sm text-destructive">
              {MSG.Auth.VERIFY_EMAIL_STILL_UNVERIFIED}
            </p>
          )}

          <div className="flex flex-col gap-2">
            <Button
              className="w-full"
              onClick={handleVerifyDone}
              disabled={checking}
            >
              {checking ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                MSG.Auth.VERIFY_EMAIL_DONE
              )}
            </Button>

            <Button
              variant="outline"
              className="w-full"
              onClick={handleResend}
              disabled={resending || resendCooldown}
            >
              {resending ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                MSG.Auth.VERIFY_EMAIL_RESEND
              )}
            </Button>

            <Button
              variant="ghost"
              className="w-full"
              onClick={handleSignOut}
            >
              Sign out
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

function AccessCheckErrorView() {
  function handleRetry() {
    window.location.reload();
  }

  function handleSignOut() {
    signOut(auth).then(() => {
      window.location.href = "/login";
    });
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-destructive/10">
            <AlertCircle className="h-5 w-5 text-destructive" />
          </div>

          <div className="text-center">
            <h1 className="text-xl font-semibold text-foreground">
              Server Error
            </h1>
          </div>
        </div>

        <div className="space-y-4 rounded-xl border border-border bg-card p-6 text-center shadow-sm">
          <p className="text-sm text-muted-foreground">
            {MSG.Auth.ACCESS_CHECK_ERROR}
          </p>

          <div className="flex flex-col gap-2">
            <Button
              className="w-full"
              onClick={handleRetry}
            >
              Try again
            </Button>

            <Button
              variant="outline"
              className="w-full"
              onClick={handleSignOut}
            >
              Sign out
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

function AccessDeniedView() {
  const navigate = useNavigate();

  function handleSignOut() {
    signOut(auth).then(() => {
      navigate("/login", {
        replace: true,
      });
    });
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-destructive/10">
            <ShieldX className="h-5 w-5 text-destructive" />
          </div>

          <div className="text-center">
            <h1 className="text-xl font-semibold text-foreground">
              Access Denied
            </h1>
          </div>
        </div>

        <div className="space-y-4 rounded-xl border border-border bg-card p-6 text-center shadow-sm">
          <p className="text-sm text-muted-foreground">
            {MSG.Auth.NO_APP_ACCESS}
          </p>

          <Button
            variant="outline"
            className="w-full"
            onClick={handleSignOut}
          >
            Sign out
          </Button>
        </div>
      </div>
    </div>
  );
}