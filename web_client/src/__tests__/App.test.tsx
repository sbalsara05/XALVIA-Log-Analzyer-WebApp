import { render, screen, act } from "@testing-library/react";
import App from "../App";

vi.mock('../firebase', () => ({
  auth: {},
  loginWithEmail: vi.fn(),
  logout: vi.fn(),
}));

vi.mock('firebase/auth', () => ({
  onAuthStateChanged: (_auth: unknown, callback: (user: null) => void) => {
    callback(null); // pretend nobody is logged in
    return () => {};
  },
}));

describe("App", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders without crashing", () => {
    const { container } = render(<App />);
    expect(container).toBeInTheDocument();
  });

  it("shows the login screen after loading", () => {
    vi.useFakeTimers();
    render(<App />);
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(screen.getByText(/authorized personnel only/i)).toBeInTheDocument();
  });
});
