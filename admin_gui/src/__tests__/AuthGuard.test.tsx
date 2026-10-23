import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Suspense } from "react";
import { MemoryRouter, Routes, Route } from "react-router-dom";

// ── Module mocks ────────────────────────────────────────────────────────────

vi.mock("firebase/auth", () => ({
  signOut: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("@/lib/firebase", () => ({
  auth: {},
}));

const mockUseAuth = vi.fn();
vi.mock("@/contexts/AuthContext", () => ({
  useAuth: () => mockUseAuth(),
}));

// ── Helpers ──────────────────────────────────────────────────────────────────

import { AuthGuard } from "@/components/AuthGuard";
import { signOut } from "firebase/auth";

function renderGuard() {
  return render(
    <Suspense fallback={<div>Loading</div>}>
      <MemoryRouter initialEntries={["/dashboard"]}>
        <Routes>
          <Route element={<AuthGuard />}>
            <Route path="/dashboard" element={<div>Dashboard</div>} />
          </Route>
          <Route path="/login" element={<div>Login</div>} />
        </Routes>
      </MemoryRouter>
    </Suspense>
  );
}

// ── Tests ────────────────────────────────────────────────────────────────────

describe("AuthGuard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // --- loading states ---

  it("shows spinner while auth is loading", () => {
    mockUseAuth.mockReturnValue({
      user: null, loading: true, hasAdminAccess: null, mfaEnrolled: null,
    });
    renderGuard();
    expect(screen.queryByText("Dashboard")).toBeNull();
    expect(screen.queryByText("Login")).toBeNull();
  });

  it("shows spinner while hasAdminAccess check is in-flight", () => {
    mockUseAuth.mockReturnValue({
      user: { uid: "u1" }, loading: false, hasAdminAccess: null, mfaEnrolled: true,
    });
    renderGuard();
    expect(screen.queryByText("Dashboard")).toBeNull();
  });

  it("shows spinner while mfaEnrolled check is in-flight", () => {
    mockUseAuth.mockReturnValue({
      user: { uid: "u1" }, loading: false, hasAdminAccess: true, mfaEnrolled: null,
    });
    renderGuard();
    expect(screen.queryByText("Dashboard")).toBeNull();
  });

  // --- unauthenticated ---

  it("redirects to /login when user is null", () => {
    mockUseAuth.mockReturnValue({
      user: null, loading: false, hasAdminAccess: null, mfaEnrolled: null,
    });
    renderGuard();
    expect(screen.getByText("Login")).toBeInTheDocument();
    expect(screen.queryByText("Dashboard")).toBeNull();
  });

  // --- access denied ---

  it("renders inline Access Denied view when hasAdminAccess is false", () => {
    mockUseAuth.mockReturnValue({
      user: { uid: "u1" }, loading: false, hasAdminAccess: false, mfaEnrolled: true,
    });
    renderGuard();
    expect(screen.getByText("Access Denied")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /sign out/i })).toBeInTheDocument();
    expect(screen.queryByText("Dashboard")).toBeNull();
  });

  it("does NOT redirect to /login immediately on access denial — user stays on the denied page", () => {
    mockUseAuth.mockReturnValue({
      user: { uid: "u1" }, loading: false, hasAdminAccess: false, mfaEnrolled: true,
    });
    renderGuard();
    // Login page must not have rendered automatically
    expect(screen.queryByText("Login")).toBeNull();
  });

  it("calls signOut then navigates to /login when Sign out button is clicked", async () => {
    mockUseAuth.mockReturnValue({
      user: { uid: "u1" }, loading: false, hasAdminAccess: false, mfaEnrolled: true,
    });
    renderGuard();
    await userEvent.click(screen.getByRole("button", { name: /sign out/i }));
    expect(vi.mocked(signOut)).toHaveBeenCalledTimes(1);
  });

  // --- MFA enrollment intercept ---

  it("renders MfaSetupView when user has admin access but mfaEnrolled is false", async () => {
    mockUseAuth.mockReturnValue({
      user: { uid: "u1" }, loading: false, hasAdminAccess: true, mfaEnrolled: false,
    });
    renderGuard();
    // findBy* retries until the lazy chunk resolves
    expect(await screen.findByText(/Setting up authenticator/i)).toBeInTheDocument();
    expect(screen.queryByText("Dashboard")).toBeNull();
  });

  it("does NOT render MfaSetupView when mfaEnrolled is true", () => {
    mockUseAuth.mockReturnValue({
      user: { uid: "u1" }, loading: false, hasAdminAccess: true, mfaEnrolled: true,
    });
    renderGuard();
    expect(screen.getByText("Dashboard")).toBeInTheDocument();
    expect(screen.queryByText(/Setting up authenticator/i)).toBeNull();
  });

  // --- happy path ---

  it("renders <Outlet /> when user is fully authenticated and enrolled", () => {
    mockUseAuth.mockReturnValue({
      user: { uid: "u1" }, loading: false, hasAdminAccess: true, mfaEnrolled: true,
    });
    renderGuard();
    expect(screen.getByText("Dashboard")).toBeInTheDocument();
  });
});
