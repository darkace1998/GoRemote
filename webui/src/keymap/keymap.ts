// Global keyboard shortcut registry with chord and modifier support.
//
// Bindings are keyed by a normalized "combo" string and dispatched by a single
// window-level keydown listener installed on first registration. Typing focus
// is detected so shortcuts don't intercept normal text input.

export type Mods = {
  ctrl?: boolean;
  meta?: boolean;
  alt?: boolean;
  shift?: boolean;
};

export interface Binding {
  /** Stable id, used to unregister. */
  id: string;
  /** One or more combos, e.g. "Mod+T", "Ctrl+Shift+Tab", "?", "F2". */
  keys: string | string[];
  /** Human-readable description shown in the cheat-sheet. */
  description: string;
  /** Group label for the cheat-sheet. */
  group?: string;
  /** Whether to fire while typing in an input/textarea/contenteditable. Default false. */
  allowInInput?: boolean;
  /** Whether to call preventDefault when matched. Default true. */
  preventDefault?: boolean;
  handler: (e: KeyboardEvent) => void;
}

export interface ParsedCombo {
  mods: Mods;
  /** "Mod" is the platform shortcut key (Cmd on macOS, Ctrl elsewhere). */
  modIsAny?: boolean;
  key: string;
}

const isMac =
  typeof navigator !== "undefined" &&
  /Mac|iPhone|iPad|iPod/.test(navigator.platform || navigator.userAgent || "");

export function parseCombo(combo: string): ParsedCombo {
  const parts = combo.split("+").map((p) => p.trim());
  const mods: Mods = {};
  let modIsAny = false;
  let key = "";
  for (const p of parts) {
    const lower = p.toLowerCase();
    switch (lower) {
      case "ctrl":
      case "control":
        mods.ctrl = true;
        break;
      case "cmd":
      case "meta":
      case "super":
      case "win":
        mods.meta = true;
        break;
      case "alt":
      case "option":
        mods.alt = true;
        break;
      case "shift":
        mods.shift = true;
        break;
      case "mod":
        modIsAny = true;
        break;
      default:
        key = lower;
    }
  }
  return { mods, modIsAny, key };
}

function eventKey(e: KeyboardEvent): string {
  const k = e.key;
  if (!k) return "";
  if (k === " ") return "space";
  return k.toLowerCase();
}

export function matches(combo: ParsedCombo, e: KeyboardEvent): boolean {
  if (combo.modIsAny) {
    const modPressed = isMac ? e.metaKey : e.ctrlKey;
    if (!modPressed) return false;
    const otherPressed = isMac ? e.ctrlKey : e.metaKey;
    if (otherPressed) return false;
  } else {
    if (!!combo.mods.ctrl !== e.ctrlKey) return false;
    if (!!combo.mods.meta !== e.metaKey) return false;
  }
  if (!!combo.mods.alt !== e.altKey) return false;
  if (!!combo.mods.shift !== e.shiftKey) return false;
  return eventKey(e) === combo.key;
}

export function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  const tag = target.tagName;
  if (tag === "INPUT") {
    const type = (target as HTMLInputElement).type;
    const nonTyping = new Set([
      "button",
      "submit",
      "checkbox",
      "radio",
      "file",
      "range",
      "color",
      "image",
      "reset",
    ]);
    return !nonTyping.has(type);
  }
  if (tag === "TEXTAREA" || tag === "SELECT") return true;
  return false;
}

class Registry {
  private bindings = new Map<string, Binding>();
  private listenerInstalled = false;
  private listener = (e: KeyboardEvent) => this.dispatch(e);

  register(b: Binding): () => void {
    this.bindings.set(b.id, b);
    if (!this.listenerInstalled && typeof window !== "undefined") {
      window.addEventListener("keydown", this.listener);
      this.listenerInstalled = true;
    }
    return () => this.unregister(b.id);
  }

  unregister(id: string): void {
    this.bindings.delete(id);
    if (
      this.bindings.size === 0 &&
      this.listenerInstalled &&
      typeof window !== "undefined"
    ) {
      window.removeEventListener("keydown", this.listener);
      this.listenerInstalled = false;
    }
  }

  list(): Binding[] {
    return [...this.bindings.values()];
  }

  dispatch(e: KeyboardEvent): void {
    const typing = isTypingTarget(e.target);
    for (const b of [...this.bindings.values()]) {
      if (typing && !b.allowInInput) continue;
      const combos = Array.isArray(b.keys) ? b.keys : [b.keys];
      for (const c of combos) {
        if (matches(parseCombo(c), e)) {
          if (b.preventDefault !== false) e.preventDefault();
          b.handler(e);
          return;
        }
      }
    }
  }

  __resetForTesting(): void {
    this.bindings.clear();
    if (this.listenerInstalled && typeof window !== "undefined") {
      window.removeEventListener("keydown", this.listener);
      this.listenerInstalled = false;
    }
  }
}

export const keymap = new Registry();

export function formatCombo(combo: string): string {
  const p = parseCombo(combo);
  const parts: string[] = [];
  if (p.modIsAny) parts.push(isMac ? "⌘" : "Ctrl");
  if (p.mods.ctrl) parts.push("Ctrl");
  if (p.mods.meta) parts.push(isMac ? "⌘" : "Meta");
  if (p.mods.alt) parts.push(isMac ? "⌥" : "Alt");
  if (p.mods.shift) parts.push("Shift");
  let k = p.key;
  if (k === "tab") k = "Tab";
  else if (k === "escape") k = "Esc";
  else if (k === "space") k = "Space";
  else if (k.length === 1) k = k.toUpperCase();
  else k = k.charAt(0).toUpperCase() + k.slice(1);
  if (parts.length === 0) return k;
  return isMac ? `${parts.join("")}${k}` : `${parts.join("+")}+${k}`;
}
