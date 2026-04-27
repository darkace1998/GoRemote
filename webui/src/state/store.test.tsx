import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, render } from "@testing-library/react";
import {
  AppStateProvider,
  useAppDispatch,
  useAppState,
  WORKSPACE_SAVE_DEBOUNCE_MS,
} from "./store";
import {
  __setWorkspaceAPIForTesting,
  defaultWorkspace,
  type Workspace,
  type WorkspaceAPI,
} from "../api/workspace";

// Minimal probe component that exposes state + dispatch via refs so tests
// can drive the store imperatively.
function Probe({
  onReady,
}: {
  onReady: (
    state: ReturnType<typeof useAppState>,
    dispatch: ReturnType<typeof useAppDispatch>,
  ) => void;
}) {
  const state = useAppState();
  const dispatch = useAppDispatch();
  onReady(state, dispatch);
  return null;
}

function makeAPI(initial: Workspace): {
  api: WorkspaceAPI;
  get: ReturnType<typeof vi.fn>;
  save: ReturnType<typeof vi.fn>;
} {
  const get = vi.fn(async () => structuredClone(initial));
  const save = vi.fn(async () => undefined);
  return { api: { get, save }, get, save };
}

describe("AppStateProvider workspace persistence", () => {
  beforeEach(() => {
    __setWorkspaceAPIForTesting(null);
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
    __setWorkspaceAPIForTesting(null);
  });

  it("hydrates tabs and activeTab from a fixture and restores active tab", async () => {
    const fixture: Workspace = {
      version: 1,
      openTabs: [
        { id: "t1", connectionId: "c1", title: "One", lastUsedAt: "" },
        { id: "t2", connectionId: "c2", title: "Two", lastUsedAt: "" },
      ],
      activeTab: "t2",
      updatedAt: "2024-01-01T00:00:00Z",
    };
    const { api, get, save } = makeAPI(fixture);
    __setWorkspaceAPIForTesting(api);

    let captured: ReturnType<typeof useAppState> | null = null;
    render(
      <AppStateProvider>
        <Probe
          onReady={(s) => {
            captured = s;
          }}
        />
      </AppStateProvider>,
    );

    // Allow the async hydrate effect to resolve.
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(get).toHaveBeenCalledOnce();
    expect(captured!.workspaceHydrated).toBe(true);
    expect(captured!.tabs.map((t) => t.id)).toEqual(["t1", "t2"]);
    expect(captured!.activeTabId).toBe("t2");
    expect(captured!.tabs[0].title).toBe("One");
    // No save fired yet beyond the initial hydration cycle's debounce window.
    expect(save).not.toHaveBeenCalled();
  });

  it("debounces SaveWorkspace to a single call after the wait window", async () => {
    const { api, save } = makeAPI(defaultWorkspace());
    __setWorkspaceAPIForTesting(api);

    let captured: ReturnType<typeof useAppDispatch> | null = null;
    let state: ReturnType<typeof useAppState> | null = null;
    render(
      <AppStateProvider>
        <Probe
          onReady={(s, d) => {
            captured = d;
            state = s;
          }}
        />
      </AppStateProvider>,
    );

    // Wait for hydration to flip workspaceHydrated.
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(state!.workspaceHydrated).toBe(true);

    // Hydration itself triggers the save effect once (with empty tabs).
    // Advance to drain that initial scheduled save before our test
    // mutations so we can assert exactly one save coalesces our changes.
    await act(async () => {
      vi.advanceTimersByTime(WORKSPACE_SAVE_DEBOUNCE_MS + 1);
      await Promise.resolve();
    });
    save.mockClear();

    // Open three tabs in rapid succession.
    await act(async () => {
      captured!({
        type: "tabs/open",
        tab: {
          id: "tab-a",
          title: "A",
          sessionHandle: "h-a",
          connectionId: "c-a",
          protocol: "ssh",
        },
      });
      captured!({
        type: "tabs/open",
        tab: {
          id: "tab-b",
          title: "B",
          sessionHandle: "h-b",
          connectionId: "c-b",
          protocol: "ssh",
        },
      });
      captured!({
        type: "tabs/open",
        tab: {
          id: "tab-c",
          title: "C",
          sessionHandle: "h-c",
          connectionId: "c-c",
          protocol: "ssh",
        },
      });
    });

    // Before the debounce window, no save.
    await act(async () => {
      vi.advanceTimersByTime(WORKSPACE_SAVE_DEBOUNCE_MS - 1);
    });
    expect(save).not.toHaveBeenCalled();

    // Cross the threshold: exactly one save with the latest state.
    await act(async () => {
      vi.advanceTimersByTime(2);
      await Promise.resolve();
    });
    expect(save).toHaveBeenCalledOnce();
    const saved = save.mock.calls[0][0] as Workspace;
    expect(saved.openTabs.map((t) => t.id)).toEqual([
      "tab-a",
      "tab-b",
      "tab-c",
    ]);
    expect(saved.activeTab).toBe("tab-c");
  });

  it("close-tab triggers a save", async () => {
    const fixture: Workspace = {
      version: 1,
      openTabs: [
        { id: "t1", connectionId: "c1", title: "One", lastUsedAt: "" },
        { id: "t2", connectionId: "c2", title: "Two", lastUsedAt: "" },
      ],
      activeTab: "t1",
      updatedAt: "",
    };
    const { api, save } = makeAPI(fixture);
    __setWorkspaceAPIForTesting(api);

    let dispatch: ReturnType<typeof useAppDispatch> | null = null;
    render(
      <AppStateProvider>
        <Probe
          onReady={(_, d) => {
            dispatch = d;
          }}
        />
      </AppStateProvider>,
    );

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    // Drain any save queued by hydration.
    await act(async () => {
      vi.advanceTimersByTime(WORKSPACE_SAVE_DEBOUNCE_MS + 1);
      await Promise.resolve();
    });
    save.mockClear();

    await act(async () => {
      dispatch!({ type: "tabs/close", id: "t1" });
    });
    await act(async () => {
      vi.advanceTimersByTime(WORKSPACE_SAVE_DEBOUNCE_MS + 1);
      await Promise.resolve();
    });

    expect(save).toHaveBeenCalledOnce();
    const saved = save.mock.calls[0][0] as Workspace;
    expect(saved.openTabs.map((t) => t.id)).toEqual(["t2"]);
    expect(saved.activeTab).toBe("t2");
  });

  it("reorder-tabs triggers a save with the new order", async () => {
    const fixture: Workspace = {
      version: 1,
      openTabs: [
        { id: "t1", connectionId: "c1", title: "One", lastUsedAt: "" },
        { id: "t2", connectionId: "c2", title: "Two", lastUsedAt: "" },
        { id: "t3", connectionId: "c3", title: "Three", lastUsedAt: "" },
      ],
      activeTab: "t1",
      updatedAt: "",
    };
    const { api, save } = makeAPI(fixture);
    __setWorkspaceAPIForTesting(api);

    let dispatch: ReturnType<typeof useAppDispatch> | null = null;
    render(
      <AppStateProvider>
        <Probe
          onReady={(_, d) => {
            dispatch = d;
          }}
        />
      </AppStateProvider>,
    );

    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    await act(async () => {
      vi.advanceTimersByTime(WORKSPACE_SAVE_DEBOUNCE_MS + 1);
      await Promise.resolve();
    });
    save.mockClear();

    await act(async () => {
      dispatch!({ type: "tabs/reorder", order: ["t3", "t1", "t2"] });
    });
    await act(async () => {
      vi.advanceTimersByTime(WORKSPACE_SAVE_DEBOUNCE_MS + 1);
      await Promise.resolve();
    });

    expect(save).toHaveBeenCalledOnce();
    const saved = save.mock.calls[0][0] as Workspace;
    expect(saved.openTabs.map((t) => t.id)).toEqual(["t3", "t1", "t2"]);
  });
});
