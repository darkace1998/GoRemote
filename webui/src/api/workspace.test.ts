import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  __setWorkspaceAPIForTesting,
  defaultWorkspace,
  validate,
  workspaceAPI,
  type Workspace,
  type WorkspaceAPI,
} from "./workspace";

const STORAGE_KEY = "goremote.workspace.v1";

function withTab(id: string, overrides: Partial<Workspace> = {}): Workspace {
  return {
    version: 1,
    openTabs: [
      { id, connectionId: `c-${id}`, title: id, lastUsedAt: "" },
    ],
    activeTab: id,
    updatedAt: "",
    ...overrides,
  };
}

describe("workspace api validate", () => {
  it("accepts a Default()", () => {
    expect(validate(defaultWorkspace())).toEqual([]);
  });

  it("flags version < 1", () => {
    const w = defaultWorkspace();
    w.version = 0;
    expect(validate(w).join(",")).toMatch(/version/);
  });

  it("flags empty id", () => {
    const w: Workspace = {
      version: 1,
      openTabs: [{ id: "", connectionId: "c", title: "x", lastUsedAt: "" }],
      updatedAt: "",
    };
    expect(validate(w).join(",")).toMatch(/id is empty/);
  });

  it("flags duplicate ids", () => {
    const w: Workspace = {
      version: 1,
      openTabs: [
        { id: "a", connectionId: "c", title: "x", lastUsedAt: "" },
        { id: "a", connectionId: "c2", title: "y", lastUsedAt: "" },
      ],
      updatedAt: "",
    };
    expect(validate(w).join(",")).toMatch(/duplicate/);
  });

  it("flags activeTab not in openTabs", () => {
    const w = withTab("a");
    w.activeTab = "b";
    expect(validate(w).join(",")).toMatch(/activeTab/);
  });

  it("accepts empty activeTab", () => {
    const w = withTab("a");
    w.activeTab = undefined;
    expect(validate(w)).toEqual([]);
  });
});

describe("workspaceAPI: localStorage-backed mock (no Wails)", () => {
  beforeEach(() => {
    localStorage.clear();
    __setWorkspaceAPIForTesting(null);
    // Ensure no stale window.go bindings from other tests.
    delete (window as unknown as { go?: unknown }).go;
  });

  afterEach(() => {
    __setWorkspaceAPIForTesting(null);
    localStorage.clear();
  });

  it("get() returns Default() when storage is empty", async () => {
    const api = workspaceAPI();
    const w = await api.get();
    expect(w).toEqual(defaultWorkspace());
  });

  it("save() persists to localStorage and stamps updatedAt", async () => {
    const api = workspaceAPI();
    const next = withTab("t1");
    await api.save(next);
    const raw = localStorage.getItem(STORAGE_KEY);
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw!) as Workspace;
    expect(parsed.openTabs).toHaveLength(1);
    expect(parsed.openTabs[0].id).toBe("t1");
    expect(parsed.updatedAt).not.toBe("");
  });

  it("get() reads back what save() wrote", async () => {
    const api = workspaceAPI();
    await api.save(withTab("t1"));
    const got = await api.get();
    expect(got.openTabs[0].id).toBe("t1");
    expect(got.activeTab).toBe("t1");
  });

  it("get() returns Default() when storage has corrupt JSON", async () => {
    localStorage.setItem(STORAGE_KEY, "{not json");
    const api = workspaceAPI();
    const w = await api.get();
    expect(w).toEqual(defaultWorkspace());
  });

  it("get() returns Default() when storage has invalid workspace", async () => {
    const bad: Workspace = {
      version: 1,
      openTabs: [
        { id: "a", connectionId: "c", title: "x", lastUsedAt: "" },
        { id: "a", connectionId: "c", title: "y", lastUsedAt: "" },
      ],
      updatedAt: "",
    };
    localStorage.setItem(STORAGE_KEY, JSON.stringify(bad));
    const api = workspaceAPI();
    const w = await api.get();
    expect(w).toEqual(defaultWorkspace());
  });

  it("save() rejects invalid input", async () => {
    const api = workspaceAPI();
    const bad = withTab("a");
    bad.activeTab = "missing";
    await expect(api.save(bad)).rejects.toThrow(/activeTab/);
    expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
  });
});

describe("workspaceAPI: Wails bridge", () => {
  let restore: (() => void) | null = null;

  beforeEach(() => {
    __setWorkspaceAPIForTesting(null);
  });

  afterEach(() => {
    __setWorkspaceAPIForTesting(null);
    if (restore) restore();
    restore = null;
  });

  it("uses GetWorkspace / SaveWorkspace when bound", async () => {
    const get = vi.fn(async () => withTab("from-bridge"));
    const save = vi.fn(async (_w: Workspace) => undefined);
    const original = (window as unknown as { go?: unknown }).go;
    (window as unknown as { go: unknown }).go = {
      main: { App: { GetWorkspace: get, SaveWorkspace: save } },
    };
    restore = () => {
      if (original === undefined) {
        delete (window as unknown as { go?: unknown }).go;
      } else {
        (window as unknown as { go: unknown }).go = original;
      }
    };

    const api = workspaceAPI();
    const w = await api.get();
    expect(w.openTabs[0].id).toBe("from-bridge");
    expect(get).toHaveBeenCalledOnce();

    await api.save(withTab("saved"));
    expect(save).toHaveBeenCalledOnce();
    expect(save.mock.calls[0][0].openTabs[0].id).toBe("saved");
  });

  it("validates before invoking SaveWorkspace", async () => {
    const get = vi.fn(async () => defaultWorkspace());
    const save = vi.fn(async (_w: Workspace) => undefined);
    const original = (window as unknown as { go?: unknown }).go;
    (window as unknown as { go: unknown }).go = {
      main: { App: { GetWorkspace: get, SaveWorkspace: save } },
    };
    restore = () => {
      if (original === undefined) {
        delete (window as unknown as { go?: unknown }).go;
      } else {
        (window as unknown as { go: unknown }).go = original;
      }
    };

    const api = workspaceAPI();
    const bad = withTab("a");
    bad.activeTab = "missing";
    await expect(api.save(bad)).rejects.toThrow(/activeTab/);
    expect(save).not.toHaveBeenCalled();
  });
});

describe("__setWorkspaceAPIForTesting", () => {
  afterEach(() => {
    __setWorkspaceAPIForTesting(null);
  });

  it("returns previous instance and replaces the cached one", async () => {
    const stub: WorkspaceAPI = {
      get: vi.fn(async () => withTab("stub")),
      save: vi.fn(async () => undefined),
    };
    const prev = __setWorkspaceAPIForTesting(stub);
    expect(prev).toBeNull();
    const got = await workspaceAPI().get();
    expect(got.openTabs[0].id).toBe("stub");
  });
});
