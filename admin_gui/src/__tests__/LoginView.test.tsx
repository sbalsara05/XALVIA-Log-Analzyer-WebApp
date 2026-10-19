import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";

// ── Module mocks ──────────────────────────────────────────────────────────────

const mockSignInWithPopup = vi.fn();
const mockSignInWithEmailAndPassword = vi.fn();
const mockGetMultiFactorResolver = vi.fn();
const mockResolveSignIn = vi.fn();

vi.mock("firebase/auth", () => ({
  signInWithEmailAndPassword: (...args: unknown[]) => mockSignInWithEmailAndPassword(...args),
  signInWithPopup: (...args: unknown[]) => mockSignInWithPopup(...args),
  getMultiFactorResolver: (...args: unknown[]) => mockGetMultiFactorResolver(...args),
  TotpMultiFactorGenerator: {
    FACTOR_ID: "totp",
    assertionForSignIn: vi.fn().mockReturnValue("mock-assertion"),
  },
}));

vi.mock("@/lib/firebase", () => ({
  auth: {},
  googleProvider: {},
}));

const mockUseAuth = vi.fn();
vi.mock("@/contexts/AuthContext", () => ({
  useAuth: () => mockUseAuth(),
}));

// ── Helpers ───────────────────────────────────────────────────────────────────

import { LoginView } from "@/views/LoginView";

function makeMfaError() {
  return Object.assign(new Error("MFA required"), {
    code: "auth/multi-factor-auth-required",
  });
}

function renderLogin() {
  return render(
    <MemoryRouter initialEntries={["/login"]}>
      <Routes>
        <Route path="/login" element={<LoginView />} />
        <Route path="/orgs" element={<div>Dashboard</div>} />
      </Routes>
    </MemoryRouter>
  );
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("LoginView — Google Sign-In MFA (B-5)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUseAuth.mockReturnValue({ user: null, loading: false });
    mockGetMultiFactorResolver.mockReturnValue({
      hints: [{ factorId: "totp", uid: "factor-uid" }],
      resolveSignIn: mockResolveSignIn,
    });
  });

  it("switches to MFA step when Google login raises auth/multi-factor-auth-required", async () => {
    mockSignInWithPopup.mockRejectedValue(makeMfaError());
    renderLogin();

    await userEvent.click(screen.getByRole("button", { name: /continue with google/i }));

    await waitFor(() =>
      expect(screen.getByLabelText(/6-digit code/i)).toBeInTheDocument()
    );
    expect(screen.queryByRole("button", { name: /continue with google/i })).toBeNull();
  });

  it("stores the resolver from getMultiFactorResolver when switching to MFA step", async () => {
    mockSignInWithPopup.mockRejectedValue(makeMfaError());
    renderLogin();

    await userEvent.click(screen.getByRole("button", { name: /continue with google/i }));
    await waitFor(() => screen.getByLabelText(/6-digit code/i));

    expect(mockGetMultiFactorResolver).toHaveBeenCalledTimes(1);
  });

  it("shows an error message (not MFA step) on a non-MFA Google error", async () => {
    mockSignInWithPopup.mockRejectedValue(
      Object.assign(new Error("popup-closed"), { code: "auth/popup-closed-by-user" })
    );
    renderLogin();

    await userEvent.click(screen.getByRole("button", { name: /continue with google/i }));

    await waitFor(() =>
      expect(screen.getByText(/Sign-in popup was closed/i)).toBeInTheDocument()
    );
    expect(screen.queryByLabelText(/6-digit code/i)).toBeNull();
  });

  it("completing MFA verify after Google login calls resolveSignIn", async () => {
    mockSignInWithPopup.mockRejectedValue(makeMfaError());
    mockResolveSignIn.mockResolvedValue(undefined);
    renderLogin();

    await userEvent.click(screen.getByRole("button", { name: /continue with google/i }));
    await waitFor(() => screen.getByLabelText(/6-digit code/i));

    await userEvent.type(screen.getByLabelText(/6-digit code/i), "123456");
    await userEvent.click(screen.getByRole("button", { name: /verify/i }));

    await waitFor(() => expect(mockResolveSignIn).toHaveBeenCalledTimes(1));
  });

  it("shows error and keeps MFA step when resolveSignIn fails after Google login", async () => {
    mockSignInWithPopup.mockRejectedValue(makeMfaError());
    mockResolveSignIn.mockRejectedValue(new Error("invalid-totp-code"));
    renderLogin();

    await userEvent.click(screen.getByRole("button", { name: /continue with google/i }));
    await waitFor(() => screen.getByLabelText(/6-digit code/i));

    await userEvent.type(screen.getByLabelText(/6-digit code/i), "000000");
    await userEvent.click(screen.getByRole("button", { name: /verify/i }));

    await waitFor(() =>
      expect(screen.getByText(/Incorrect code/i)).toBeInTheDocument()
    );
    // Still on MFA step
    expect(screen.getByLabelText(/6-digit code/i)).toBeInTheDocument();
  });
});

describe("LoginView — email/password MFA (existing behaviour, regression)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUseAuth.mockReturnValue({ user: null, loading: false });
    mockGetMultiFactorResolver.mockReturnValue({
      hints: [{ factorId: "totp", uid: "factor-uid" }],
      resolveSignIn: mockResolveSignIn,
    });
  });

  it("switches to MFA step when email login raises auth/multi-factor-auth-required", async () => {
    mockSignInWithEmailAndPassword.mockRejectedValue(makeMfaError());
    renderLogin();

    await userEvent.type(screen.getByLabelText(/email/i), "user@example.com");
    await userEvent.type(screen.getByLabelText(/password/i), "password");
    await userEvent.click(screen.getByRole("button", { name: /^sign in$/i }));

    await waitFor(() =>
      expect(screen.getByLabelText(/6-digit code/i)).toBeInTheDocument()
    );
  });

  it("shows friendly error on wrong password", async () => {
    mockSignInWithEmailAndPassword.mockRejectedValue(
      Object.assign(new Error("invalid-credential"), { code: "auth/invalid-credential" })
    );
    renderLogin();

    await userEvent.type(screen.getByLabelText(/email/i), "user@example.com");
    await userEvent.type(screen.getByLabelText(/password/i), "wrongpassword");
    await userEvent.click(screen.getByRole("button", { name: /^sign in$/i }));

    await waitFor(() =>
      expect(screen.getByText(/Invalid email or password/i)).toBeInTheDocument()
    );
  });
});
