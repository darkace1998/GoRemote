import { useEffect } from "react";
import { keymap, type Binding } from "./keymap";

/**
 * Registers a keyboard binding on mount and unregisters on unmount.
 * The binding object is captured fresh each render via deps so that handlers
 * always see the latest closure.
 */
export function useKeymap(binding: Binding, deps: unknown[] = []): void {
  useEffect(() => {
    return keymap.register(binding);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
}

/**
 * Convenience: register multiple bindings at once.
 */
export function useKeymapBindings(
  bindings: Binding[],
  deps: unknown[] = [],
): void {
  useEffect(() => {
    const offs = bindings.map((b) => keymap.register(b));
    return () => offs.forEach((off) => off());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
}
