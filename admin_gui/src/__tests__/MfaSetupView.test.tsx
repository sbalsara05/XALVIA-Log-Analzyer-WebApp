import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// ── Module mocks ──────────────────────────────────────────────────────────────

const mockGetSession = vi.fn();
const mockGenerateSecret = vi.fn();
const mockAssertionForEnrollment = vi.fn();
const mockEnroll = vi.fn();
const mockMultiFactor = vi.fn();

vi.mock("firebase/auth", () => ({
  TotpMultiFactorGenerator: {
    generateSecret: (...args: unknown[]) => mockGenerateSecret(...args),
    assertionForEnrollment: (...args: unknown[]) => mockAssertionForEnrollment(...args),
  },
  multiFactor: (...args: unknown[]) => mockMultiFactor(...args),
}));

vi.mock("qrcode.react", () => ({
  QRCodeSVG: ({ value }: { value: string }) => (
    <img alt="qr-code" data-testid="qr-code" data-value={value} />
  ),
}));

const mockUseAuth = vi.fn();
vi.mock("@/contexts/AuthContext", () => ({
  useAuth: () => mockUseAuth(),
}));

// ── Fixtures ──────────────────────────────────────────────────────────────────

import { MfaSetupView } from "@/views/MfaSetupView";

const MOCK_USER = { uid: "u1", email: "test@example.com" };
const MOCK_SECRET_KEY = "ABCDEFGHIJKLMNOP";
const MOCK_QR_URI = "otpauth://totp/XALIVA%20Admin:test%40example.com?secret=ABCDEFGHIJKLMNOP";

const mockTotpSecret = {
  secretKey: MOCK_SECRET_KEY,
  generateQrCodeUrl: vi.fn().mockReturnValue(MOCK_QR_URI),
};

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("MfaSetupView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUseAuth.mockReturnValue({ user: MOCK_USER });
    mockMultiFactor.mockReturnValue({ getSession: mockGetSession, enroll: mockEnroll });
    mockGetSession.mockResolvedValue("mock-session");
    mockGenerateSecret.mockResolvedValue(mockTotpSecret);
    mockAssertionForEnrollment.mockReturnValue("mock-assertion");
    mockEnroll.mockResolvedValue(undefined);
  });

  // --- loading state ---

  it("shows spinner while generating TOTP secret", () => {
    mockGetSession.mockReturnValue(new Promise(() => {})); // never resolves
    render(<MfaSetupView />);
    expect(document.querySelector(".animate-spin")).toBeInTheDocument();
    expect(screen.queryByTestId("qr-code")).toBeNull();
  });

  // --- happy path render ---

  it("renders QR code and secret key after setup completes", async () => {
    render(<MfaSetupView />);
    await waitFor(() => expect(screen.getByTestId("qr-code")).toBeInTheDocument());
    expect(screen.getByText(MOCK_SECRET_KEY)).toBeInTheDocument();
  });

  it("passes the generated URI to QRCodeSVG", async () => {
    render(<MfaSetupView />);
    await waitFor(() => screen.getByTestId("qr-code"));
    expect(screen.getByTestId("qr-code")).toHaveAttribute("data-value", MOCK_QR_URI);
  });

  it("generates secret using the session returned from getSession", async () => {
    render(<MfaSetupView />);
    await waitFor(() => screen.getByTestId("qr-code"));
    expect(mockGenerateSecret).toHaveBeenCalledWith("mock-session");
  });

  // --- input validation ---

  it("Confirm button is disabled until exactly 6 digits are entered", async () => {
    render(<MfaSetupView />);
    await waitFor(() => screen.getByRole("button", { name: /confirm/i }));
    const button = screen.getByRole("button", { name: /confirm/i });
    expect(button).toBeDisabled();

    await userEvent.type(screen.getByLabelText(/6-digit code/i), "12345");
    expect(button).toBeDisabled();

    await userEvent.type(screen.getByLabelText(/6-digit code/i), "6");
    expect(button).not.toBeDisabled();
  });

  it("strips non-numeric characters from the code input", async () => {
    render(<MfaSetupView />);
    await waitFor(() => screen.getByLabelText(/6-digit code/i));
    await userEvent.type(screen.getByLabelText(/6-digit code/i), "1a2b3c");
    expect(screen.getByLabelText(/6-digit code/i)).toHaveValue("123");
  });

  // --- submission ---

  it("calls assertionForEnrollment and enroll with correct arguments on submit", async () => {
    render(<MfaSetupView />);
    await waitFor(() => screen.getByRole("button", { name: /confirm/i }));
    await userEvent.type(screen.getByLabelText(/6-digit code/i), "123456");
    await userEvent.click(screen.getByRole("button", { name: /confirm/i }));
    await waitFor(() => expect(mockEnroll).toHaveBeenCalledTimes(1));
    expect(mockAssertionForEnrollment).toHaveBeenCalledWith(mockTotpSecret, "123456");
    expect(mockEnroll).toHaveBeenCalledWith("mock-assertion", "XALIVA TOTP");
  });

  it("disables Confirm button while submission is in-flight", async () => {
    mockEnroll.mockReturnValue(new Promise(() => {})); // never resolves
    render(<MfaSetupView />);
    await waitFor(() => screen.getByRole("button", { name: /confirm/i }));
    await userEvent.type(screen.getByLabelText(/6-digit code/i), "123456");
    await userEvent.click(screen.getByRole("button", { name: /confirm/i }));
    expect(screen.getByRole("button", { name: /confirm/i })).toBeDisabled();
  });

  // --- error states ---

  it("shows MFA_ENROLL_FAILED banner and clears code input on enrollment failure", async () => {
    mockEnroll.mockRejectedValue(new Error("invalid-totp-code"));
    render(<MfaSetupView />);
    await waitFor(() => screen.getByRole("button", { name: /confirm/i }));
    await userEvent.type(screen.getByLabelText(/6-digit code/i), "000000");
    await userEvent.click(screen.getByRole("button", { name: /confirm/i }));
    await waitFor(() =>
      expect(screen.getByText(/Code verification failed/i)).toBeInTheDocument()
    );
    expect(screen.getByLabelText(/6-digit code/i)).toHaveValue("");
  });

  it("shows init error and hides QR when secret generation fails", async () => {
    mockGetSession.mockRejectedValue(new Error("network error"));
    render(<MfaSetupView />);
    await waitFor(() =>
      expect(screen.getByText(/Failed to start setup/i)).toBeInTheDocument()
    );
    expect(screen.queryByTestId("qr-code")).toBeNull();
    expect(screen.queryByRole("button", { name: /confirm/i })).toBeNull();
  });
});
