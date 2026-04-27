// debounce returns a wrapped function that delays invoking `fn` until
// `waitMs` have elapsed since the last call.
//
// Semantics tailored to the workspace persistence use case:
//
//   - Trailing-only: rapid successive calls collapse to one invocation
//     whose args are the most recent.
//   - In-flight aware: while an async invocation is running, additional
//     calls schedule at most one trailing call to run after it resolves
//     (either fulfilled or rejected). This ensures the on-disk state
//     converges to the latest in-memory state without piling up saves.
//
// cancel() drops any pending invocation. The returned function does not
// pass through fn's return value: callers that need the result should
// observe side effects (e.g. error notifications inside fn).
export interface Debounced<A extends unknown[]> {
  (...args: A): void;
  cancel(): void;
}

export function debounce<A extends unknown[]>(
  fn: (...args: A) => void | Promise<void>,
  waitMs: number,
): Debounced<A> {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let inFlight = false;
  let pendingArgs: A | null = null;

  const fire = (args: A) => {
    inFlight = true;
    let result: void | Promise<void>;
    try {
      result = fn(...args);
    } catch {
      inFlight = false;
      drainPending();
      return;
    }
    if (result && typeof (result as Promise<void>).then === "function") {
      (result as Promise<void>).then(
        () => {
          inFlight = false;
          drainPending();
        },
        () => {
          inFlight = false;
          drainPending();
        },
      );
    } else {
      inFlight = false;
      drainPending();
    }
  };

  const drainPending = () => {
    if (pendingArgs === null) return;
    const args = pendingArgs;
    pendingArgs = null;
    fire(args);
  };

  const debounced = ((...args: A) => {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = null;
      if (inFlight) {
        pendingArgs = args;
        return;
      }
      fire(args);
    }, waitMs);
  }) as Debounced<A>;

  debounced.cancel = () => {
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
    pendingArgs = null;
  };

  return debounced;
}
