import { useRef, useState } from "react";
import { Navigate, useSearchParams } from "react-router-dom";
import { Loader2, ShieldCheck, ArrowLeft } from "lucide-react";

import {
  type MultiFactorError,
  type MultiFactorResolver,
  TotpMultiFactorGenerator,
  getMultiFactorResolver,
  sendPasswordResetEmail,
  signInWithEmailAndPassword,
} from "firebase/auth";

import { auth } from "@/lib/firebase";
import { useAuth } from "@/contexts/AuthContext";
import { MSG } from "@/lib/constants";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

type LoginStep = "credentials" | "mfa";

export function LoginView() {
  const { user, loading } = useAuth();

  const [searchParams] = useSearchParams();

  // MfaSetupView redirects here after first-time MFA enrollment
  // so the user can complete a fresh sign-in with the new factor.
  const mfaJustEnrolled =
    searchParams.get("reason") === "mfa_enrolled";

  const [step, setStep] =
    useState<LoginStep>("credentials");

  // Credentials-step state.
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  // The resolver represents a pending Firebase MFA login.
  // It is kept in a ref because changing it does not need
  // to cause a React re-render.
  const resolverRef =
    useRef<MultiFactorResolver | null>(null);

  const [totpCode, setTotpCode] = useState("");

  const [error, setError] = useState("");
  const [submitting, setSubmitting] =
    useState(false);

  const [resetSending, setResetSending] =
    useState(false);

  const [resetSent, setResetSent] =
    useState(false);

  // If Firebase has already restored an authenticated session,
  // leave the login page. The protected route's AuthGuard will
  // decide whether email verification, MFA setup, or backend
  // verification is still required.
  if (!loading && user) {
    return <Navigate to="/orgs" replace />;
  }

  async function handleEmailLogin(
    e: React.FormEvent
  ) {
    e.preventDefault();

    setError("");
    setResetSent(false);
    setSubmitting(true);

    try {
      await signInWithEmailAndPassword(
        auth,
        email.trim(),
        password
      );

      // If Firebase returns successfully, no additional MFA
      // challenge was required for this sign-in.
      //
      // onAuthStateChanged will update AuthContext, and the
      // authenticated user will leave this page automatically.
    } catch (err: unknown) {
      if (isMfaError(err)) {
        // Firebase signals an MFA challenge by throwing
        // auth/multi-factor-auth-required.
        //
        // The resolver contains the pending first-factor
        // credential and the enrolled MFA methods.
        resolverRef.current =
          getMultiFactorResolver(auth, err);

        setTotpCode("");
        setStep("mfa");
      } else {
        setError(
          err instanceof Error
            ? friendlyFirebaseError(err.message)
            : "Login failed."
        );
      }
    } finally {
      setSubmitting(false);
    }
  }

  async function handleMfaVerify(
    e: React.FormEvent
  ) {
    e.preventDefault();

    const resolver = resolverRef.current;

    if (!resolver) {
      setError(
        "The MFA sign-in session is no longer available. Please sign in again."
      );

      setStep("credentials");
      return;
    }

    setError("");
    setSubmitting(true);

    try {
      // A Firebase account could theoretically have more than
      // one MFA factor. Find the enrolled TOTP factor specifically.
      const totpHint = resolver.hints.find(
        (hint) =>
          hint.factorId ===
          TotpMultiFactorGenerator.FACTOR_ID
      );

      if (!totpHint) {
        setError(
          "No TOTP factor is enrolled for this account."
        );
        return;
      }

      // Build an MFA assertion using the enrolled factor
      // and the 6-digit code entered by the user.
      const assertion =
        TotpMultiFactorGenerator.assertionForSignIn(
          totpHint.uid,
          totpCode
        );

      // Completing the resolver finishes the Firebase sign-in.
      //
      // Firebase then updates auth.currentUser and fires
      // onAuthStateChanged. AuthContext takes over from there:
      //
      // Firebase user
      //   → email verification
      //   → MFA enrollment state
      //   → backend verification
      //   → AuthGuard
      await resolver.resolveSignIn(assertion);
    } catch (err: unknown) {
      setError(
        err instanceof Error
          ? friendlyFirebaseError(err.message)
          : "Verification failed."
      );

      setTotpCode("");
    } finally {
      setSubmitting(false);
    }
  }

  async function handlePasswordReset() {
    setError("");
    setResetSent(false);

    const trimmedEmail = email.trim();

    if (!trimmedEmail) {
      setError(
        "Enter your work email first, then select Forgot password."
      );
      return;
    }

    setResetSending(true);

    try {
      await sendPasswordResetEmail(
        auth,
        trimmedEmail
      );

      setResetSent(true);
    } catch (err: unknown) {
      setError(
        err instanceof Error
          ? friendlyFirebaseError(err.message)
          : "Unable to send password reset email."
      );
    } finally {
      setResetSending(false);
    }
  }

  function handleBackToCredentials() {
    // Abandon the pending MFA challenge and return to
    // the first-factor login form.
    resolverRef.current = null;

    setTotpCode("");
    setError("");
    setStep("credentials");
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <div className="w-full max-w-sm">
        {mfaJustEnrolled && (
          <div className="mb-5 rounded-lg border border-green-200 bg-green-50 px-4 py-3 text-sm text-green-800">
            {MSG.Auth.MFA_ENROLLED_BANNER}
          </div>
        )}

        <div className="mb-8 flex flex-col items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-primary">
            <ShieldCheck className="h-5 w-5 text-white" />
          </div>

          <div className="text-center">
            <h1 className="text-xl font-semibold text-foreground">
              {MSG.Auth.SIGN_IN_TITLE}
            </h1>

            <p className="mt-1 text-sm text-muted-foreground">
              {step === "mfa"
                ? MSG.Auth.MFA_SUBTITLE
                : MSG.Auth.SIGN_IN_SUBTITLE}
            </p>
          </div>
        </div>

        <div className="rounded-xl border border-border bg-card p-6 shadow-sm">
          {step === "credentials" ? (
            <form
              onSubmit={handleEmailLogin}
              className="space-y-4"
            >
              <div className="space-y-1.5">
                <Label htmlFor="email">
                  Work email
                </Label>

                <Input
                  id="email"
                  type="email"
                  placeholder="you@example.com"
                  autoComplete="email"
                  value={email}
                  onChange={(e) =>
                    setEmail(e.target.value)
                  }
                  required
                />
              </div>

              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <Label htmlFor="password">
                    Password
                  </Label>

                  <button
                    type="button"
                    onClick={handlePasswordReset}
                    disabled={resetSending}
                    className="text-xs text-primary hover:underline disabled:opacity-50"
                  >
                    {resetSending
                      ? "Sending..."
                      : "Forgot password?"}
                  </button>
                </div>

                <Input
                  id="password"
                  type="password"
                  placeholder="••••••••"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) =>
                    setPassword(e.target.value)
                  }
                  required
                />
              </div>

              {resetSent && (
                <p className="rounded-md bg-green-50 px-3 py-2 text-xs text-green-800">
                  Password reset email sent. Check
                  your inbox and spam folder.
                </p>
              )}

              {error && (
                <ErrorBanner message={error} />
              )}

              <Button
                type="submit"
                className="w-full"
                disabled={submitting}
              >
                {submitting && (
                  <Loader2 className="h-4 w-4 animate-spin" />
                )}

                Sign in securely
              </Button>
            </form>
          ) : (
            <form
              onSubmit={handleMfaVerify}
              className="space-y-4"
            >
              <div className="space-y-1.5">
                <Label htmlFor="totp-code">
                  6-digit code
                </Label>

                <Input
                  id="totp-code"
                  type="text"
                  inputMode="numeric"
                  placeholder="000000"
                  maxLength={6}
                  autoComplete="one-time-code"
                  autoFocus
                  value={totpCode}
                  onChange={(e) =>
                    setTotpCode(
                      e.target.value.replace(
                        /\D/g,
                        ""
                      )
                    )
                  }
                  required
                />
              </div>

              {error && (
                <ErrorBanner message={error} />
              )}

              <Button
                type="submit"
                className="w-full"
                disabled={
                  submitting ||
                  totpCode.length !== 6
                }
              >
                {submitting && (
                  <Loader2 className="h-4 w-4 animate-spin" />
                )}

                Verify
              </Button>

              <button
                type="button"
                onClick={handleBackToCredentials}
                className="flex w-full items-center justify-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
              >
                <ArrowLeft className="h-3 w-3" />
                Back to sign in
              </button>
            </form>
          )}
        </div>
      </div>
    </div>
  );
}

function ErrorBanner({
  message,
}: {
  message: string;
}) {
  return (
    <p className="rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
      {message}
    </p>
  );
}

function isMfaError(
  err: unknown
): err is MultiFactorError {
  return (
    typeof err === "object" &&
    err !== null &&
    "code" in err &&
    (err as { code: string }).code ===
      "auth/multi-factor-auth-required"
  );
}

function friendlyFirebaseError(
  msg: string
): string {
  if (
    msg.includes("invalid-totp-code") ||
    msg.includes("INVALID_OTP")
  ) {
    return "Incorrect code. Check your authenticator app and try again.";
  }

  if (
    msg.includes("user-not-found") ||
    msg.includes("wrong-password") ||
    msg.includes("invalid-credential")
  ) {
    return "Invalid email or password.";
  }

  if (msg.includes("invalid-email")) {
    return "Enter a valid email address.";
  }

  if (msg.includes("too-many-requests")) {
    return "Too many attempts. Try again later.";
  }

  return "Authentication failed. Please try again.";
}