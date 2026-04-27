import { mockBridge } from "./mockBridge";
import { wailsBridge } from "./wailsBridge";
import type { Bridge } from "./bridge";

const hasWails =
  typeof window !== "undefined" &&
  Boolean(
    (window as unknown as { go?: { main?: { App?: unknown } } }).go?.main?.App,
  );

export const bridge: Bridge = hasWails ? wailsBridge : mockBridge;
export type { Bridge } from "./bridge";
