import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { debounce } from "./debounce";

describe("debounce", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("collapses rapid calls into a single trailing invocation", () => {
    const fn = vi.fn();
    const d = debounce(fn, 500);
    d(1);
    d(2);
    d(3);
    expect(fn).not.toHaveBeenCalled();
    vi.advanceTimersByTime(499);
    expect(fn).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(fn).toHaveBeenCalledOnce();
    expect(fn).toHaveBeenLastCalledWith(3);
  });

  it("queues at most one trailing call while async fn is in flight", async () => {
    let resolve: () => void = () => {};
    const inFlight = new Promise<void>((r) => {
      resolve = r;
    });
    const fn = vi.fn((_v: string) => inFlight);
    const d = debounce(fn, 100);

    d("a");
    vi.advanceTimersByTime(100);
    expect(fn).toHaveBeenCalledTimes(1);
    expect(fn).toHaveBeenLastCalledWith("a");

    // While first invocation is in flight, schedule several more.
    d("b");
    d("c");
    d("d");
    vi.advanceTimersByTime(100);
    // Still only one call: the trailing call is queued.
    expect(fn).toHaveBeenCalledTimes(1);

    // Resolve the in-flight promise; trailing call fires with last args.
    resolve();
    await inFlight;
    // microtask flush
    await Promise.resolve();
    expect(fn).toHaveBeenCalledTimes(2);
    expect(fn).toHaveBeenLastCalledWith("d");
  });

  it("cancel() drops a pending call", () => {
    const fn = vi.fn();
    const d = debounce(fn, 200);
    d(1);
    d.cancel();
    vi.advanceTimersByTime(500);
    expect(fn).not.toHaveBeenCalled();
  });
});
