import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { LiveAnnouncer, useAnnounce } from "./announce";

afterEach(() => {
  vi.useRealTimers();
  document.body.innerHTML = "";
});

function Harness({
  message,
  priority,
}: {
  message: string;
  priority?: "polite" | "assertive";
}) {
  const announce = useAnnounce();
  return (
    <button
      onClick={() => announce(message, priority)}
      data-testid="emit"
    >
      go
    </button>
  );
}

describe("useAnnounce", () => {
  it("debounces and writes the latest polite message into the live region", async () => {
    vi.useFakeTimers();
    render(
      <LiveAnnouncer>
        <Harness message="first" />
      </LiveAnnouncer>,
    );

    const button = screen.getByTestId("emit");
    act(() => {
      button.click();
      button.click();
      button.click();
    });

    // Before debounce flush, the live region is still empty.
    expect(screen.getByTestId("live-region-polite").textContent).toBe("");

    // Advance the debounce timer.
    await act(async () => {
      vi.advanceTimersByTime(100);
    });
    // Allow queued microtasks (which set the actual text) to run.
    await act(async () => {
      await Promise.resolve();
    });

    expect(screen.getByTestId("live-region-polite").textContent).toBe("first");
  });

  it("routes assertive messages to the alert region", async () => {
    vi.useFakeTimers();
    render(
      <LiveAnnouncer>
        <Harness message="boom" priority="assertive" />
      </LiveAnnouncer>,
    );
    act(() => {
      screen.getByTestId("emit").click();
    });
    await act(async () => {
      vi.advanceTimersByTime(100);
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByTestId("live-region-assertive").textContent).toBe(
      "boom",
    );
    expect(screen.getByTestId("live-region-polite").textContent).toBe("");
  });

  it("renders both regions with correct ARIA attributes", () => {
    render(
      <LiveAnnouncer>
        <span />
      </LiveAnnouncer>,
    );
    const polite = screen.getByTestId("live-region-polite");
    const assertive = screen.getByTestId("live-region-assertive");
    expect(polite).toHaveAttribute("role", "status");
    expect(polite).toHaveAttribute("aria-live", "polite");
    expect(assertive).toHaveAttribute("role", "alert");
    expect(assertive).toHaveAttribute("aria-live", "assertive");
  });
});
