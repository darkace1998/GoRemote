import { afterEach, describe, expect, it } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { Modal } from "./Modal";

afterEach(() => {
  document.body.innerHTML = "";
});

describe("Modal focus management", () => {
  it("traps Tab within the dialog and restores focus on close", async () => {
    // Pre-existing trigger button outside the modal.
    const trigger = document.createElement("button");
    trigger.textContent = "Open";
    document.body.appendChild(trigger);
    trigger.focus();
    expect(document.activeElement).toBe(trigger);

    const { unmount } = render(
      <Modal onClose={() => {}} label="Test dialog">
        <button>First</button>
        <button>Second</button>
        <button>Last</button>
      </Modal>,
    );

    // Wait for the queueMicrotask focus.
    await act(async () => {
      await Promise.resolve();
    });

    const first = screen.getByRole("button", { name: "First" });
    const last = screen.getByRole("button", { name: "Last" });
    expect(document.activeElement).toBe(first);

    // Tab from the last element wraps to the first.
    last.focus();
    fireEvent.keyDown(last, { key: "Tab" });
    expect(document.activeElement).toBe(first);

    // Shift+Tab from the first element wraps to the last.
    first.focus();
    fireEvent.keyDown(first, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(last);

    unmount();
    // Focus should be restored to the trigger.
    expect(document.activeElement).toBe(trigger);
  });

  it("calls onClose on Escape", () => {
    let closed = false;
    render(
      <Modal onClose={() => (closed = true)} label="Test">
        <button>Only</button>
      </Modal>,
    );
    const dialog = screen.getByRole("dialog");
    fireEvent.keyDown(dialog, { key: "Escape" });
    expect(closed).toBe(true);
  });

  it("exposes role=dialog with aria-modal", () => {
    render(
      <Modal onClose={() => {}} label="Hi">
        <p>body</p>
      </Modal>,
    );
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(dialog).toHaveAttribute("aria-label", "Hi");
  });
});
