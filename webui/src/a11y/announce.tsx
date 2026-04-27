import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";

export type AnnouncePriority = "polite" | "assertive";

interface AnnounceContextValue {
  announce: (message: string, priority?: AnnouncePriority) => void;
}

const AnnounceCtx = createContext<AnnounceContextValue | null>(null);

/**
 * Hosts two visually-hidden ARIA live regions (polite + assertive) and exposes
 * an `announce()` callback through context. Repeated identical messages are
 * cleared and re-set so screen readers re-announce them. Calls are debounced
 * by ~50ms so a burst of state updates merges into a single announcement.
 */
export function LiveAnnouncer({ children }: { children: ReactNode }) {
  const [polite, setPolite] = useState("");
  const [assertive, setAssertive] = useState("");
  const queue = useRef<{ message: string; priority: AnnouncePriority }[]>([]);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const flush = useCallback(() => {
    timer.current = null;
    const items = queue.current;
    queue.current = [];
    const lastPolite = [...items].reverse().find((i) => i.priority === "polite");
    const lastAssertive = [...items]
      .reverse()
      .find((i) => i.priority === "assertive");
    if (lastPolite) {
      setPolite("");
      // Force re-render so identical consecutive messages still announce.
      queueMicrotask(() => setPolite(lastPolite.message));
    }
    if (lastAssertive) {
      setAssertive("");
      queueMicrotask(() => setAssertive(lastAssertive.message));
    }
  }, []);

  const announce = useCallback(
    (message: string, priority: AnnouncePriority = "polite") => {
      queue.current.push({ message, priority });
      if (timer.current == null) {
        timer.current = setTimeout(flush, 50);
      }
    },
    [flush],
  );

  useEffect(() => {
    return () => {
      if (timer.current != null) clearTimeout(timer.current);
    };
  }, []);

  return (
    <AnnounceCtx.Provider value={{ announce }}>
      {children}
      <div
        role="status"
        aria-live="polite"
        aria-atomic="true"
        className="sr-only"
        data-testid="live-region-polite"
      >
        {polite}
      </div>
      <div
        role="alert"
        aria-live="assertive"
        aria-atomic="true"
        className="sr-only"
        data-testid="live-region-assertive"
      >
        {assertive}
      </div>
    </AnnounceCtx.Provider>
  );
}

/**
 * Hook for components to push messages to the global live region.
 * Falls back to a no-op outside an `<LiveAnnouncer>` so unit-tested
 * components don't crash when they don't need announcements.
 */
export function useAnnounce(): (
  message: string,
  priority?: AnnouncePriority,
) => void {
  const ctx = useContext(AnnounceCtx);
  return useCallback(
    (message: string, priority: AnnouncePriority = "polite") => {
      ctx?.announce(message, priority);
    },
    [ctx],
  );
}
