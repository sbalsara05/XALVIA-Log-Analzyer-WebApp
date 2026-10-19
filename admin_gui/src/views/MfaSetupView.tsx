import { useEffect, useRef, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import {
  TotpMultiFactorGenerator,
  type TotpSecret,
  multiFactor,
  signOut,
} from "firebase/auth";
import { Loader2, ShieldCheck } from "lucide-react";

import { MSG } from "@/lib/constants";
import { useAuth } from "@/contexts/AuthContext";
import { auth } from "@/lib/firebase";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function MfaSetupView() {
  const { user } = useAuth();

  // Keep the TotpSecret object in a ref because it is needed
  // during enrollment but does not need to trigger UI updates.
  const secretRef = useRef<TotpSecret | null>(null);

  const [qrUri, setQrUri] = useState<string | null>(null);
  const [secretKey, setSecretKey] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [initError, setInitError] = useState("");

  useEffect(() => {
    if (!user) {
      return;
    }

    (async () => {
      try {
        const session = await multiFactor(user).getSession();

        const totpSecret =
          await TotpMultiFactorGenerator.generateSecret(session);

        secretRef.current = totpSecret;

        setSecretKey(totpSecret.secretKey);

        setQrUri(
          totpSecret.generateQrCodeUrl(
            user.email!,
            "XALIVA Log Analyzer"
          )
        );
      } catch {
        setInitError(
          "Failed to start setup. Please refresh and try again."
        );
      }
    })();
  }, [user]);

  async function handleConfirm(
    e: React.FormEvent
  ) {
    e.preventDefault();

    if (!secretRef.current || !user) {
      return;
    }

    setError("");
    setSubmitting(true);

    try {
      const assertion =
        TotpMultiFactorGenerator.assertionForEnrollment(
          secretRef.current,
          code
        );

      await multiFactor(user).enroll(
        assertion,
        "XALIVA TOTP"
      );

      // Enrollment succeeds for the account, but the current Firebase
      // ID token was issued before this sign-in satisfied MFA.
      // Sign out so the user completes a fresh MFA login and receives
      // a token that the backend can verify as MFA-authenticated.
      await signOut(auth);

      window.location.replace(
        "/login?reason=mfa_enrolled"
      );
    } catch {
      setError(MSG.Auth.MFA_ENROLL_FAILED);
      setCode("");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-primary">
            <ShieldCheck className="h-5 w-5 text-white" />
          </div>

          <div className="text-center">
            <h1 className="text-xl font-semibold text-foreground">
              {MSG.Auth.MFA_SETUP_TITLE}
            </h1>

            <p className="mt-1 text-sm text-muted-foreground">
              {MSG.Auth.MFA_SETUP_BODY}
            </p>
          </div>
        </div>

        <div className="rounded-xl border border-border bg-card p-6 shadow-sm">
          {initError ? (
            <p className="text-center text-sm text-destructive">
              {initError}
            </p>
          ) : !qrUri ? (
            <div className="flex justify-center py-8">
              <Loader2 className="h-6 w-6 animate-spin text-primary" />
            </div>
          ) : (
            <form
              onSubmit={handleConfirm}
              className="space-y-5"
            >
              <div className="flex justify-center">
                <QRCodeSVG
                  value={qrUri}
                  size={200}
                />
              </div>

              <div className="rounded-md bg-muted px-3 py-2 text-center">
                <p className="mb-1 text-xs text-muted-foreground">
                  Manual entry key
                </p>

                <p className="select-all break-all font-mono text-sm">
                  {secretKey}
                </p>
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="totp-setup-code">
                  6-digit code from your app
                </Label>

                <Input
                  id="totp-setup-code"
                  type="text"
                  inputMode="numeric"
                  placeholder="000000"
                  maxLength={6}
                  autoComplete="one-time-code"
                  autoFocus
                  value={code}
                  onChange={(e) =>
                    setCode(
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
                <p className="rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
                  {error}
                </p>
              )}

              <Button
                type="submit"
                className="w-full"
                disabled={
                  submitting ||
                  code.length !== 6
                }
              >
                {submitting && (
                  <Loader2 className="h-4 w-4 animate-spin" />
                )}

                Confirm
              </Button>
            </form>
          )}
        </div>
      </div>
    </div>
  );
}