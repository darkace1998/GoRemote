// Typed wrapper around the Wails-bound GetSettings / UpdateSettings
// methods. In dev / test (no Wails runtime), we transparently fall back
// to an in-memory mock so the Settings UI is fully usable.
//
// Mirror of app/settings.Settings on the Go side. Any field changes here
// must stay in sync with that struct's JSON tags.

export type Theme = "system" | "light" | "dark";

export interface Settings {
  theme: Theme;
  fontFamily: string;
  fontSizePx: number;
  confirmOnClose: boolean;
  autoReconnect: boolean;
  reconnectMaxN: number;
  reconnectDelayMs: number;
  telemetryEnabled: boolean;
  /** ISO-8601 timestamp set by the backend on each Update. */
  updatedAt: string;
}

export function defaultSettings(): Settings {
  return {
    theme: "system",
    fontFamily: "",
    fontSizePx: 13,
    confirmOnClose: true,
    autoReconnect: false,
    reconnectMaxN: 3,
    reconnectDelayMs: 2000,
    telemetryEnabled: false,
    updatedAt: "",
  };
}

export interface SettingsAPI {
  get(): Promise<Settings>;
  update(next: Settings): Promise<Settings>;
}

interface WailsBindings {
  GetSettings?: () => Promise<Settings>;
  UpdateSettings?: (s: Settings) => Promise<Settings>;
}

function bindings(): WailsBindings | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { go?: { main?: { App?: WailsBindings } } }).go
    ?.main?.App;
}

// In-memory dev/test mock. Keeps a single "saved" snapshot.
function createMockAPI(): SettingsAPI {
  let saved: Settings = defaultSettings();
  return {
    async get() {
      return { ...saved };
    },
    async update(next: Settings) {
      const errs = validate(next);
      if (errs.length > 0) {
        throw new Error(errs.join("; "));
      }
      saved = { ...next, updatedAt: new Date().toISOString() };
      return { ...saved };
    },
  };
}

/**
 * Client-side mirror of the Go Validate() rules. Used by the mock and as a
 * pre-flight check in the form so the user gets immediate feedback before
 * the backend round-trip.
 */
export function validate(s: Settings): string[] {
  const errs: string[] = [];
  if (s.theme !== "system" && s.theme !== "light" && s.theme !== "dark") {
    errs.push(`invalid theme "${s.theme}"`);
  }
  if (s.fontSizePx < 8 || s.fontSizePx > 72) {
    errs.push(`fontSizePx ${s.fontSizePx} out of range [8,72]`);
  }
  if (s.reconnectMaxN < 0 || s.reconnectMaxN > 50) {
    errs.push(`reconnectMaxN ${s.reconnectMaxN} out of range [0,50]`);
  }
  if (s.reconnectDelayMs < 0 || s.reconnectDelayMs > 60000) {
    errs.push(`reconnectDelayMs ${s.reconnectDelayMs} out of range [0,60000]`);
  }
  return errs;
}

function realAPI(b: WailsBindings): SettingsAPI {
  return {
    async get() {
      if (!b.GetSettings) throw new Error("GetSettings unavailable");
      return b.GetSettings();
    },
    async update(next: Settings) {
      if (!b.UpdateSettings) throw new Error("UpdateSettings unavailable");
      return b.UpdateSettings(next);
    },
  };
}

let cached: SettingsAPI | null = null;

export function settingsAPI(): SettingsAPI {
  if (cached) return cached;
  const b = bindings();
  cached = b?.GetSettings && b.UpdateSettings ? realAPI(b) : createMockAPI();
  return cached;
}

/**
 * Test-only escape hatch: replace the cached API instance with a stub.
 * Returns the previous instance so tests can restore it.
 */
export function __setSettingsAPIForTesting(
  next: SettingsAPI | null,
): SettingsAPI | null {
  const prev = cached;
  cached = next;
  return prev;
}
