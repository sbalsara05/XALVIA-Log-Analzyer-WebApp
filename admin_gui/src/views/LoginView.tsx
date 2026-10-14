import { useRef, useState } from "react";
import { Navigate, useSearchParams } from "react-router-dom";
import { Loader2, ShieldCheck, ArrowLeft } from "lucide-react";
import { MSG } from "@/lib/constants";
import {
  MultiFactorError,
  MultiFactorResolver,
  TotpMultiFactorGenerator,
  getMultiFactorResolver,
  signInWithEmailAndPassword,
  signInWithPopup,
} from "firebase/auth";
import { auth, googleProvider } from "@/lib/firebase";
import { useAuth } from "@/contexts/AuthContext";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

type LoginStep = "credentials" | "mfa";

export function LoginView() {
  const { user, loading } = useAuth();
  const [searchParams] = useSearchParams();
  const sessionExpired = searchParams.get("reason") === "expired";
  const mfaJustEnrolled = searchParams.get("reason") === "mfa_enrolled";
  const [step, setStep] = useState<LoginStep>("credentials");

  // Credentials step state
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  // MFA step state — resolver is held in a ref (not state) so it never
  // triggers a re-render and isn't accessible from the JS console via React DevTools state
  const resolverRef = useRef<MultiFactorResolver | null>(null);
  const [totpCode, setTotpCode] = useState("");

  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  if (!loading && user) {
    return <Navigate to="/orgs" replace />;
  }

  async function handleEmailLogin(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      await signInWithEmailAndPassword(auth, email, password);
      // If we reach here with no throw, MFA is not enrolled — login complete.
    } catch (err: unknown) {
      if (isMfaError(err)) {
        // Firebase signals MFA is required via this specific error code.
        // The resolver object contains the pending credential — store it in a ref.
        resolverRef.current = getMultiFactorResolver(auth, err);
        setStep("mfa");
      } else {
        setError(err instanceof Error ? friendlyFirebaseError(err.message) : "Login failed");
      }
    } finally {
      setSubmitting(false);
    }
  }

  async function handleMfaVerify(e: React.FormEvent) {
    e.preventDefault();
    const resolver = resolverRef.current;
    if (!resolver) return;

    setError("");
    setSubmitting(true);
    try {
      // Find the TOTP factor among the enrolled hints
      const totpHint = resolver.hints.find(
        (h) => h.factorId === TotpMultiFactorGenerator.FACTOR_ID
      );
      if (!totpHint) {
        setError("No TOTP factor enrolled on this account.");
        return;
      }

      // Build the assertion from the 6-digit code
      const assertion = TotpMultiFactorGenerator.assertionForSignIn(
        totpHint.uid,
        totpCode
      );

      // Complete sign-in — on success Firebase updates auth.currentUser,
      // onAuthStateChanged fires, AuthContext sets user, AuthGuard redirects.
      await resolver.resolveSignIn(assertion);
    } catch (err: unknown) {
      setError(err instanceof Error ? friendlyFirebaseError(err.message) : "Verification failed");
      setTotpCode("");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleGoogleLogin() {
    setError("");
    setSubmitting(true);
    try {
      await signInWithPopup(auth, googleProvider);
    } catch (err: unknown) {
      if (isMfaError(err)) {
        resolverRef.current = getMultiFactorResolver(auth, err);
        setStep("mfa");
      } else {
        setError(err instanceof Error ? friendlyFirebaseError(err.message) : "Google sign-in failed");
      }
    } finally {
      setSubmitting(false);
    }
  }

  function handleBackToCredentials() {
    resolverRef.current = null;
    setTotpCode("");
    setError("");
    setStep("credentials");
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <div className="w-full max-w-sm">

        {/* MFA enrolled banner */}
        {mfaJustEnrolled && (
          <div className="mb-5 rounded-lg border border-green-200 bg-green-50 px-4 py-3 text-sm text-green-800">
            {MSG.Auth.MFA_ENROLLED_BANNER}
          </div>
        )}

        {/* Session expired banner */}
        {sessionExpired && (
          <div className="mb-5 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
            {MSG.Auth.SESSION_EXPIRED}
          </div>
        )}

        {/* Logo */}
        <div className="mb-8 flex flex-col items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-primary">
            <ShieldCheck className="h-5 w-5 text-white" />
          </div>
          <div className="text-center">
            <h1 className="text-xl font-semibold text-foreground">{MSG.Auth.SIGN_IN_TITLE}</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              {step === "mfa" ? MSG.Auth.MFA_SUBTITLE : MSG.Auth.SIGN_IN_SUBTITLE}
            </p>
          </div>
        </div>

        {/* Card */}
        <div className="rounded-xl border border-border bg-card p-6 shadow-sm">
          {step === "credentials" ? (
            <>
              <form onSubmit={handleEmailLogin} className="space-y-4">
                <div className="space-y-1.5">
                  <Label htmlFor="email">Email</Label>
                  <Input
                    id="email"
                    type="email"
                    placeholder="you@example.com"
                    autoComplete="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    required
                  />
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="password">Password</Label>
                  <Input
                    id="password"
                    type="password"
                    placeholder="••••••••"
                    autoComplete="current-password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    required
                  />
                </div>

                {error && <ErrorBanner message={error} />}

                <Button type="submit" className="w-full" disabled={submitting}>
                  {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
                  Sign in
                </Button>
              </form>

              <div className="mt-4 flex items-center gap-3">
                <div className="flex-1 border-t border-border" />
                <span className="text-xs text-muted-foreground">or</span>
                <div className="flex-1 border-t border-border" />
              </div>

              <Button
                type="button"
                variant="outline"
                className="mt-4 w-full"
                onClick={handleGoogleLogin}
                disabled={submitting}
              >
                <GoogleIcon />
                Continue with Google
              </Button>
            </>
          ) : (
            /* ── MFA step ── */
            <form onSubmit={handleMfaVerify} className="space-y-4">
              <div className="space-y-1.5">
                <Label htmlFor="totp-code">6-digit code</Label>
                <Input
                  id="totp-code"
                  type="text"
                  inputMode="numeric"
                  placeholder="000000"
                  maxLength={6}
                  autoComplete="one-time-code"
                  autoFocus
                  value={totpCode}
                  onChange={(e) => setTotpCode(e.target.value.replace(/\D/g, ""))}
                  required
                />
              </div>

              {error && <ErrorBanner message={error} />}

              <Button
                type="submit"
                className="w-full"
                disabled={submitting || totpCode.length !== 6}
              >
                {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
                Verify
              </Button>

              <button
                type="button"
                onClick={handleBackToCredentials}
                className="flex w-full items-center justify-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
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

function ErrorBanner({ message }: { message: string }) {
  return (
    <p className="rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
      {message}
    </p>
  );
}

function GoogleIcon() {
  return (
    <svg className="h-4 w-4" viewBox="0 0 24 24">
      <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z" fill="#4285F4" />
      <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853" />
      <path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" fill="#FBBC05" />
      <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335" />
    </svg>
  );
}

function isMfaError(err: unknown): err is MultiFactorError {
  return (
    typeof err === "object" &&
    err !== null &&
    "code" in err &&
    (err as { code: string }).code === "auth/multi-factor-auth-required"
  );
}

function friendlyFirebaseError(msg: string): string {
  if (msg.includes("invalid-totp-code") || msg.includes("INVALID_OTP"))
    return "Incorrect code. Check your authenticator app and try again.";
  if (msg.includes("user-not-found") || msg.includes("wrong-password") || msg.includes("invalid-credential"))
    return "Invalid email or password.";
  if (msg.includes("too-many-requests"))
    return "Too many attempts. Try again later.";
  if (msg.includes("popup-closed"))
    return "Sign-in popup was closed.";
  return "Authentication failed. Please try again.";
}
