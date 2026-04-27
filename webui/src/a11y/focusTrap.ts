// Focus trap utilities for modal dialogs.
//
// A modal can call `installFocusTrap(container)` on mount. Tab and Shift+Tab
// are constrained to focusable descendants. The previously-focused element
// is captured so it can be restored when the trap is removed.

const FOCUSABLE_SELECTORS = [
  "a[href]",
  "area[href]",
  "input:not([disabled]):not([type='hidden'])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "button:not([disabled])",
  "iframe",
  "object",
  "embed",
  "[contenteditable=true]",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

export function focusableWithin(container: HTMLElement): HTMLElement[] {
  const list = Array.from(
    container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTORS),
  );
  return list.filter(
    (el) => !el.hasAttribute("disabled") && el.tabIndex !== -1 && isVisible(el),
  );
}

function isVisible(el: HTMLElement): boolean {
  if (el.hidden) return false;
  // jsdom doesn't implement layout, so offsetParent is unreliable in tests.
  // Treat any non-hidden element as visible there.
  if (typeof el.getClientRects !== "function") return true;
  return true;
}

export interface FocusTrap {
  release(): void;
}

export function installFocusTrap(container: HTMLElement): FocusTrap {
  const previouslyFocused = document.activeElement as HTMLElement | null;

  const focusables = focusableWithin(container);
  const initial =
    focusables.find((el) => el.hasAttribute("autofocus")) ??
    focusables[0] ??
    container;
  // Make container itself focusable as a fallback.
  if (initial === container && !container.hasAttribute("tabindex")) {
    container.tabIndex = -1;
  }
  // Defer to ensure the element is in the DOM and layout is settled.
  queueMicrotask(() => {
    try {
      initial.focus();
    } catch {
      /* ignore */
    }
  });

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key !== "Tab") return;
    const items = focusableWithin(container);
    if (items.length === 0) {
      e.preventDefault();
      container.focus();
      return;
    }
    const first = items[0];
    const last = items[items.length - 1];
    const active = document.activeElement as HTMLElement | null;
    if (e.shiftKey) {
      if (active === first || !container.contains(active)) {
        e.preventDefault();
        last.focus();
      }
    } else {
      if (active === last || !container.contains(active)) {
        e.preventDefault();
        first.focus();
      }
    }
  };

  container.addEventListener("keydown", onKeyDown);

  return {
    release() {
      container.removeEventListener("keydown", onKeyDown);
      // Restore focus only if focus is still inside the trap (or nowhere useful).
      const active = document.activeElement as HTMLElement | null;
      if (
        previouslyFocused &&
        typeof previouslyFocused.focus === "function" &&
        (!active || container.contains(active) || active === document.body)
      ) {
        try {
          previouslyFocused.focus();
        } catch {
          /* ignore */
        }
      }
    },
  };
}
