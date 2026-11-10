import { render, screen, act } from "@testing-library/react";
import App from "../App";

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
