// Typed wrapper around the Wails-bound GetWorkspace / SaveWorkspace
// methods. In dev / test (no Wails runtime) the wrapper transparently
// falls back to a localStorage-backed mock so the UI is fully usable
// without the bridge.
//
// Mirror of app/workspace.Workspace on the Go side. Field names and JSON
// tags must stay in sync with that struct.

export interface TabState {
  id: string;
  connectionId: string;
  title: string;
  paneGroup?: string;
  pinned?: boolean;
  /** ISO-8601 timestamp. */
  lastUsedAt: string;
}

export interface Workspace {
  version: number;
  openTabs: TabState[];
  activeTab?: string;
  /** ISO-8601 timestamp set by the backend on each Save. */
  updatedAt: string;
}

export const CURRENT_VERSION = 1;

export function defaultWorkspace(): Workspace {
  return {
    version: CURRENT_VERSION,
    openTabs: [],
    updatedAt: "",
  };
}

export interface WorkspaceAPI {
  get(): Promise<Workspace>;
  save(next: Workspace): Promise<void>;
}

interface WailsBindings {
  GetWorkspace?: () => Promise<Workspace>;
  SaveWorkspace?: (w: Workspace) => Promise<void>;
}

function bindings(): WailsBindings | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { go?: { main?: { App?: WailsBindings } } }).go
    ?.main?.App;
}

const STORAGE_KEY = "goremote.workspace.v1";

/**
 * Client-side mirror of the Go Validate() rules. Used by the mock and as
 * a pre-flight check before saving so the UI surfaces errors immediately.
 */
export function validate(w: Workspace): string[] {
  const errs: string[] = [];
  if (!Number.isInteger(w.version) || w.version < 1) {
    errs.push(`version ${w.version} invalid: want >= 1`);
  }
  const seen = new Set<string>();
  w.openTabs.forEach((t, i) => {
    if (!t.id) {
      errs.push(`openTabs[${i}]: id is empty`);
      return;
    }
    if (seen.has(t.id)) {
      errs.push(`openTabs[${i}]: duplicate id "${t.id}"`);
      return;
    }
    seen.add(t.id);
  });
  if (w.activeTab && !seen.has(w.activeTab)) {
    errs.push(`activeTab "${w.activeTab}" not present in openTabs`);
  }
  return errs;
}

interface StorageLike {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

function getStorage(): StorageLike | null {
  try {
    if (typeof localStorage !== "undefined") return localStorage;
  } catch {
    // Access can throw in sandboxed contexts.
  }
  return null;
}

function createMockAPI(): WorkspaceAPI {
  // Even when localStorage is unavailable we keep an in-memory fallback so
  // tests and SSR contexts still work.
  let memory: Workspace = defaultWorkspace();
  return {
    async get() {
      const storage = getStorage();
      if (storage) {
        const raw = storage.getItem(STORAGE_KEY);
        if (!raw) return defaultWorkspace();
        try {
          const parsed = JSON.parse(raw) as Workspace;
          if (validate(parsed).length > 0) return defaultWorkspace();
          return parsed;
        } catch {
          return defaultWorkspace();
        }
      }
      return { ...memory, openTabs: [...memory.openTabs] };
    },
    async save(next: Workspace) {
      const errs = validate(next);
      if (errs.length > 0) throw new Error(errs.join("; "));
      const stamped: Workspace = {
        ...next,
        updatedAt: new Date().toISOString(),
      };
      const storage = getStorage();
      if (storage) {
        storage.setItem(STORAGE_KEY, JSON.stringify(stamped));
      } else {
        memory = stamped;
      }
    },
  };
}

function realAPI(b: WailsBindings): WorkspaceAPI {
  return {
    async get() {
      if (!b.GetWorkspace) throw new Error("GetWorkspace unavailable");
      return b.GetWorkspace();
    },
    async save(next: Workspace) {
      if (!b.SaveWorkspace) throw new Error("SaveWorkspace unavailable");
      const errs = validate(next);
      if (errs.length > 0) throw new Error(errs.join("; "));
      await b.SaveWorkspace(next);
    },
  };
}

let cached: WorkspaceAPI | null = null;

export function workspaceAPI(): WorkspaceAPI {
  if (cached) return cached;
  const b = bindings();
  cached = b?.GetWorkspace && b.SaveWorkspace ? realAPI(b) : createMockAPI();
  return cached;
}

/**
 * Test-only escape hatch: replace the cached API instance with a stub.
 * Returns the previous instance so tests can restore it.
 */
export function __setWorkspaceAPIForTesting(
  next: WorkspaceAPI | null,
): WorkspaceAPI | null {
  const prev = cached;
  cached = next;
  return prev;
}
